package telemetry

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
)

// ConnectionGaugeCollector exposes connection status counts as Prometheus gauges.
// It polls the database at a configurable interval to avoid per-scrape query load.
type ConnectionGaugeCollector struct {
	connRepo repository.ConnectionRepository
	gauge    *prometheus.GaugeVec
	mu       sync.RWMutex
	counts   map[string]int64
}

// NewConnectionGaugeCollector creates and starts a background collector that
// periodically queries connection counts by status and exposes them as gauges.
func NewConnectionGaugeCollector(connRepo repository.ConnectionRepository, interval time.Duration) *ConnectionGaugeCollector {
	gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nexus_connections_total",
		Help: "Current number of connections by status",
	}, []string{"status"})

	if err := prometheus.Register(gauge); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
			panic(err)
		}
	}

	c := &ConnectionGaugeCollector{
		connRepo: connRepo,
		gauge:    gauge,
		counts:   make(map[string]int64),
	}

	go c.poll(interval)
	return c
}

func (c *ConnectionGaugeCollector) poll(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once immediately at startup
	c.refresh()

	for range ticker.C {
		c.refresh()
	}
}

func (c *ConnectionGaugeCollector) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	counts, err := c.connRepo.CountByStatus(ctx)
	if err != nil {
		log.Printf("[WARN] connection gauge refresh failed: %v", err)
		return
	}

	c.mu.Lock()
	c.counts = counts
	c.mu.Unlock()

	// Reset all gauges then set current values to handle statuses
	// that may have gone to zero
	c.gauge.Reset()
	for status, count := range counts {
		c.gauge.WithLabelValues(status).Set(float64(count))
	}
}
