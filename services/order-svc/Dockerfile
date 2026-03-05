# Dockerfile untuk order-svc
# Struktur identik dengan user-svc, konsistensi di monorepo adalah kekuatan!
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /workspace

COPY go.work go.work.sum ./
COPY shared/go.mod shared/go.sum ./shared/
COPY gen/go/go.mod gen/go/go.sum ./gen/go/
COPY services/user-svc/go.mod services/user-svc/go.sum ./services/user-svc/
COPY services/order-svc/go.mod services/order-svc/go.sum ./services/order-svc/

RUN go work sync

COPY shared/ ./shared/
COPY gen/ ./gen/
COPY services/order-svc/ ./services/order-svc/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/order-svc \
    ./services/order-svc/cmd/server

# ── Final Image ──
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /bin/order-svc /usr/local/bin/order-svc

EXPOSE 8081

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/order-svc"]
