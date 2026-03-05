// services/user-svc/go.mod
//
// Module user-svc adalah microservice pertama kita.
// Dia expose:
//   1. REST API (HTTP :8080) via Chi — untuk diakses oleh frontend/client eksternal
//   2. gRPC server (TCP :9090) — untuk diakses oleh service internal lain
//
// Kenapa dua protokol?
//   - REST/JSON: lebih mudah di-consume oleh browser, mobile app, atau tools seperti Postman
//   - gRPC/Protobuf: lebih efisien (binary encoding), strongly typed, dan ada streaming.
//     Perfect untuk komunikasi internal antar service di dalam cluster.

module github.com/semmidev/gomonorepo/services/user-svc

go 1.26.0

require (
	// Chi: HTTP router yang ringan dan idiomatic untuk Go.
	// Tidak se-"magic" Gin atau Echo, tapi sangat composable dan mudah dipahami.
	// Cocok untuk production-grade REST API.
	github.com/go-chi/chi/v5 v5.2.5

	// UUID untuk generate unique ID
	github.com/google/uuid v1.6.0

	// Generated protobuf code
	github.com/semmidev/gomonorepo/gen/go v0.0.0

	// Shared module dari monorepo kita sendiri!
	github.com/semmidev/gomonorepo/shared v0.0.0

	// Zap untuk logging
	go.uber.org/zap v1.27.0

	// google.golang.org/grpc: library official Google untuk gRPC di Go
	google.golang.org/grpc v1.68.0

	// google.golang.org/protobuf: library untuk bekerja dengan Protobuf messages
	google.golang.org/protobuf v1.35.2 // indirect
)

require (
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.31.0 // indirect
	golang.org/x/sys v0.27.0 // indirect
	golang.org/x/text v0.20.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241118233622-e639e219e697 // indirect
)

replace (
	github.com/semmidev/gomonorepo/gen/go => ../../gen/go
	github.com/semmidev/gomonorepo/shared => ../../shared
)
