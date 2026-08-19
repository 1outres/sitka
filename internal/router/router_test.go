package router

import (
	"context"
	"net/http"
	"testing"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/provider"
)

type stubProvider struct {
	name string
}

func (s stubProvider) Name() string { return s.name }

func (s stubProvider) Messages(http.ResponseWriter, *http.Request, string, []byte) {}

func (s stubProvider) CountTokens(http.ResponseWriter, *http.Request, string, []byte) {}

func (s stubProvider) Models(context.Context) ([]anthropic.Model, error) { return nil, nil }

func TestNew(t *testing.T) {
	fallback := stubProvider{name: "anthropic"}

	tests := []struct {
		name      string
		fallback  provider.Provider
		providers []provider.Provider
		wantErr   bool
	}{
		{
			name:      "no prefixed providers",
			fallback:  fallback,
			providers: nil,
		},
		{
			name:      "unique names",
			fallback:  fallback,
			providers: []provider.Provider{stubProvider{name: "openai"}, stubProvider{name: "openrouter"}},
		},
		{
			name:      "missing fallback",
			fallback:  nil,
			providers: []provider.Provider{stubProvider{name: "openai"}},
			wantErr:   true,
		},
		{
			name:      "empty name",
			fallback:  fallback,
			providers: []provider.Provider{stubProvider{name: ""}},
			wantErr:   true,
		},
		{
			name:      "duplicate names",
			fallback:  fallback,
			providers: []provider.Provider{stubProvider{name: "openai"}, stubProvider{name: "openai"}},
			wantErr:   true,
		},
		{
			name:      "nil provider in list",
			fallback:  fallback,
			providers: []provider.Provider{nil},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := New(tt.fallback, tt.providers)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("New() error = nil, want an error")
				}
				if r != nil {
					t.Errorf("New() router = %v, want nil on error", r)
				}
				return
			}
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			if r == nil {
				t.Fatal("New() router = nil, want a router")
			}
		})
	}
}

func TestRouterRoute(t *testing.T) {
	fallback := stubProvider{name: "anthropic"}
	openai := stubProvider{name: "openai"}
	openrouter := stubProvider{name: "openrouter"}

	r, err := New(fallback, []provider.Provider{openai, openrouter})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name         string
		modelID      string
		wantProvider provider.Provider
		wantUpstream string
	}{
		{
			name:         "claude model goes to the fallback",
			modelID:      "claude-sonnet-5",
			wantProvider: fallback,
			wantUpstream: "claude-sonnet-5",
		},
		{
			name:         "prefix selects the provider",
			modelID:      "openai-gpt-5.2",
			wantProvider: openai,
			wantUpstream: "gpt-5.2",
		},
		{
			name:         "only the first dash is a separator",
			modelID:      "openai-gpt-4o-mini",
			wantProvider: openai,
			wantUpstream: "gpt-4o-mini",
		},
		{
			name:         "second provider is reachable",
			modelID:      "openrouter-anthropic/claude-sonnet-4",
			wantProvider: openrouter,
			wantUpstream: "anthropic/claude-sonnet-4",
		},
		{
			name:         "empty upstream model is not a match",
			modelID:      "openai-",
			wantProvider: fallback,
			wantUpstream: "openai-",
		},
		{
			name:         "no separator is not a match",
			modelID:      "openai",
			wantProvider: fallback,
			wantUpstream: "openai",
		},
		{
			name:         "unknown prefix goes to the fallback",
			modelID:      "unknown-thing",
			wantProvider: fallback,
			wantUpstream: "unknown-thing",
		},
		{
			name:         "empty model id goes to the fallback",
			modelID:      "",
			wantProvider: fallback,
			wantUpstream: "",
		},
		{
			name:         "matching is case sensitive",
			modelID:      "OPENAI-gpt-5.2",
			wantProvider: fallback,
			wantUpstream: "OPENAI-gpt-5.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, upstream := r.Route(tt.modelID)
			if got != tt.wantProvider {
				t.Errorf("Route(%q) provider = %v, want %v", tt.modelID, got, tt.wantProvider)
			}
			if upstream != tt.wantUpstream {
				t.Errorf("Route(%q) upstreamModel = %q, want %q", tt.modelID, upstream, tt.wantUpstream)
			}
		})
	}
}

func TestRouterProviders(t *testing.T) {
	fallback := stubProvider{name: "anthropic"}
	want := []provider.Provider{stubProvider{name: "openai"}, stubProvider{name: "openrouter"}}

	r, err := New(fallback, want)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got := r.Providers()
	if len(got) != len(want) {
		t.Fatalf("Providers() length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Providers()[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	got[0] = stubProvider{name: "tampered"}
	if again := r.Providers(); again[0] != want[0] {
		t.Errorf("Providers() returned a slice the caller can mutate: got %v, want %v", again[0], want[0])
	}
}

func TestRouterProvidersExcludesFallback(t *testing.T) {
	fallback := stubProvider{name: "anthropic"}

	r, err := New(fallback, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := r.Providers(); len(got) != 0 {
		t.Errorf("Providers() = %v, want no providers", got)
	}
}
