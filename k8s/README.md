# Kubernetes Deployment Guide — Docker Desktop

Panduan lengkap untuk deploy dan mengelola Go monorepo di Kubernetes lokal menggunakan Docker Desktop.

---

## Mengapa Kubernetes untuk Local Development?

Mungkin kamu bertanya: "Kan sudah ada Docker Compose, kenapa perlu belajar Kubernetes di lokal?" Pertanyaan yang bagus. Alasannya adalah **paritas environment** — semakin mirip environment development kamu dengan production, semakin kecil kemungkinan ada bug yang "only works on my machine."

Kubernetes juga mengajarkan kamu konsep yang tidak ada di Docker Compose: health probes, pod disruption budgets, horizontal autoscaling, dan resource limits. Semua konsep ini sangat penting di production.

---

## Arsitektur di Kubernetes

Sebelum menjalankan apapun, penting untuk memahami bagaimana request mengalir melalui sistem kita:

```
Internet / Browser / curl
         │
         ▼
[NGINX Ingress Controller]  ← satu entry point, port 80
    user.gomonorepo.local ──────────────────────────────────────────────┐
    order.gomonorepo.local ─────────────────────────────────────────┐   │
                                                                    │   │
         ┌──────────────────────────────────────────────────────────┘   │
         │                                                              │
         ▼                                                              ▼
  [Service: order-svc]                                      [Service: user-svc]
    ClusterIP, port 80                                        ClusterIP, port 80 & 9090
         │                                                              │
         ▼                                                              ▼
  [Pod: order-svc] ──── gRPC :9090 ──────────────────────▶ [Pod: user-svc]
  [Pod: order-svc]                                          [Pod: user-svc]
```

Perhatikan bahwa Ingress Controller adalah satu-satunya komponen yang menerima traffic dari luar. Semua Service internal menggunakan tipe `ClusterIP` yang hanya bisa diakses dari dalam cluster — ini adalah prinsip keamanan yang baik.

---

## Step-by-Step Setup

### Langkah 0: Aktifkan Kubernetes di Docker Desktop

Buka Docker Desktop → Settings → Kubernetes → centang "Enable Kubernetes" → Apply & Restart. Tunggu sampai status bar di kiri bawah menunjukkan "Kubernetes running" (ikon hijau).

Verifikasi dengan menjalankan perintah ini di terminal. Outputnya harus menunjukkan kamu terhubung ke cluster lokal:

```bash
kubectl cluster-info
# Output: Kubernetes control plane is running at https://kubernetes.docker.internal:6443

kubectl get nodes
# Output: NAME             STATUS   ROLES           AGE   VERSION
#         docker-desktop   Ready    control-plane   5m    v1.31.x
```

### Langkah 1: Install Prerequisites

Script ini menginstall dua komponen penting yang tidak ada secara default di Docker Desktop Kubernetes:

```bash
chmod +x k8s/scripts/*.sh
./k8s/scripts/01-install-prerequisites.sh
```

Script tersebut melakukan tiga hal. Pertama, menginstall **NGINX Ingress Controller** — ini adalah Pod yang berjalan di namespace `ingress-nginx` dan bertanggung jawab membaca semua Ingress resources di cluster, lalu mengkonfigurasi nginx sebagai reverse proxy. Tanpa ini, Ingress resource yang kita buat hanya "deklarasi kosong" tanpa ada yang mengeksekusinya.

Kedua, menginstall **Metrics Server** — komponen yang mengumpulkan data CPU dan memory dari semua Pod. HPA (HorizontalPodAutoscaler) yang kita buat di manifest tidak bisa bekerja tanpa Metrics Server karena dia perlu tahu berapa CPU yang dipakai untuk memutuskan apakah perlu scale up atau down.

Ketiga, menambahkan entri ke `/etc/hosts` agar hostname lokal kita bisa di-resolve:
```
127.0.0.1 user.gomonorepo.local
127.0.0.1 order.gomonorepo.local
```

### Langkah 2: Build Docker Images

```bash
./k8s/scripts/02-build-images.sh
```

Script ini menjalankan `docker build` untuk user-svc dan order-svc dengan tag `local`. Perhatikan bahwa build context selalu dari **root monorepo** (`.`), bukan dari dalam direktori service. Ini penting karena Dockerfile butuh akses ke `shared/` dan `gen/` yang berada di root.

Setelah build selesai, kamu bisa lihat imagenya:
```bash
docker images | grep gomonorepo
# ghcr.io/semmidev/gomonorepo/user-svc    local    xxx    15MB
# ghcr.io/semmidev/gomonorepo/order-svc   local    xxx    15MB
```

### Langkah 3: Deploy ke Kubernetes

```bash
./k8s/scripts/03-deploy.sh development
```

Di balik layar, script ini menjalankan `kubectl apply -k k8s/overlays/development`. Kustomize akan membaca semua resource dari base, menerapkan semua patch dari overlay development (kurangi replica ke 1, set `imagePullPolicy: Never`, set `APP_ENV: development`), lalu menghasilkan YAML final yang di-apply ke cluster.

Kamu bisa lihat YAML yang akan di-apply tanpa benar-benar apply dengan perintah:
```bash
kubectl kustomize k8s/overlays/development
```

### Langkah 4: Verifikasi Deployment

```bash
# Lihat semua Pod — pastikan STATUS = Running
kubectl get pods -n gomonorepo

# Output yang diharapkan:
# NAME                         READY   STATUS    RESTARTS   AGE
# order-svc-7d9f8b6c5-xk2p9   1/1     Running   0          30s
# user-svc-6c8b9d7f4-m3n7t    1/1     Running   0          30s
```

Kolom `READY: 1/1` artinya 1 dari 1 container di Pod siap menerima traffic (readiness probe lulus). Kalau kamu lihat `0/1`, Pod sedang dalam proses startup atau ada masalah.

### Langkah 5: Test Endpoint

```bash
# Setup port-forward di terminal terpisah
./k8s/scripts/04-debug.sh port-forward

# Di terminal lain, jalankan test suite
./k8s/scripts/04-debug.sh test
```

Atau akses langsung via Ingress (setelah `/etc/hosts` di-setup):
```bash
# user-svc via Ingress
curl http://user.gomonorepo.local/api/v1/users | jq .

# order-svc via Ingress (gRPC call ke user-svc terjadi di sini)
curl http://order.gomonorepo.local/api/v1/orders/order-001 | jq .
```

---

## Memahami Kustomize (Sangat Penting!)

Kustomize adalah tool manajemen konfigurasi yang dibangun langsung ke dalam `kubectl`. Konsep utamanya adalah memisahkan **base config** dari **environment-specific overrides**.

Bayangkan kamu punya resep masakan (base). Untuk diet, kamu tidak menulis ulang seluruh resep — kamu hanya catat "ganti gula dengan stevia, kurangi garam". Overlay Kustomize bekerja persis seperti "catatan modifikasi" ini.

Struktur direktori kita:
```
k8s/
├── base/           ← "resep asli" — konfigurasi yang berlaku di semua env
│   ├── user-svc/
│   ├── order-svc/
│   └── ingress/
└── overlays/
    ├── development/ ← "catatan modifikasi" untuk dev (1 replica, image local)
    └── production/  ← "catatan modifikasi" untuk prod (3 replica, image dari registry)
```

Untuk melihat YAML final yang dihasilkan untuk setiap environment:
```bash
# Lihat YAML development
kubectl kustomize k8s/overlays/development

# Lihat YAML production
kubectl kustomize k8s/overlays/production

# Bandingkan keduanya
diff <(kubectl kustomize k8s/overlays/development) <(kubectl kustomize k8s/overlays/production)
```

---

## Troubleshooting Umum

**Pod status `ImagePullBackOff` atau `ErrImagePull`**

Ini artinya Kubernetes gagal pull Docker image. Untuk development lokal, pastikan `imagePullPolicy: Never` sudah di-set (sudah ada di overlay development kita) dan image memang ada dengan nama yang benar:
```bash
docker images | grep gomonorepo
# Jika kosong, jalankan: ./k8s/scripts/02-build-images.sh
```

**Pod status `Pending`**

Pod tidak bisa dijadwalkan. Kemungkinan penyebabnya adalah topologySpreadConstraints yang tidak bisa dipenuhi di single-node cluster. Overlay development kita sudah menangani ini dengan patch yang menghapus constraint tersebut, tapi kalau masih terjadi, cek dengan:
```bash
kubectl describe pod <nama-pod> -n gomonorepo
# Lihat bagian "Events:" di output untuk alasan kenapa Pending
```

**Pod status `CrashLoopBackOff`**

Aplikasi berjalan tapi terus crash. Lihat log untuk tahu penyebabnya:
```bash
kubectl logs -n gomonorepo -l app=user-svc --previous
# Flag --previous: lihat log dari container sebelumnya yang crash
```

**Ingress tidak bisa diakses**

Pertama pastikan NGINX Ingress Controller running:
```bash
kubectl get pods -n ingress-nginx
# Harus ada pod dengan status Running
```

Lalu pastikan entri `/etc/hosts` sudah ada:
```bash
cat /etc/hosts | grep gomonorepo
# Harus ada: 127.0.0.1 user.gomonorepo.local
```

**gRPC connection antara order-svc dan user-svc gagal**

Cek apakah order-svc bisa resolve DNS user-svc:
```bash
# Masuk ke Pod order-svc
kubectl exec -it deploy/order-svc -n gomonorepo -- sh
# Di dalam Pod:
nslookup user-svc  # Harus resolve ke ClusterIP
```

---

## Perintah kubectl Penting untuk Sehari-hari

Berikut adalah "vocabulary" kubectl yang perlu kamu hafal. Semua perintah ini ditambahkan `-n gomonorepo` untuk menentukan namespace kita.

```bash
# Lihat semua resource di namespace
kubectl get all -n gomonorepo

# Lihat log secara real-time (seperti tail -f)
kubectl logs -f -l app=user-svc -n gomonorepo

# Restart Deployment (berguna setelah update ConfigMap)
# ConfigMap tidak otomatis di-reload oleh Pod yang sudah jalan!
kubectl rollout restart deployment/user-svc -n gomonorepo

# Lihat history rolling update
kubectl rollout history deployment/user-svc -n gomonorepo

# Rollback ke versi sebelumnya
kubectl rollout undo deployment/user-svc -n gomonorepo

# Scale manual (tanpa HPA)
kubectl scale deployment/user-svc --replicas=3 -n gomonorepo

# Lihat resource usage (butuh Metrics Server)
kubectl top pods -n gomonorepo

# Port-forward untuk akses langsung ke Pod (bypass Service dan Ingress)
kubectl port-forward pod/<nama-pod> 8080:8080 -n gomonorepo
```

---

## Alur Kerja Update Code

Saat kamu mengubah kode dan ingin deploy ulang, urutannya adalah:

Pertama, ubah kode kamu. Kedua, rebuild image: `./k8s/scripts/02-build-images.sh`. Ketiga, restart Deployment agar Kubernetes pull image baru (karena tag `local` tidak berubah, Kubernetes tidak tahu ada image baru):

```bash
kubectl rollout restart deployment/user-svc -n gomonorepo
kubectl rollout restart deployment/order-svc -n gomonorepo
```

Atau lebih mudah, tambahkan target ini ke Makefile:
```bash
make k8s-redeploy  # Rebuild image + restart deployment
```

Di production, alur ini digantikan oleh CI/CD pipeline yang otomatis build image dengan tag baru (Git SHA) dan update image di deployment.

---

## Menghapus Resource (Teardown)

Jika kamu sudah selesai dan ingin menghapus semua resource (Deployments, Services, Ingress, dll) dari cluster Kubernetes lokalmu, cara termudah adalah menggunakan Kustomize command terbalik:

```bash
kubectl delete -k k8s/overlays/development
```

Ini akan membaca konfigurasi yang sama dengan saat deploy, dan menghapus komponen-komponen tersebut secara otomatis.

Jika kamu ingin memastikan **semuanya** 100% bersih (termasuk isi namespace):

```bash
kubectl delete namespace gomonorepo
```

Kami juga telah menyediakan script otomatis di:
```bash
./k8s/scripts/05-teardown.sh development
```
