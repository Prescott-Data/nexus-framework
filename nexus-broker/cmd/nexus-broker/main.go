package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository/instrumented"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository/postgres"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/service"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/caching"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/config"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/handlers"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/server"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/go-redis/redis/v8"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

var Version = "dev"

func main() {
	isWorkerOnly := false
	if len(os.Args) > 1 {
		for _, arg := range os.Args[1:] {
			if arg == "-v" || arg == "--version" {
				log.Printf("Nexus Broker version: %s", Version)
				os.Exit(0)
			}
			if arg == "--worker-only" {
				isWorkerOnly = true
			}
		}
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

	connRepo := instrumented.NewConnectionRepository(postgres.NewConnectionRepository(db))
	tokenRepo := instrumented.NewTokenRepository(postgres.NewTokenRepository(db))

	connSvc := service.NewConnectionService(
		connRepo,
		tokenRepo,
		store,
		auditSvc,
		cfg.BaseURL,
		cfg.RedirectPath,
		cfg.EncryptionKey,
		cfg.StateKey,
		cachingClient,
		cfg.EnforceReturnURL,
		cfg.AllowedReturnDomains,
	)

	consentHandler := handlers.NewConsentHandler(handlers.ConsentHandlerConfig{
		Service: connSvc,
	})
	callbackHandler := handlers.NewCallbackHandler(handlers.CallbackHandlerConfig{
		Service: connSvc,
		Audit:   auditSvc,
	})
	auditHandler := handlers.NewAuditHandler(db)
	connectionsHandler := handlers.NewConnectionsHandler(connSvc)
	samlHandler := handlers.NewSAMLHandler(connSvc)
	apiKeySource, err := server.NewReloadingAPIKeySource(cfg.APIKeys, cfg.APIKeyFiles, cfg.APIKeyReloadInterval)
	if err != nil {
		log.Fatalf("Fatal API key configuration error: %v", err)
	}

	router := srv.Router()
	router.Get("/auth/callback", callbackHandler.Handle)
	router.Method("GET", "/metrics", server.MetricsHandler())
	router.Get("/auth/capture-schema", callbackHandler.GetCaptureSchema)
	router.Post("/auth/capture-credential", callbackHandler.SaveCredential)
	router.Post("/saml/acs", samlHandler.ACS)

	protected := router.With(
		server.ApiKeySourceMiddleware(cfg.RequireAPIKey, apiKeySource),
		server.AllowlistMiddleware(cfg.RequireAllowlist, cfg.AllowedCIDRs),
	)
	protected.Get("/audit", auditHandler.List)
	protected.Route("/providers", func(r chi.Router) {
		r.Post("/", providersHandler.Register)
		r.Get("/", providersHandler.List)
		r.Get("/health", providersHandler.Health)
		r.Get("/metadata", providersHandler.Metadata)
		r.Get("/by-name/{name}", providersHandler.GetByName)
		r.Delete("/by-name/{name}", providersHandler.DeleteByName)
		r.Get("/{id}", providersHandler.Get)
		r.Put("/{id}", providersHandler.Update)
		r.Patch("/{id}", providersHandler.Patch)
		r.Delete("/{id}", providersHandler.Delete)
	})
	protected.Post("/auth/consent-spec", consentHandler.GetSpec)
	protected.Get("/connections", connectionsHandler.List)
	protected.Get("/connections/resolve", callbackHandler.ResolveToken)
	protected.Get("/connections/{connectionID}/token", callbackHandler.GetToken)
	protected.Post("/connections/{connectionID}/refresh", callbackHandler.Refresh)
	protected.Get("/saml/metadata/{providerID}", samlHandler.Metadata)

	router.Get("/health", server.HealthHandler)

	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	go handlers.StartOrphanTokenCleanup(cleanupCtx, db, 1*time.Hour)

	// Start provider health worker (polls every 5m)
	healthWorker := provider.NewHealthWorker(store, 5*time.Minute)
	go healthWorker.Start(cleanupCtx)

	// Start connection health worker (polls every 1m)
	// The store implements ProviderHealthLookup via GetHealthStatus(uuid.UUID)
	connHealthWorker := service.NewConnectionHealthWorker(connRepo, connSvc, store, 1*time.Minute)
	go connHealthWorker.Start(cleanupCtx)

	// Start connection health gauge (polls every 30s)
	telemetry.NewConnectionGaugeCollector(connRepo, 30*time.Second)

	if isWorkerOnly {
		log.Printf("Starting Nexus Broker in WORKER-ONLY mode")
		log.Printf("Version: %s", Version)

		// Wait for OS signal for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down workers...", sig)
		cleanupCancel()
	} else {
		log.Printf("Starting OAuth Broker server on port %s", cfg.Port)
		log.Printf("Version: %s", Version)
		log.Printf("Base URL: %s", cfg.BaseURL)

		if err := srv.Start(); err != nil {
			log.Fatal("Server failed to start:", err)
		}
	}
}
