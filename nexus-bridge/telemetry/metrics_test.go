package telemetry

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewMetrics_WithLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	agentLabels := map[string]string{
		"agent_id": "test-agent",
		"version":  "0.1.0",
	}

	m := NewMetrics(registry, agentLabels)

	// Increment a counter to ensure it's registered and has value
	m.IncConnections()

	// Gather metrics from the registry
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "bridge_connections_total" {
			found = true
			for _, m := range mf.GetMetric() {
				labels := m.GetLabel()
				if len(labels) != 2 {
					t.Errorf("expected 2 labels, got %d", len(labels))
				}

				labelMap := make(map[string]string)
				for _, l := range labels {
					labelMap[l.GetName()] = l.GetValue()
				}

				if labelMap["agent_id"] != "test-agent" {
					t.Errorf("expected agent_id=test-agent, got %s", labelMap["agent_id"])
				}
				if labelMap["version"] != "0.1.0" {
					t.Errorf("expected version=0.1.0, got %s", labelMap["version"])
				}

				if m.GetCounter().GetValue() != 1 {
					t.Errorf("expected counter value 1, got %f", m.GetCounter().GetValue())
				}
			}
		}
	}

	if !found {
		t.Error("metric bridge_connections_total not found")
	}
}

func TestNewMetrics_NilLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry, nil)

	m.IncConnections()

	metricFamilies, _ := registry.Gather()
	for _, mf := range metricFamilies {
		if mf.GetName() == "bridge_connections_total" {
			for _, m := range mf.GetMetric() {
				if len(m.GetLabel()) != 0 {
					t.Errorf("expected 0 labels, got %d", len(m.GetLabel()))
				}
			}
		}
	}
}

func TestNewMetrics_RegistersTimingHistograms(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry, map[string]string{"agent_id": "test-agent"})

	m.ObserveConnectionDuration(2 * time.Minute)
	m.ObserveTokenRefreshLatency(1500 * time.Millisecond)

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	assertHistogram := func(name string) {
		t.Helper()

		for _, mf := range metricFamilies {
			if mf.GetName() != name {
				continue
			}

			for _, metric := range mf.GetMetric() {
				if metric.GetHistogram().GetSampleCount() != 1 {
					t.Fatalf("%s expected sample count 1, got %d", name, metric.GetHistogram().GetSampleCount())
				}

				labels := metric.GetLabel()
				if len(labels) != 1 || labels[0].GetName() != "agent_id" || labels[0].GetValue() != "test-agent" {
					t.Fatalf("%s expected agent_id label, got %v", name, labels)
				}
			}
			return
		}

		t.Fatalf("metric %s not found", name)
	}

	assertHistogram("bridge_connection_duration_seconds")
	assertHistogram("bridge_token_refresh_latency_seconds")
}
