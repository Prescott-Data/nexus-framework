//go:build integration

// Package service integration tests exercise revocation against a real
// PostgreSQL instance. The unit tests cover the decision logic with mocks; this
// file exists to prove the SQL itself is correct — column names, the partial
// index, the transaction boundary and the agent-session cascade cannot be
// validated by sqlmock, which happily accepts queries a real server rejects.
//
// Run with:
//
//	NEXUS_TEST_DATABASE_URL="postgres://..." go test -tags=integration ./internal/service/ -run Integration -v
package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/repository/postgres"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// testEncryptionKey is a fixed 32-byte AES key. Deterministic on purpose so a
// failed run leaves decryptable rows behind for inspection.
var testEncryptionKey = []byte("0123456789abcdef0123456789abcdef")

// statusCodeOf extracts the HTTP status a service error carries, or 0.
func statusCodeOf(err error) int {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.HTTPStatus
	}
	return 0
}

func requireDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("NEXUS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NEXUS_TEST_DATABASE_URL not set; skipping integration test")
	}
	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedFixture creates a provider profile, an active connection, an encrypted
// token and an open agent session, and returns their identifiers.
func seedFixture(t *testing.T, db *sqlx.DB, workspaceID, revocationEndpoint string) (providerID, connectionID uuid.UUID, providerName, sessionID string) {
	t.Helper()

	providerID = uuid.New()
	connectionID = uuid.New()
	providerName = "itest-" + providerID.String()[:8]
	sessionID = "sess-" + connectionID.String()[:8]

	params := map[string]string{}
	if revocationEndpoint != "" {
		params["revocation_endpoint"] = revocationEndpoint
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO provider_profiles (id, name, client_id, client_secret, auth_url, token_url, scopes, auth_type, params, enable_discovery)
		VALUES ($1, $2, 'client-abc', 'secret-xyz', 'https://example.test/auth', 'https://example.test/token', ARRAY['repo'], 'oauth2', $3, false)`,
		providerID, providerName, paramsJSON); err != nil {
		t.Fatalf("seed provider: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO connections (id, workspace_id, provider_id, status, scopes, return_url, created_at, updated_at)
		VALUES ($1, $2, $3, 'active', ARRAY['repo'], 'https://app.test/done', now(), now())`,
		connectionID, workspaceID, providerID); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	credential, err := json.Marshal(map[string]interface{}{
		"access_token":  "access-token-value",
		"refresh_token": "refresh-token-value",
		"token_type":    "Bearer",
	})
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	encrypted, err := vault.Encrypt(testEncryptionKey, credential)
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO tokens (id, connection_id, encrypted_data, expires_at, created_at)
		VALUES ($1, $2, $3, now() + interval '1 hour', now())`,
		uuid.New(), connectionID, encrypted); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	agentID := "agent-" + connectionID.String()[:8]
	if _, err := db.Exec(`INSERT INTO agents (id, description, allowed_scopes, created_at, active)
		VALUES ($1, $2, ARRAY['repo'], now(), true)
		ON CONFLICT (id) DO NOTHING`, agentID, "integration test agent"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (session_id, agent_id, connection_id, scopes_granted, expires_at, obo, clearance_level, created_at)
		VALUES ($1, $2, $3, ARRAY['repo'], now() + interval '1 hour', false, 0, now())`,
		sessionID, agentID, connectionID); err != nil {
		t.Fatalf("seed agent session: %v", err)
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM agent_sessions WHERE connection_id = $1`, connectionID)
		db.Exec(`DELETE FROM agents WHERE id = $1`, agentID)
		db.Exec(`DELETE FROM tokens WHERE connection_id = $1`, connectionID)
		db.Exec(`DELETE FROM connections WHERE id = $1`, connectionID)
		db.Exec(`DELETE FROM provider_profiles WHERE id = $1`, providerID)
	})

	return providerID, connectionID, providerName, sessionID
}

func newIntegrationService(t *testing.T, db *sqlx.DB, sessions bool) ConnectionService {
	t.Helper()
	connRepo := postgres.NewConnectionRepository(db)
	tokenRepo := postgres.NewTokenRepository(db)
	agentRepo := postgres.NewAgentRepository(db)
	store := provider.NewStore(db)

	opts := []ConnectionServiceOption{}
	if sessions {
		opts = append(opts, WithAgentSessionCloser(agentRepo))
	}

	return NewConnectionService(
		connRepo,
		tokenRepo,
		store,
		audit.NewService(db),
		"https://broker.test",
		"/callback",
		testEncryptionKey,
		[]byte("state-key-0123456789abcdef012345"),
		http.DefaultClient,
		false,
		nil,
		opts...,
	)
}

// revocationRecorder stands in for a provider's RFC 7009 endpoint.
type revocationRecorder struct {
	mu    sync.Mutex
	hints []string
}

func (r *revocationRecorder) server(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			t.Errorf("provider: parse form: %v", err)
		}
		r.mu.Lock()
		r.hints = append(r.hints, req.Form.Get("token_type_hint"))
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (r *revocationRecorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hints...)
}

func TestIntegrationRevokeConnectionDestroysCredential(t *testing.T) {
	db := requireDB(t)
	recorder := &revocationRecorder{}
	srv := recorder.server(t, http.StatusOK)

	workspaceID := "ws-" + uuid.New().String()[:8]
	_, connectionID, _, sessionID := seedFixture(t, db, workspaceID, srv.URL+"/revoke")

	svc := newIntegrationService(t, db, true)
	result, err := svc.RevokeConnection(context.Background(), RevokeRequest{
		ConnectionID: connectionID,
		WorkspaceID:  workspaceID,
		Reason:       "employee offboarded",
	})
	if err != nil {
		t.Fatalf("RevokeConnection: %v", err)
	}

	if !result.TokenDeleted {
		t.Error("expected TokenDeleted to be true")
	}
	if !result.ProviderRevoked {
		t.Errorf("expected ProviderRevoked to be true, error was %q", result.ProviderRevocationError)
	}
	if result.Status != StatusRevoked {
		t.Errorf("status = %q, want %q", result.Status, StatusRevoked)
	}
	if result.SessionsClosed != 1 {
		t.Errorf("SessionsClosed = %d, want 1", result.SessionsClosed)
	}

	// RFC 7009 §2.1: the refresh token is revoked first so the provider can
	// cascade to derived access tokens.
	hints := recorder.seen()
	want := []string{"refresh_token", "access_token"}
	if len(hints) != len(want) {
		t.Fatalf("provider received %v, want %v", hints, want)
	}
	for i := range want {
		if hints[i] != want[i] {
			t.Errorf("provider call %d hint = %q, want %q", i, hints[i], want[i])
		}
	}

	// The credential must be gone from the database, not merely flagged.
	var tokenCount int
	if err := db.QueryRow(`SELECT count(*) FROM tokens WHERE connection_id = $1`, connectionID).Scan(&tokenCount); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if tokenCount != 0 {
		t.Errorf("token rows = %d, want 0", tokenCount)
	}

	var status string
	var revokedAt *time.Time
	var reason *string
	if err := db.QueryRow(`SELECT status, revoked_at, revocation_reason FROM connections WHERE id = $1`,
		connectionID).Scan(&status, &revokedAt, &reason); err != nil {
		t.Fatalf("read connection: %v", err)
	}
	if status != StatusRevoked {
		t.Errorf("db status = %q, want %q", status, StatusRevoked)
	}
	if revokedAt == nil {
		t.Error("revoked_at was not set")
	}
	if reason == nil || *reason != "employee offboarded" {
		t.Errorf("revocation_reason = %v, want %q", reason, "employee offboarded")
	}

	var closedAt *time.Time
	if err := db.QueryRow(`SELECT closed_at FROM agent_sessions WHERE session_id = $1`, sessionID).Scan(&closedAt); err != nil {
		t.Fatalf("read agent session: %v", err)
	}
	if closedAt == nil {
		t.Error("agent session was not closed by the revocation cascade")
	}
}

func TestIntegrationRevokeIsIdempotent(t *testing.T) {
	db := requireDB(t)
	recorder := &revocationRecorder{}
	srv := recorder.server(t, http.StatusOK)

	workspaceID := "ws-" + uuid.New().String()[:8]
	_, connectionID, _, _ := seedFixture(t, db, workspaceID, srv.URL+"/revoke")

	svc := newIntegrationService(t, db, true)
	req := RevokeRequest{ConnectionID: connectionID, WorkspaceID: workspaceID, Reason: "first"}

	if _, err := svc.RevokeConnection(context.Background(), req); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	second, err := svc.RevokeConnection(context.Background(), RevokeRequest{
		ConnectionID: connectionID, WorkspaceID: workspaceID, Reason: "second",
	})
	if err != nil {
		t.Fatalf("second revoke should succeed, got %v", err)
	}
	if !second.AlreadyRevoked {
		t.Error("expected AlreadyRevoked on the second call")
	}

	// The provider must not be called again on a repeat revoke.
	if got := len(recorder.seen()); got != 2 {
		t.Errorf("provider calls = %d, want 2 (the second revoke must not call upstream)", got)
	}

	// The original reason must survive: MarkRevoked is guarded on revoked_at IS NULL.
	var reason string
	if err := db.QueryRow(`SELECT revocation_reason FROM connections WHERE id = $1`, connectionID).Scan(&reason); err != nil {
		t.Fatalf("read reason: %v", err)
	}
	if reason != "first" {
		t.Errorf("revocation_reason = %q, want %q (second revoke must not overwrite)", reason, "first")
	}
}

func TestIntegrationRevokedConnectionRejectsTokenIssuance(t *testing.T) {
	db := requireDB(t)
	recorder := &revocationRecorder{}
	srv := recorder.server(t, http.StatusOK)

	workspaceID := "ws-" + uuid.New().String()[:8]
	_, connectionID, providerName, _ := seedFixture(t, db, workspaceID, srv.URL+"/revoke")

	svc := newIntegrationService(t, db, true)
	if _, err := svc.RevokeConnection(context.Background(), RevokeRequest{
		ConnectionID: connectionID, WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	_, _, err := svc.GetToken(context.Background(), connectionID)
	if err == nil {
		t.Fatal("expected GetToken to fail for a revoked connection")
	}
	if status := statusCodeOf(err); status != http.StatusGone {
		t.Errorf("GetToken status = %d, want %d (410 Gone)", status, http.StatusGone)
	}

	_ = providerName
}

func TestIntegrationRevokeRejectsForeignWorkspace(t *testing.T) {
	db := requireDB(t)
	recorder := &revocationRecorder{}
	srv := recorder.server(t, http.StatusOK)

	workspaceID := "ws-" + uuid.New().String()[:8]
	_, connectionID, _, _ := seedFixture(t, db, workspaceID, srv.URL+"/revoke")

	svc := newIntegrationService(t, db, true)
	_, err := svc.RevokeConnection(context.Background(), RevokeRequest{
		ConnectionID: connectionID,
		WorkspaceID:  "ws-someone-else",
	})
	if err == nil {
		t.Fatal("expected revoke to fail for a foreign workspace")
	}
	// 404 rather than 403: a 403 would confirm the connection ID exists.
	if status := statusCodeOf(err); status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", status, http.StatusNotFound)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM connections WHERE id = $1`, connectionID).Scan(&status); err != nil {
		t.Fatalf("read connection: %v", err)
	}
	if status != "active" {
		t.Errorf("connection status = %q, want it untouched (active)", status)
	}
}

// TestIntegrationRevokeSurvivesProviderOutage is the important failure case: a
// provider that is unreachable must never leave a live credential behind.
func TestIntegrationRevokeSurvivesProviderOutage(t *testing.T) {
	db := requireDB(t)
	recorder := &revocationRecorder{}
	srv := recorder.server(t, http.StatusInternalServerError)

	workspaceID := "ws-" + uuid.New().String()[:8]
	_, connectionID, _, _ := seedFixture(t, db, workspaceID, srv.URL+"/revoke")

	svc := newIntegrationService(t, db, true)
	result, err := svc.RevokeConnection(context.Background(), RevokeRequest{
		ConnectionID: connectionID, WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("revoke must succeed even when the provider fails: %v", err)
	}
	if result.ProviderRevoked {
		t.Error("ProviderRevoked should be false when the provider returns 500")
	}
	if result.ProviderRevocationError == "" {
		t.Error("expected ProviderRevocationError to explain the failure")
	}
	if !result.TokenDeleted {
		t.Error("the local credential must be destroyed regardless of provider outcome")
	}

	var tokenCount int
	if err := db.QueryRow(`SELECT count(*) FROM tokens WHERE connection_id = $1`, connectionID).Scan(&tokenCount); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if tokenCount != 0 {
		t.Errorf("token rows = %d, want 0 — a provider outage must not preserve credentials", tokenCount)
	}
}

// TestIntegrationListByWorkspaceExposesRevokedAt covers the widened
// ListByWorkspace scan, which sqlmock cannot validate against a real schema.
func TestIntegrationListByWorkspaceExposesRevokedAt(t *testing.T) {
	db := requireDB(t)
	recorder := &revocationRecorder{}
	srv := recorder.server(t, http.StatusOK)

	workspaceID := "ws-" + uuid.New().String()[:8]
	_, connectionID, _, _ := seedFixture(t, db, workspaceID, srv.URL+"/revoke")

	svc := newIntegrationService(t, db, true)
	if _, err := svc.RevokeConnection(context.Background(), RevokeRequest{
		ConnectionID: connectionID, WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	conns, err := postgres.NewConnectionRepository(db).ListByWorkspace(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}

	var found *domain.ConnectionSummary
	for i := range conns {
		if conns[i].ID == connectionID {
			found = &conns[i]
			break
		}
	}
	if found == nil {
		t.Fatal("revoked connection missing from ListByWorkspace")
	}
	if found.Status != StatusRevoked {
		t.Errorf("summary status = %q, want %q", found.Status, StatusRevoked)
	}
	if found.RevokedAt == nil {
		t.Error("summary RevokedAt was not populated")
	}
}
