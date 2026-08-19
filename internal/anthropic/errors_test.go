package anthropic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorTypeForStatus(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{http.StatusBadRequest, ErrInvalidRequest},
		{http.StatusUnauthorized, ErrAuthentication},
		{http.StatusForbidden, ErrPermission},
		{http.StatusNotFound, ErrNotFound},
		{http.StatusRequestEntityTooLarge, ErrRequestTooLarge},
		{http.StatusTooManyRequests, ErrRateLimit},
		{529, ErrOverloaded},
		{http.StatusInternalServerError, ErrAPI},
		{http.StatusBadGateway, ErrAPI},
	}

	for _, tt := range tests {
		if got := ErrorTypeForStatus(tt.status); got != tt.want {
			t.Errorf("ErrorTypeForStatus(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, ErrInvalidRequest, "model is required")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body.Type != "error" {
		t.Errorf("Type = %q, want error", body.Type)
	}
	if body.Error.Type != ErrInvalidRequest {
		t.Errorf("Error.Type = %q, want %q", body.Error.Type, ErrInvalidRequest)
	}
	if body.Error.Message != "model is required" {
		t.Errorf("Error.Message = %q, want %q", body.Error.Message, "model is required")
	}
}
