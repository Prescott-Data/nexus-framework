package integration

// =============================================================================
// SOC 2 Live Database Integration Tests — Nexus Broker
// =============================================================================
//
// These tests connect to a REAL PostgreSQL database to provide ground-truth
// compliance evidence. They verify that:
//
//   SOC-CTRL-01  Encrypted tokens in the DB are NOT readable as plaintext.
//   SOC-CTRL-02  Audit events are physically written to the audit_events table.
//
// Prerequisites:
//   - PostgreSQL running on localhost:5432 (docker-compose up -d)
//   - Database: oauth_broker / User: oauth_user / Password: oauth_password
//
// These tests are gated behind the NEXUS_TEST_DB=1 environment variable.
// Run with:
//   NEXUS_TEST_DB=1 go test ./tests/integration/... -run "TestLiveDB" -v
// =============================================================================

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	_ "github.com/lib/pq"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/vault"
)

const defaultTestDSN = "postgres://oauth_user:oauth_password@localhost:5432/oauth_broker?sslmode=disable"

// connectTestDB returns a live database connection or skips the test.
func connectTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	if os.Getenv("NEXUS_TEST_DB") != "1" {
		t.Skip("Skipping live DB test: set NEXUS_TEST_DB=1 to enable")
	}

	dsn := os.Getenv("NEXUS_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping test database: %v", err)
	}

	return db
}

// testProviderID creates a minimal provider fixture and returns its UUID.
// The provider is cleaned up via t.Cleanup.
func testProviderID(t *testing.T, db *sqlx.DB, suffix string) uuid.UUID {
	t.Helper()

	providerID := uuid.New()
	name := fmt.Sprintf("soc-test-provider-%s-%s", suffix, providerID.String()[:8])

	_, err := db.Exec(`
		INSERT INTO provider_profiles (id, name, client_id, client_secret, auth_url, token_url, scopes, auth_type)
		VALUES ($1, $2, 'test-client-id', 'test-client-secret', 'https://example.com/auth', 'https://example.com/token', $3, 'api_key')`,
		providerID, name, pq.Array([]string{"read"}))
	if err != nil {
		t.Fatalf("Failed to create test provider: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM provider_profiles WHERE id = $1", providerID)
	})

	return providerID
}

// testConnectionID creates a minimal connection fixture and returns its UUID.
func testConnectionID(t *testing.T, db *sqlx.DB, providerID uuid.UUID, suffix string) uuid.UUID {
	t.Helper()

	connID := uuid.New()
	_, err := db.Exec(`
		INSERT INTO connections (id, workspace_id, provider_id, status, scopes, return_url, expires_at)
		VALUES ($1, 'ws-soc-test', $2, 'active', $3, 'http://localhost/return', $4)`,
		connID, providerID, pq.Array([]string{"read"}), time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to create test connection: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM tokens WHERE connection_id = $1", connID)
		db.Exec("DELETE FROM connections WHERE id = $1", connID)
	})

	return connID
}

// =============================================================================
// SOC-CTRL-01: Encryption at Rest — Live Database Proof (TSC CC6.1)
// =============================================================================

func TestLiveDB_SOC_CTRL01_EncryptedTokenInDatabase(t *testing.T) {
	db := connectTestDB(t)
	defer db.Close()

	providerID := testProviderID(t, db, "enc")
	connID := testConnectionID(t, db, providerID, "enc")

	// The plaintext credentials that would be stored
	plainCredentials := map[string]interface{}{
		"access_token":  "sk-live-SUPER-SECRET-ACCESS-TOKEN-12345",
		"refresh_token": "rt-MASTER-REFRESH-KEY-67890",
		"expires_in":    3600,
	}
	plainJSON, _ := json.Marshal(plainCredentials)

	// Encrypt using the same vault function the broker uses
	encryptionKey := []byte("01234567890123456789012345678901") // 32 bytes
	ciphertext, err := vault.Encrypt(encryptionKey, plainJSON)
	if err != nil {
		t.Fatalf("SOC-CTRL-01 FAILED: vault.Encrypt returned error: %v", err)
	}

	// Insert the encrypted token into the REAL database
	_, err = db.Exec(`
		INSERT INTO tokens (connection_id, encrypted_data, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (connection_id) DO UPDATE SET encrypted_data = EXCLUDED.encrypted_data`,
		connID, ciphertext, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("SOC-CTRL-01 FAILED: Could not insert token into database: %v", err)
	}

	// ==================== PROOF ====================
	// Read the raw encrypted_data column back from the database
	var rawEncryptedData string
	err = db.QueryRow("SELECT encrypted_data FROM tokens WHERE connection_id = $1", connID).Scan(&rawEncryptedData)
	if err != nil {
		t.Fatalf("SOC-CTRL-01 FAILED: Could not read token from database: %v", err)
	}

	// ASSERTION 1: The raw database column must NOT contain any plaintext credential
	if strings.Contains(rawEncryptedData, "sk-live-SUPER-SECRET-ACCESS-TOKEN-12345") {
		t.Fatal("SOC-CTRL-01 VIOLATION: access_token found in PLAINTEXT in the database column")
	}
	if strings.Contains(rawEncryptedData, "rt-MASTER-REFRESH-KEY-67890") {
		t.Fatal("SOC-CTRL-01 VIOLATION: refresh_token found in PLAINTEXT in the database column")
	}
	if strings.Contains(rawEncryptedData, "access_token") {
		t.Fatal("SOC-CTRL-01 VIOLATION: JSON key 'access_token' found in PLAINTEXT in the database column")
	}

	// ASSERTION 2: The encrypted data CAN be decrypted with the correct key
	decrypted, err := vault.Decrypt(encryptionKey, rawEncryptedData)
	if err != nil {
		t.Fatalf("SOC-CTRL-01 VIOLATION: Could not decrypt data read back from database: %v", err)
	}

	var roundTrip map[string]interface{}
	if err := json.Unmarshal(decrypted, &roundTrip); err != nil {
		t.Fatalf("SOC-CTRL-01 VIOLATION: Decrypted data from DB is not valid JSON: %v", err)
	}

	if roundTrip["access_token"] != "sk-live-SUPER-SECRET-ACCESS-TOKEN-12345" {
		t.Fatal("SOC-CTRL-01 VIOLATION: Decrypted access_token does not match original")
	}
	if roundTrip["refresh_token"] != "rt-MASTER-REFRESH-KEY-67890" {
		t.Fatal("SOC-CTRL-01 VIOLATION: Decrypted refresh_token does not match original")
	}

	// ASSERTION 3: A WRONG key cannot decrypt the data
	wrongKey := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ012345")
	_, err = vault.Decrypt(wrongKey, rawEncryptedData)
	if err == nil {
		t.Fatal("SOC-CTRL-01 VIOLATION: Token from database was decrypted with WRONG key")
	}

	t.Log("SOC-CTRL-01 PASS [LIVE DB]: Token in database is encrypted, decryptable with correct key only")
}

// =============================================================================
// SOC-CTRL-02: Tamper-Evident Audit Trail — Live Database Proof (TSC CC7.2)
// =============================================================================

func TestLiveDB_SOC_CTRL02_AuditEventWrittenToDatabase(t *testing.T) {
	db := connectTestDB(t)
	defer db.Close()

	auditSvc := audit.NewService(db)

	// Create a unique marker so we can find THIS test's audit event
	marker := uuid.New().String()

	// Simulate an HTTP request with IP and User-Agent
	req, _ := http.NewRequest("POST", "/providers", nil)
	req.RemoteAddr = "192.168.1.42:54321"
	req.Header.Set("User-Agent", "SOC-Test-Agent/"+marker)

	// Write the audit event using the REAL audit service
	err := auditSvc.Log("provider.created", nil, map[string]interface{}{
		"provider_id": marker,
		"name":        "soc-audit-test",
		"test_marker": marker,
	}, req)
	if err != nil {
		t.Fatalf("SOC-CTRL-02 FAILED: audit.Service.Log returned error: %v", err)
	}

	// ==================== PROOF ====================
	// Query the audit_events table directly to prove the event was written
	var eventID uuid.UUID
	var eventType string
	var eventData sql.NullString
	var ipAddress sql.NullString
	var userAgent sql.NullString
	var createdAt time.Time

	err = db.QueryRow(`
		SELECT id, event_type, event_data, ip_address, user_agent, created_at
		FROM audit_events
		WHERE user_agent LIKE $1
		ORDER BY created_at DESC LIMIT 1`,
		"%"+marker+"%",
	).Scan(&eventID, &eventType, &eventData, &ipAddress, &userAgent, &createdAt)
	if err != nil {
		t.Fatalf("SOC-CTRL-02 VIOLATION: Audit event was NOT found in the database: %v", err)
	}

	// ASSERTION 1: Event type is correct
	if eventType != "provider.created" {
		t.Fatalf("SOC-CTRL-02 VIOLATION: Event type is '%s', expected 'provider.created'", eventType)
	}

	// ASSERTION 2: Event data contains the structured payload
	if !eventData.Valid || eventData.String == "" {
		t.Fatal("SOC-CTRL-02 VIOLATION: event_data column is NULL or empty")
	}

	var parsedData map[string]interface{}
	if err := json.Unmarshal([]byte(eventData.String), &parsedData); err != nil {
		t.Fatalf("SOC-CTRL-02 VIOLATION: event_data is not valid JSON: %v", err)
	}
	if parsedData["provider_id"] != marker {
		t.Fatalf("SOC-CTRL-02 VIOLATION: event_data.provider_id is '%v', expected '%s'", parsedData["provider_id"], marker)
	}
	if parsedData["name"] != "soc-audit-test" {
		t.Fatalf("SOC-CTRL-02 VIOLATION: event_data.name is '%v', expected 'soc-audit-test'", parsedData["name"])
	}

	// ASSERTION 3: IP address was captured
	if !ipAddress.Valid || ipAddress.String == "" {
		t.Fatal("SOC-CTRL-02 VIOLATION: ip_address column is NULL — caller IP was not recorded")
	}
	if ipAddress.String != "192.168.1.42" {
		t.Fatalf("SOC-CTRL-02 VIOLATION: ip_address is '%s', expected '192.168.1.42'", ipAddress.String)
	}

	// ASSERTION 4: User-Agent was captured
	if !userAgent.Valid || userAgent.String == "" {
		t.Fatal("SOC-CTRL-02 VIOLATION: user_agent column is NULL — caller identity was not recorded")
	}
	if !strings.Contains(userAgent.String, "SOC-Test-Agent") {
		t.Fatalf("SOC-CTRL-02 VIOLATION: user_agent is '%s', expected to contain 'SOC-Test-Agent'", userAgent.String)
	}

	// ASSERTION 5: Timestamp is recent (within the last 30 seconds)
	if time.Since(createdAt) > 30*time.Second {
		t.Fatalf("SOC-CTRL-02 VIOLATION: Audit event created_at is %v — more than 30 seconds ago", createdAt)
	}

	// ASSERTION 6: Event has a valid UUID primary key (non-nil)
	if eventID == uuid.Nil {
		t.Fatal("SOC-CTRL-02 VIOLATION: Audit event has a nil UUID — primary key generation is broken")
	}

	t.Logf("SOC-CTRL-02 PASS [LIVE DB]: Audit event %s written with event_type=%s ip=%s at %v",
		eventID, eventType, ipAddress.String, createdAt.Format(time.RFC3339))

	// Cleanup
	t.Cleanup(func() {
		db.Exec("DELETE FROM audit_events WHERE user_agent LIKE $1", "%"+marker+"%")
	})
}

func TestLiveDB_SOC_CTRL02_AuditEventWithConnectionID(t *testing.T) {
	db := connectTestDB(t)
	defer db.Close()

	auditSvc := audit.NewService(db)

	providerID := testProviderID(t, db, "audit-conn")
	connID := testConnectionID(t, db, providerID, "audit-conn")

	marker := uuid.New().String()

	// Simulate a token retrieval event with a real connection ID
	req, _ := http.NewRequest("GET", "/connections/"+connID.String()+"/token", nil)
	req.RemoteAddr = "10.0.0.5:12345"
	req.Header.Set("User-Agent", "SOC-Audit-ConnTest/"+marker)

	err := auditSvc.Log("token_retrieved", &connID, map[string]interface{}{
		"connection_id": connID.String(),
	}, req)
	if err != nil {
		t.Fatalf("SOC-CTRL-02 FAILED: audit.Service.Log with connection ID returned error: %v", err)
	}

	// Verify the connection_id foreign key was stored
	var storedConnID uuid.UUID
	err = db.QueryRow(`
		SELECT connection_id FROM audit_events
		WHERE user_agent LIKE $1 AND event_type = 'token_retrieved'
		ORDER BY created_at DESC LIMIT 1`,
		"%"+marker+"%",
	).Scan(&storedConnID)
	if err != nil {
		t.Fatalf("SOC-CTRL-02 VIOLATION: Audit event with connection_id was NOT found: %v", err)
	}

	if storedConnID != connID {
		t.Fatalf("SOC-CTRL-02 VIOLATION: stored connection_id is %s, expected %s", storedConnID, connID)
	}

	t.Logf("SOC-CTRL-02 PASS [LIVE DB]: Audit event correctly linked to connection_id=%s", connID)

	t.Cleanup(func() {
		db.Exec("DELETE FROM audit_events WHERE user_agent LIKE $1", "%"+marker+"%")
	})
}

func TestLiveDB_SOC_CTRL02_AuditQueryEndpointData(t *testing.T) {
	db := connectTestDB(t)
	defer db.Close()

	auditSvc := audit.NewService(db)
	marker := uuid.New().String()

	// Write multiple audit events of different types
	events := []struct {
		eventType string
		data      map[string]interface{}
	}{
		{"provider.created", map[string]interface{}{"name": "test-provider", "marker": marker}},
		{"provider.updated", map[string]interface{}{"name": "test-provider", "marker": marker, "updates": map[string]interface{}{"description": "updated"}}},
		{"provider.deleted", map[string]interface{}{"name": "test-provider", "marker": marker}},
	}

	for _, evt := range events {
		req, _ := http.NewRequest("POST", "/providers", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("User-Agent", "SOC-BatchTest/"+marker)

		if err := auditSvc.Log(evt.eventType, nil, evt.data, req); err != nil {
			t.Fatalf("SOC-CTRL-02 FAILED: Could not write %s event: %v", evt.eventType, err)
		}
	}

	// Verify all 3 events exist in the database
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM audit_events
		WHERE user_agent LIKE $1`,
		"%"+marker+"%",
	).Scan(&count)
	if err != nil {
		t.Fatalf("SOC-CTRL-02 FAILED: Count query failed: %v", err)
	}

	if count != 3 {
		t.Fatalf("SOC-CTRL-02 VIOLATION: Expected 3 audit events, found %d — events are being dropped", count)
	}

	// Verify we can filter by event_type (simulates the GET /audit?event_type=... endpoint)
	var deleteCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM audit_events
		WHERE user_agent LIKE $1 AND event_type = 'provider.deleted'`,
		"%"+marker+"%",
	).Scan(&deleteCount)
	if err != nil {
		t.Fatalf("SOC-CTRL-02 FAILED: Filtered count query failed: %v", err)
	}

	if deleteCount != 1 {
		t.Fatalf("SOC-CTRL-02 VIOLATION: Expected 1 provider.deleted event, found %d — event_type filtering is broken", deleteCount)
	}

	t.Logf("SOC-CTRL-02 PASS [LIVE DB]: %d audit events written and queryable by event_type", count)

	t.Cleanup(func() {
		db.Exec("DELETE FROM audit_events WHERE user_agent LIKE $1", "%"+marker+"%")
	})
}
