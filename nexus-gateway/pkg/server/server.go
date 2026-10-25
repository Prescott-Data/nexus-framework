package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/config"
	gwmiddleware "github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/middleware"
	"github.com/Prescott-Data/nexus-framework/nexus-gateway/pkg/usecase"
)

type Server struct {
	mux     *chi.Mux
	port    string
	handler *usecase.Handler

	requireAPIKey bool
	apiKeys       gwmiddleware.APIKeySource
}

// Option configures a Server.
type Option func(*Server)

// WithAPIKey requires X-API-Key on every route except the browser-facing
// ones (see PublicPaths). A nil source with require=true rejects everything.
func WithAPIKey(require bool, source gwmiddleware.APIKeySource) Option {
	return func(s *Server) {
		s.requireAPIKey = require
		s.apiKeys = source
	}
}

// PublicPaths are reachable without an API key: the health check for load
// balancers and uptime probes, the OAuth callback that providers redirect
// the user's browser to, and the static-credential capture pages that the
// consent UI calls. Everything else needs the key.
var PublicPaths = []string{"/health", "/auth/callback", "/v1/capture-schema", "/v1/capture-credential"}

func New(port, brokerBaseURL string, stateKey []byte, httpClient *http.Client, redisClient *redis.Client, opts ...Option) *Server {
	mux := chi.NewRouter()

	// CORS Setup
	mux.Use(cors.Handler(cors.Options{
		AllowedOrigins:   config.GetAllowedOrigins(),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-API-Key"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	mux.Use(middleware.RequestID)
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(middleware.Timeout(30 * time.Second))
	mux.Use(middleware.RealIP)

	// Add Rate Limiter
	mux.Use(gwmiddleware.RateLimiter(redisClient))

	h := usecase.NewHandler(brokerBaseURL, stateKey, httpClient)

	s := &Server{mux: mux, port: port, handler: h}
	for _, o := range opts {
		o(s)
	}
	mux.Use(gwmiddleware.APIKey(s.requireAPIKey, s.apiKeys, PublicPaths...))
	mux.Use(gwmiddleware.Subject)
	s.routes()
	return s
}

// Handler exposes the router for tests.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// Prometheus metrics
	s.mux.Handle("/metrics", promhttp.Handler())

	s.mux.Post("/v1/request-connection", s.handler.RequestConnection)
	s.mux.Get("/v1/check-connection/{connectionID}", s.handler.CheckConnection)
	s.mux.Get("/v1/resolve", s.handler.ResolveToken)
	s.mux.Get("/v1/token/{connectionID}", s.handler.GetToken)
	s.mux.Post("/v1/refresh/{connectionID}", s.handler.RefreshConnection)
	s.mux.Delete("/v1/connections/{connectionID}", s.handler.RevokeConnection)
	s.mux.Get("/v1/providers", s.handler.GetProviders)
	s.mux.Get("/v1/providers/metadata", s.handler.GetProviders)
	s.mux.Post("/v1/providers", s.handler.CreateProvider)
	s.mux.Get("/v1/providers/{id}", s.handler.GetProvider)
	s.mux.Put("/v1/providers/{id}", s.handler.UpdateProvider)
	s.mux.Patch("/v1/providers/{id}", s.handler.PatchProvider)
	s.mux.Delete("/v1/providers/{id}", s.handler.DeleteProvider)

	// Callback Proxy
	s.mux.Handle("/auth/callback", http.HandlerFunc(s.handler.ProxyCallback))

	// API key / static credential capture (proxied from broker)
	s.mux.Get("/v1/capture-schema", s.handler.CaptureSchema)
	s.mux.Post("/v1/capture-credential", s.handler.CaptureCredential)
}

func (s *Server) Start() error {
	log.Printf("HTTP server listening on :%s", s.port)
	return http.ListenAndServe(":"+s.port, s.mux)
}
