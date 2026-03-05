// services/user-svc/cmd/server/main.go
//
// Ini adalah TITIK MASUK (entry point) dari user-svc.
// main.go bertanggung jawab untuk:
//   1. Membaca konfigurasi (dari env vars atau file)
//   2. Membuat semua dependencies (repository, logger, dll)
//   3. Menghubungkan semua komponen (wiring/dependency injection)
//   4. Menjalankan HTTP server dan gRPC server secara BERSAMAAN
//   5. Menangani graceful shutdown ketika ada signal SIGTERM/SIGINT
//
// POLA ARSITEKTUR: Composition Root
// Semua "penghubungan" dependency dilakukan di sini, bukan di dalam komponen.
// Komponen (handler, repository, grpcserver) tidak tahu bagaimana mereka dibuat —
// mereka hanya menerima dependencies melalui constructor.
// Ini membuat komponen mudah di-test dan mudah di-swap implementasinya.
//
// POLA LIFECYCLE: Server berjalan hingga ada interrupt signal.
// Ketika CTRL+C atau SIGTERM diterima (misal dari Kubernetes saat rolling update),
// server menyelesaikan request yang sedang berjalan sebelum shutdown.
// Ini disebut "graceful shutdown" dan penting untuk menghindari request yang
// terputus di tengah jalan.

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	userv1 "github.com/semmidev/gomonorepo/gen/go/user/v1"
	sharedlogger "github.com/semmidev/gomonorepo/shared/pkg/logger"
	sharedmiddleware "github.com/semmidev/gomonorepo/shared/pkg/middleware"
	"github.com/semmidev/gomonorepo/services/user-svc/internal/grpcserver"
	"github.com/semmidev/gomonorepo/services/user-svc/internal/handler"
	"github.com/semmidev/gomonorepo/services/user-svc/internal/repository"
)

// config menyimpan semua konfigurasi yang dibutuhkan service.
// Best practice: baca konfigurasi dari environment variables, BUKAN hardcode.
// Kenapa env vars? Karena:
//   1. 12-Factor App methodology (https://12factor.net/config)
//   2. Secrets tidak masuk ke source code
//   3. Mudah di-override di berbagai environment (dev, staging, prod)
//   4. Kubernetes ConfigMap dan Secret bisa di-mount sebagai env vars
type config struct {
	HTTPPort string // Port untuk REST API server
	GRPCPort string // Port untuk gRPC server
	AppEnv   string // "development" atau "production"
}

// loadConfig membaca konfigurasi dari env vars dengan nilai default yang masuk akal.
// Menggunakan helper getEnv agar tidak verbose.
func loadConfig() config {
	return config{
		HTTPPort: getEnv("HTTP_PORT", "8080"),
		GRPCPort: getEnv("GRPC_PORT", "9090"),
		AppEnv:   getEnv("APP_ENV", "development"),
	}
}

func getEnv(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

func main() {
	cfg := loadConfig()

	// ── 1. Inisialisasi Logger ──────────────────────────────────────────
	// Logger adalah dependency pertama yang dibuat karena semua komponen
	// lain membutuhkannya. "user-svc" akan muncul di setiap log entry
	// sehingga mudah di-filter saat menganalisis log dari banyak service.
	log := sharedlogger.New("user-svc")
	defer log.Sync() // Flush semua buffered log sebelum program exit

	log.Info("starting user-svc",
		zap.String("http_port", cfg.HTTPPort),
		zap.String("grpc_port", cfg.GRPCPort),
		zap.String("env", cfg.AppEnv),
	)

	// ── 2. Inisialisasi Repository ──────────────────────────────────────
	// Repository adalah dependency yang paling "dalam".
	// Semua layer di atasnya bergantung padanya.
	// Di production, kamu akan inject *sql.DB atau Redis client di sini.
	userRepo := repository.NewInMemoryUserRepository()

	// ── 3. Setup HTTP Server (REST API dengan Chi) ──────────────────────
	// Chi router sangat composable — kamu bisa mount sub-router dengan prefix.
	// Pattern ini memudahkan versioning API: /api/v1, /api/v2, dll.
	httpServer := setupHTTPServer(cfg, log, userRepo)

	// ── 4. Setup gRPC Server ─────────────────────────────────────────────
	grpcSrv := setupGRPCServer(log, userRepo)

	// ── 5. Jalankan kedua server secara concurrent ────────────────────────
	// Kita perlu menjalankan HTTP dan gRPC server SECARA BERSAMAAN.
	// Di Go, ini dilakukan dengan goroutine.
	//
	// Diagram eksekusi:
	//   main goroutine: menunggu shutdown signal
	//   goroutine 1:    menjalankan HTTP server (blocking)
	//   goroutine 2:    menjalankan gRPC server (blocking)
	//
	// Jika salah satu server error fatal, kita kirim signal ke main goroutine
	// melalui channel agar bisa shutdown dengan bersih.
	errCh := make(chan error, 2) // Buffered channel untuk menangkap error dari goroutines

	// Goroutine untuk HTTP server
	go func() {
		log.Info("HTTP server listening", zap.String("addr", ":"+cfg.HTTPPort))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()

	// Goroutine untuk gRPC server
	go func() {
		lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
		if err != nil {
			errCh <- fmt.Errorf("failed to listen on gRPC port: %w", err)
			return
		}
		log.Info("gRPC server listening", zap.String("addr", ":"+cfg.GRPCPort))
		if err := grpcSrv.Serve(lis); err != nil {
			errCh <- fmt.Errorf("gRPC server error: %w", err)
		}
	}()

	// ── 6. Graceful Shutdown ─────────────────────────────────────────────
	// Tunggu sampai ada SIGINT (Ctrl+C) atau SIGTERM (dari Docker/Kubernetes)
	// atau salah satu server error fatal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Error("server error, initiating shutdown", zap.Error(err))
	case sig := <-sigCh:
		log.Info("received shutdown signal", zap.String("signal", sig.String()))
	}

	// Beri waktu 10 detik untuk menyelesaikan request yang sedang berjalan.
	// Kubernetes default terminationGracePeriodSeconds adalah 30 detik,
	// jadi 10 detik adalah nilai yang aman.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown HTTP server dengan graceful
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("HTTP server shutdown error", zap.Error(err))
	}

	// Shutdown gRPC server — GracefulStop menunggu semua ongoing RPCs selesai
	grpcSrv.GracefulStop()

	log.Info("user-svc stopped gracefully")
}

// setupHTTPServer membuat dan mengkonfigurasi HTTP server dengan semua middleware.
// Dipisah ke fungsi sendiri agar main() tidak terlalu panjang dan mudah dibaca.
func setupHTTPServer(cfg config, log *zap.Logger, userRepo repository.UserRepository) *http.Server {
	r := chi.NewRouter()

	// ── Middleware Stack ──
	// Middleware dijalankan secara berurutan dari atas ke bawah untuk setiap request.
	// Urutan PENTING! RequestID harus sebelum ZapLogger agar logger bisa mencatat requestID.

	// RequestID: generate unique ID untuk setiap request.
	// Sangat penting untuk distributed tracing dan debugging.
	// Client bisa kirim header X-Request-ID, atau server yang generate sendiri.
	r.Use(chimiddleware.RequestID)

	// RealIP: ambil IP asli klien dari header X-Forwarded-For atau X-Real-IP.
	// Penting jika service kamu berada di belakang load balancer/proxy.
	r.Use(chimiddleware.RealIP)

	// ZapLogger kita: mencatat semua request dengan format structured JSON.
	r.Use(sharedmiddleware.ZapLogger(log))

	// Recovery: tangkap panic agar server tidak crash.
	// Ini HARUS ada di semua production service.
	r.Use(sharedmiddleware.Recovery(log))

	// Compress: gzip response untuk menghemat bandwidth.
	r.Use(chimiddleware.Compress(5))

	// ── Health Check Endpoint ──
	// Endpoint ini digunakan oleh:
	//   - Kubernetes liveness probe (/healthz) dan readiness probe (/readyz)
	//   - Load balancer health check
	//   - Monitoring tools
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"user-svc"}`))
	})

	// ── API Routes ──
	// Mount user handler di bawah /api/v1/users
	// Semua routes yang didefinisikan di handler.Routes() akan
	// otomatis mendapat prefix /api/v1/users
	userHandler := handler.NewUserHandler(userRepo, log)
	r.Mount("/api/v1/users", userHandler.Routes())

	return &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: r,

		// Timeout ini SANGAT penting untuk production.
		// Tanpa timeout, koneksi yang "hang" bisa menghabiskan resource server.
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// setupGRPCServer membuat dan mengkonfigurasi gRPC server.
func setupGRPCServer(log *zap.Logger, userRepo repository.UserRepository) *grpc.Server {
	// Interceptor adalah "middleware" untuk gRPC.
	// grpc.ChainUnaryInterceptor memungkinkan chaining multiple interceptors.
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			// Interceptor 1: Logging — catat setiap gRPC call
			loggingInterceptor(log),

			// Interceptor 2: Recovery — tangkap panic di gRPC handler
			recoveryInterceptor(log),
		),
	)

	// Daftarkan implementasi UserGRPCServer ke gRPC server.
	// Ini menghubungkan: gRPC framework → implementasi kita
	userGRPCServer := grpcserver.NewUserGRPCServer(userRepo, log)
	userv1.RegisterUserServiceServer(grpcSrv, userGRPCServer)

	// Reflection memungkinkan tools seperti grpcurl dan Postman
	// untuk discover service kita secara otomatis.
	// JANGAN enable di production karena mengekspose API schema kamu!
	// Gunakan hanya di development.
	reflection.Register(grpcSrv)

	return grpcSrv
}

// loggingInterceptor adalah gRPC interceptor yang mencatat setiap RPC call.
// Analoginya seperti ZapLogger middleware untuk HTTP, tapi untuk gRPC.
func loggingInterceptor(log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		// Panggil handler berikutnya (bisa interceptor lain atau implementasi sebenarnya)
		resp, err := handler(ctx, req)

		// Log setelah handler selesai
		log.Info("gRPC call",
			zap.String("method", info.FullMethod),
			zap.Duration("duration", time.Since(start)),
			zap.Error(err),
		)

		return resp, err
	}
}

// recoveryInterceptor menangkap panic di gRPC handler, mirip dengan recovery middleware HTTP.
func recoveryInterceptor(log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("gRPC panic recovered",
					zap.Any("panic", r),
					zap.String("method", info.FullMethod),
				)
				err = fmt.Errorf("internal server error")
			}
		}()
		return handler(ctx, req)
	}
}
