// services/order-svc/internal/repository/order_repository.go
//
// Package repository untuk order-svc mengikuti pola yang sama persis
// dengan user-svc. Ini adalah kekuatan dari monorepo + consistent patterns:
// seorang developer yang sudah paham user-svc langsung bisa membaca kode ini.
//
// Domain model Order menyimpan user_id sebagai referensi ke user-svc.
// Order-svc TIDAK menyimpan data user secara lokal (anti-pattern: data duplication).
// Sebaliknya, saat order-svc perlu info user, dia memanggil user-svc via gRPC.

package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// OrderStatus merepresentasikan status siklus hidup sebuah order.
// Menggunakan custom type string (bukan plain string) agar type-safe:
// kamu tidak bisa secara tidak sengaja assign "invalid_status" ke field OrderStatus.
type OrderStatus string

const (
	OrderStatusPending    OrderStatus = "pending"    // baru dibuat, belum diproses
	OrderStatusProcessing OrderStatus = "processing" // sedang diproses
	OrderStatusCompleted  OrderStatus = "completed"  // selesai
	OrderStatusCancelled  OrderStatus = "cancelled"  // dibatalkan
)

// Order adalah domain model untuk order.
type Order struct {
	ID          string
	UserID      string      // Referensi ke User di user-svc (bukan embed data user)
	Items       []OrderItem // Nested struct untuk item-item dalam order
	TotalAmount float64
	Status      OrderStatus
	CreatedAt   time.Time
}

// OrderItem merepresentasikan satu item dalam order.
type OrderItem struct {
	ProductID   string
	ProductName string
	Quantity    int
	UnitPrice   float64
}

// OrderRepository mendefinisikan kontrak untuk operasi pada Order.
type OrderRepository interface {
	FindByID(ctx context.Context, id string) (*Order, error)
	FindByUserID(ctx context.Context, userID string) ([]*Order, error)
	FindAll(ctx context.Context) ([]*Order, error)
	Save(ctx context.Context, order *Order) (*Order, error)
	UpdateStatus(ctx context.Context, id string, status OrderStatus) (*Order, error)
}

// inMemoryOrderRepo mengimplementasikan OrderRepository dengan in-memory map.
// Sama seperti user-svc: thread-safe menggunakan sync.RWMutex.
type inMemoryOrderRepo struct {
	mu    sync.RWMutex
	store map[string]*Order
}

// NewInMemoryOrderRepository membuat repository dengan seed data yang mengacu
// ke user IDs yang ada di user-svc (user-001, user-002).
func NewInMemoryOrderRepository() OrderRepository {
	repo := &inMemoryOrderRepo{
		store: make(map[string]*Order),
	}

	// Seed data — perhatikan UserID-nya cocok dengan seed data di user-svc
	seeds := []Order{
		{
			ID:     "order-001",
			UserID: "user-001", // Alice
			Items: []OrderItem{
				{ProductID: "prod-001", ProductName: "Laptop Pro X", Quantity: 1, UnitPrice: 15000000},
				{ProductID: "prod-002", ProductName: "Wireless Mouse", Quantity: 2, UnitPrice: 250000},
			},
			TotalAmount: 15500000,
			Status:      OrderStatusCompleted,
			CreatedAt:   time.Now().Add(-48 * time.Hour),
		},
		{
			ID:     "order-002",
			UserID: "user-002", // Bob
			Items: []OrderItem{
				{ProductID: "prod-003", ProductName: "Mechanical Keyboard", Quantity: 1, UnitPrice: 1200000},
			},
			TotalAmount: 1200000,
			Status:      OrderStatusPending,
			CreatedAt:   time.Now().Add(-2 * time.Hour),
		},
	}

	for i := range seeds {
		o := seeds[i]
		repo.store[o.ID] = &o
	}

	return repo
}

// FindByID mencari order berdasarkan ID.
func (r *inMemoryOrderRepo) FindByID(ctx context.Context, id string) (*Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	order, ok := r.store[id]
	if !ok {
		return nil, fmt.Errorf("order %q not found", id)
	}

	// Deep copy untuk menghindari race condition
	orderCopy := copyOrder(order)
	return orderCopy, nil
}

// FindByUserID mencari semua order yang dimiliki oleh user tertentu.
// Ini adalah query yang umum: "tampilkan semua order saya".
func (r *inMemoryOrderRepo) FindByUserID(ctx context.Context, userID string) ([]*Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var orders []*Order
	for _, o := range r.store {
		if o.UserID == userID {
			orders = append(orders, copyOrder(o))
		}
	}

	return orders, nil
}

// FindAll mengembalikan semua orders.
func (r *inMemoryOrderRepo) FindAll(ctx context.Context) ([]*Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	orders := make([]*Order, 0, len(r.store))
	for _, o := range r.store {
		orders = append(orders, copyOrder(o))
	}

	return orders, nil
}

// Save menyimpan order baru.
func (r *inMemoryOrderRepo) Save(ctx context.Context, order *Order) (*Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	if order.ID == "" {
		order.ID = uuid.New().String()
	}
	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now()
	}
	if order.Status == "" {
		order.Status = OrderStatusPending
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	orderCopy := copyOrder(order)
	r.store[order.ID] = orderCopy

	return copyOrder(orderCopy), nil
}

// UpdateStatus memperbarui status sebuah order.
// Ini adalah contoh "partial update" — hanya mengubah satu field.
func (r *inMemoryOrderRepo) UpdateStatus(ctx context.Context, id string, newStatus OrderStatus) (*Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	order, ok := r.store[id]
	if !ok {
		return nil, fmt.Errorf("order %q not found", id)
	}

	order.Status = newStatus
	return copyOrder(order), nil
}

// copyOrder membuat deep copy dari Order untuk menghindari race condition.
// Kita perlu copy []OrderItem secara eksplisit karena slice adalah reference type di Go.
func copyOrder(o *Order) *Order {
	if o == nil {
		return nil
	}

	items := make([]OrderItem, len(o.Items))
	copy(items, o.Items)

	return &Order{
		ID:          o.ID,
		UserID:      o.UserID,
		Items:       items,
		TotalAmount: o.TotalAmount,
		Status:      o.Status,
		CreatedAt:   o.CreatedAt,
	}
}
