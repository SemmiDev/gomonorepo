// services/order-svc/go.mod
//
// Module order-svc adalah service kedua di monorepo kita.
// Dia expose REST API (HTTP :8081) dan MENGGUNAKAN gRPC untuk
// berkomunikasi dengan user-svc untuk memverifikasi user.
//
// Hal menarik di sini: order-svc mengimport DUA module lokal dari monorepo:
//   1. github.com/semmidev/gomonorepo/shared     → untuk logger dan middleware
//   2. github.com/semmidev/gomonorepo/gen/go     → untuk gRPC client stub
//
// Dengan go.work, kedua import ini "resolves" ke source code lokal kita,
// bukan dari remote registry. Ini adalah kekuatan utama go workspaces!

module github.com/semmidev/gomonorepo/services/order-svc

go 1.26.0

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	github.com/semmidev/gomonorepo/gen/go v0.0.0
	github.com/semmidev/gomonorepo/shared v0.0.0
	go.uber.org/zap v1.27.0
	google.golang.org/grpc v1.68.0
)

require (
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.31.0 // indirect
	golang.org/x/sys v0.27.0 // indirect
	golang.org/x/text v0.20.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241118233622-e639e219e697 // indirect
	google.golang.org/protobuf v1.35.2 // indirect
)

replace (
	github.com/semmidev/gomonorepo/gen/go => ../../gen/go
	github.com/semmidev/gomonorepo/shared => ../../shared
)
