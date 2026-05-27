package instrumented

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository"
)

type txRunner interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

var (
	dbOpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nexus_db_operation_duration_seconds",
		Help:    "Duration of database operations by repository and method",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
	}, []string{"repo", "method"})
)

func init() {
	prometheus.MustRegister(dbOpDuration)
}

func observe(repo, method string, start time.Time) {
	dbOpDuration.WithLabelValues(repo, method).Observe(time.Since(start).Seconds())
}

// --- ConnectionRepository decorator ---

// ConnectionRepository wraps repository.ConnectionRepository with latency instrumentation.
type ConnectionRepository struct {
	inner repository.ConnectionRepository
}

// NewConnectionRepository wraps a ConnectionRepository with Prometheus latency histograms.
func NewConnectionRepository(inner repository.ConnectionRepository) repository.ConnectionRepository {
	return &ConnectionRepository{inner: inner}
}

func (r *ConnectionRepository) Create(ctx context.Context, conn *domain.Connection) error {
	defer observe("connection", "Create", time.Now())
	return r.inner.Create(ctx, conn)
}

func (r *ConnectionRepository) GetPending(ctx context.Context, id uuid.UUID) (*domain.Connection, error) {
	defer observe("connection", "GetPending", time.Now())
	return r.inner.GetPending(ctx, id)
}

func (r *ConnectionRepository) GetWithProvider(ctx context.Context, id uuid.UUID) (*domain.ConnectionWithProvider, error) {
	defer observe("connection", "GetWithProvider", time.Now())
	return r.inner.GetWithProvider(ctx, id)
}

func (r *ConnectionRepository) GetReturnURL(ctx context.Context, id uuid.UUID) (string, error) {
	defer observe("connection", "GetReturnURL", time.Now())
	return r.inner.GetReturnURL(ctx, id)
}

func (r *ConnectionRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	defer observe("connection", "UpdateStatus", time.Now())
	return r.inner.UpdateStatus(ctx, id, status)
}

func (r *ConnectionRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	defer observe("connection", "CountByStatus", time.Now())
	return r.inner.CountByStatus(ctx)
}

func (r *ConnectionRepository) GetActiveByWorkspaceAndProvider(ctx context.Context, workspaceID, providerName string) (*domain.ConnectionWithProvider, error) {
	defer observe("connection", "GetActiveByWorkspaceAndProvider", time.Now())
	return r.inner.GetActiveByWorkspaceAndProvider(ctx, workspaceID, providerName)
}

func (r *ConnectionRepository) GetForHealthCheck(ctx context.Context, limit int) ([]*domain.ConnectionWithProvider, error) {
	defer observe("connection", "GetForHealthCheck", time.Now())
	return r.inner.GetForHealthCheck(ctx, limit)
}

func (r *ConnectionRepository) UpdateHealthStatus(ctx context.Context, id uuid.UUID, status string) error {
	defer observe("connection", "UpdateHealthStatus", time.Now())
	return r.inner.UpdateHealthStatus(ctx, id, status)
}

func (r *ConnectionRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]domain.ConnectionSummary, error) {
	defer observe("connection", "ListByWorkspace", time.Now())
	return r.inner.ListByWorkspace(ctx, workspaceID)
}

// InTx forwards transactional execution when the wrapped repository supports it.
func (r *ConnectionRepository) InTx(ctx context.Context, fn func(context.Context) error) error {
	if runner, ok := r.inner.(txRunner); ok {
		defer observe("connection", "InTx", time.Now())
		return runner.InTx(ctx, fn)
	}
	return fn(ctx)
}

// --- TokenRepository decorator ---

// TokenRepository wraps repository.TokenRepository with latency instrumentation.
type TokenRepository struct {
	inner repository.TokenRepository
}

// NewTokenRepository wraps a TokenRepository with Prometheus latency histograms.
func NewTokenRepository(inner repository.TokenRepository) repository.TokenRepository {
	return &TokenRepository{inner: inner}
}

func (r *TokenRepository) Upsert(ctx context.Context, token *domain.Token) error {
	defer observe("token", "Upsert", time.Now())
	return r.inner.Upsert(ctx, token)
}

func (r *TokenRepository) Get(ctx context.Context, connectionID uuid.UUID) (*domain.Token, error) {
	defer observe("token", "Get", time.Now())
	return r.inner.Get(ctx, connectionID)
}
