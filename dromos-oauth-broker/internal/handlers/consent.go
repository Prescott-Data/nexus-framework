package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"dromos-oauth-broker/internal/auth"
)

// ConsentSpec represents the response for consent specification
type ConsentSpec struct {
	AuthURL    string   `json:"authUrl"`
	State      string   `json:"state"`
	Scopes     []string `json:"scopes"`
	ProviderID string   `json:"provider_id"`
}

// ConsentHandler handles OAuth consent flow
type ConsentHandler struct {
	db       *sqlx.DB
	baseURL  string
	stateKey []byte
}

// NewConsentHandler creates a new consent handler
func NewConsentHandler(db *sqlx.DB, baseURL string, stateKey []byte) *ConsentHandler {
	return &ConsentHandler{
		db:       db,
		baseURL:  baseURL,
		stateKey: stateKey,
	}
}

// GetSpec handles POST /auth/consent-spec
func (h *ConsentHandler) GetSpec(w http.ResponseWriter, r *http.Request) {
	var request struct {
		WorkspaceID string   `json:"workspace_id"`
		ProviderID  string   `json:"provider_id"`
		Scopes      []string `json:"scopes"`
		ReturnURL   string   `json:"return_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if request.WorkspaceID == "" || request.ProviderID == "" || len(request.Scopes) == 0 || request.ReturnURL == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Get provider profile
	var provider struct {
		ID       uuid.UUID `db:"id"`
		AuthURL  string    `db:"auth_url"`
		ClientID string    `db:"client_id"`
		Scopes   []string  `db:"scopes"`
	}

	err := h.db.QueryRow("SELECT id, auth_url, client_id, scopes FROM provider_profiles WHERE id = $1",
		request.ProviderID).Scan(&provider.ID, &provider.AuthURL, &provider.ClientID, pq.Array(&provider.Scopes))
	if err != nil {
		log.Printf("/auth/consent-spec provider lookup error: %v", err)
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	// Generate PKCE
	codeVerifier, codeChallenge, err := auth.GeneratePKCE()
	if err != nil {
		http.Error(w, "Failed to generate PKCE", http.StatusInternalServerError)
		return
	}

	// Create connection record
	connectionID := uuid.New()
	expiresAt := time.Now().Add(10 * time.Minute)

	_, err = h.db.Exec(`
		INSERT INTO connections (id, workspace_id, provider_id, code_verifier, scopes, return_url, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		connectionID, request.WorkspaceID, request.ProviderID, codeVerifier, pq.Array(request.Scopes), request.ReturnURL, expiresAt)
	if err != nil {
		http.Error(w, "Failed to create connection", http.StatusInternalServerError)
		return
	}

	// Generate signed state
	stateData := auth.StateData{
		WorkspaceID: request.WorkspaceID,
		ProviderID:  request.ProviderID,
		Nonce:       connectionID.String(),
		IAT:         time.Now(),
	}

	signedState, err := auth.SignState(h.stateKey, stateData)
	if err != nil {
		http.Error(w, "Failed to sign state", http.StatusInternalServerError)
		return
	}

	// Build auth URL
	authURL, err := h.buildAuthURL(provider.AuthURL, provider.ClientID, signedState, codeChallenge, request.Scopes)
	if err != nil {
		http.Error(w, "Failed to build auth URL", http.StatusInternalServerError)
		return
	}

	response := ConsentSpec{
		AuthURL:    authURL,
		State:      signedState,
		Scopes:     request.Scopes,
		ProviderID: request.ProviderID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// buildAuthURL constructs the OAuth authorization URL
func (h *ConsentHandler) buildAuthURL(providerAuthURL, clientID, state, codeChallenge string, scopes []string) (string, error) {
	baseURL := strings.TrimSuffix(h.baseURL, "/")
	redirectPath := os.Getenv("REDIRECT_PATH")
	if redirectPath == "" {
		redirectPath = "/auth/callback"
	}

	u, err := url.Parse(providerAuthURL)
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", baseURL+redirectPath)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")

	u.RawQuery = q.Encode()
	return u.String(), nil
}
