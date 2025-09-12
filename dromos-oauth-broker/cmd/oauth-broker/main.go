package main

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"os"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"dromos-oauth-broker/internal/handlers"
	"dromos-oauth-broker/internal/server"
)

func main() {
	// Get configuration from environment
	port := getEnv("PORT", "8080")
	databaseURL := getEnv("DATABASE_URL", "postgres://user:password@localhost/oauth_broker?sslmode=disable")
	baseURL := getEnv("BASE_URL", "http://localhost:8080")
	encryptionKeyStr := getEnv("ENCRYPTION_KEY", "")
	stateKeyStr := getEnv("STATE_KEY", "")

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

	// Create HTTP server
	srv := server.NewServer(port)

	// Setup handlers
	providersHandler := handlers.NewProvidersHandler(db)
	consentHandler := handlers.NewConsentHandler(db, baseURL, stateKey)
	callbackHandler := handlers.NewCallbackHandler(db, encryptionKey, stateKey)

	// Setup routes
	router := srv.Router()
	router.Post("/providers", providersHandler.Register)
	router.Post("/auth/consent-spec", consentHandler.GetSpec)
	router.Get("/auth/callback", callbackHandler.Handle)
	router.Get("/connections/{connectionID}/token", callbackHandler.GetToken)
	router.Post("/connections/{connectionID}/refresh", callbackHandler.Refresh)
	router.Method("GET", "/metrics", server.MetricsHandler())

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
