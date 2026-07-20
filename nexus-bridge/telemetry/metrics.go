package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// PromMetrics implements the bridge.Metrics interface using Prometheus.
type PromMetrics struct {
	connections         prometheus.Counter
	disconnects         prometheus.Counter
	tokenRefreshes      prometheus.Counter
	connStatus          prometheus.Gauge
	connectionDuration  prometheus.Histogram
	tokenRefreshLatency prometheus.Histogram
}

// NewMetrics creates and registers standard bridge metrics.
// If registry is nil, it uses the global default registry.
func NewMetrics(registry prometheus.Registerer, agentLabels map[string]string) *PromMetrics {
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}

	m := &PromMetrics{
		connections: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "bridge",
			Name:        "connections_total",
			Help:        "Total number of successful WebSocket connections established.",
			ConstLabels: agentLabels,
		}),
		disconnects: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "bridge",
			Name:        "disconnects_total",
			Help:        "Total number of WebSocket disconnects.",
			ConstLabels: agentLabels,
		}),
		tokenRefreshes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "bridge",
			Name:        "token_refreshes_total",
			Help:        "Total number of token refresh operations.",
			ConstLabels: agentLabels,
		}),
		connStatus: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   "bridge",
			Name:        "connection_status",
			Help:        "Current status of the connection (1 = connected, 0 = disconnected).",
			ConstLabels: agentLabels,
		}),
		connectionDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace:   "bridge",
			Name:        "connection_duration_seconds",
			Help:        "Duration a bridge connection remained alive before disconnecting.",
			ConstLabels: agentLabels,
			Buckets:     []float64{1, 5, 15, 30, 60, 300, 900, 1800, 3600, 7200, 21600, 43200, 86400},
		}),
		tokenRefreshLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace:   "bridge",
			Name:        "token_refresh_latency_seconds",
			Help:        "Latency of token refresh operations.",
			ConstLabels: agentLabels,
			Buckets:     []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}),
	}

	registry.MustRegister(m.connections)
	registry.MustRegister(m.disconnects)
	registry.MustRegister(m.tokenRefreshes)
	registry.MustRegister(m.connStatus)
	registry.MustRegister(m.connectionDuration)
	registry.MustRegister(m.tokenRefreshLatency)

	return m
}

func (m *PromMetrics) IncConnections() {
	m.connections.Inc()
}

func (m *PromMetrics) IncDisconnects() {
	m.disconnects.Inc()
}

func (m *PromMetrics) IncTokenRefreshes() {
	m.tokenRefreshes.Inc()
}

func (m *PromMetrics) ObserveConnectionDuration(duration time.Duration) {
	m.connectionDuration.Observe(duration.Seconds())
}

func (m *PromMetrics) ObserveTokenRefreshLatency(duration time.Duration) {
	m.tokenRefreshLatency.Observe(duration.Seconds())
}

func (m *PromMetrics) SetConnectionStatus(status float64) {
	m.connStatus.Set(status)
}
