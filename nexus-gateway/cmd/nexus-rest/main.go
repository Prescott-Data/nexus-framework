package main

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-gateway/internal/server"
)

var Version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		log.Printf("Nexus Gateway (REST) version: %s", Version)
		os.Exit(0)
	}

	// Config
	port := getEnv("PORT", "8090")
	brokerBaseURL := getEnv("BROKER_BASE_URL", "http://localhost:8080")
	stateKeyStr := getEnv("STATE_KEY", "")

	if brokerBaseURL == "" {
		log.Fatal("BROKER_BASE_URL is required")
	}

	var stateKey []byte
	if stateKeyStr != "" {
		key, err := base64.StdEncoding.DecodeString(stateKeyStr)
		if err != nil {
			log.Fatal("Invalid STATE_KEY format, must be base64 encoded")
		}
		stateKey = key
	} else {
		if os.Getenv("GO_ENV") == "production" {
			log.Fatal("STATE_KEY is required in production environment")
		}
		// Dev fallback: generate a random key
		log.Println("WARNING: Using generated state key. Set STATE_KEY to match broker in production.")
		stateKey = make([]byte, 32)
		if _, err := rand.Read(stateKey); err != nil {
			log.Fatal("Failed to generate state key:", err)
		}
	}

	// HTTP client with sane timeouts and connection reuse
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	srv := server.New(port, brokerBaseURL, stateKey, httpClient)

	log.Printf("Starting Nexus on port %s, broker=%s", port, brokerBaseURL)
	log.Printf("Version: %s", Version)
	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}