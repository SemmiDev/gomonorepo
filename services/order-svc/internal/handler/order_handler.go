// services/order-svc/internal/handler/order_handler.go
//
// Package handler untuk order-svc.
//
// POIN KUNCI YANG MEMBEDAKAN order-svc dari user-svc:
// Handler ini memiliki dependency ke userServiceClient — sebuah gRPC client
// yang terhubung ke user-svc. Ketika kita membuat order baru, kita PERTAMA
// memverifikasi bahwa user tersebut ada dengan memanggil user-svc via gRPC.
//
// Ini adalah pola komunikasi antar-service yang sangat umum di microservices:
//   order-svc (HTTP REST) ←── client request
//   order-svc ──────────────▶ user-svc (gRPC) untuk verifikasi user
//   order-svc (HTTP REST) ──▶ response ke client
//
// KENAPA gRPC UNTUK KOMUNIKASI INTERNAL?
// Bayangkan kamu punya 20 microservices yang saling berkomunikasi.
// Kalau pakai HTTP/JSON:
//   - Setiap service harus tahu URL service lain
//   - Tidak ada type checking: kamu bisa kirim field yang salah
//   - Performa lebih lambat (JSON parsing vs binary protobuf)
//
// Dengan gRPC + Protobuf:
//   - Kontrak terdefinisi di .proto file (single source of truth)
//   - Compiler Go akan error jika kamu pakai field yang tidak ada
//   - Binary encoding 3-10x lebih compact dari JSON
//   - Built-in support untuk streaming, deadline, cancellation

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/semmidev/gomonorepo/gen/go/user/v1"
	"github.com/semmidev/gomonorepo/services/order-svc/internal/repository"
)

// ─────────────────────────────────────────────
//  REQUEST/RESPONSE TYPES
// ─────────────────────────────────────────────

// OrderItemRequest adalah format JSON untuk item dalam request create order.
type OrderItemRequest struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
}

// CreateOrderRequest adalah body JSON untuk membuat order baru.
type CreateOrderRequest struct {
	UserID string             `json:"user_id"`
	Items  []OrderItemRequest `json:"items"`
}

// OrderItemResponse adalah format JSON untuk item dalam response.
type OrderItemResponse struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	Subtotal    float64 `json:"subtotal"` // Field computed, tidak ada di repository
}

// OrderResponse adalah format JSON yang dikirim ke client.
// Perhatikan field "user" — ini di-populate dari gRPC call ke user-svc!
// Kita "memperkaya" (enrich) response dengan data dari service lain.
type OrderResponse struct {
	ID          string              `json:"id"`
	UserID      string              `json:"user_id"`
	UserName    string              `json:"user_name,omitempty"`  // Dari user-svc via gRPC
	UserEmail   string              `json:"user_email,omitempty"` // Dari user-svc via gRPC
	Items       []OrderItemResponse `json:"items"`
	TotalAmount float64             `json:"total_amount"`
	Status      string              `json:"status"`
	CreatedAt   string              `json:"created_at"`
}

// UpdateStatusRequest adalah body JSON untuk update status order.
type UpdateStatusRequest struct {
	Status string `json:"status"`
}

// ErrorResponse untuk format error yang konsisten.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// ─────────────────────────────────────────────
//  HANDLER STRUCT
// ─────────────────────────────────────────────

// OrderHandler memegang semua dependencies termasuk gRPC client ke user-svc.
type OrderHandler struct {
	repo           repository.OrderRepository
	userSvcClient  userv1.UserServiceClient // ← gRPC client! Ini yang membedakan dari user-svc handler
	log            *zap.Logger
}

// NewOrderHandler membuat handler baru.
// userClient bisa nil jika user-svc tidak tersedia (degraded mode).
func NewOrderHandler(
	repo repository.OrderRepository,
	userClient userv1.UserServiceClient,
	log *zap.Logger,
) *OrderHandler {
	return &OrderHandler{
		repo:          repo,
		userSvcClient: userClient,
		log:           log,
	}
}

// Routes mendaftarkan semua endpoint order.
func (h *OrderHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Endpoint yang kita expose:
	//   GET    /              → ListOrders    (semua order)
	//   POST   /              → CreateOrder   (buat order baru)
	//   GET    /{id}          → GetOrder      (satu order by ID, diperkaya dengan data user)
	//   GET    /user/{userId} → GetUserOrders (semua order milik satu user)
	//   PATCH  /{id}/status   → UpdateStatus  (ubah status order)
	r.Get("/", h.ListOrders)
	r.Post("/", h.CreateOrder)
	r.Get("/{id}", h.GetOrder)
	r.Get("/user/{userId}", h.GetUserOrders)
	r.Patch("/{id}/status", h.UpdateStatus)

	return r
}

// ─────────────────────────────────────────────
//  HANDLER METHODS
// ─────────────────────────────────────────────

// ListOrders menangani GET /api/v1/orders
func (h *OrderHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := h.repo.FindAll(r.Context())
	if err != nil {
		h.log.Error("failed to list orders", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal_server_error"})
		return
	}

	responses := make([]OrderResponse, 0, len(orders))
	for _, o := range orders {
		responses = append(responses, toOrderResponse(o))
	}

	writeJSON(w, http.StatusOK, responses)
}

// GetOrder menangani GET /api/v1/orders/{id}
// FITUR UTAMA: Memperkaya response dengan data user dari gRPC call ke user-svc.
func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	order, err := h.repo.FindByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error:   "not_found",
			Message: err.Error(),
		})
		return
	}

	resp := toOrderResponse(order)

	// ── Enrich dengan data user dari user-svc via gRPC ──────────────────
	// Kita memanggil user-svc untuk mendapatkan nama dan email user.
	// Pola ini disebut "service composition" atau "API aggregation":
	// kita menggabungkan data dari beberapa service menjadi satu response.
	//
	// RESILIENCE PATTERN: Kita tidak gagal jika user-svc tidak bisa dihubungi.
	// Kita tetap return order data, hanya tanpa informasi user (degraded gracefully).
	// Ini disebut "graceful degradation" — sangat penting di microservices
	// karena network failures adalah hal normal, bukan exception.
	if h.userSvcClient != nil {
		// Gunakan context dengan timeout terpisah untuk gRPC call.
		// Kita tidak mau gRPC call yang lambat memblok response HTTP kita.
		// 2 detik adalah timeout yang reasonable untuk internal call.
		grpcCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		userResp, err := h.userSvcClient.GetUser(grpcCtx, &userv1.GetUserRequest{
			Id: order.UserID,
		})

		if err != nil {
			// Cek apakah error ini bisa diabaikan (user not found) atau serius
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				h.log.Warn("user not found for order enrichment",
					zap.String("order_id", id),
					zap.String("user_id", order.UserID),
				)
			} else {
				// Error lain (timeout, connection refused) — log tapi tetap lanjut
				h.log.Warn("failed to enrich order with user data (degraded mode)",
					zap.String("order_id", id),
					zap.Error(err),
				)
			}
			// Tetap return order tanpa data user (graceful degradation)
		} else {
			// Enrichment berhasil! Tambahkan data user ke response.
			resp.UserName = userResp.GetUser().GetName()
			resp.UserEmail = userResp.GetUser().GetEmail()
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// CreateOrder menangani POST /api/v1/orders
// Verifikasi user dulu via gRPC sebelum membuat order.
func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "bad_request",
			Message: "invalid JSON: " + err.Error(),
		})
		return
	}
	defer r.Body.Close()

	if req.UserID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Message: "user_id is required",
		})
		return
	}

	if len(req.Items) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Message: "at least one item is required",
		})
		return
	}

	// ── Verifikasi User via gRPC ────────────────────────────────────────
	// BERBEDA dengan GetOrder, di sini verifikasi user adalah WAJIB.
	// Kita tidak mau membuat order untuk user yang tidak ada.
	// Jika user-svc tidak bisa dihubungi, kita HARUS gagal (fail fast).
	if h.userSvcClient != nil {
		grpcCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		_, err := h.userSvcClient.GetUser(grpcCtx, &userv1.GetUserRequest{
			Id: req.UserID,
		})

		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				// User tidak ditemukan — tolak request
				writeJSON(w, http.StatusBadRequest, ErrorResponse{
					Error:   "invalid_user",
					Message: "user not found: " + req.UserID,
				})
				return
			}
			// Service tidak bisa dihubungi — return 503 Service Unavailable
			h.log.Error("user-svc unavailable", zap.Error(err))
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{
				Error:   "service_unavailable",
				Message: "unable to verify user, please try again",
			})
			return
		}
	}

	// Hitung total amount dan buat domain objects
	var totalAmount float64
	items := make([]repository.OrderItem, 0, len(req.Items))
	for _, item := range req.Items {
		totalAmount += float64(item.Quantity) * item.UnitPrice
		items = append(items, repository.OrderItem{
			ProductID:   item.ProductID,
			ProductName: item.ProductName,
			Quantity:    item.Quantity,
			UnitPrice:   item.UnitPrice,
		})
	}

	newOrder := &repository.Order{
		UserID:      req.UserID,
		Items:       items,
		TotalAmount: totalAmount,
	}

	saved, err := h.repo.Save(r.Context(), newOrder)
	if err != nil {
		h.log.Error("failed to save order", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal_server_error"})
		return
	}

	writeJSON(w, http.StatusCreated, toOrderResponse(saved))
}

// GetUserOrders menangani GET /api/v1/orders/user/{userId}
func (h *OrderHandler) GetUserOrders(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")

	orders, err := h.repo.FindByUserID(r.Context(), userID)
	if err != nil {
		h.log.Error("failed to find orders by user", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal_server_error"})
		return
	}

	responses := make([]OrderResponse, 0, len(orders))
	for _, o := range orders {
		responses = append(responses, toOrderResponse(o))
	}

	writeJSON(w, http.StatusOK, responses)
}

// UpdateStatus menangani PATCH /api/v1/orders/{id}/status
func (h *OrderHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "bad_request", Message: err.Error()})
		return
	}

	validStatuses := map[string]bool{
		"pending": true, "processing": true, "completed": true, "cancelled": true,
	}
	if !validStatuses[req.Status] {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Message: "status must be one of: pending, processing, completed, cancelled",
		})
		return
	}

	updated, err := h.repo.UpdateStatus(r.Context(), id, repository.OrderStatus(req.Status))
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "not_found", Message: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, toOrderResponse(updated))
}

// ─────────────────────────────────────────────
//  HELPER FUNCTIONS
// ─────────────────────────────────────────────

func toOrderResponse(o *repository.Order) OrderResponse {
	items := make([]OrderItemResponse, 0, len(o.Items))
	for _, item := range o.Items {
		items = append(items, OrderItemResponse{
			ProductID:   item.ProductID,
			ProductName: item.ProductName,
			Quantity:    item.Quantity,
			UnitPrice:   item.UnitPrice,
			Subtotal:    float64(item.Quantity) * item.UnitPrice,
		})
	}

	return OrderResponse{
		ID:          o.ID,
		UserID:      o.UserID,
		Items:       items,
		TotalAmount: o.TotalAmount,
		Status:      string(o.Status),
		CreatedAt:   o.CreatedAt.Format(time.RFC3339),
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
