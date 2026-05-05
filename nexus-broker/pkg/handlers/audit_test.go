package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/handlers"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	sqlmock "gopkg.in/DATA-DOG/go-sqlmock.v1"
)

// newSqlxDB wraps a sqlmock connection in a sqlx.DB so handlers can use it.
func newSqlxDB(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	return sqlx.NewDb(db, "postgres"), mock
}

func TestAuditList_NoFilters_ReturnsDefaultLimit(t *testing.T) {
	db, mock := newSqlxDB(t)
	defer db.Close()

	id := uuid.New()
	connID := uuid.New()
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "connection_id", "event_type", "event_data", "ip_address", "user_agent", "created_at",
	}).AddRow(
		id, &connID, "provider.created", `{"name":"google"}`, "127.0.0.1", "curl/7.88", now,
	)

	mock.ExpectQuery(`SELECT id, connection_id, event_type, event_data, ip_address, user_agent, created_at`).
		WithArgs(50). // default limit
		WillReturnRows(rows)

	handler := handlers.NewAuditHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result))
	}
	if result[0]["event_type"] != "provider.created" {
		t.Errorf("expected event_type provider.created, got %v", result[0]["event_type"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestAuditList_EventTypeFilter(t *testing.T) {
	db, mock := newSqlxDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{
		"id", "connection_id", "event_type", "event_data", "ip_address", "user_agent", "created_at",
	}) // empty result

	mock.ExpectQuery(`SELECT id, connection_id, event_type, event_data, ip_address, user_agent, created_at`).
		WithArgs("provider.deleted", 50).
		WillReturnRows(rows)

	handler := handlers.NewAuditHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/audit?event_type=provider.deleted", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Empty result should return [] not null
	var result []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result == nil {
		t.Error("expected empty array, got null")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestAuditList_InvalidSinceParam_Returns400(t *testing.T) {
	db, _ := newSqlxDB(t)
	defer db.Close()

	handler := handlers.NewAuditHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/audit?since=not-a-date", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid since param, got %d", w.Code)
	}
}

func TestAuditList_CustomLimit(t *testing.T) {
	db, mock := newSqlxDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{
		"id", "connection_id", "event_type", "event_data", "ip_address", "user_agent", "created_at",
	})

	mock.ExpectQuery(`SELECT id, connection_id, event_type, event_data, ip_address, user_agent, created_at`).
		WithArgs(10).
		WillReturnRows(rows)

	handler := handlers.NewAuditHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/audit?limit=10", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestAuditList_LimitAboveMax_FallsBackToDefault(t *testing.T) {
	db, mock := newSqlxDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{
		"id", "connection_id", "event_type", "event_data", "ip_address", "user_agent", "created_at",
	})

	// limit=9999 should be clamped to 50 (default, since > 1000 is rejected)
	mock.ExpectQuery(`SELECT id, connection_id, event_type, event_data, ip_address, user_agent, created_at`).
		WithArgs(50).
		WillReturnRows(rows)

	handler := handlers.NewAuditHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/audit?limit=9999", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}
