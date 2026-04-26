package httputil

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON_Success(t *testing.T) {
	w := httptest.NewRecorder()
	payload := map[string]string{"hello": "world"}

	WriteJSON(w, http.StatusOK, payload)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if got["hello"] != "world" {
		t.Fatalf("expected hello=world, got %q", got["hello"])
	}
}

func TestWriteJSON_CustomStatus(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusCreated, map[string]string{"id": "abc"})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}
}

func TestWriteJSON_MarshalFailure(t *testing.T) {
	w := httptest.NewRecorder()

	// math.NaN is not representable in JSON — Marshal will fail.
	WriteJSON(w, http.StatusOK, math.NaN())

	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500 on marshal failure, got %d", resp.StatusCode)
	}
}

func TestWriteJSON_NilPayload(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, nil)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if body := w.Body.String(); body != "null" {
		t.Fatalf("expected null body, got %q", body)
	}
}

func TestWriteJSON_EmptySlice(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, []string{})

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if body := w.Body.String(); body != "[]" {
		t.Fatalf("expected [] body, got %q", body)
	}
}

func TestWriteError_StructuredJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusBadRequest, "invalid_id", "Invalid provider ID")

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}

	var got APIError
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if got.Error != "invalid_id" {
		t.Errorf("expected error code invalid_id, got %q", got.Error)
	}
	if got.Message != "Invalid provider ID" {
		t.Errorf("expected message 'Invalid provider ID', got %q", got.Message)
	}
	if got.Details != nil {
		t.Errorf("expected nil details, got %v", got.Details)
	}
}

func TestWriteError_500(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusInternalServerError, "internal_error", "something broke")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestWriteErrorWithDetails(t *testing.T) {
	w := httptest.NewRecorder()
	WriteErrorWithDetails(w, http.StatusUnprocessableEntity, "validation_failed", "bad input",
		map[string]string{"field": "email"})

	var got APIError
	if err := json.NewDecoder(w.Result().Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got.Error != "validation_failed" {
		t.Errorf("expected error code validation_failed, got %q", got.Error)
	}
	if got.Details == nil {
		t.Fatal("expected non-nil details")
	}
}
