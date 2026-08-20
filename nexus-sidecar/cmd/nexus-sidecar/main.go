package main

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
	"github.com/Prescott-Data/nexus-framework/nexus-sidecar/internal/config"
	"github.com/Prescott-Data/nexus-framework/nexus-sidecar/internal/sidecar"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Version = "dev"

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	routes := make(map[string]sidecar.Route, len(cfg.Routes))
	routeNames := make([]string, 0, len(cfg.Routes))
	for name, route := range cfg.Routes {
		routes[name] = sidecar.Route{Name: route.Name, Target: route.Target}
		routeNames = append(routeNames, name)
	}
	sort.Strings(routeNames)

	tokenClient := oauthsdk.New(cfg.GatewayBaseURL, oauthsdk.WithRetry(oauthsdk.RetryPolicy{
		Retries:    2,
		MinDelay:   200 * time.Millisecond,
		MaxDelay:   2 * time.Second,
		RetryOn429: true,
	}))

	proxy, err := sidecar.NewProxy(sidecar.Config{
		Routes:           routes,
		TokenProvider:    tokenClient,
		TokenCacheTTL:    cfg.TokenCacheTTL,
		RequestBodyLimit: cfg.RequestBodyLimit,
	})
	if err != nil {
		log.Fatalf("sidecar setup error: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", loggingMiddleware(proxy))

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("nexus-sidecar version=%s port=%s gateway=%s routes=%s", Version, cfg.Port, cfg.GatewayBaseURL, strings.Join(routeNames, ","))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("sidecar server failed: %v", err)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		log.Printf("method=%s path=%s status=%d duration=%s", r.Method, r.URL.Path, recorder.status, time.Since(started))
	})
}
