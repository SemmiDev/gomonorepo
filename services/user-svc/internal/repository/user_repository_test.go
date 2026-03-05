// services/user-svc/internal/repository/user_repository_test.go
//
// Unit test untuk in-memory repository.
//
// FILOSOFI TESTING DI GO:
// Go memiliki testing framework built-in (testing package), tidak perlu library eksternal.
// Konvensi: test file berakhiran _test.go, test function diawali TestXxx.
//
// JENIS TEST YANG PENTING:
//
//   1. Unit Test (ini):    Test satu komponen secara terisolasi
//   2. Integration Test:  Test beberapa komponen bersama (misalnya handler + repo)
//   3. Race Condition Test: go test -race untuk deteksi concurrent bugs
//
// PATTERN TABLE-DRIVEN TESTS:
// Go sangat mendorong "table-driven tests" di mana test cases didefinisikan
// sebagai slice of structs. Ini membuat test lebih terorganisir dan mudah
// ditambahkan case baru tanpa code duplication.

package repository_test

import (
	"context"
	"sync"
	"testing"

	"github.com/semmidev/gomonorepo/services/user-svc/internal/repository"
)

// TestInMemoryUserRepo_FindByID menguji FindByID dengan berbagai scenario.
// Table-driven test: setiap entry dalam `tests` adalah satu test case.
func TestInMemoryUserRepo_FindByID(t *testing.T) {
	// Setup: buat repository yang sama untuk semua sub-tests
	repo := repository.NewInMemoryUserRepository()

	// Definisikan test cases sebagai table.
	// Pendekatan ini jauh lebih bersih daripada menulis TestFindByID_Found,
	// TestFindByID_NotFound, TestFindByID_EmptyID secara terpisah.
	tests := []struct {
		name        string // Nama test case (muncul di output)
		id          string // Input: user ID yang dicari
		expectError bool   // Apakah kita expect error?
	}{
		{
			name:        "found: existing seed user",
			id:          "user-001",
			expectError: false,
		},
		{
			name:        "not found: non-existent ID",
			id:          "user-999",
			expectError: true,
		},
		{
			name:        "not found: empty ID",
			id:          "",
			expectError: true,
		},
	}

	for _, tc := range tests {
		// t.Run membuat sub-test dengan nama tc.name.
		// Keuntungan: kamu bisa jalankan test tertentu saja:
		// go test -run TestInMemoryUserRepo_FindByID/found
		t.Run(tc.name, func(t *testing.T) {
			// t.Parallel() memungkinkan sub-tests berjalan secara concurrent.
			// Ini mempercepat test suite untuk test yang independent.
			t.Parallel()

			user, err := repo.FindByID(context.Background(), tc.id)

			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
				if user != nil {
					t.Errorf("expected nil user on error, got %+v", user)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
				if user == nil {
					t.Fatal("expected user to be non-nil") // Fatal menghentikan test case ini
				}
				if user.ID != tc.id {
					t.Errorf("expected ID %q, got %q", tc.id, user.ID)
				}
			}
		})
	}
}

// TestInMemoryUserRepo_Save menguji operasi simpan user baru.
func TestInMemoryUserRepo_Save(t *testing.T) {
	repo := repository.NewInMemoryUserRepository()

	t.Run("save new user generates ID and timestamp", func(t *testing.T) {
		newUser := &repository.User{
			Name:  "Test User",
			Email: "test@example.com",
			Role:  "customer",
		}

		saved, err := repo.Save(context.Background(), newUser)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verifikasi bahwa ID di-generate secara otomatis
		if saved.ID == "" {
			t.Error("expected ID to be generated, got empty string")
		}

		// Verifikasi bahwa CreatedAt di-set
		if saved.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set, got zero time")
		}

		// Verifikasi bahwa user bisa ditemukan setelah disimpan
		found, err := repo.FindByID(context.Background(), saved.ID)
		if err != nil {
			t.Errorf("expected to find saved user, got error: %v", err)
		}
		if found.Name != newUser.Name {
			t.Errorf("expected name %q, got %q", newUser.Name, found.Name)
		}
	})

	t.Run("save user with existing ID updates data", func(t *testing.T) {
		// Simpan user dengan ID eksplisit
		user := &repository.User{ID: "explicit-id", Name: "Original", Email: "o@test.com", Role: "customer"}
		_, err := repo.Save(context.Background(), user)
		if err != nil {
			t.Fatalf("unexpected error on first save: %v", err)
		}

		// Update dengan ID yang sama
		updated := &repository.User{ID: "explicit-id", Name: "Updated", Email: "u@test.com", Role: "admin"}
		_, err = repo.Save(context.Background(), updated)
		if err != nil {
			t.Fatalf("unexpected error on update: %v", err)
		}

		found, _ := repo.FindByID(context.Background(), "explicit-id")
		if found.Name != "Updated" {
			t.Errorf("expected name to be updated to 'Updated', got %q", found.Name)
		}
	})
}

// TestInMemoryUserRepo_ConcurrentAccess adalah test yang PALING PENTING
// untuk in-memory repository. Kita mensimulasikan banyak goroutine yang
// membaca dan menulis secara bersamaan.
//
// Jalankan dengan: go test -race ./...
// Flag -race mengaktifkan Go race detector yang akan SEGERA melaporkan
// jika ada race condition, bahkan yang jarang terjadi.
func TestInMemoryUserRepo_ConcurrentAccess(t *testing.T) {
	repo := repository.NewInMemoryUserRepository()

	// Buat 50 goroutine yang semuanya berjalan secara bersamaan:
	//   - 25 goroutine membaca (FindAll)
	//   - 25 goroutine menulis (Save)
	// Tanpa sync.RWMutex yang benar, ini PASTI akan panic atau corrupt data.
	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // *2 karena readers dan writers

	// Goroutine readers
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()
			_, err := repo.FindAll(context.Background())
			if err != nil {
				t.Errorf("reader goroutine %d error: %v", n, err)
			}
		}(i)
	}

	// Goroutine writers
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()
			user := &repository.User{
				Name:  "Concurrent User",
				Email: "concurrent@test.com",
				Role:  "customer",
			}
			_, err := repo.Save(context.Background(), user)
			if err != nil {
				t.Errorf("writer goroutine %d error: %v", n, err)
			}
		}(i)
	}

	// Tunggu semua goroutine selesai
	wg.Wait()
	// Jika kode sampai sini tanpa panic, test concurrency lulus!
}

// TestInMemoryUserRepo_CancelledContext memverifikasi bahwa operasi
// menghormati context cancellation. Ini penting untuk memastikan
// service bisa "bersih-bersih" ketika client disconnect.
func TestInMemoryUserRepo_CancelledContext(t *testing.T) {
	repo := repository.NewInMemoryUserRepository()

	// Buat context yang sudah di-cancel
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel SEGERA

	_, err := repo.FindByID(ctx, "user-001")
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}
