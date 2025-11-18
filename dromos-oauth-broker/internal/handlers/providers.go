package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"dromos.com/oauth-broker/internal/provider"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ProvidersHandler handles provider-related HTTP requests
type ProvidersHandler struct {
	store provider.ProfileStorer
}

// NewProvidersHandler creates a new providers handler
func NewProvidersHandler(store provider.ProfileStorer) *ProvidersHandler {
	return &ProvidersHandler{store: store}
}

// Get handles GET /providers/{id} to retrieve a provider profile
func (h *ProvidersHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
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
	idStr := chi.URLParam(r, "id")
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
	idStr := chi.URLParam(r, "id")
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

	if request.Profile == nil {
		http.Error(w, "Invalid JSON: missing 'profile' key", http.StatusBadRequest)
		return
	}

	// Call the store, which now contains all validation and SQL logic.
	// The RegisterProfile function takes a string, so we just pass the RawMessage.
	profile, err := h.store.RegisterProfile(string(request.Profile))
	if err != nil {
		// The store's validation error will be passed back.
		// We can return a 400 since it's most likely a validation failure.
		http.Error(w, fmt.Sprintf("Failed to create provider: %v", err), http.StatusBadRequest)
		return
	}

	// This part stays the same
	response := map[string]interface{}{
		"id":      profile.ID,
		"message": "Provider profile created successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// List handles GET /providers to list provider ids and names
func (h *ProvidersHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListProfiles()
	if err != nil {
		http.Error(w, "Failed to list providers", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

// GetByName handles GET /providers/by-name/{name} with basic normalization
func (h *ProvidersHandler) GetByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	norm := normalizeName(name)

	profile, err := h.store.GetProfileByName(norm)
	if err != nil {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": profile.ID.String()})
}

// DeleteByName handles DELETE /providers/by-name/{name} to delete a provider by name
func (h *ProvidersHandler) DeleteByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	norm := normalizeName(name)

	profile, err := h.store.GetProfileByName(norm)
	if err != nil {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	if err := h.store.DeleteProfile(profile.ID); err != nil {
		http.Error(w, "Failed to delete provider profile", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
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
