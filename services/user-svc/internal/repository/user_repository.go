// services/user-svc/internal/repository/user_repository.go
//
// Package repository mengimplementasikan DATA ACCESS LAYER.
//
// Ini adalah layer yang paling dekat dengan "storage" — entah itu database,
// in-memory map, Redis, atau apapun. Layer di atasnya (handler, grpcserver)
// tidak perlu tahu bagaimana data disimpan; mereka cukup memanggil interface ini.
//
// PATTERN: Repository Pattern
// ┌─────────────┐    ┌──────────────────┐    ┌──────────────────┐
// │   Handler   │───▶│  UserRepository  │───▶│  InMemoryStore   │
// │  (HTTP/gRPC)│    │   (interface)    │    │  (implementasi)  │
// └─────────────┘    └──────────────────┘    └──────────────────┘
//
// Keuntungan pattern ini:
//   1. Mudah di-test: tinggal mock interface-nya
//   2. Mudah ganti storage: dari in-memory ke PostgreSQL cukup buat implementasi baru
//   3. Business logic tidak tercampur dengan storage logic

package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────
//  DOMAIN MODEL
// ─────────────────────────────────────────────

// User adalah model domain kita di layer repository.
// Ini BERBEDA dengan User di protobuf (user.pb.go).
// Kenapa dipisah? Karena domain model bisa punya field yang tidak ingin
// kita expose ke luar (misalnya: password hash), dan protobuf model
// mungkin punya field yang tidak relevan untuk storage.
// Konversi antara keduanya dilakukan di layer handler/grpcserver.
type User struct {
	ID        string
	Name      string
	Email     string
	Role      string
	CreatedAt time.Time
}

// ─────────────────────────────────────────────
//  REPOSITORY INTERFACE
// ─────────────────────────────────────────────

// UserRepository mendefinisikan operasi apa saja yang bisa dilakukan
// pada entitas User di storage. Interface ini adalah "kontrak" antara
// layer business logic dan layer data access.
//
// Semua method menerima context.Context sebagai parameter pertama.
// Ini adalah best practice Go karena:
//   1. Mendukung cancellation (misalnya jika client disconnect)
//   2. Mendukung timeout (misalnya query tidak boleh lebih dari 5 detik)
//   3. Mendukung tracing (meneruskan trace ID antar layer)
type UserRepository interface {
	FindByID(ctx context.Context, id string) (*User, error)
	FindAll(ctx context.Context) ([]*User, error)
	Save(ctx context.Context, user *User) (*User, error)
	Delete(ctx context.Context, id string) error
}

// ─────────────────────────────────────────────
//  IN-MEMORY IMPLEMENTATION
// ─────────────────────────────────────────────

// inMemoryUserRepo adalah implementasi UserRepository yang menyimpan data
// di dalam memory menggunakan Go map. Cocok untuk:
//   - Development & testing (cepat, tidak perlu setup database)
//   - Aplikasi yang datanya tidak perlu persisten
//   - Proof of concept
//
// CONCURRENCY SAFETY: Go map TIDAK thread-safe secara default!
// Jika dua goroutine menulis ke map yang sama secara bersamaan,
// program akan panic. Solusinya: gunakan sync.RWMutex.
//
// sync.RWMutex memungkinkan:
//   - Banyak goroutine membaca SECARA BERSAMAAN (RLock/RUnlock)
//   - Hanya SATU goroutine menulis pada satu waktu, dan tidak ada yang membaca (Lock/Unlock)
// Ini lebih efisien daripada sync.Mutex biasa untuk workload read-heavy.
type inMemoryUserRepo struct {
	mu    sync.RWMutex      // Guard untuk akses concurrent ke map
	store map[string]*User  // Key: user ID, Value: user data
}

// NewInMemoryUserRepository membuat repository baru dengan beberapa seed data.
// Dalam production, kamu akan punya factory function yang menerima *sql.DB atau
// semacamnya, tapi principlenya sama.
func NewInMemoryUserRepository() UserRepository {
	repo := &inMemoryUserRepo{
		store: make(map[string]*User),
	}

	// Seed data agar kita bisa langsung test tanpa perlu create user dulu
	seedUsers := []User{
		{ID: "user-001", Name: "Alice Wonderland", Email: "alice@example.com", Role: "admin", CreatedAt: time.Now().Add(-72 * time.Hour)},
		{ID: "user-002", Name: "Bob Builder", Email: "bob@example.com", Role: "customer", CreatedAt: time.Now().Add(-48 * time.Hour)},
		{ID: "user-003", Name: "Charlie Chaplin", Email: "charlie@example.com", Role: "customer", CreatedAt: time.Now().Add(-24 * time.Hour)},
	}

	for i := range seedUsers {
		u := seedUsers[i] // Create local copy to avoid loop variable capture
		repo.store[u.ID] = &u
	}

	return repo
}

// FindByID mencari user berdasarkan ID.
// Menggunakan RLock karena ini operasi READ — boleh concurrent.
func (r *inMemoryUserRepo) FindByID(ctx context.Context, id string) (*User, error) {
	// Selalu check context cancellation di awal.
	// Ini penting untuk operasi yang mungkin lambat (misalnya query ke database).
	// Meskipun in-memory cepat, good practice untuk selalu check.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	r.mu.RLock()         // Acquire read lock
	defer r.mu.RUnlock() // Release lock ketika fungsi return (apapun yang terjadi)

	user, ok := r.store[id]
	if !ok {
		// Kembalikan error yang deskriptif, bukan hanya nil.
		// Caller bisa check tipe error-nya untuk membedakan "not found" vs error lain.
		return nil, fmt.Errorf("user with id %q not found", id)
	}

	// Kembalikan copy dari data, bukan pointer ke map value langsung.
	// Kenapa? Jika kita kembalikan pointer langsung, caller bisa mengubah data
	// tanpa melalui mutex, menyebabkan race condition!
	userCopy := *user
	return &userCopy, nil
}

// FindAll mengembalikan semua users.
// Menggunakan RLock karena ini operasi READ.
func (r *inMemoryUserRepo) FindAll(ctx context.Context) ([]*User, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Inisialisasi slice dengan kapasitas yang sudah diketahui
	// untuk menghindari realokasi slice saat append.
	users := make([]*User, 0, len(r.store))
	for _, u := range r.store {
		userCopy := *u // Copy untuk menghindari race condition
		users = append(users, &userCopy)
	}

	return users, nil
}

// Save menyimpan user baru ke dalam store.
// Menggunakan Lock (bukan RLock) karena ini operasi WRITE.
func (r *inMemoryUserRepo) Save(ctx context.Context, user *User) (*User, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Generate ID baru jika belum ada.
	// UUID v4 adalah pilihan yang baik untuk ID karena:
	//   - Unik secara global tanpa koordinasi
	//   - Tidak bisa di-guess (aman untuk URL)
	//   - Tidak mengungkapkan urutan/jumlah data
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}

	r.mu.Lock()         // Acquire write lock — exclusive, tidak ada yang bisa baca/tulis
	defer r.mu.Unlock()

	// Simpan copy agar caller tidak bisa memodifikasi data di store melalui pointer yang dikembalikan
	userCopy := *user
	r.store[user.ID] = &userCopy

	return user, nil
}

// Delete menghapus user dari store berdasarkan ID.
func (r *inMemoryUserRepo) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.store[id]; !ok {
		return fmt.Errorf("user with id %q not found", id)
	}

	delete(r.store, id)
	return nil
}
