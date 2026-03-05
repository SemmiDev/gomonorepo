// shared/pkg/logger/logger.go
//
// Package logger menyediakan structured logging yang konsisten
// di seluruh service dalam monorepo kita.
//
// Kenapa perlu shared logger?
// Bayangkan kamu punya 5 service dan setiap service punya cara logging
// yang berbeda. Debugging menjadi mimpi buruk karena format log tidak konsisten.
// Dengan shared logger, semua service menghasilkan log dengan format yang sama,
// sehingga mudah di-aggregate oleh tools seperti Grafana Loki atau ELK Stack.
//
// Kenapa zap?
// zap dari Uber adalah logger Go paling performant. Dia menggunakan zero-allocation
// architecture untuk hot path. Perbandingan:
//   - log (stdlib)    : lambat, tidak structured
//   - logrus          : structured, tapi lambat
//   - zap             : structured, SANGAT cepat, strongly typed

package logger

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger adalah type alias agar service tidak perlu import zap secara langsung.
// Kalau suatu saat kita ganti library logging, cukup ubah di sini.
type Logger = *zap.Logger

// New membuat logger baru yang dikonfigurasi sesuai environment.
// Di development, output-nya colorful dan human-readable.
// Di production, output-nya JSON yang bisa di-parse oleh log aggregator.
func New(serviceName string) Logger {
	env := strings.ToLower(os.Getenv("APP_ENV"))

	var config zap.Config

	if env == "production" || env == "prod" {
		// Production: JSON format, hanya log Warning ke atas secara default
		// Log ini akan di-parse oleh Grafana Loki, Datadog, dsb.
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	} else {
		// Development: format yang enak dibaca manusia
		// Dengan warna, timestamp yang readable, dan stack trace yang bagus
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// Setiap log message akan selalu menyertakan field "service".
	// Ini penting saat log dari banyak service digabung di satu sistem.
	// Contoh output JSON: {"level":"info","service":"user-svc","msg":"server started","port":8080}
	config.InitialFields = map[string]interface{}{
		"service": serviceName,
	}

	logger, err := config.Build(
		// Tambahkan caller info (file:line) untuk memudahkan debugging
		zap.AddCaller(),
		// Skip 1 frame di call stack agar log menunjuk ke kode kita,
		// bukan ke helper logger ini.
		zap.AddCallerSkip(0),
	)
	if err != nil {
		// Kalau logger gagal dibuat, fallback ke logger paling sederhana.
		// Ini tidak boleh terjadi, tapi lebih baik tidak panic.
		fallback, _ := zap.NewProduction()
		return fallback
	}

	return logger
}

// NewNop membuat logger yang membuang semua output.
// Sangat berguna untuk unit testing agar tidak ada noise di test output.
// Contoh penggunaan:
//
//	func TestSomething(t *testing.T) {
//	    log := logger.NewNop()
//	    svc := NewService(log)
//	    // ... test tanpa log noise
//	}
func NewNop() Logger {
	return zap.NewNop()
}
