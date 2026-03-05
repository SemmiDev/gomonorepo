// services/user-svc/internal/handler/user_handler.go
//
// Package handler mengimplementasikan HTTP REST API untuk User service.
//
// ARSITEKTUR LAYER:
// ┌──────────────────────────────────────────────┐
// │  HTTP Request (JSON)                         │
// │       ↓                                      │
// │  UserHandler  ← layer ini (parsing + routing)│
// │       ↓                                      │
// │  UserRepository (data access)                │
// │       ↓                                      │
// │  In-Memory Map (storage)                     │
// └──────────────────────────────────────────────┘
//
// Handler bertanggung jawab untuk:
//   1. Parse HTTP request (path params, query params, request body)
//   2. Validasi input
//   3. Panggil repository
//   4. Format dan kirim HTTP response
//
// Handler TIDAK boleh berisi business logic yang kompleks.
// Kalau ada logic yang rumit, taruh di "service layer" antara handler dan repository.
// Untuk kesederhanaan tutorial ini, kita langsung ke repository.

package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/semmidev/gomonorepo/services/user-svc/internal/repository"
)

// ─────────────────────────────────────────────
//  RESPONSE / REQUEST TYPES
// ─────────────────────────────────────────────
// Kita pisahkan "API types" (untuk HTTP JSON) dari "domain types" (untuk storage).
// Ini memberi kita fleksibilitas untuk:
//   - Mengubah nama field di API tanpa mengubah storage schema
//   - Menambah/menghapus field dari API response tanpa mengubah domain model
//   - Menghindari expose field sensitif (misalnya password hash)

// UserResponse adalah format JSON yang dikirim ke client.
// json:"..." tag menentukan nama field di JSON output.
// omitempty: field tidak akan muncul di JSON jika nilainya zero value.
type UserResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"` // ISO 8601 format, lebih readable daripada Unix timestamp
}

// CreateUserRequest adalah format JSON yang diterima dari client saat create user.
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// ErrorResponse adalah format standar error yang dikirim ke client.
// Konsistensi format error penting agar client bisa handle error dengan mudah.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// ─────────────────────────────────────────────
//  HANDLER STRUCT & CONSTRUCTOR
// ─────────────────────────────────────────────

// UserHandler memegang semua dependencies yang dibutuhkan oleh handler.
// Dengan dependency injection seperti ini, testing menjadi mudah:
// kamu bisa inject mock repository saat testing.
type UserHandler struct {
	repo repository.UserRepository
	log  *zap.Logger
}

// NewUserHandler membuat handler baru. Ini adalah constructor yang diinjeksi.
func NewUserHandler(repo repository.UserRepository, log *zap.Logger) *UserHandler {
	return &UserHandler{
		repo: repo,
		log:  log,
	}
}

// ─────────────────────────────────────────────
//  ROUTE REGISTRATION
// ─────────────────────────────────────────────

// Routes mengembalikan chi.Router yang sudah dikonfigurasi dengan semua routes.
// Kenapa method Routes() terpisah?
// Ini pattern yang bagus karena:
//   1. Handler bisa mendaftarkan routes-nya sendiri (self-contained)
//   2. main.go tinggal mount: r.Mount("/api/v1/users", handler.Routes())
//   3. Mudah untuk testing: bisa test router secara terisolasi
func (h *UserHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Chi menggunakan RESTful routing pattern.
	// Endpoint yang kita expose:
	//   GET    /              → ListUsers   (ambil semua user)
	//   POST   /              → CreateUser  (buat user baru)
	//   GET    /{id}          → GetUser     (ambil user by ID)
	//   DELETE /{id}          → DeleteUser  (hapus user)
	r.Get("/", h.ListUsers)
	r.Post("/", h.CreateUser)
	r.Get("/{id}", h.GetUser)
	r.Delete("/{id}", h.DeleteUser)

	return r
}

// ─────────────────────────────────────────────
//  HANDLER METHODS
// ─────────────────────────────────────────────

// ListUsers menangani GET /api/v1/users
// Mengembalikan semua users dalam format JSON array.
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.repo.FindAll(r.Context())
	if err != nil {
		h.log.Error("failed to find all users", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "internal_server_error",
		})
		return
	}

	// Konversi dari domain model ke response model
	responses := make([]UserResponse, 0, len(users))
	for _, u := range users {
		responses = append(responses, toUserResponse(u))
	}

	writeJSON(w, http.StatusOK, responses)
}

// GetUser menangani GET /api/v1/users/{id}
// Mengembalikan satu user berdasarkan ID di URL path.
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	// chi.URLParam mengambil path parameter {id} dari URL.
	// Ini lebih aman daripada parse manual dari r.URL.Path.
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "bad_request",
			Message: "id is required",
		})
		return
	}

	user, err := h.repo.FindByID(r.Context(), id)
	if err != nil {
		// Dalam production, kamu ingin membedakan "not found" (404) dari
		// error lain seperti database error (500). Bisa dengan custom error types.
		// Untuk simplicity, kita return 404 untuk semua error FindByID.
		h.log.Warn("user not found", zap.String("id", id), zap.Error(err))
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error:   "not_found",
			Message: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(user))
}

// CreateUser menangani POST /api/v1/users
// Menerima JSON body dan membuat user baru.
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	// Decode JSON body dari request.
	// json.NewDecoder lebih efisien dari json.Unmarshal untuk request body
	// karena dia membaca langsung dari io.Reader tanpa perlu membaca semua ke buffer dulu.
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "bad_request",
			Message: "invalid JSON body: " + err.Error(),
		})
		return
	}
	defer r.Body.Close()

	// Validasi input dasar.
	// Dalam production, gunakan library validasi seperti go-playground/validator
	// yang mendukung struct tags: `validate:"required,email"` dsb.
	if req.Name == "" || req.Email == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Message: "name and email are required",
		})
		return
	}

	if req.Role == "" {
		req.Role = "customer" // Default role
	}

	newUser := &repository.User{
		Name:  req.Name,
		Email: req.Email,
		Role:  req.Role,
	}

	saved, err := h.repo.Save(r.Context(), newUser)
	if err != nil {
		h.log.Error("failed to save user", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "internal_server_error",
		})
		return
	}

	// HTTP 201 Created untuk resource yang berhasil dibuat
	writeJSON(w, http.StatusCreated, toUserResponse(saved))
}

// DeleteUser menangani DELETE /api/v1/users/{id}
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.repo.Delete(r.Context(), id); err != nil {
		h.log.Warn("failed to delete user", zap.String("id", id), zap.Error(err))
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error:   "not_found",
			Message: err.Error(),
		})
		return
	}

	// HTTP 204 No Content: sukses tapi tidak ada response body
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────────────────────────
//  HELPER FUNCTIONS (private)
// ─────────────────────────────────────────────

// toUserResponse mengkonversi dari domain model ke response model.
// Ini adalah "mapper" function — simple tapi penting untuk decoupling.
func toUserResponse(u *repository.User) UserResponse {
	return UserResponse{
		ID:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		Role:      u.Role,
		CreatedAt: u.CreatedAt.Format(time.RFC3339), // ISO 8601 format
	}
}

// writeJSON adalah helper untuk menulis JSON response dengan content-type yang tepat.
// Kita extract ini agar tidak ada duplikasi kode di setiap handler.
// Dalam production, kamu mungkin ingin menambahkan error handling jika json.Encode gagal.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Kalau encode gagal, tidak banyak yang bisa kita lakukan karena
		// header sudah dikirim. Log dan lanjutkan.
		_ = err // Di production: log error ini
	}
}
