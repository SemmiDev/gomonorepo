#!/usr/bin/env bash
# k8s/scripts/03-deploy.sh
#
# Script utama untuk deploy ke Kubernetes menggunakan Kustomize overlay.
#
# Cara pakai:
#   ./k8s/scripts/03-deploy.sh development   # deploy ke development (default)
#   ./k8s/scripts/03-deploy.sh production    # deploy ke production
#
# Yang dilakukan script ini:
#   1. Validasi Kustomize overlay
#   2. Preview perubahan (dry-run)
#   3. Apply ke cluster
#   4. Tunggu semua Deployment ready
#   5. Tampilkan status akhir

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
log_info()    { echo -e "${CYAN}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }
log_header()  { echo -e "\n${BOLD}${CYAN}══ $1 ══${NC}\n"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_ROOT"

# Environment default: development
ENVIRONMENT="${1:-development}"
OVERLAY_PATH="k8s/overlays/${ENVIRONMENT}"
NAMESPACE="gomonorepo"

# Validasi environment
if [[ ! -d "$OVERLAY_PATH" ]]; then
    log_error "Overlay not found: $OVERLAY_PATH"
fi

log_header "Deploy gomonorepo → $ENVIRONMENT"
log_info "Overlay path: $OVERLAY_PATH"
log_info "Target namespace: $NAMESPACE"

# ── Step 1: Validasi Kustomize output ────────────────────────────────────
log_info "Validating Kustomize configuration..."

# kubectl kustomize membuild YAML final dari overlay tanpa apply ke cluster.
# Ini adalah cara terbaik untuk debug: lihat YAML yang akan di-apply
# setelah semua patches dan transformasi diterapkan.
kubectl kustomize "$OVERLAY_PATH" > /dev/null || log_error "Kustomize validation failed!"
log_success "Kustomize configuration is valid"

# ── Step 2: Preview (opsional, hanya untuk confirmation di production) ──
if [[ "$ENVIRONMENT" == "production" ]]; then
    log_warn "You are about to deploy to PRODUCTION!"
    log_info "Preview of resources to be applied:"
    kubectl kustomize "$OVERLAY_PATH" | kubectl diff -f - || true  # diff returns non-zero if there are changes
    echo ""
    read -p "Continue with production deployment? (yes/N): " -r CONFIRM
    [[ "$CONFIRM" == "yes" ]] || { log_warn "Deployment cancelled."; exit 0; }
fi

# ── Step 3: Apply ke cluster ─────────────────────────────────────────────
log_info "Applying resources to cluster..."

# kubectl apply -k adalah shorthand untuk kubectl kustomize + kubectl apply
# --server-side: lebih robust untuk large configs, menghindari "too long annotation" error
kubectl apply -k "$OVERLAY_PATH" --server-side

log_success "Resources applied successfully!"

# ── Step 4: Tunggu Deployments ready ────────────────────────────────────
log_info "Waiting for deployments to be ready..."

# kubectl rollout status memantau progress rolling update.
# Dia akan menunggu sampai semua Pod baru ready atau timeout.
# Sangat penting untuk CI/CD — kamu tahu kapan deployment benar-benar selesai.
kubectl rollout status deployment/user-svc -n "$NAMESPACE" --timeout=180s
kubectl rollout status deployment/order-svc -n "$NAMESPACE" --timeout=180s

log_success "All deployments are ready!"

# ── Step 5: Tampilkan status ─────────────────────────────────────────────
log_header "Deployment Status"

echo -e "${BOLD}Pods:${NC}"
kubectl get pods -n "$NAMESPACE" -o wide

echo ""
echo -e "${BOLD}Services:${NC}"
kubectl get svc -n "$NAMESPACE"

echo ""
echo -e "${BOLD}Ingress:${NC}"
kubectl get ingress -n "$NAMESPACE"

echo ""
log_success "Deployment complete! 🚀"
echo ""
echo -e "${CYAN}Access your services:${NC}"
echo "  user-svc  REST API : http://user.gomonorepo.local/api/v1/users"
echo "  order-svc REST API : http://order.gomonorepo.local/api/v1/orders"
echo ""
echo -e "${CYAN}Or use port-forward (if Ingress not setup):${NC}"
echo "  kubectl port-forward svc/user-svc 8080:80 -n gomonorepo"
echo "  kubectl port-forward svc/order-svc 8081:80 -n gomonorepo"
