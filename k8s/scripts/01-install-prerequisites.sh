#!/usr/bin/env bash
# k8s/scripts/01-install-prerequisites.sh
#
# Script ini menginstall semua prerequisite untuk menjalankan monorepo
# kita di Docker Desktop Kubernetes. Jalankan sekali sebelum deploy pertama kali.
#
# Yang diinstall:
#   1. NGINX Ingress Controller  — reverse proxy untuk routing external traffic
#   2. Metrics Server            — dibutuhkan HPA untuk baca CPU/memory metrics
#
# Cara jalankan:
#   chmod +x k8s/scripts/01-install-prerequisites.sh
#   ./k8s/scripts/01-install-prerequisites.sh

set -euo pipefail  # Exit jika ada command gagal, undefined variable, atau pipe failure

# Warna untuk output yang lebih readable
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

log_info()    { echo -e "${CYAN}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# ── Cek Prerequisites ──────────────────────────────────────────────────
log_info "Checking prerequisites..."

command -v kubectl >/dev/null 2>&1 || log_error "kubectl not found. Install from: https://kubernetes.io/docs/tasks/tools/"
command -v docker >/dev/null 2>&1  || log_error "docker not found. Install Docker Desktop."

# Pastikan kubectl terhubung ke Docker Desktop cluster
CONTEXT=$(kubectl config current-context 2>/dev/null || echo "none")
if [[ "$CONTEXT" != "docker-desktop" ]]; then
    log_warn "Current kubectl context is: $CONTEXT"
    log_warn "Expected: docker-desktop"
    log_warn "Switch with: kubectl config use-context docker-desktop"
    read -p "Continue anyway? (y/N): " -n 1 -r
    echo
    [[ $REPLY =~ ^[Yy]$ ]] || exit 1
fi
log_success "Connected to cluster: $CONTEXT"

# ── 1. Install NGINX Ingress Controller ─────────────────────────────────
log_info "Installing NGINX Ingress Controller..."

# Cek apakah sudah terinstall
if kubectl get namespace ingress-nginx >/dev/null 2>&1; then
    log_warn "NGINX Ingress Controller namespace already exists, skipping install."
else
    # Install NGINX Ingress Controller untuk Docker Desktop
    # URL ini adalah manifest resmi dari kubernetes/ingress-nginx
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.11.3/deploy/static/provider/cloud/deploy.yaml

    log_info "Waiting for NGINX Ingress Controller to be ready (this may take 1-2 minutes)..."
    kubectl wait --namespace ingress-nginx \
        --for=condition=ready pod \
        --selector=app.kubernetes.io/component=controller \
        --timeout=120s

    log_success "NGINX Ingress Controller is ready!"
fi

# ── 2. Install Metrics Server ────────────────────────────────────────────
log_info "Installing Metrics Server (required for HPA)..."

if kubectl get deployment metrics-server -n kube-system >/dev/null 2>&1; then
    log_warn "Metrics Server already exists, skipping install."
else
    # Download metrics-server manifest
    METRICS_SERVER_URL="https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml"

    # Download dulu, lalu patch untuk menambahkan --kubelet-insecure-tls
    # Flag ini dibutuhkan karena Docker Desktop menggunakan self-signed certificate
    # yang tidak trusted oleh metrics-server.
    curl -sL "$METRICS_SERVER_URL" | \
        sed 's/args:/args:\n        - --kubelet-insecure-tls/' | \
        kubectl apply -f -

    log_info "Waiting for Metrics Server to be ready..."
    kubectl wait --namespace kube-system \
        --for=condition=ready pod \
        --selector=k8s-app=metrics-server \
        --timeout=120s

    log_success "Metrics Server is ready!"
fi

# ── 3. Setup /etc/hosts untuk local domain ──────────────────────────────
log_info "Checking /etc/hosts entries..."

HOSTS_ENTRIES=(
    "127.0.0.1 user.gomonorepo.local"
    "127.0.0.1 order.gomonorepo.local"
)

for entry in "${HOSTS_ENTRIES[@]}"; do
    HOST=$(echo "$entry" | awk '{print $2}')
    if grep -q "$HOST" /etc/hosts; then
        log_warn "/etc/hosts already has entry for $HOST"
    else
        echo "$entry" | sudo tee -a /etc/hosts > /dev/null
        log_success "Added to /etc/hosts: $entry"
    fi
done

echo ""
log_success "All prerequisites installed!"
echo ""
echo -e "${CYAN}Next step:${NC} Run ./k8s/scripts/02-build-images.sh"
