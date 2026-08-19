package anthropicpass

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// upstreamCall is what the fake Anthropic API received.
type upstreamCall struct {
	method        string
	path          string
	rawQuery      string
	header        http.Header
	body          []byte
	contentLength int64
}

func newProvider(t *testing.T, baseURL string) *Provider {
	t.Helper()

	p, err := New(baseURL, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New(%q) returned error: %v", baseURL, err)
	}
	return p
}

// recordingUpstream starts a fake Anthropic API that reports every request it
// received on the returned channel and answers with respond.
func recordingUpstream(t *testing.T, respond http.HandlerFunc) (*httptest.Server, <-chan upstreamCall) {
	t.Helper()

	calls := make(chan upstreamCall, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("upstream could not read the request body: %v", err)
			return
		}
		calls <- upstreamCall{
			method:        r.Method,
			path:          r.URL.EscapedPath(),
			rawQuery:      r.URL.RawQuery,
			header:        r.Header.Clone(),
			body:          body,
			contentLength: r.ContentLength,
		}
		respond(w, r)
	}))
	t.Cleanup(server.Close)

	return server, calls
}

func TestName(t *testing.T) {
	p := newProvider(t, "https://api.anthropic.com")

	if got := p.Name(); got != "anthropic" {
		t.Errorf("Name() = %q, want %q", got, "anthropic")
	}
}

func TestNewRejectsAnInvalidBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
	}{
		{"empty", ""},
		{"blank", "   "},
		{"no scheme", "api.anthropic.com"},
		{"path only", "/v1"},
		{"unsupported scheme", "ftp://api.anthropic.com"},
		{"no host", "https://"},
		{"unparsable", "https://api.anthropic.com/%zz"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(tc.baseURL, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err == nil {
				t.Fatalf("New(%q) = %v, nil error, want an error", tc.baseURL, p)
			}
		})
	}
}

func TestNewAcceptsAnAbsoluteBaseURL(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:8080",
		"https://api.anthropic.com",
		"https://api.anthropic.com/",
		"https://gateway.example.com/anthropic",
	}

	for _, baseURL := range cases {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := New(baseURL, nil); err != nil {
				t.Fatalf("New(%q) returned error: %v", baseURL, err)
			}
		})
	}
}
