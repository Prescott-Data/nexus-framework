package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/caching"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/config"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/handlers"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-redis/redis/v8"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

var Version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		log.Printf("Nexus Broker version: %s", Version)
		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Fatal configuration error: %v", err)
	}

	log.Printf("ENCRYPTION_KEY fingerprint: %s", config.KeyFingerprint(cfg.EncryptionKey))
	log.Printf("STATE_KEY fingerprint: %s", config.KeyFingerprint(cfg.StateKey))

	db, err := sqlx.Connect("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}
	log.Println("Successfully connected to database")

	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatal("Failed to parse REDIS_URL:", err)
	}
	redisClient := redis.NewClient(opts)
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to ping Redis:", err)
	}
	log.Println("Successfully connected to Redis")

	cachingClient := caching.NewCachingClient(redisClient, 1*time.Hour)

	srv := server.NewServer(cfg.Port)
	store := provider.NewStore(db)
	auditSvc := audit.NewService(db)

	providersHandler := handlers.NewProvidersHandler(store, auditSvc)
	consentHandler := handlers.NewConsentHandler(handlers.ConsentHandlerConfig{
		DB:                   db,
		BaseURL:              cfg.BaseURL,
		RedirectPath:         cfg.RedirectPath,
		StateKey:             cfg.StateKey,
		HTTPClient:           cachingClient,
		EnforceReturnURL:     cfg.EnforceReturnURL,
		AllowedReturnDomains: cfg.AllowedReturnDomains,
	})
	callbackHandler := handlers.NewCallbackHandler(handlers.CallbackHandlerConfig{
		DB:                   db,
		Audit:                auditSvc,
		BaseURL:              cfg.BaseURL,
		RedirectPath:         cfg.RedirectPath,
		EncryptionKey:        cfg.EncryptionKey,
		StateKey:             cfg.StateKey,
		HTTPClient:           cachingClient,
		EnforceReturnURL:     cfg.EnforceReturnURL,
		AllowedReturnDomains: cfg.AllowedReturnDomains,
	})
	auditHandler := handlers.NewAuditHandler(db)

	router := srv.Router()
	router.Get("/auth/callback", callbackHandler.Handle)
	router.Method("GET", "/metrics", server.MetricsHandler())
	router.Get("/auth/capture-schema", callbackHandler.GetCaptureSchema)
	router.Post("/auth/capture-credential", callbackHandler.SaveCredential)

	protected := router.With(
		server.ApiKeyMiddleware(cfg.RequireAPIKey, cfg.APIKeys),
		server.AllowlistMiddleware(cfg.RequireAllowlist, cfg.AllowedCIDRs),
	)
	protected.Get("/audit", auditHandler.List)
	protected.Route("/providers", func(r chi.Router) {
		r.Post("/", providersHandler.Register)
		r.Get("/", providersHandler.List)
		r.Get("/metadata", providersHandler.Metadata)
		r.Get("/by-name/{name}", providersHandler.GetByName)
		r.Delete("/by-name/{name}", providersHandler.DeleteByName)
		r.Get("/{id}", providersHandler.Get)
		r.Put("/{id}", providersHandler.Update)
		r.Patch("/{id}", providersHandler.Patch)
		r.Delete("/{id}", providersHandler.Delete)
	})
	protected.Post("/auth/consent-spec", consentHandler.GetSpec)
	protected.Get("/connections/{connectionID}/token", callbackHandler.GetToken)
	protected.Post("/connections/{connectionID}/refresh", callbackHandler.Refresh)

	router.Get("/health", server.HealthHandler)

	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	go handlers.StartOrphanTokenCleanup(cleanupCtx, db, 1*time.Hour)

	log.Printf("Starting OAuth Broker server on port %s", cfg.Port)
	log.Printf("Version: %s", Version)
	log.Printf("Base URL: %s", cfg.BaseURL)

	if err := srv.Start(); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
