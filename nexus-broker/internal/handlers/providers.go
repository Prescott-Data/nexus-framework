package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/provider"

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

// Patch handles PATCH /providers/{id} to update specific fields of a provider profile
func (h *ProvidersHandler) Patch(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := h.store.PatchProfile(id, updates); err != nil {
		http.Error(w, "Failed to patch provider profile", http.StatusInternalServerError)
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
	w.Header().Set("Content-Type", "application/json")

	var request struct {
		Profile json.RawMessage `json:"profile"`
	}

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "invalid_json",
			"message": "Invalid JSON payload",
		})
		return
	}

	if request.Profile == nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "missing_profile",
			"message": "Missing 'profile' key in JSON",
		})
		return
	}

	// Register the profile using the store
	profile, err := h.store.RegisterProfile(string(request.Profile))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)

		// Default error key
		errorKey := "provider_creation_failed"

		// Use error prefix from store if present
		if strings.Contains(err.Error(), "name:") || strings.Contains(err.Error(), "invalid provider name") {
			errorKey = "invalid_provider_name"
		} else if strings.Contains(err.Error(), "missing required field") {
			field := strings.Split(err.Error(), ":")[1]
			errorKey = "missing_" + strings.TrimSpace(field)
		}

		json.NewEncoder(w).Encode(map[string]string{
			"error":   errorKey,
			"message": err.Error(),
		})
		return
	}

	// Success response
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      profile.ID,
		"message": "Provider profile created successfully",
	})
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

// GetByName handles GET /providers/by-name/{name}
func (h *ProvidersHandler) GetByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}

	// Normalize to lowercase
	name = strings.ToLower(strings.TrimSpace(name))

	profile, err := h.store.GetProfileByName(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": profile.ID.String()})
}

// DeleteByName handles DELETE /providers/by-name/{name} to delete ALL providers with that name
func (h *ProvidersHandler) DeleteByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}

	rowsAffected, err := h.store.DeleteProfileByName(name)
	if err != nil {
		http.Error(w, "Failed to delete provider profile", http.StatusInternalServerError)
		return
	}

	if rowsAffected == 0 {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("Deleted %d provider(s)", rowsAffected)))
}

// Metadata handles GET /providers/metadata to retrieve grouped integration config
func (h *ProvidersHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	metadata, err := h.store.GetMetadata()
	if err != nil {
		http.Error(w, "Failed to retrieve metadata", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}
