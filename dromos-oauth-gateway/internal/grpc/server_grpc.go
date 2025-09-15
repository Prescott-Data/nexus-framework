package grpcsrv

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	ohapb "dromos-oauth-gateway/gen/go/api/proto/oha/v1"
	"dromos-oauth-gateway/internal/usecase"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type Service struct {
	usecaseHandler *usecase.Handler
	ohapb.UnimplementedOHAServiceServer
}

func NewService(handler *usecase.Handler) *Service {
	return &Service{usecaseHandler: handler}
}

// RequestConnection implements OHAServiceServer.RequestConnection.
func (s *Service) RequestConnection(ctx context.Context, req *ohapb.RequestConnectionRequest) (*ohapb.RequestConnectionResponse, error) {
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
	return &ohapb.RequestConnectionResponse{
		AuthUrl:      out.AuthURL,
		State:        out.State,
		Scopes:       out.Scopes,
		ProviderId:   out.ProviderID,
		ConnectionId: out.ConnectionID,
	}, nil
}

// CheckConnection implements OHAServiceServer.CheckConnection.

func (s *Service) CheckConnection(ctx context.Context, req *ohapb.CheckConnectionRequest) (*ohapb.CheckConnectionResponse, error) {
	if req == nil || req.GetConnectionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing connection_id")
	}
	statusStr, err := s.usecaseHandler.CheckConnectionCore(ctx, req.GetConnectionId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check connection failed: %v", err)
	}
	return &ohapb.CheckConnectionResponse{Status: statusStr}, nil
}

// GetToken implements OHAServiceServer.GetToken.
func (s *Service) GetToken(ctx context.Context, req *ohapb.GetTokenRequest) (*ohapb.GetTokenResponse, error) {
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
	return &ohapb.GetTokenResponse{Token: st}, nil
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
	ohapb.RegisterOHAServiceServer(grpcSrv, service)
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
	if err := ohapb.RegisterOHAServiceHandlerFromEndpoint(ctx, gwMux, s.grpcAddress, dialOpts); err != nil {
		return fmt.Errorf("register gateway: %w", err)
	}

	httpSrv := &http.Server{
		Addr:              s.httpAddress,
		Handler:           gwMux,
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
