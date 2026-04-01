package server

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server wraps the HTTP server
type Server struct {
	router *chi.Mux
	port   string
}

// NewServer creates a new HTTP server
func NewServer(port string) *Server {
	s := &Server{
		router: chi.NewRouter(),
		port:   port,
	}

	s.setupMiddleware()
	return s
}

// setupMiddleware configures middleware for the router
func (s *Server) setupMiddleware() {
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(30 * time.Second))
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.RequestID)
}

// Router returns the chi router for adding routes
func (s *Server) Router() *chi.Mux {
	return s.router
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("Starting OAuth Broker server on port %s", s.port)
	return http.ListenAndServe(":"+s.port, s.router)
}

// HealthHandler for health checks
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "healthy"}`))
}

// MetricsHandler exposes Prometheus metrics
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
