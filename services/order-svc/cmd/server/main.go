// services/order-svc/cmd/server/main.go
//
// Entry point untuk order-svc.
//
// Perbedaan utama dari user-svc/main.go:
//   1. order-svc TIDAK menjalankan gRPC server (dia consumer, bukan provider gRPC)
//   2. order-svc MEMBUAT gRPC client connection ke user-svc
//   3. Demonstrasi circuit breaker pattern: jika user-svc tidak tersedia saat startup,
//      order-svc tetap bisa jalan dalam "degraded mode"

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	userv1 "github.com/semmidev/gomonorepo/gen/go/user/v1"
	sharedlogger "github.com/semmidev/gomonorepo/shared/pkg/logger"
	sharedmiddleware "github.com/semmidev/gomonorepo/shared/pkg/middleware"
	"github.com/semmidev/gomonorepo/services/order-svc/internal/handler"
	"github.com/semmidev/gomonorepo/services/order-svc/internal/repository"
)

type config struct {
	HTTPPort    string
	AppEnv      string
	UserSvcAddr string // Alamat gRPC user-svc, contoh: "user-svc:9090" atau "localhost:9090"
}

func loadConfig() config {
	return config{
		HTTPPort:    getEnv("HTTP_PORT", "8081"),
		AppEnv:      getEnv("APP_ENV", "development"),
		UserSvcAddr: getEnv("USER_SVC_ADDR", "localhost:9090"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := loadConfig()
	log := sharedlogger.New("order-svc")
	defer log.Sync()

	log.Info("starting order-svc",
		zap.String("http_port", cfg.HTTPPort),
		zap.String("user_svc_addr", cfg.UserSvcAddr),
	)

	// ── Setup gRPC Connection ke user-svc ────────────────────────────────
	// Di sini kita membuat koneksi ke user-svc menggunakan gRPC client.
	//
	// grpc.NewClient adalah non-blocking — dia tidak langsung connect saat dipanggil.
	// Koneksi sebenarnya dibuat saat pertama kali ada RPC call.
	// Ini adalah "lazy connection" pattern yang bagus untuk startup performance.
	//
	// credentials/insecure: untuk development/internal cluster.
	// Di production: gunakan TLS dengan grpc.WithTransportCredentials(creds).
	var userClient userv1.UserServiceClient

	grpcConn, err := grpc.NewClient(
		cfg.UserSvcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Di production, tambahkan:
		// grpc.WithKeepalive(...) untuk koneksi yang tetap hidup
		// grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`) untuk load balancing
	)

	if err != nil {
		// grpc.NewClient hanya bisa error karena config yang invalid, bukan karena network.
		// Jadi kalau error di sini, itu adalah bug konfigurasi.
		log.Warn("failed to create gRPC client for user-svc, running in degraded mode",
			zap.Error(err),
			zap.String("addr", cfg.UserSvcAddr),
		)
		// userClient tetap nil — handler akan handle nil client dengan graceful degradation
	} else {
		userClient = userv1.NewUserServiceClient(grpcConn)
		log.Info("gRPC client connected to user-svc", zap.String("addr", cfg.UserSvcAddr))

		// Pastikan koneksi ditutup saat program exit
		defer func() {
			if err := grpcConn.Close(); err != nil {
				log.Error("error closing gRPC connection", zap.Error(err))
			}
		}()
	}

	// ── Inisialisasi Komponen ────────────────────────────────────────────
	orderRepo := repository.NewInMemoryOrderRepository()
	orderHandler := handler.NewOrderHandler(orderRepo, userClient, log)

	// ── HTTP Server ──────────────────────────────────────────────────────
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(sharedmiddleware.ZapLogger(log))
	r.Use(sharedmiddleware.Recovery(log))
	r.Use(chimiddleware.Compress(5))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"order-svc"}`))
	})

	r.Mount("/api/v1/orders", orderHandler.Routes())

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           r,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ── Jalankan server dan graceful shutdown ────────────────────────────
	errCh := make(chan error, 1)
	go func() {
		log.Info("HTTP server listening", zap.String("addr", ":"+cfg.HTTPPort))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Error("server error", zap.Error(err))
	case sig := <-sigCh:
		log.Info("received signal", zap.String("signal", sig.String()))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown error", zap.Error(err))
	}

	log.Info("order-svc stopped gracefully")
}
