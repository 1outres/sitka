package anthropicpass

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/provider"
)

func credentialsContext(t *testing.T) context.Context {
	t.Helper()

	return provider.ContextWithCredentials(t.Context(), provider.Credentials{
		APIKey:        "sk-ant-test",
		Authorization: "Bearer oauth-token",
	})
}

func TestModelsReturnsTheUpstreamList(t *testing.T) {
	const listBody = `{"data":[` +
		`{"id":"claude-sonnet-5","type":"model","display_name":"Claude Sonnet 5","created_at":"2026-02-19T00:00:00Z"},` +
		`{"id":"claude-opus-5","type":"model","display_name":"Claude Opus 5","created_at":"2026-01-14T00:00:00Z"}` +
		`],"has_more":false,"first_id":null,"last_id":null}`

	upstream, calls := recordingUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, listBody); err != nil {
			t.Errorf("upstream could not write the model list: %v", err)
		}
	})
	p := newProvider(t, upstream.URL)

	models, err := p.Models(credentialsContext(t))
	if err != nil {
		t.Fatalf("Models returned error: %v", err)
	}

	want := []anthropic.Model{
		{ID: "claude-sonnet-5", Type: "model", DisplayName: "Claude Sonnet 5", CreatedAt: "2026-02-19T00:00:00Z"},
		{ID: "claude-opus-5", Type: "model", DisplayName: "Claude Opus 5", CreatedAt: "2026-01-14T00:00:00Z"},
	}
	if !slices.Equal(models, want) {
		t.Errorf("Models = %+v, want %+v", models, want)
	}

	got := <-calls
	if got.method != http.MethodGet {
		t.Errorf("upstream method = %q, want %q", got.method, http.MethodGet)
	}
	if got.path != "/v1/models" {
		t.Errorf("upstream path = %q, want %q", got.path, "/v1/models")
	}
	if got.rawQuery != "limit=1000" {
		t.Errorf("upstream raw query = %q, want %q", got.rawQuery, "limit=1000")
	}
	wantHeaders := map[string]string{
		"anthropic-version": "2023-06-01",
		"x-api-key":         "sk-ant-test",
		"Authorization":     "Bearer oauth-token",
	}
	for name, value := range wantHeaders {
		if sent := got.header.Get(name); sent != value {
			t.Errorf("upstream %s = %q, want %q", name, sent, value)
		}
	}
}

func TestModelsNeedsTheClientCredentials(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("Models called the upstream without the client credentials")
	}))
	t.Cleanup(upstream.Close)
	p := newProvider(t, upstream.URL)

	models, err := p.Models(t.Context())
	if err == nil {
		t.Fatal("Models returned no error, want one when the context carries no credentials")
	}
	if models != nil {
		t.Errorf("Models = %+v, want no models", models)
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("error = %q, want it to explain that the client's own API key is missing", err)
	}
}

func TestModelsReportsAnUpstreamFailure(t *testing.T) {
	const errorBody = `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`

	cases := []struct {
		name   string
		status int
		body   string
		want   []string
	}{
		{"unauthorized", http.StatusUnauthorized, errorBody, []string{"401", "invalid x-api-key"}},
		{"server error", http.StatusInternalServerError, "upstream is down", []string{"500", "upstream is down"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream, _ := recordingUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				if _, err := io.WriteString(w, tc.body); err != nil {
					t.Errorf("upstream could not write the error body: %v", err)
				}
			})
			p := newProvider(t, upstream.URL)

			models, err := p.Models(credentialsContext(t))
			if err == nil {
				t.Fatalf("Models = %+v, nil error, want an error for status %d", models, tc.status)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to quote %q", err, want)
				}
			}
		})
	}
}

func TestModelsRejectsAnUnreadableBody(t *testing.T) {
	upstream, _ := recordingUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, "not json"); err != nil {
			t.Errorf("upstream could not write the body: %v", err)
		}
	})
	p := newProvider(t, upstream.URL)

	if _, err := p.Models(credentialsContext(t)); err == nil {
		t.Fatal("Models returned no error, want one when the upstream body is not a model list")
	}
}
