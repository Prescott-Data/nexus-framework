package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
)

// PromMetrics implements the bridge.Metrics interface using Prometheus.
type PromMetrics struct {
	connections    prometheus.Counter
	disconnects    prometheus.Counter
	tokenRefreshes prometheus.Counter
	connStatus     prometheus.Gauge
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
	}

	registry.MustRegister(m.connections)
	registry.MustRegister(m.disconnects)
	registry.MustRegister(m.tokenRefreshes)
	registry.MustRegister(m.connStatus)

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

func (m *PromMetrics) SetConnectionStatus(status float64) {
	m.connStatus.Set(status)
}
