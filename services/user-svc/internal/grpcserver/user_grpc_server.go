// services/user-svc/internal/grpcserver/user_grpc_server.go
//
// Package grpcserver mengimplementasikan gRPC SERVER untuk User service.
//
// INI ADALAH LAYER YANG PALING MENARIK dari tutorial ini.
// Di sinilah kita mengimplementasikan interface UserServiceServer yang di-generate
// oleh buf/protoc dari file .proto kita.
//
// PERBEDAAN HTTP HANDLER vs gRPC SERVER:
//
//   HTTP Handler:
//     - Input: http.Request (parse manual dari JSON body/URL)
//     - Output: http.ResponseWriter (write manual JSON string)
//     - Diakses oleh: browser, Postman, mobile app, service lain
//
//   gRPC Server:
//     - Input: Protobuf struct (sudah di-deserialize otomatis oleh gRPC framework)
//     - Output: Protobuf struct (akan di-serialize otomatis oleh gRPC framework)
//     - Diakses oleh: service internal lain yang punya stub client
//
// Jadi gRPC server jauh lebih bersih — tidak ada boilerplate parsing/encoding!
// gRPC framework mengurus semua itu berdasarkan .proto schema.

package grpcserver

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/semmidev/gomonorepo/gen/go/user/v1"
	"github.com/semmidev/gomonorepo/services/user-svc/internal/repository"
)

// UserGRPCServer mengimplementasikan interface userv1.UserServiceServer.
// Interface ini di-generate dari .proto file kita oleh protoc-gen-go-grpc.
// Compiler Go akan error jika kita tidak mengimplementasikan semua method.
type UserGRPCServer struct {
	// Embed UnimplementedUserServiceServer untuk forward compatibility.
	// Jika .proto kita tambah method baru, server ini tidak langsung compile error —
	// method baru akan return "Unimplemented" sampai kita implement sendiri.
	userv1.UnimplementedUserServiceServer

	repo repository.UserRepository
	log  *zap.Logger
}

// NewUserGRPCServer membuat gRPC server baru dengan dependency injection.
func NewUserGRPCServer(repo repository.UserRepository, log *zap.Logger) *UserGRPCServer {
	return &UserGRPCServer{
		repo: repo,
		log:  log,
	}
}

// ─────────────────────────────────────────────
//  IMPLEMENTASI gRPC METHODS
// ─────────────────────────────────────────────
// Setiap method di sini berkorespondensi dengan satu RPC di .proto file.
// Signature-nya persis seperti yang di-generate oleh protoc-gen-go-grpc.

// GetUser mengimplementasikan: rpc GetUser(GetUserRequest) returns (GetUserResponse)
//
// Perhatikan betapa bersihnya kode ini dibanding HTTP handler:
//   - Tidak ada json.NewDecoder
//   - Tidak ada chi.URLParam
//   - Tidak ada w.WriteHeader
// Semua itu diurus oleh gRPC framework berdasarkan .proto schema.
func (s *UserGRPCServer) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.GetUserResponse, error) {
	s.log.Info("gRPC GetUser called", zap.String("id", req.GetId()))

	if req.GetId() == "" {
		// Di gRPC, kita return error dengan "status code" gRPC, bukan HTTP status code.
		// codes.InvalidArgument ≈ HTTP 400
		// codes.NotFound        ≈ HTTP 404
		// codes.Internal        ≈ HTTP 500
		// codes.Unauthenticated ≈ HTTP 401
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	user, err := s.repo.FindByID(ctx, req.GetId())
	if err != nil {
		s.log.Warn("user not found via gRPC", zap.String("id", req.GetId()), zap.Error(err))
		return nil, status.Error(codes.NotFound, fmt.Sprintf("user %q not found", req.GetId()))
	}

	// Konversi dari domain model ke protobuf message
	return &userv1.GetUserResponse{
		User: toProtoUser(user),
	}, nil
}

// ListUsers mengimplementasikan: rpc ListUsers(ListUsersRequest) returns (ListUsersResponse)
func (s *UserGRPCServer) ListUsers(ctx context.Context, req *userv1.ListUsersRequest) (*userv1.ListUsersResponse, error) {
	s.log.Info("gRPC ListUsers called")

	users, err := s.repo.FindAll(ctx)
	if err != nil {
		s.log.Error("failed to list users via gRPC", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to retrieve users")
	}

	protoUsers := make([]*userv1.User, 0, len(users))
	for _, u := range users {
		protoUsers = append(protoUsers, toProtoUser(u))
	}

	return &userv1.ListUsersResponse{
		Users: protoUsers,
	}, nil
}

// CreateUser mengimplementasikan: rpc CreateUser(CreateUserRequest) returns (CreateUserResponse)
func (s *UserGRPCServer) CreateUser(ctx context.Context, req *userv1.CreateUserRequest) (*userv1.CreateUserResponse, error) {
	s.log.Info("gRPC CreateUser called", zap.String("name", req.GetName()))

	if req.GetName() == "" || req.GetEmail() == "" {
		return nil, status.Error(codes.InvalidArgument, "name and email are required")
	}

	role := req.GetRole()
	if role == "" {
		role = "customer"
	}

	newUser := &repository.User{
		Name:  req.GetName(),
		Email: req.GetEmail(),
		Role:  role,
	}

	saved, err := s.repo.Save(ctx, newUser)
	if err != nil {
		s.log.Error("failed to save user via gRPC", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to create user")
	}

	return &userv1.CreateUserResponse{
		User: toProtoUser(saved),
	}, nil
}

// ─────────────────────────────────────────────
//  HELPER: Domain Model → Protobuf Message
// ─────────────────────────────────────────────

// toProtoUser mengkonversi repository.User (domain model) ke userv1.User (protobuf message).
// Ini disebut "mapper" atau "converter" function.
//
// Kenapa perlu konversi manual? Karena:
//   1. Field names mungkin berbeda (snake_case vs CamelCase)
//   2. Tipe data mungkin berbeda (time.Time vs int64 Unix timestamp)
//   3. Mungkin ada field yang tidak ingin kita expose via gRPC
//
// Di project besar, pattern ini sering diimplementasikan dengan library seperti
// google/go-cmp atau bahkan code generation (mapstruct di Java, tapi di Go biasanya manual).
func toProtoUser(u *repository.User) *userv1.User {
	return &userv1.User{
		Id:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		Role:      u.Role,
		CreatedAt: u.CreatedAt.Unix(), // Konversi time.Time ke Unix timestamp (int64)
	}
}
