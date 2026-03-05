# ══════════════════════════════════════════════════════════════════════
#  Dockerfile untuk user-svc
#  Menggunakan MULTI-STAGE BUILD — best practice untuk production
#
#  Kenapa multi-stage build?
#  Bayangkan kamu butuh palu untuk memaku paku. Setelah pakunya dipaku,
#  kamu tidak perlu membawa palu ke mana-mana. Multi-stage build bekerja
#  persis seperti itu: stage "builder" berisi semua tools kompilasi Go
#  (yang besar ~800MB), tapi image final hanya berisi binary yang sudah
#  dikompilasi (jauh lebih kecil, ~15MB).
#
#  Manfaat:
#    - Image lebih kecil → deploy lebih cepat, storage lebih hemat
#    - Attack surface lebih kecil → lebih aman (tidak ada compiler di production)
#    - Build cache yang efisien karena layer dependencies dipisah dari source code
# ══════════════════════════════════════════════════════════════════════

# ── STAGE 1: Builder ──────────────────────────────────────────────────
# Image ini HANYA digunakan untuk kompilasi. Tidak akan ada di image final.
FROM golang:1.26-alpine AS builder

# Instal git (dibutuhkan oleh beberapa go modules) dan ca-certificates
# (dibutuhkan untuk HTTPS calls ke registry)
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /workspace

# ── Copy go.work dan semua go.mod/go.sum LEBIH DULU ──
# Kenapa urutan ini penting? Docker layer caching!
# Layer berisi dependencies (go.mod/go.sum) jarang berubah.
# Layer berisi source code sering berubah.
# Dengan memisahkan keduanya:
#   1. Build pertama: semua layer di-download (lambat)
#   2. Build berikutnya: layer dependencies di-cache, hanya source code yang di-rebuild (cepat)
COPY go.work go.work.sum ./
COPY shared/go.mod shared/go.sum ./shared/
COPY gen/go/go.mod gen/go/go.sum ./gen/go/
COPY services/user-svc/go.mod services/user-svc/go.sum ./services/user-svc/
COPY services/order-svc/go.mod services/order-svc/go.sum ./services/order-svc/
# Download semua dependencies (layer ini di-cache jika go.mod tidak berubah)
RUN go work sync

# ── Copy source code ──
# Ini layer yang paling sering berubah, jadi ditaruh paling akhir
# agar layer di atasnya bisa di-cache.
COPY shared/ ./shared/
COPY gen/ ./gen/
COPY services/user-svc/ ./services/user-svc/

# ── Compile ──
# Flag penjelasan:
#   -ldflags="-s -w"  : strip debug info → binary lebih kecil (~30%)
#   -s                : strip symbol table
#   -w                : strip DWARF debug info
#   CGO_ENABLED=0     : compile static binary (tidak bergantung pada libc)
#                       Ini penting agar binary bisa jalan di scratch/distroless image
#   GOOS=linux        : target OS Linux (bisa berbeda dari mesin yang build)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo 'dev')" \
    -o /bin/user-svc \
    ./services/user-svc/cmd/server

# ── STAGE 2: Final Image ───────────────────────────────────────────────
# Pilihan image final:
#   - scratch         : paling minimal, hanya binary (0MB base)
#   - gcr.io/distroless/static : minimal + useful tools (5MB)
#   - alpine          : jika butuh shell untuk debugging (7MB)
#
# Kita pakai distroless karena:
#   1. Tidak ada shell → attacker tidak bisa exec ke container
#   2. Tidak ada package manager → tidak ada CVE dari packages yang tidak dipakai
#   3. Ada timezone data dan CA certificates → bisa HTTPS dan timezone-aware
FROM gcr.io/distroless/static:nonroot

# Salin ca-certificates dari builder untuk HTTPS requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Salin timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Salin binary dari stage builder
COPY --from=builder /bin/user-svc /usr/local/bin/user-svc

# Expose ports yang digunakan service kita:
#   8080 → HTTP REST API
#   9090 → gRPC server
EXPOSE 8080 9090

# Jalankan sebagai non-root user untuk keamanan
# "nonroot" adalah user yang sudah ada di distroless image (UID 65532)
USER nonroot:nonroot

# Titik masuk container
ENTRYPOINT ["/usr/local/bin/user-svc"]
