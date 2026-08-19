package openaicompat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1outres/sitka/internal/anthropic"
)

func TestCountTokensIsNotServed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("CountTokens called the upstream, want no call at all")
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", http.NoBody)

	p.CountTokens(rec, r, upstreamModelID, requestBody(t, userRequest(false)))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	got := decodeError(t, rec.Body.Bytes())
	if got.Error.Type != anthropic.ErrNotFound {
		t.Errorf("error type = %q, want %q", got.Error.Type, anthropic.ErrNotFound)
	}
	if !strings.Contains(got.Error.Message, "messages endpoint") {
		t.Errorf("error message = %q, want it to point at the messages endpoint", got.Error.Message)
	}
}
