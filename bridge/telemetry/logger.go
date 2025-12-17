package telemetry

import (
	"log/slog"
	"os"
)

// SlogLogger implements the bridge.Logger interface using log/slog.
type SlogLogger struct {
	logger *slog.Logger
}

// NewLogger creates a new structured logger that writes JSON to stdout.
func NewLogger() *SlogLogger {
	return &SlogLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
}

// Info logs an informational message.
func (l *SlogLogger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.Info(msg, keysAndValues...)
}

// Error logs an error message.
func (l *SlogLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	// We append the error to the keysAndValues
	args := append(keysAndValues, "error", err)
	l.logger.Error(msg, args...)
}
