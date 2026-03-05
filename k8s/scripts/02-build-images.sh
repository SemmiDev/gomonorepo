#!/usr/bin/env bash
# k8s/scripts/02-build-images.sh
#
# Script ini membangun Docker image untuk semua service dan meload-nya
# ke Docker Desktop Kubernetes.
#
# KONSEP PENTING: Kenapa kita perlu "load" image ke Kubernetes?
#
# Docker Desktop Kubernetes sebenarnya menggunakan Docker daemon yang sama
# dengan Docker yang kamu pakai sehari-hari. Jadi saat kamu build image
# dengan "docker build", image itu otomatis tersedia untuk Docker Desktop K8s.
#
# Bedanya dengan Kubernetes di cloud (EKS, GKE, AKS):
# Di cloud, cluster Kubernetes punya worker nodes yang berbeda dari mesin kamu.
# Mereka tidak punya akses ke Docker daemon lokal kamu.
# Kamu HARUS push image ke container registry (ECR, GCR, ACR) terlebih dahulu.
#
# Di Docker Desktop: build → langsung bisa dipakai (tidak perlu push/pull)
# Di cloud Kubernetes: build → push ke registry → cluster pull dari registry

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
log_info()    { echo -e "${CYAN}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Pindah ke root monorepo (dua level di atas scripts/)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_ROOT"
log_info "Working directory: $REPO_ROOT"

# Tag untuk development local
TAG="local"

# ── Build user-svc ───────────────────────────────────────────────────────
log_info "Building user-svc image..."
echo ""

# Build image menggunakan Dockerfile di services/user-svc/
# Build CONTEXT adalah root repo (.) bukan services/user-svc/
# karena Dockerfile butuh akses ke shared/ dan gen/ yang ada di root.
# -t: nama dan tag image
# -f: path ke Dockerfile
docker build \
    --tag "ghcr.io/semmidev/gomonorepo/user-svc:${TAG}" \
    --file "services/user-svc/Dockerfile" \
    --build-arg BUILDKIT_INLINE_CACHE=1 \
    . # ← titik ini adalah build context (root repo)

log_success "user-svc image built: ghcr.io/semmidev/gomonorepo/user-svc:${TAG}"

# ── Build order-svc ──────────────────────────────────────────────────────
log_info "Building order-svc image..."
echo ""

docker build \
    --tag "ghcr.io/semmidev/gomonorepo/order-svc:${TAG}" \
    --file "services/order-svc/Dockerfile" \
    --build-arg BUILDKIT_INLINE_CACHE=1 \
    .

log_success "order-svc image built: ghcr.io/semmidev/gomonorepo/order-svc:${TAG}"

# ── Verifikasi images ada ────────────────────────────────────────────────
echo ""
log_info "Verifying built images:"
docker images | grep "gomonorepo" | grep "$TAG"

echo ""
log_success "All images built successfully!"
echo ""
echo -e "${CYAN}Image sizes:${NC}"
docker images --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}" | grep -E "REPOSITORY|gomonorepo"

echo ""
echo -e "${CYAN}Next step:${NC} Run ./k8s/scripts/03-deploy.sh"
