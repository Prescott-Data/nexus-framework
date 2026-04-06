package main

import (
	"context"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

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

	// Get configuration from environment
	port := getEnv("PORT", "8080")
	databaseURL := getEnv("DATABASE_URL", "postgres://user:password@localhost/oauth_broker?sslmode=disable")
	databaseURL = enforceDBSSL(databaseURL)
	baseURL := getEnv("BASE_URL", "http://localhost:8080")
	encryptionKeyStr := getEnv("ENCRYPTION_KEY", "")
	stateKeyStr := getEnv("STATE_KEY", "")
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379/0")

	// Validate required environment variables
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}
	if baseURL == "" {
		log.Fatal("BASE_URL environment variable is required")
	}

	// Validate required cryptographic keys — broker must not start without them.
	// An ephemeral key would silently encrypt tokens that become unreadable on restart.
	encryptionKey, err := config.ValidateKey("ENCRYPTION_KEY", encryptionKeyStr)
	if err != nil {
		log.Fatalf("Fatal configuration error: %v", err)
	}
	stateKey, err := config.ValidateKey("STATE_KEY", stateKeyStr)
	if err != nil {
		log.Fatalf("Fatal configuration error: %v", err)
	}

	log.Printf("ENCRYPTION_KEY fingerprint: %s", config.KeyFingerprint(encryptionKey))
	log.Printf("STATE_KEY fingerprint: %s", config.KeyFingerprint(stateKey))

	// Connect to database
	db, err := sqlx.Connect("postgres", databaseURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	log.Println("Successfully connected to database")

	// Connect to Redis
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatal("Failed to parse REDIS_URL:", err)
	}

	redisClient := redis.NewClient(opts)
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to ping Redis:", err)
	}
	log.Println("Successfully connected to Redis")

	// Create caching client
	cachingClient := caching.NewCachingClient(redisClient, 1*time.Hour)

	// Create HTTP server
	srv := server.NewServer(port)

	// Create the REAL store
	store := provider.NewStore(db)

	// Setup handlers
	redirectPath := os.Getenv("REDIRECT_PATH")
	if redirectPath == "" {
		redirectPath = "/auth/callback"
	}
	providersHandler := handlers.NewProvidersHandler(store)
	consentHandler := handlers.NewConsentHandler(db, baseURL, redirectPath, stateKey, cachingClient)
	callbackHandler := handlers.NewCallbackHandler(db, baseURL, redirectPath, encryptionKey, stateKey, cachingClient)


	// Setup routes
	router := srv.Router()
	// Public endpoints
	router.Get("/auth/callback", callbackHandler.Handle)
	router.Method("GET", "/metrics", server.MetricsHandler())
	// This is a new public, state-protected endpoint for the frontend
	router.Get("/auth/capture-schema", callbackHandler.GetCaptureSchema)
	router.Post("/auth/capture-credential", callbackHandler.SaveCredential)
	// Protected endpoints: API key + allowlist
	protected := router.With(server.ApiKeyMiddleware(), server.AllowlistMiddleware())
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

	// Health check
	router.Get("/health", server.HealthHandler)

	// Start background orphan token cleanup (safety net for deleted connections)
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	go handlers.StartOrphanTokenCleanup(cleanupCtx, db, 1*time.Hour)

	log.Printf("Starting OAuth Broker server on port %s", port)
	log.Printf("Version: %s", Version)
	log.Printf("Base URL: %s", baseURL)

	if err := srv.Start(); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

// getEnv gets an environment variable with a fallback value
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// enforceDBSSL optionally enforces sslmode on the Postgres DSN based on env flags.
// ENFORCE_DB_SSL=true enables enforcement. DB_SSLMODE (default "require") sets the mode.
// DB_SSLROOTCERT can be provided to append sslrootcert=... for verify-ca/verify-full.
func enforceDBSSL(dsn string) string {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("ENFORCE_DB_SSL")), "true") {
		return dsn
	}
	mode := strings.TrimSpace(os.Getenv("DB_SSLMODE"))
	if mode == "" {
		mode = "require"
	}
	root := strings.TrimSpace(os.Getenv("DB_SSLROOTCERT"))

	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" || !strings.HasPrefix(dsn, "postgres") {
		// Fallback: simple string append if DSN isn't a URL
		if !strings.Contains(dsn, "sslmode=") {
			if strings.Contains(dsn, "?") {
				dsn += "&sslmode=" + mode
			} else {
				dsn += "?sslmode=" + mode
			}
		}
		if root != "" && !strings.Contains(dsn, "sslrootcert=") {
			if strings.Contains(dsn, "?") {
				dsn += "&sslrootcert=" + url.QueryEscape(root)
			} else {
				dsn += "?sslrootcert=" + url.QueryEscape(root)
			}
		}
		return dsn
	}
	q := u.Query()
	q.Set("sslmode", mode)
	if root != "" && q.Get("sslrootcert") == "" {
		q.Set("sslrootcert", root)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
