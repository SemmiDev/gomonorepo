# ============================================================
#  Makefile - Monorepo Root
#  Semua perintah penting tersentralisasi di sini.
#  Jalankan dari root direktori: make <target>
# ============================================================

.PHONY: help setup buf-gen lint test build-all run-user run-order docker-up docker-down tidy

# Warna untuk output yang lebih enak dibaca
GREEN  := \033[0;32m
YELLOW := \033[1;33m
CYAN   := \033[0;36m
RESET  := \033[0m

## help: tampilkan semua perintah yang tersedia
help:
	@echo ""
	@echo "$(CYAN)╔══════════════════════════════════════════╗$(RESET)"
	@echo "$(CYAN)║     Go Monorepo - Available Commands     ║$(RESET)"
	@echo "$(CYAN)╚══════════════════════════════════════════╝$(RESET)"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
	@echo ""

## setup: install semua tools yang dibutuhkan (buf, protoc-gen-go, dll)
setup:
	@echo "$(YELLOW)► Installing tools...$(RESET)"
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "$(GREEN)✓ Tools installed$(RESET)"

## buf-gen: generate gRPC & Protobuf code dari .proto files menggunakan Buf
buf-gen:
	@echo "$(YELLOW)► Generating protobuf code...$(RESET)"
	cd proto && buf generate
	@echo "$(GREEN)✓ Code generated to gen/$(RESET)"

## tidy: jalankan go mod tidy di semua module
tidy:
	@echo "$(YELLOW)► Tidying all modules...$(RESET)"
	cd shared && go mod tidy
	cd services/user-svc && go mod tidy
	cd services/order-svc && go mod tidy
	@echo "$(GREEN)✓ All modules tidied$(RESET)"

## lint: jalankan linter di semua service
lint:
	@echo "$(YELLOW)► Linting...$(RESET)"
	cd services/user-svc && golangci-lint run ./...
	cd services/order-svc && golangci-lint run ./...
	@echo "$(GREEN)✓ Lint passed$(RESET)"

## test: jalankan semua unit test
test:
	@echo "$(YELLOW)► Running tests...$(RESET)"
	go test ./... -v -race -coverprofile=coverage.out
	@echo "$(GREEN)✓ Tests passed$(RESET)"

## test-cover: lihat coverage report di browser
test-cover: test
	go tool cover -html=coverage.out

## build-user: build binary untuk user-service
build-user:
	@echo "$(YELLOW)► Building user-svc...$(RESET)"
	cd services/user-svc && go build -ldflags="-s -w" -o ../../bin/user-svc ./cmd/server
	@echo "$(GREEN)✓ bin/user-svc ready$(RESET)"

## build-order: build binary untuk order-service
build-order:
	@echo "$(YELLOW)► Building order-svc...$(RESET)"
	cd services/order-svc && go build -ldflags="-s -w" -o ../../bin/order-svc ./cmd/server
	@echo "$(GREEN)✓ bin/order-svc ready$(RESET)"

## build-all: build semua service
build-all: build-user build-order

## run-user: jalankan user-service secara lokal
run-user:
	@echo "$(CYAN)► Starting user-svc (HTTP :8080, gRPC :9090)...$(RESET)"
	cd services/user-svc && go run ./cmd/server

## run-order: jalankan order-service secara lokal
run-order:
	@echo "$(CYAN)► Starting order-svc (HTTP :8081)...$(RESET)"
	cd services/order-svc && go run ./cmd/server

## docker-up: jalankan semua service dengan Docker Compose
docker-up:
	docker compose up --build -d
	@echo "$(GREEN)✓ Services running$(RESET)"

## docker-down: matikan semua Docker container
docker-down:
	docker compose down
	@echo "$(GREEN)✓ Services stopped$(RESET)"

# ─── KUBERNETES ────────────────────────────────────────────────────────

## k8s-setup: install nginx ingress + metrics server di Docker Desktop K8s
k8s-setup:
	@echo "$(YELLOW)► Installing Kubernetes prerequisites...$(RESET)"
	chmod +x k8s/scripts/01-install-prerequisites.sh
	./k8s/scripts/01-install-prerequisites.sh

## k8s-build: build Docker images untuk Kubernetes (tag: local)
k8s-build:
	@echo "$(YELLOW)► Building images...$(RESET)"
	chmod +x k8s/scripts/02-build-images.sh
	./k8s/scripts/02-build-images.sh

## k8s-preview: lihat YAML final yang akan di-apply tanpa apply
k8s-preview:
	kubectl kustomize k8s/overlays/development

## k8s-deploy: build + deploy ke Docker Desktop Kubernetes
k8s-deploy: k8s-build
	chmod +x k8s/scripts/03-deploy.sh
	./k8s/scripts/03-deploy.sh development

## k8s-redeploy: rebuild images + restart deployment (setelah update code)
k8s-redeploy: k8s-build
	kubectl rollout restart deployment/user-svc -n gomonorepo
	kubectl rollout restart deployment/order-svc -n gomonorepo
	kubectl rollout status deployment/user-svc -n gomonorepo --timeout=60s
	kubectl rollout status deployment/order-svc -n gomonorepo --timeout=60s
	@echo "$(GREEN)✓ Redeployed$(RESET)"

## k8s-status: lihat status semua resource
k8s-status:
	chmod +x k8s/scripts/04-debug.sh
	./k8s/scripts/04-debug.sh status

## k8s-logs: lihat logs semua service
k8s-logs:
	./k8s/scripts/04-debug.sh logs

## k8s-forward: setup port-forward (user-svc :8080, order-svc :8081)
k8s-forward:
	./k8s/scripts/04-debug.sh port-forward

## k8s-test: test API endpoints (butuh port-forward aktif)
k8s-test:
	./k8s/scripts/04-debug.sh test

## k8s-clean: hapus semua resource di namespace gomonorepo
k8s-clean:
	./k8s/scripts/04-debug.sh clean

## proto-lint: lint .proto files dengan buf
proto-lint:
	cd proto && buf lint

## proto-breaking: cek breaking changes pada .proto files
proto-breaking:
	cd proto && buf breaking --against '.git#branch=main'
