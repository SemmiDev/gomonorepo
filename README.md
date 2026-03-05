# Go Monorepo — Deep Dive Guide

Panduan lengkap monorepo Go dengan `go work`

---

## Daftar Isi

1. [Apa itu Monorepo?](#1-apa-itu-monorepo)
2. [Kenapa Go Workspaces?](#2-kenapa-go-workspaces)
3. [Struktur Project](#3-struktur-project)
4. [Setup dari Nol](#4-setup-dari-nol)
5. [Memahami Setiap Komponen](#5-memahami-setiap-komponen)
6. [Development Workflow](#6-development-workflow)
7. [Deployment](#7-deployment)
8. [Best Practices](#8-best-practices)
9. [Referensi API](#9-referensi-api)

---

## 1. Apa itu Monorepo?

**Monorepo** (mono repository) adalah pendekatan di mana *semua* kode dari semua service/module disimpan dalam **satu repository Git**. Ini berlawanan dengan "polyrepo" di mana setiap service punya repository sendiri.

Bayangkan kamu punya dua service: `user-svc` dan `order-svc`. Di polyrepo, ada dua repo Git terpisah. Di monorepo, keduanya ada dalam satu repo.

**Keuntungan Monorepo:**

Pertama, *atomic changes*. Kalau kamu mengubah interface gRPC di `user-svc` yang mempengaruhi `order-svc`, kamu bisa commit perubahan keduanya dalam satu commit. Reviewer bisa melihat seluruh perubahan sekaligus. Di polyrepo, ini butuh dua PR di dua repo yang harus di-coordinate dengan hati-hati.

Kedua, *shared code tanpa drama*. Kode yang dipakai bersama (logger, middleware, error types) bisa taruh di satu tempat dan langsung dipakai. Di polyrepo, kamu harus publish ke registry atau copy-paste — keduanya menyakitkan.

Ketiga, *satu CI/CD pipeline*. Lebih mudah di-manage daripada mengurus pipeline untuk 10+ repo.

**Kekurangan Monorepo:**

Repo bisa menjadi sangat besar seiring waktu. CI/CD perlu cerdas agar tidak re-build semua service ketika hanya satu yang berubah. Untuk masalah ini, ada tools seperti Turborepo (JS) atau Bazel (Google) — tapi untuk Go dengan go work, setup dasar sudah sangat manageable.

---

## 2. Kenapa Go Workspaces?

Sebelum Go 1.18, jika kamu ingin develop dua module secara bersamaan, kamu harus menambahkan `replace` directive di `go.mod`:

```
# go.mod (cara LAMA — sebelum go workspaces)
require github.com/semmidev/shared v0.0.0

replace github.com/semmidev/shared => ../shared  # Hardcode path lokal!
```

Masalahnya, directive `replace` ini tidak boleh di-commit ke production karena akan break build di server CI. Kamu harus ingat untuk menghapusnya sebelum commit — sangat rawan error.

**Go Workspaces (go work)** menyelesaikan masalah ini dengan elegan. File `go.work` menjadi "lapisan override" yang terpisah dari `go.mod`. `go.mod` tetap bersih dengan dependency yang benar, sementara `go.work` memberitahu Go toolchain untuk "gunakan source code lokal ini selama development."

```
# go.work
go 1.26.0

use (
    ./services/user-svc   # Go akan gunakan source lokal ini...
    ./services/order-svc  # ...bukan dari remote registry
    ./shared
)
```

Hasilnya: kamu bisa mengubah package `shared/pkg/logger` dan perubahan itu **langsung terlihat** oleh `user-svc` dan `order-svc` tanpa perlu publish apapun.

---

## 3. Struktur Project

```
gomonorepo/
│
├── go.work                    # Workspace root — mendaftarkan semua modules
├── go.work.sum                # Checksum file (di-generate otomatis)
├── Makefile                   # Semua perintah tersentralisasi di sini
├── docker-compose.yml         # Orchestrasi untuk development lokal
├── .golangci.yml              # Konfigurasi linter
│
├── proto/                     # "Source of truth" untuk semua API contracts
│   ├── buf.yaml               # Konfigurasi Buf (linting, breaking change detection)
│   ├── buf.gen.yaml           # Konfigurasi code generation
│   └── user/
│       └── v1/
│           └── user.proto     # Definisi UserService gRPC + messages
│
├── gen/                       # Kode yang DI-GENERATE dari proto (jangan edit manual!)
│   └── go/
│       └── user/
│           └── v1/
│               ├── user.pb.go         # Go structs dari proto messages
│               └── user_grpc.pb.go    # gRPC client & server interfaces
│
├── shared/                    # Kode yang dipakai BERSAMA oleh semua services
│   ├── go.mod
│   └── pkg/
│       ├── logger/            # Structured logger (zap)
│       └── middleware/        # HTTP middleware (logging, recovery)
│
└── services/
    ├── user-svc/              # Service 1: mengelola data user
    │   ├── go.mod
    │   ├── Dockerfile
    │   ├── cmd/
    │   │   └── server/
    │   │       └── main.go    # Entry point: wiring semua dependencies
    │   └── internal/
    │       ├── handler/       # HTTP REST API handlers (Chi)
    │       ├── repository/    # Data access layer (in-memory map)
    │       └── grpcserver/    # gRPC server implementation
    │
    └── order-svc/             # Service 2: mengelola data order
        ├── go.mod
        ├── Dockerfile
        ├── cmd/
        │   └── server/
        │       └── main.go    # Entry point + gRPC client ke user-svc
        └── internal/
            ├── handler/       # HTTP REST API handlers + gRPC client calls
            └── repository/    # Data access layer
```

Perhatikan pola `cmd/server/main.go` dan `internal/`. Ini adalah struktur standar Go:

`cmd/` berisi "entry points" — program yang bisa di-compile dan dijalankan. Nama direktori di dalam `cmd/` biasanya menjadi nama binary-nya.

`internal/` adalah direktori spesial di Go. Package di dalam `internal/` **hanya bisa diimport oleh code dalam parent module yang sama**. Ini mencegah service lain mengimport implementation detail yang tidak seharusnya mereka akses.

---

## 4. Setup dari Nol

### Prasyarat

Kamu butuh go 1.26.0+, Buf v2, dan protoc plugins untuk Go. Jalankan:

```bash
# Install Buf
go install github.com/bufbuild/buf/cmd/buf@latest

# Install protoc-gen-go dan protoc-gen-go-grpc
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Atau gunakan Makefile kita:
make setup
```

### Langkah 1: Clone dan Buat Workspace

```bash
git clone https://github.com/semmidev/gomonorepo
cd gomonorepo
```

File `go.work` sudah ada. Tapi kalau kamu ingin memahami cara membuatnya dari nol:

```bash
# Di direktori root, inisialisasi workspace
go work init

# Tambahkan setiap module ke workspace
go work use ./shared
go work use ./services/user-svc
go work use ./services/order-svc
```

Ini akan menghasilkan file `go.work` yang mendaftarkan semua module.

### Langkah 2: Generate Proto Code

```bash
make buf-gen
```

Perintah ini menjalankan `buf generate` di direktori `proto/`, yang membaca `buf.gen.yaml` dan menghasilkan file Go di direktori `gen/go/`.

Kamu harus jalankan ini setiap kali ada perubahan di file `.proto`. File yang di-generate (`gen/`) **di-commit ke repository** — ini adalah best practice karena:
1. CI tidak perlu install buf untuk build
2. Code review bisa melihat perubahan generated code
3. Build reproducible tanpa bergantung pada tools

### Langkah 3: Download Dependencies

```bash
make tidy
# Setara dengan: cd shared && go mod tidy && cd services/user-svc && go mod tidy && ...
```

### Langkah 4: Jalankan Service

```bash
# Terminal 1: jalankan user-svc
make run-user
# Output: {"level":"info","service":"user-svc","msg":"HTTP server listening","addr":":8080"}
# Output: {"level":"info","service":"user-svc","msg":"gRPC server listening","addr":":9090"}

# Terminal 2: jalankan order-svc
make run-order
# Output: {"level":"info","service":"order-svc","msg":"gRPC client connected to user-svc"}
# Output: {"level":"info","service":"order-svc","msg":"HTTP server listening","addr":":8081"}
```

---

## 5. Memahami Setiap Komponen

### 5.1 Protobuf + Buf

File `.proto` adalah **kontrak API** yang language-agnostic. Dari satu file ini, kamu bisa generate kode untuk Go, Python, Java, TypeScript — semua secara konsisten.

Buf adalah pengganti `protoc` yang lebih modern. Keunggulan utamanya adalah:

**Linting** — `buf lint` akan error jika kamu menulis `.proto` yang tidak mengikuti best practices Google. Misalnya, message harus `PascalCase`, field harus `snake_case`.

**Breaking change detection** — `buf breaking --against '.git#branch=main'` akan error jika kamu mengubah field number, menghapus field, atau mengubah tipe data. Ini melindungi backward compatibility API kamu. Kalau kamu ubah `string id = 1` menjadi `int64 id = 1`, Buf akan menolaknya karena client lama akan kirim string dan server baru tidak bisa baca.

### 5.2 Chi Router

Chi adalah HTTP router minimalis yang berbeda dari Gin atau Echo. Chi tidak "magic" — tidak ada reflection, tidak ada struct tags untuk routing. Routing-nya eksplisit:

```go
r := chi.NewRouter()
r.Get("/users/{id}", handler.GetUser)  // GET /users/abc123
r.Post("/users", handler.CreateUser)   // POST /users
```

Chi juga sangat composable. Kamu bisa nest router dan apply middleware hanya ke subset routes:

```go
r.Route("/admin", func(r chi.Router) {
    r.Use(adminAuthMiddleware)  // Middleware ini HANYA berlaku untuk /admin/*
    r.Get("/dashboard", adminHandler)
})
```

### 5.3 gRPC Communication

Ini adalah bagian yang paling menarik. Mari ikuti alur sebuah request:

Seorang client memanggil `GET /api/v1/orders/order-001` ke order-svc.

Order-svc memanggil `repo.FindByID("order-001")` dan mendapat data order. Data order ini hanya berisi `UserID: "user-001"`, bukan nama atau email user.

Order-svc kemudian membuat gRPC call ke user-svc:

```go
userResp, err := h.userSvcClient.GetUser(grpcCtx, &userv1.GetUserRequest{
    Id: order.UserID,
})
```

Di balik layar, gRPC framework melakukan: serialize `GetUserRequest` struct menjadi binary protobuf → kirim via TCP ke user-svc:9090 → user-svc deserialize → jalankan `GetUser` method → serialize `GetUserResponse` → kirim balik → order-svc deserialize.

Semua ini terjadi secara transparan. Order-svc menulis `h.userSvcClient.GetUser(...)` seolah memanggil fungsi lokal biasa.

### 5.4 In-Memory Store dengan sync.RWMutex

Go map **tidak thread-safe**. Kalau dua goroutine menulis ke map bersamaan, program akan panic dengan pesan `concurrent map writes`. Ini bukan bug yang kadang-kadang muncul — ini adalah undefined behavior yang bisa menyebabkan corruption.

Solusinya adalah `sync.RWMutex`. Ini adalah "read-write lock" yang memungkinkan:

Banyak goroutine bisa membaca **bersamaan** (menggunakan `RLock()`). Ini efisien untuk workload read-heavy seperti API yang sering di-query.

Hanya SATU goroutine bisa menulis pada satu waktu, dan saat menulis tidak ada yang bisa membaca (menggunakan `Lock()`). Ini memastikan konsistensi data.

```go
// Operasi READ: boleh concurrent
func (r *repo) FindByID(ctx context.Context, id string) (*User, error) {
    r.mu.RLock()         // Kunci untuk read
    defer r.mu.RUnlock() // Lepas kunci ketika fungsi return
    return r.store[id], nil
}

// Operasi WRITE: exclusive, tidak ada yang bisa baca/tulis bersamaan
func (r *repo) Save(ctx context.Context, user *User) (*User, error) {
    r.mu.Lock()         // Kunci exclusive untuk write
    defer r.mu.Unlock()
    r.store[user.ID] = user
    return user, nil
}
```

---

## 6. Development Workflow

### Cara Kerja go work dalam Praktek

Bayangkan kamu ingin menambahkan method `GetUserByEmail` ke shared logger. Tanpa go work, kamu harus:
1. Edit `shared/`
2. Commit & push
3. Update `go.mod` di setiap service
4. Baru bisa test

Dengan go work:
1. Edit `shared/`
2. Langsung test di service manapun — perubahan **langsung terlihat**

Ini karena `go.work` memberitahu Go: "ketika ada import dari `github.com/semmidev/gomonorepo/shared`, gunakan source code di `./shared/`, bukan dari cache."

### Testing

```bash
# Test dengan race detector (WAJIB untuk concurrent code)
go test ./... -race

# Test dengan coverage
go test ./... -race -coverprofile=coverage.out
go tool cover -html=coverage.out  # Buka browser dengan visualisasi coverage
```

### Debugging gRPC dengan grpcurl

Karena kita mengaktifkan gRPC reflection di development, kamu bisa menggunakan `grpcurl` untuk test gRPC endpoints seperti curl untuk HTTP:

```bash
# List semua services
grpcurl -plaintext localhost:9090 list

# Deskripsi service
grpcurl -plaintext localhost:9090 describe user.v1.UserService

# Panggil GetUser
grpcurl -plaintext -d '{"id": "user-001"}' localhost:9090 user.v1.UserService/GetUser

# Panggil CreateUser
grpcurl -plaintext \
  -d '{"name": "Dave", "email": "dave@example.com", "role": "customer"}' \
  localhost:9090 user.v1.UserService/CreateUser
```

---

## 7. Deployment

### Docker Multi-Stage Build

Dockerfile kita menggunakan multi-stage build yang menghasilkan image sangat kecil (~15MB vs ~800MB jika pakai full Go image):

```
Stage 1 (builder): golang:1.26-alpine (~300MB)
    → copy source code
    → go build → /bin/user-svc

Stage 2 (final):   distroless/static (~5MB)
    → copy /bin/user-svc dari stage 1
    → EXPOSE 8080 9090
    → ENTRYPOINT ["/usr/local/bin/user-svc"]
```

Image final tidak memiliki shell, compiler, atau package manager. Ini sangat aman untuk production karena attacker tidak punya tools jika berhasil masuk ke container.

### Docker Compose untuk Development

```bash
# Build semua image dan jalankan
make docker-up

# Cek status
docker compose ps

# Lihat logs secara real-time
docker compose logs -f

# Matikan semua
make docker-down
```

### Catatan Penting: Build Context

Perhatikan di `docker-compose.yml`, `context: .` menunjuk ke **root monorepo**, bukan direktori service. Ini penting karena Dockerfile user-svc membutuhkan akses ke direktori `shared/` dan `gen/` yang berada di root — bukan di dalam `services/user-svc/`.

---

## 8. Best Practices

**Tentang go.work:** Putuskan apakah kamu mau commit `go.work` atau tidak. Untuk monorepo yang di-deploy bersama (semua service dalam satu repository dan di-deploy bersamaan), commit `go.work` adalah masuk akal. Untuk monorepo di mana setiap service punya release cycle terpisah, lebih baik tambahkan `go.work` ke `.gitignore` dan biarkan setiap developer generate sendiri.

**Jangan commit go.work.sum ke .gitignore** — `go.work.sum` berisi checksum dependencies dan harus di-commit untuk reproducible builds.

**Tentang generated code:** Selalu commit file yang di-generate (`gen/`) ke repository. Ini memungkinkan developer yang tidak punya `buf` installed untuk tetap bisa build project. CI juga bisa memverifikasi bahwa generated code up-to-date dengan menjalankan `buf generate` lalu `git diff --exit-code`.

**Tentang versioning API:** Gunakan direktori versi di path proto (`user/v1/user.proto`). Ketika kamu perlu membuat breaking change, buat `user/v2/user.proto` alih-alih mengubah v1. Ini memungkinkan client lama tetap pakai v1 sementara client baru migrasi ke v2.

**Tentang graceful shutdown:** Selalu implement graceful shutdown. Kubernetes mengirim SIGTERM saat rolling update, memberi waktu service menyelesaikan request yang sedang berjalan sebelum dihentikan. Tanpa graceful shutdown, user bisa mendapat error di tengah transaksi.

---

## 9. Referensi API

### user-svc (HTTP :8080)

| Method | Endpoint | Deskripsi |
|--------|----------|-----------|
| GET | /healthz | Health check |
| GET | /api/v1/users | List semua users |
| POST | /api/v1/users | Buat user baru |
| GET | /api/v1/users/{id} | Ambil user by ID |
| DELETE | /api/v1/users/{id} | Hapus user |

### order-svc (HTTP :8081)

| Method | Endpoint | Deskripsi |
|--------|----------|-----------|
| GET | /healthz | Health check |
| GET | /api/v1/orders | List semua orders |
| POST | /api/v1/orders | Buat order baru (verifikasi user via gRPC) |
| GET | /api/v1/orders/{id} | Ambil order by ID (diperkaya data user via gRPC) |
| GET | /api/v1/orders/user/{userId} | Orders milik user tertentu |
| PATCH | /api/v1/orders/{id}/status | Update status order |

### user-svc (gRPC :9090)

```protobuf
service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
}
```

### Contoh Request

```bash
# Buat user baru
curl -X POST http://localhost:8080/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Eve", "email": "eve@example.com", "role": "customer"}'

# Buat order (user-001 harus ada di user-svc)
curl -X POST http://localhost:8081/api/v1/orders \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user-001",
    "items": [
      {"product_id": "prod-005", "product_name": "Monitor 4K", "quantity": 1, "unit_price": 5000000}
    ]
  }'

# Ambil order detail (akan di-enrich dengan data user dari gRPC call)
curl http://localhost:8081/api/v1/orders/order-001
# Response akan berisi user_name dan user_email yang diambil dari user-svc!
```
