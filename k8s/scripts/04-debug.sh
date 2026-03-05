#!/usr/bin/env bash
# k8s/scripts/04-debug.sh
#
# Toolkit debugging untuk Kubernetes deployment kita.
# Kumpulkan semua informasi yang dibutuhkan untuk diagnosa masalah.
#
# Cara pakai:
#   ./k8s/scripts/04-debug.sh          # Tampilkan semua status
#   ./k8s/scripts/04-debug.sh logs     # Tampilkan logs semua service
#   ./k8s/scripts/04-debug.sh exec     # Masuk ke shell Pod (untuk debugging interaktif)

set -euo pipefail

BOLD='\033[1m'; CYAN='\033[0;36m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
NS="gomonorepo"

section() { echo -e "\n${BOLD}${CYAN}▶ $1${NC}"; }
cmd()     { echo -e "${YELLOW}$ $*${NC}"; eval "$@"; }

ACTION="${1:-status}"

case "$ACTION" in

  status)
    section "Namespace"
    cmd kubectl get namespace $NS

    section "Pods (with details)"
    cmd kubectl get pods -n $NS -o wide

    section "Services"
    cmd kubectl get svc -n $NS

    section "Ingress"
    cmd kubectl get ingress -n $NS

    section "ConfigMaps"
    cmd kubectl get configmaps -n $NS

    section "HPA (Horizontal Pod Autoscaler)"
    cmd kubectl get hpa -n $NS

    section "Events (recent, sorted by time)"
    cmd kubectl get events -n $NS --sort-by='.lastTimestamp' | tail -20

    section "Resource Usage (requires Metrics Server)"
    kubectl top pods -n $NS 2>/dev/null || echo "(Metrics Server not available)"
    ;;

  logs)
    section "user-svc logs (last 50 lines)"
    cmd kubectl logs -n $NS -l app=user-svc --tail=50 --prefix=true

    echo ""
    section "order-svc logs (last 50 lines)"
    cmd kubectl logs -n $NS -l app=order-svc --tail=50 --prefix=true
    ;;

  follow)
    SERVICE="${2:-user-svc}"
    section "Following logs for $SERVICE (Ctrl+C to stop)"
    kubectl logs -n $NS -l "app=$SERVICE" --follow --prefix=true
    ;;

  describe)
    SERVICE="${2:-user-svc}"
    section "Describing Deployment: $SERVICE"
    kubectl describe deployment "$SERVICE" -n $NS

    section "Describing Pods for $SERVICE"
    kubectl describe pods -n $NS -l "app=$SERVICE"
    ;;

  exec)
    # Cara debug yang powerful: masuk ke dalam Pod
    # Catatan: image kita (distroless) tidak punya shell!
    # Untuk debugging, kamu perlu debug container atau ephemeral container.
    # Cara 1: kubectl debug (Kubernetes 1.26+)
    # Cara 2: build image development dengan shell (alpine base)
    section "Starting debug session"
    POD=$(kubectl get pods -n $NS -l app=user-svc -o jsonpath='{.items[0].metadata.name}')
    echo "Pod: $POD"
    echo ""
    echo "Note: distroless image has no shell. Using kubectl debug with busybox..."
    # Ephemeral debug container: tidak perlu restart Pod, container debug "nempel" ke Pod yang ada
    kubectl debug -it "$POD" -n $NS --image=busybox:latest --target=user-svc -- sh
    ;;

  port-forward)
    section "Setting up port-forwards for direct access (tanpa Ingress)"
    echo "user-svc  → http://localhost:8080"
    echo "order-svc → http://localhost:8081"
    echo "(Press Ctrl+C to stop)"
    kubectl port-forward svc/user-svc 8080:80 -n $NS &
    kubectl port-forward svc/order-svc 8081:80 -n $NS &
    wait
    ;;

  test)
    section "Running quick API tests"
    BASE_URL="${2:-http://localhost}"

    echo "Testing via port-forward (make sure to run: ./04-debug.sh port-forward)"
    echo ""

    echo -e "${BOLD}[1] user-svc health check${NC}"
    curl -sf http://localhost:8080/healthz | jq . || echo "FAILED - is port-forward running?"

    echo ""
    echo -e "${BOLD}[2] List users${NC}"
    curl -sf http://localhost:8080/api/v1/users | jq . || echo "FAILED"

    echo ""
    echo -e "${BOLD}[3] Get specific user${NC}"
    curl -sf http://localhost:8080/api/v1/users/user-001 | jq . || echo "FAILED"

    echo ""
    echo -e "${BOLD}[4] order-svc health check${NC}"
    curl -sf http://localhost:8081/healthz | jq . || echo "FAILED"

    echo ""
    echo -e "${BOLD}[5] List orders${NC}"
    curl -sf http://localhost:8081/api/v1/orders | jq . || echo "FAILED"

    echo ""
    echo -e "${BOLD}[6] Get order with user enrichment (gRPC call!)${NC}"
    curl -sf http://localhost:8081/api/v1/orders/order-001 | jq . || echo "FAILED"

    echo ""
    echo -e "${BOLD}[7] Create new user${NC}"
    curl -sf -X POST http://localhost:8080/api/v1/users \
      -H "Content-Type: application/json" \
      -d '{"name":"Test User K8s","email":"testk8s@example.com","role":"customer"}' | jq .

    echo ""
    echo -e "${GREEN}Tests complete!${NC}"
    ;;

  clean)
    section "Removing all gomonorepo resources"
    echo -e "${YELLOW}WARNING: This will delete all resources in namespace $NS${NC}"
    read -p "Continue? (yes/N): " -r CONFIRM
    if [[ "$CONFIRM" == "yes" ]]; then
      kubectl delete namespace $NS
      echo "Namespace deleted."
    fi
    ;;

  *)
    echo "Usage: $0 {status|logs|follow|describe|exec|port-forward|test|clean}"
    exit 1
    ;;
esac
