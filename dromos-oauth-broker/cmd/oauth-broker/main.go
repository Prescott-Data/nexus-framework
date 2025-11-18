package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"dromos.com/oauth-broker/internal/caching"
	"dromos.com/oauth-broker/internal/handlers"
	"dromos.com/oauth-broker/internal/provider"
	"dromos.com/oauth-broker/internal/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-redis/redis/v8"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func main() {
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

	// Setup encryption key (32 bytes for AES-256)
	var encryptionKey []byte
	if encryptionKeyStr != "" {
		key, err := base64.StdEncoding.DecodeString(encryptionKeyStr)
		if err != nil {
			log.Fatal("Invalid ENCRYPTION_KEY format, must be base64 encoded")
		}
		if len(key) != 32 {
			log.Fatal("ENCRYPTION_KEY must be 32 bytes (256 bits)")
		}
		encryptionKey = key
	} else {
		// Generate a random key for development
		log.Println("WARNING: Using generated encryption key. Set ENCRYPTION_KEY environment variable for production.")
		encryptionKey = make([]byte, 32)
		if _, err := rand.Read(encryptionKey); err != nil {
			log.Fatal("Failed to generate encryption key:", err)
		}
	}

	// Setup state signing key
	var stateKey []byte
	if stateKeyStr != "" {
		key, err := base64.StdEncoding.DecodeString(stateKeyStr)
		if err != nil {
			log.Fatal("Invalid STATE_KEY format, must be base64 encoded")
		}
		stateKey = key
	} else {
		// Generate a random key for development
		log.Println("WARNING: Using generated state key. Set STATE_KEY environment variable for production.")
		stateKey = make([]byte, 32)
		if _, err := rand.Read(stateKey); err != nil {
			log.Fatal("Failed to generate state key:", err)
		}
	}

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
	providersHandler := handlers.NewProvidersHandler(store)
	consentHandler := handlers.NewConsentHandler(db, baseURL, stateKey, cachingClient)
	callbackHandler := handlers.NewCallbackHandler(db, encryptionKey, stateKey, cachingClient)

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
	protected.Post("/providers", providersHandler.Register)
	protected.Get("/providers", providersHandler.List)
	protected.Route("/providers", func(r chi.Router) {
		r.Get("/by-name/{name}", providersHandler.GetByName)
		r.Get("/{id}", providersHandler.Get)
		r.Put("/{id}", providersHandler.Update)
		r.Delete("/{id}", providersHandler.Delete)
	})
	protected.Post("/auth/consent-spec", consentHandler.GetSpec)
	protected.Get("/connections/{connectionID}/token", callbackHandler.GetToken)
	protected.Post("/connections/{connectionID}/refresh", callbackHandler.Refresh)

	// Health check
	router.Get("/health", server.HealthHandler)

	log.Printf("Starting OAuth Broker server on port %s", port)
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
