package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/Prescott-Data/nexus-framework/nexus-broker/internal/audit"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/httputil"
	"github.com/Prescott-Data/nexus-framework/nexus-broker/pkg/provider"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ProvidersHandler handles provider-related HTTP requests
type ProvidersHandler struct {
	store provider.ProfileStorer
	audit *audit.Service
}

// NewProvidersHandler creates a new providers handler
func NewProvidersHandler(store provider.ProfileStorer, auditSvc *audit.Service) *ProvidersHandler {
	return &ProvidersHandler{store: store, audit: auditSvc}
}

// Get handles GET /providers/{id} to retrieve a provider profile
func (h *ProvidersHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_provider_id", "Invalid provider ID")
		return
	}
	profile, err := h.store.GetProfile(id)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "provider_not_found", "Provider not found")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, profile)
}

// Update handles PUT /providers/{id} to update a provider profile
func (h *ProvidersHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_provider_id", "Invalid provider ID")
		return
	}

	var profile provider.Profile
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	profile.ID = id

	if err := h.store.UpdateProfile(&profile); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "update_failed", "Failed to update provider profile")
		return
	}

	if h.audit != nil {
		if err := h.audit.Log("provider.updated", nil, map[string]interface{}{"provider_id": profile.ID, "name": profile.Name}, r); err != nil {
			log.Printf("audit: failed to log provider.updated for provider_id=%v: %v", profile.ID, err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// Patch handles PATCH /providers/{id} to update specific fields of a provider profile
func (h *ProvidersHandler) Patch(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_provider_id", "Invalid provider ID")
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if err := h.store.PatchProfile(id, updates); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "patch_failed", "Failed to patch provider profile")
		return
	}

	if h.audit != nil {
		if err := h.audit.Log("provider.updated", nil, map[string]interface{}{"provider_id": id.String(), "updates": updates}, r); err != nil {
			log.Printf("audit: failed to log provider.updated for provider_id=%v: %v", id, err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// Delete handles DELETE /providers/{id} to delete a provider profile
func (h *ProvidersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid_provider_id", "Invalid provider ID")
		return
	}

	if err := h.store.DeleteProfile(id); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "delete_failed", "Failed to delete provider profile")
		return
	}

	if h.audit != nil {
		if err := h.audit.Log("provider.deleted", nil, map[string]interface{}{"provider_id": id.String()}, r); err != nil {
			log.Printf("audit: failed to log provider.deleted for provider_id=%v: %v", id, err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// Register handles POST /providers for registering a new provider profile
func (h *ProvidersHandler) Register(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Profile json.RawMessage `json:"profile"`
	}

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_json",
			"message": "Invalid JSON payload",
		})
		return
	}

	if request.Profile == nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{
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

		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":   errorKey,
			"message": err.Error(),
		})
		return
	}

	if h.audit != nil {
		if err := h.audit.Log("provider.created", nil, map[string]interface{}{"provider_id": profile.ID, "name": profile.Name}, r); err != nil {
			log.Printf("audit: failed to log provider.created for provider_id=%v: %v", profile.ID, err)
		}
	}

	httputil.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"id":      profile.ID,
		"message": "Provider profile created successfully",
	})
}

// List handles GET /providers to list provider ids and names
func (h *ProvidersHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListProfiles()
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "list_failed", "Failed to list providers")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, rows)
}

// GetByName handles GET /providers/by-name/{name}
func (h *ProvidersHandler) GetByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing_name", "missing name")
		return
	}

	// Normalize to lowercase
	name = strings.ToLower(strings.TrimSpace(name))

	profile, err := h.store.GetProfileByName(name)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "provider_not_found", err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"id": profile.ID.String()})
}

// DeleteByName handles DELETE /providers/by-name/{name} to delete ALL providers with that name
func (h *ProvidersHandler) DeleteByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing_name", "missing name")
		return
	}

	rowsAffected, err := h.store.DeleteProfileByName(name)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "delete_failed", "Failed to delete provider profile")
		return
	}

	if rowsAffected == 0 {
		httputil.WriteError(w, http.StatusNotFound, "provider_not_found", "provider not found")
		return
	}

	if h.audit != nil {
		if err := h.audit.Log("provider.deleted", nil, map[string]interface{}{"provider_name": name, "rows_affected": rowsAffected}, r); err != nil {
			log.Printf("audit: failed to log provider.deleted for provider_name=%s: %v", name, err)
		}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Deleted %d provider(s)", rowsAffected)})
}

// Metadata handles GET /providers/metadata to retrieve grouped integration config
func (h *ProvidersHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	metadata, err := h.store.GetMetadata()
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "metadata_failed", "Failed to retrieve metadata")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, metadata)
}
