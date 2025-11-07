package handlers

import (
	"dromos-oauth-broker/internal/provider"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// ProvidersHandler handles provider-related HTTP requests
type ProvidersHandler struct {
	db    *sqlx.DB
	store *provider.Store
}

// NewProvidersHandler creates a new providers handler
func NewProvidersHandler(db *sqlx.DB) *ProvidersHandler {
	return &ProvidersHandler{db: db, store: provider.NewStore(db)}
}

// Get handles GET /providers/{id} to retrieve a provider profile
func (h *ProvidersHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/providers/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}
	profile, err := h.store.GetProfile(id)
	if err != nil {
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// Update handles PUT /providers/{id} to update a provider profile
func (h *ProvidersHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/providers/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	var profile provider.Profile
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	profile.ID = id

	if err := h.store.UpdateProfile(&profile); err != nil {
		http.Error(w, "Failed to update provider profile", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Delete handles DELETE /providers/{id} to delete a provider profile
func (h *ProvidersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/providers/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	if err := h.store.DeleteProfile(id); err != nil {
		http.Error(w, "Failed to delete provider profile", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Register handles POST /providers for registering a new provider profile
func (h *ProvidersHandler) Register(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Profile json.RawMessage `json:"profile"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate profile JSON structure
	var profile struct {
		Name         string   `json:"name"`
		AuthType     string   `json:"auth_type,omitempty"`
		AuthHeader   string   `json:"auth_header,omitempty"`
		ClientID     string   `json:"client_id,omitempty"`
		ClientSecret string   `json:"client_secret,omitempty"`
		AuthURL      string   `json:"auth_url,omitempty"`
		TokenURL     string   `json:"token_url,omitempty"`
		Scopes       []string `json:"scopes"`
	}

	if err := json.Unmarshal(request.Profile, &profile); err != nil {
		http.Error(w, "Invalid profile structure", http.StatusBadRequest)
		return
	}

	// Validate required fields based on auth_type
	switch profile.AuthType {
	case "oauth2", "": // Default to "oauth2"
		if profile.Name == "" || profile.ClientID == "" || profile.ClientSecret == "" ||
			profile.AuthURL == "" || profile.TokenURL == "" {
			http.Error(w, "Missing required OAuth2 fields (name, client_id, client_secret, auth_url, token_url)", http.StatusBadRequest)
			return
		}
	case "api_key", "basic_auth":
		if profile.Name == "" {
			http.Error(w, "Missing required field: name", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "Unsupported auth_type", http.StatusBadRequest)
		return
	}

	// Insert into database
	query := `
		INSERT INTO provider_profiles (
			name, client_id, client_secret, auth_url, token_url, scopes,
			auth_type, auth_header
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`

	var id string
	err := h.db.QueryRow(query,
		profile.Name, profile.ClientID, profile.ClientSecret,
		profile.AuthURL, profile.TokenURL, pq.Array(profile.Scopes),
		profile.AuthType, profile.AuthHeader, // <-- NEW PARAMS
	).Scan(&id)

	if err != nil {
		log.Printf("/providers insert error: %v", err)
		http.Error(w, "Failed to create provider profile", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"id":      id,
		"message": "Provider profile created successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// List handles GET /providers to list provider ids and names
func (h *ProvidersHandler) List(w http.ResponseWriter, r *http.Request) {
	type row struct {
		ID   string `db:"id" json:"id"`
		Name string `db:"name" json:"name"`
	}
	var rows []row
	if err := h.db.Select(&rows, `SELECT id, name FROM provider_profiles WHERE deleted_at IS NULL ORDER BY created_at DESC`); err != nil {
		http.Error(w, "Failed to list providers", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

// GetByName handles GET /providers/by-name/{name} with basic normalization
func (h *ProvidersHandler) GetByName(w http.ResponseWriter, r *http.Request) {
	nameEnc := strings.TrimSpace(strings.TrimPrefix(r.URL.EscapedPath(), "/providers/by-name/"))
	name, _ := url.PathUnescape(nameEnc)
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	norm := normalizeName(name)
	type row struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}
	var rows []row
	if err := h.db.Select(&rows, `SELECT id, name FROM provider_profiles WHERE deleted_at IS NULL`); err != nil {
		http.Error(w, "Failed to query providers", http.StatusInternalServerError)
		return
	}
	// Check alias table first
	var alias struct {
		ProviderID string `db:"provider_id"`
	}
	if err := h.db.Get(&alias, `SELECT provider_id FROM provider_aliases WHERE alias_norm = $1`, norm); err == nil && alias.ProviderID != "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": alias.ProviderID})
		return
	}
	// Collect exact and contains matches deterministically
	exact := make([]row, 0, 1)
	contains := make([]row, 0, 2)
	for _, r2 := range rows {
		normName := normalizeName(r2.Name)
		if normName == norm {
			exact = append(exact, r2)
		} else if strings.Contains(normName, norm) {
			contains = append(contains, r2)
		}
	}
	var chosen *row
	switch {
	case len(exact) == 1:
		chosen = &exact[0]
	case len(exact) > 1:
		http.Error(w, "provider ambiguous", http.StatusConflict)
		return
	case len(contains) == 1:
		chosen = &contains[0]
	case len(contains) > 1:
		http.Error(w, "provider ambiguous", http.StatusConflict)
		return
	default:
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": chosen.ID})
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// replace any non-alphanumeric with a single space
	nonAlnum := regexp.MustCompile(`[^a-z0-9]+`)
	s = nonAlnum.ReplaceAllString(s, " ")
	// collapse multiple spaces
	s = strings.Join(strings.Fields(s), " ")
	// unify common variants for Azure
	s = strings.ReplaceAll(s, "azure active directory", "azure ad")
	s = strings.ReplaceAll(s, "microsoft entra id", "azure ad")
	s = strings.ReplaceAll(s, "entra id", "azure ad")
	s = strings.ReplaceAll(s, "entra", "azure ad")
	return s
}
