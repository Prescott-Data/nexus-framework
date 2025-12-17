package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns the standard Prometheus metrics handler.
// Mount this at "/metrics" in your application.
func Handler() http.Handler {
	return promhttp.Handler()
}
