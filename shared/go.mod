// shared/go.mod
//
// Module "shared" berisi kode yang dipakai BERSAMA oleh semua service.
// Contoh: logger, middleware, error types, config helpers, dll.
//
// Filosofi: Jangan copy-paste kode yang sama di setiap service!
// Taruh di shared/ dan import dari sana. Dengan go.work,
// perubahan di shared/ langsung terlihat oleh user-svc dan order-svc
// tanpa perlu publish ke Go registry.

module github.com/semmidev/gomonorepo/shared

go 1.26.0

require (
	github.com/go-chi/chi/v5 v5.2.5
	go.uber.org/zap v1.27.0
)

require go.uber.org/multierr v1.11.0 // indirect
