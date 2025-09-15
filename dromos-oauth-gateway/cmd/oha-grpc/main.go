package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	grpcsrv "dromos-oauth-gateway/internal/grpc"
	"dromos-oauth-gateway/internal/usecase"
)

func main() {
	portHTTP := getEnv("PORT_HTTP", "8090")
	portGRPC := getEnv("PORT_GRPC", "9090")
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
		log.Println("WARNING: Using generated state key. Set STATE_KEY to match broker in production.")
		stateKey = make([]byte, 32)
		if _, err := rand.Read(stateKey); err != nil {
			log.Fatal("Failed to generate state key:", err)
		}
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	}
	httpClient := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	handler := usecase.NewHandler(brokerBaseURL, stateKey, httpClient)

	srv, err := grpcsrv.NewServer(grpcsrv.Options{
		GRPCAddress: ":" + portGRPC,
		HTTPAddress: ":" + portHTTP,
		Handler:     handler,
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("Starting OHA gRPC on %s and HTTP gateway on %s, broker=%s", ":"+portGRPC, ":"+portHTTP, brokerBaseURL)
	if err := srv.Start(ctx); err != nil {
		log.Fatal(err)
	}

	// Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down gRPC and HTTP gateway...")
	shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	_ = srv.Shutdown(shutdownCtx)
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
