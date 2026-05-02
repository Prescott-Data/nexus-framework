package httputil

import (
	"encoding/json"
	"log"
	"net/http"
)

// APIError is the standard JSON error body returned by all broker endpoints.
// Every failure path must use this shape so clients can parse errors uniformly.
type APIError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// WriteJSON marshals v to JSON and writes it to w with the given status code.
// If marshalling fails, a 500 error response is sent instead (headers are not
// yet committed, so this is safe). Write errors after headers are sent are
// logged but cannot be recovered from — typically a client disconnect.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "marshal_failed", "internal server error")
		log.Printf("WriteJSON: marshal failed: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		log.Printf("WriteJSON: write failed (client likely disconnected): %v", err)
	}
}

// WriteError writes a structured JSON error response. It replaces http.Error
// across all handler code to ensure clients always receive application/json.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, APIError{Error: code, Message: message})
}

// WriteErrorWithDetails writes a structured JSON error response with extra detail.
func WriteErrorWithDetails(w http.ResponseWriter, status int, code, message string, details any) {
	WriteJSON(w, status, APIError{Error: code, Message: message, Details: details})
}
