package bridge

import (
	"time"

	"github.com/gorilla/websocket"
)

// --- Interfaces ---

// Logger is an interface that allows for plugging in custom structured loggers.
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Error(err error, msg string, keysAndValues ...interface{})
}

// Metrics is an interface that allows for plugging in custom metrics collectors.
type Metrics interface {
	IncConnections()
	IncDisconnects()
	IncTokenRefreshes()
	SetConnectionStatus(status float64)
}

// --- No-op Implementations ---

type nopLogger struct{}

func (l *nopLogger) Info(msg string, keysAndValues ...interface{})  {}
func (l *nopLogger) Error(err error, msg string, keysAndValues ...interface{}) {}

type nopMetrics struct{}

func (m *nopMetrics) IncConnections()             {}
func (m *nopMetrics) IncDisconnects()             {}
func (m *nopMetrics) IncTokenRefreshes()          {}
func (m *nopMetrics) SetConnectionStatus(status float64) {}

// --- Configuration ---

// RetryPolicy defines the backoff strategy for reconnections.
type RetryPolicy struct {
	MinBackoff time.Duration
	MaxBackoff time.Duration
	Jitter     time.Duration
}

// Option is a function that configures a Bridge.
type Option func(*Bridge)

// WithLogger sets a custom logger for the Bridge.
func WithLogger(logger Logger) Option {
	return func(b *Bridge) {
		b.logger = logger
	}
}

// WithRetryPolicy sets the reconnection policy for the Bridge.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(b *Bridge) {
		b.retryPolicy = policy
	}
}

// WithRefreshBuffer sets the duration before token expiry to attempt a refresh.
// Defaults to 5 minutes.
func WithRefreshBuffer(d time.Duration) Option {
	return func(b *Bridge) {
		b.refreshBuffer = d
	}
}

// WithDialer sets a custom websocket.Dialer for the Bridge.
func WithDialer(dialer *websocket.Dialer) Option {
	return func(b *Bridge) {
		b.dialer = dialer
	}
}

// WithMetrics sets a custom metrics collector for the Bridge.
func WithMetrics(metrics Metrics) Option {
	return func(b *Bridge) {
		b.metrics = metrics
	}
}
