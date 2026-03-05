// shared/pkg/middleware/middleware.go
//
// Package middleware berisi HTTP middleware yang dipakai bersama oleh
// semua service yang menggunakan Chi router.
//
// Middleware adalah fungsi yang "membungkus" HTTP handler.
// Visualisasinya seperti lapisan bawang:
//
//   Request →  [Logger] → [Recovery] → [RequestID] → [CORS] → Handler
//   Response ← [Logger] ← [Recovery] ← [RequestID] ← [CORS] ← Handler
//
// Setiap lapisan bisa:
//   1. Memodifikasi request sebelum diteruskan ke handler
//   2. Memodifikasi response setelah handler selesai
//   3. Menghentikan request (misalnya jika auth gagal)
//   4. Menangani panic (recovery middleware)

package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// ZapLogger adalah middleware yang mencatat setiap HTTP request menggunakan zap.
// Dia mencatat: method, path, status code, durasi, dan request ID.
//
// Kenapa tidak pakai middleware bawaan Chi?
// Chi punya middleware.Logger bawaan, tapi dia menggunakan fmt.Printf biasa.
// Dengan ZapLogger kita, format log konsisten dengan logger di seluruh service.
//
// Contoh output:
//
//	{"level":"info","service":"user-svc","method":"GET","path":"/users","status":200,"duration":"1.264ms","requestId":"abc123"}
func ZapLogger(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// responseWriter wrapper untuk menangkap status code.
			// Chi sudah menyediakan ini via middleware.NewWrapResponseWriter.
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			// Lanjutkan ke handler berikutnya
			next.ServeHTTP(ww, r)

			// Setelah handler selesai, catat log
			log.Info("http request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", ww.Status()),
				zap.Duration("duration", time.Since(start)),
				zap.String("request_id", middleware.GetReqID(r.Context())),
				zap.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// Recovery adalah middleware yang menangkap panic dan mengubahnya menjadi
// HTTP 500 response, sehingga server tidak crash.
//
// Kenapa ini penting?
// Tanpa recovery middleware, sebuah panic di satu goroutine handler akan
// crash SELURUH server. Dengan recovery, panic di-catch, error di-log,
// dan request lain tetap bisa dilayani.
func Recovery(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					// Log panic beserta stack trace untuk debugging
					log.Error("panic recovered",
						zap.Any("panic", rvr),
						zap.String("stack", string(debug.Stack())),
						zap.String("path", r.URL.Path),
					)

					// Kirim response 500 ke client
					http.Error(w,
						fmt.Sprintf(`{"error":"internal server error","request_id":"%s"}`,
							middleware.GetReqID(r.Context())),
						http.StatusInternalServerError,
					)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
