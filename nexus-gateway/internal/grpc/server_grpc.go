package grpcsrv

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	nexuspb "nexus-gateway/gen/go/api/proto/nexus/v1"
	"nexus-gateway/internal/usecase"

	"github.com/go-chi/cors"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type Service struct {
	usecaseHandler *usecase.Handler
	nexuspb.UnimplementedNexusServiceServer
}

func NewService(handler *usecase.Handler) *Service {
	return &Service{usecaseHandler: handler}
}

// RequestConnection implements NexusServiceServer.RequestConnection.
func (s *Service) RequestConnection(ctx context.Context, req *nexuspb.RequestConnectionRequest) (*nexuspb.RequestConnectionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	out, err := s.usecaseHandler.RequestConnectionCore(ctx, usecase.RequestConnectionInput{
		UserID:       req.GetUserId(),
		ProviderID:   req.GetProviderId(),
		ProviderName: req.GetProviderName(),
		Scopes:       req.GetScopes(),
		ReturnURL:    req.GetReturnUrl(),
		Action:       req.GetAction(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "request connection failed: %v", err)
	}
	return &nexuspb.RequestConnectionResponse{
		AuthUrl:      out.AuthURL,
		State:        out.State,
		Scopes:       out.Scopes,
		ProviderId:   out.ProviderID,
		ConnectionId: out.ConnectionID,
	}, nil
}

// CheckConnection implements NexusServiceServer.CheckConnection.

func (s *Service) CheckConnection(ctx context.Context, req *nexuspb.CheckConnectionRequest) (*nexuspb.CheckConnectionResponse, error) {
	if req == nil || req.GetConnectionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing connection_id")
	}
	statusStr, err := s.usecaseHandler.CheckConnectionCore(ctx, req.GetConnectionId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check connection failed: %v", err)
	}
	return &nexuspb.CheckConnectionResponse{Status: statusStr}, nil
}

// GetToken implements NexusServiceServer.GetToken.
func (s *Service) GetToken(ctx context.Context, req *nexuspb.GetTokenRequest) (*nexuspb.GetTokenResponse, error) {
	if req == nil || req.GetConnectionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing connection_id")
	}
	data, code, err := s.usecaseHandler.GetTokenCore(ctx, req.GetConnectionId())
	if err != nil {
		_ = code // keep the HTTP status for potential mapping if needed later
		return nil, status.Errorf(codes.Internal, "get token failed: %v", err)
	}
	st, err := structpb.NewStruct(data)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode token failed: %v", err)
	}
	return &nexuspb.GetTokenResponse{Token: st}, nil
}

// RefreshConnection implements NexusServiceServer.RefreshConnection.
func (s *Service) RefreshConnection(ctx context.Context, req *nexuspb.RefreshConnectionRequest) (*nexuspb.RefreshConnectionResponse, error) {
	if req == nil || req.GetConnectionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing connection_id")
	}
	data, code, err := s.usecaseHandler.RefreshConnectionCore(ctx, req.GetConnectionId())
	if err != nil {
		_ = code // unused
		return nil, status.Errorf(codes.Internal, "refresh connection failed: %v", err)
	}
	st, err := structpb.NewStruct(data)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode token failed: %v", err)
	}
	return &nexuspb.RefreshConnectionResponse{Token: st}, nil
}

type Server struct {
	grpcAddress string
	httpAddress string
	grpcServer  *grpc.Server
	httpServer  *http.Server
	listener    net.Listener
	service     *Service
}

type Options struct {
	GRPCAddress string
	HTTPAddress string
	Handler     *usecase.Handler
}

func NewServer(opts Options) (*Server, error) {
	if opts.Handler == nil {
		return nil, errors.New("handler is required")
	}
	if opts.GRPCAddress == "" {
		opts.GRPCAddress = ":9090"
	}
	if opts.HTTPAddress == "" {
		opts.HTTPAddress = ":8090"
	}
	service := NewService(opts.Handler)
	grpcSrv := grpc.NewServer()
	nexuspb.RegisterNexusServiceServer(grpcSrv, service)
	return &Server{
		grpcAddress: opts.GRPCAddress,
		httpAddress: opts.HTTPAddress,
		grpcServer:  grpcSrv,
		service:     service,
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	l, err := net.Listen("tcp", s.grpcAddress)
	if err != nil {
		return fmt.Errorf("listen gRPC: %w", err)
	}
	s.listener = l

	go func() {
		log.Printf("gRPC listening on %s", s.grpcAddress)
		if err := s.grpcServer.Serve(l); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("gRPC serve error: %v", err)
		}
	}()

	gwMux := runtime.NewServeMux()
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := nexuspb.RegisterNexusServiceHandlerFromEndpoint(ctx, gwMux, s.grpcAddress, dialOpts); err != nil {
		return fmt.Errorf("register gateway: %w", err)
	}

	// CORS Setup
	corsMiddleware := cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "Grpc-Metadata-X-Request-ID"},
		ExposedHeaders:   []string{"Link", "Grpc-Metadata-X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	})

	httpSrv := &http.Server{
		Addr:              s.httpAddress,
		Handler:           corsMiddleware(gwMux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	s.httpServer = httpSrv

	go func() {
		log.Printf("HTTP gateway listening on %s", s.httpAddress)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("gateway serve error: %v", err)
		}
	}()

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.grpcServer.GracefulStop()
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}
