package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func TestOBOSessionPersistence(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewAgentRepository(sqlx.NewDb(db, "sqlmock"))
	now := time.Now().UTC()
	session := &domain.AgentSession{SessionID: "sess_obo", AgentID: "agent", ConnectionID: uuid.New(), ScopesGranted: []string{"read"}, ExpiresAt: now.Add(time.Minute), OBO: true, ActingFor: "alice", TenantID: "org", ClearanceLevel: 3}
	mock.ExpectQuery("WITH live_connection AS").WithArgs(session.SessionID, session.AgentID, session.ConnectionID, pq.Array(session.ScopesGranted), session.ExpiresAt, nil, true, "alice", "org", 3).WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
	require.NoError(t, repo.CreateSession(context.Background(), session))
	mock.ExpectQuery("SELECT session_id, agent_id, connection_id").WithArgs("sess_obo").WillReturnRows(sqlmock.NewRows([]string{"session_id", "agent_id", "connection_id", "scopes_granted", "expires_at", "closed_at", "obo", "acting_for", "tenant_id", "clearance_level", "created_at"}).AddRow("sess_obo", "agent", session.ConnectionID.String(), "{read}", session.ExpiresAt, nil, true, "alice", "org", 3, now))
	stored, err := repo.GetSession(context.Background(), "sess_obo")
	require.NoError(t, err)
	require.Equal(t, session, stored)
	require.NoError(t, mock.ExpectationsWereMet())
}
