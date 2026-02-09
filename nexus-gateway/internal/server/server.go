package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"nexus-gateway/internal/usecase"
)

type Server struct {
	mux     *chi.Mux
	port    string
	handler *usecase.Handler
}

func New(port, brokerBaseURL string, stateKey []byte, httpClient *http.Client) *Server {
	mux := chi.NewRouter()

	// CORS Setup
	mux.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	mux.Use(middleware.RequestID)
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(middleware.Timeout(30 * time.Second))
	mux.Use(middleware.RealIP)

	h := usecase.NewHandler(brokerBaseURL, stateKey, httpClient)

	s := &Server{mux: mux, port: port, handler: h}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// Prometheus metrics
	s.mux.Handle("/metrics", promhttp.Handler())

	s.mux.Post("/v1/request-connection", s.handler.RequestConnection)
	s.mux.Get("/v1/check-connection/{connectionID}", s.handler.CheckConnection)
	s.mux.Get("/v1/token/{connectionID}", s.handler.GetToken)
	s.mux.Post("/v1/refresh/{connectionID}", s.handler.RefreshConnection)
	s.mux.Get("/v1/providers", s.handler.GetProviders)
	s.mux.Post("/v1/providers", s.handler.CreateProvider)
	s.mux.Get("/v1/providers/{id}", s.handler.GetProvider)
	s.mux.Put("/v1/providers/{id}", s.handler.UpdateProvider)
	s.mux.Patch("/v1/providers/{id}", s.handler.PatchProvider)
	s.mux.Delete("/v1/providers/{id}", s.handler.DeleteProvider)

	// Callback Proxy
	s.mux.Handle("/auth/callback", http.HandlerFunc(s.handler.ProxyCallback))
}

func (s *Server) Start() error {
	log.Printf("HTTP server listening on :%s", s.port)
	return http.ListenAndServe(":"+s.port, s.mux)
}
