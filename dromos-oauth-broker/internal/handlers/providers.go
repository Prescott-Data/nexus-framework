package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// ProvidersHandler handles provider-related HTTP requests
type ProvidersHandler struct {
	db *sqlx.DB
}

// NewProvidersHandler creates a new providers handler
func NewProvidersHandler(db *sqlx.DB) *ProvidersHandler {
	return &ProvidersHandler{db: db}
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
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		AuthURL      string   `json:"auth_url"`
		TokenURL     string   `json:"token_url"`
		Scopes       []string `json:"scopes"`
	}

	if err := json.Unmarshal(request.Profile, &profile); err != nil {
		http.Error(w, "Invalid profile structure", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if profile.Name == "" || profile.ClientID == "" || profile.ClientSecret == "" ||
		profile.AuthURL == "" || profile.TokenURL == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Insert into database
	query := `
		INSERT INTO provider_profiles (name, client_id, client_secret, auth_url, token_url, scopes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`

	var id string
	err := h.db.QueryRow(query, profile.Name, profile.ClientID, profile.ClientSecret,
		profile.AuthURL, profile.TokenURL, pq.Array(profile.Scopes)).Scan(&id)

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
