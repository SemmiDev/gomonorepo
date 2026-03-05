#!/usr/bin/env bash
# Script ini digunakan untuk menghapus semua resource Kubernetes yang dibuat untuk gomonorepo.
#
# Penggunaan:
# ./05-teardown.sh [development|production]
#
# Jika tidak ada argumen yang diberikan, default-nya adalah 'development'

set -e

ENV=${1:-development}

echo "[INFO] Menghapus deployment di environment: $ENV"

# Menghapus resource menggunakan definisi kustomize
if [ -d "k8s/overlays/$ENV" ]; then
    kubectl delete -k "k8s/overlays/$ENV" || true
    echo "[OK] Resource kustomize untuk $ENV berhasil dihapus."
else
    echo "[ERROR] Direktori k8s/overlays/$ENV tidak ditemukan!"
fi

# (Opsional) Menghapus namespace secara keseluruhan jika kamu ingin membersihkan total
echo "[INFO] Apakah kamu juga ingin menghapus namespace 'gomonorepo'? (y/N)"
read -r response
if [[ "$response" =~ ^([yY][eE][sS]|[yY])+$ ]]; then
    kubectl delete namespace gomonorepo || true
    echo "[OK] Namespace 'gomonorepo' berhasil dihapus."
fi

echo "[INFO] Teardown selesai!"
