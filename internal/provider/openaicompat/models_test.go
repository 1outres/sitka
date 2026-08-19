package openaicompat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/1outres/sitka/internal/anthropic"
)

func TestModelsPrefixesIDsWithTheProviderID(t *testing.T) {
	recorder := &upstreamRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		writeJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "gpt-5.2", "object": "model"},
				{"id": "o4-mini", "object": "model"},
			},
		})
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL, withHeader("X-Title", "sitka"))
	got, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}

	want := []anthropic.Model{
		{ID: "openai-gpt-5.2", Type: "model", DisplayName: "gpt-5.2"},
		{ID: "openai-o4-mini", Type: "model", DisplayName: "o4-mini"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %+v, want %+v", got, want)
	}

	sent := recorder.last(t)
	if sent.method != http.MethodGet || sent.path != "/models" {
		t.Errorf("upstream call = %s %s, want GET /models", sent.method, sent.path)
	}
	if want := "Bearer " + testAPIKey; sent.header.Get("Authorization") != want {
		t.Errorf("Authorization = %q, want %q", sent.header.Get("Authorization"), want)
	}
	if sent.header.Get("X-Title") != "sitka" {
		t.Errorf("X-Title = %q, want %q", sent.header.Get("X-Title"), "sitka")
	}
}

func TestModelsFailsOnUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid api key"))
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	_, err := p.Models(context.Background())
	if err == nil {
		t.Fatal("Models returned no error, want one naming the upstream status")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error = %q, want it to quote the status and the body", err)
	}
}

func TestModelsFailsOnUndecodableBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>not a model list</html>"))
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	if _, err := p.Models(context.Background()); err == nil {
		t.Fatal("Models returned no error, want one for the undecodable body")
	}
}
