package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/provider"
	"github.com/1outres/sitka/internal/router"
)

// call records one routed call so a test can assert what the router decided.
type call struct {
	upstreamModel string
	body          string
}

type stubProvider struct {
	name string

	messages    []call
	countTokens []call
	models      []anthropic.Model
	modelsErr   error
	modelsCtx   context.Context

	handler http.HandlerFunc
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Messages(w http.ResponseWriter, _ *http.Request, upstreamModel string, body []byte) {
	s.messages = append(s.messages, call{upstreamModel: upstreamModel, body: string(body)})
	if s.handler != nil {
		s.handler(w, nil)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *stubProvider) CountTokens(w http.ResponseWriter, _ *http.Request, upstreamModel string, body []byte) {
	s.countTokens = append(s.countTokens, call{upstreamModel: upstreamModel, body: string(body)})
	w.WriteHeader(http.StatusOK)
}

func (s *stubProvider) Models(ctx context.Context) ([]anthropic.Model, error) {
	s.modelsCtx = ctx
	return s.models, s.modelsErr
}

// stubPassthrough is the fallback upstream, which also serves unrouted paths.
type stubPassthrough struct {
	stubProvider

	served []string
}

func (s *stubPassthrough) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.served = append(s.served, r.Method+" "+r.URL.Path)
	if s.handler != nil {
		s.handler(w, r)
		return
	}
	w.WriteHeader(http.StatusTeapot)
}

func newTestServer(t *testing.T, fallback *stubPassthrough, providers ...provider.Provider) http.Handler {
	t.Helper()
	routes, err := router.New(fallback, providers)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return New(routes, fallback, slog.New(slog.DiscardHandler)).Handler()
}

func post(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestMessagesRoutingByModel(t *testing.T) {
	tests := []struct {
		name              string
		model             string
		wantProvider      string
		wantUpstreamModel string
	}{
		{
			name:              "a claude model goes to the passthrough unchanged",
			model:             "claude-sonnet-5",
			wantProvider:      "anthropic",
			wantUpstreamModel: "claude-sonnet-5",
		},
		{
			name:              "a prefixed model goes to its provider without the prefix",
			model:             "openai-gpt-5.2",
			wantProvider:      "openai",
			wantUpstreamModel: "gpt-5.2",
		},
		{
			name:              "an unknown prefix falls back to the passthrough",
			model:             "mistral-large",
			wantProvider:      "anthropic",
			wantUpstreamModel: "mistral-large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
			openai := &stubProvider{name: "openai"}
			handler := newTestServer(t, fallback, openai)

			body := `{"model":"` + tt.model + `","max_tokens":16,"messages":[]}`
			if rec := post(t, handler, "/v1/messages", body); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}

			got := openai.messages
			if tt.wantProvider == "anthropic" {
				got = fallback.messages
			}
			if len(got) != 1 {
				t.Fatalf("%s received %d calls, want 1", tt.wantProvider, len(got))
			}
			if got[0].upstreamModel != tt.wantUpstreamModel {
				t.Errorf("upstreamModel = %q, want %q", got[0].upstreamModel, tt.wantUpstreamModel)
			}
			if got[0].body != body {
				t.Errorf("body = %q, want it unchanged: %q", got[0].body, body)
			}
		})
	}
}

func TestCountTokensRoutingByModel(t *testing.T) {
	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
	openai := &stubProvider{name: "openai"}
	handler := newTestServer(t, fallback, openai)

	post(t, handler, "/v1/messages/count_tokens", `{"model":"openai-gpt-5.2","messages":[]}`)

	if len(openai.countTokens) != 1 {
		t.Fatalf("openai received %d count_tokens calls, want 1", len(openai.countTokens))
	}
	if got := openai.countTokens[0].upstreamModel; got != "gpt-5.2" {
		t.Errorf("upstreamModel = %q, want %q", got, "gpt-5.2")
	}
}

func TestMessagesRejectsUnroutableBody(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "not JSON", body: `{`},
		{name: "no model field", body: `{"messages":[]}`},
		{name: "empty model", body: `{"model":"","messages":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
			handler := newTestServer(t, fallback)

			rec := post(t, handler, "/v1/messages", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			var body anthropic.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if body.Error.Type != anthropic.ErrInvalidRequest {
				t.Errorf("error type = %q, want %q", body.Error.Type, anthropic.ErrInvalidRequest)
			}
			if len(fallback.messages) != 0 {
				t.Error("an unroutable request must not reach an upstream")
			}
		})
	}
}

func TestUnhandledPathsGoToThePassthrough(t *testing.T) {
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodHead, "/api/hello"},
		{http.MethodPost, "/v1/messages/batches"},
		{http.MethodGet, "/v1/organizations/me"},
	}

	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
	handler := newTestServer(t, fallback)

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusTeapot {
			t.Errorf("%s %s reached status %d, want the passthrough's %d", tt.method, tt.path, rec.Code, http.StatusTeapot)
		}
	}

	want := []string{"HEAD /api/hello", "POST /v1/messages/batches", "GET /v1/organizations/me"}
	if len(fallback.served) != len(want) {
		t.Fatalf("passthrough served %v, want %v", fallback.served, want)
	}
	for i := range want {
		if fallback.served[i] != want[i] {
			t.Errorf("served[%d] = %q, want %q", i, fallback.served[i], want[i])
		}
	}
}

func TestModelsAggregatesEveryUpstream(t *testing.T) {
	fallback := &stubPassthrough{stubProvider: stubProvider{
		name:   "anthropic",
		models: []anthropic.Model{{ID: "claude-sonnet-5"}},
	}}
	openai := &stubProvider{
		name:   "openai",
		models: []anthropic.Model{{ID: "openai-gpt-5.2"}, {ID: "openai-o5"}},
	}
	handler := newTestServer(t, fallback, openai)

	req := httptest.NewRequest(http.MethodGet, "/v1/models?limit=1000", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var list anthropic.ModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	want := []string{"claude-sonnet-5", "openai-gpt-5.2", "openai-o5"}
	if len(list.Data) != len(want) {
		t.Fatalf("got %d models, want %d", len(list.Data), len(want))
	}
	for i, id := range want {
		if list.Data[i].ID != id {
			t.Errorf("model %d = %q, want %q", i, list.Data[i].ID, id)
		}
	}
	if list.FirstID == nil || *list.FirstID != want[0] {
		t.Errorf("FirstID = %v, want %q", list.FirstID, want[0])
	}
	if list.LastID == nil || *list.LastID != want[len(want)-1] {
		t.Errorf("LastID = %v, want %q", list.LastID, want[len(want)-1])
	}
}

func TestModelsReportsAFailingUpstream(t *testing.T) {
	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
	openai := &stubProvider{name: "openai", modelsErr: errors.New("upstream returned 401")}
	handler := newTestServer(t, fallback, openai)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}

	var body anthropic.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !strings.Contains(body.Error.Message, "openai") {
		t.Errorf("message = %q, want it to name the failing provider", body.Error.Message)
	}
}

func TestModelsPassesTheClientCredentials(t *testing.T) {
	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
	handler := newTestServer(t, fallback)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("x-api-key", "sk-ant-123")
	req.Header.Set("Authorization", "Bearer oauth-token")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	got, ok := provider.CredentialsFrom(fallback.modelsCtx)
	if !ok {
		t.Fatal("the passthrough got no credentials, but it has no key of its own")
	}
	if got.APIKey != "sk-ant-123" || got.Authorization != "Bearer oauth-token" {
		t.Errorf("credentials = %+v, want the client's own headers", got)
	}
}

// TestStreamingIsNotBuffered proves the logging wrapper keeps the response
// flushable. Claude Code stalls when a gateway holds a stream back.
func TestStreamingIsNotBuffered(t *testing.T) {
	release := make(chan struct{})
	fallback := &stubPassthrough{stubProvider: stubProvider{
		name: "anthropic",
		handler: func(w http.ResponseWriter, _ *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Error("the handler lost http.Flusher, so server-sent events cannot stream")
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
			<-release
			_, _ = io.WriteString(w, "event: message_stop\ndata: {}\n\n")
			flusher.Flush()
		},
	}}

	upstream := httptest.NewServer(newTestServer(t, fallback))
	defer upstream.Close()
	defer close(release)

	resp, err := http.Post(upstream.URL+"/v1/messages", "application/json",
		strings.NewReader(`{"model":"claude-sonnet-5","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	first := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(resp.Body).ReadString('\n')
		first <- line
	}()

	select {
	case line := <-first:
		if !strings.Contains(line, "ping") {
			t.Errorf("first line = %q, want the ping event", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first event never arrived, so the response is being buffered")
	}
}

func TestLoggingRecordsTheRoutingDecision(t *testing.T) {
	var logged bytes.Buffer
	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
	openai := &stubProvider{name: "openai"}
	routes, err := router.New(fallback, []provider.Provider{openai})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	handler := New(routes, fallback, slog.New(slog.NewJSONHandler(&logged, nil))).Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"openai-gpt-5.2","messages":[]}`))
	req.Header.Set("x-claude-code-session-id", "session-1")
	req.Header.Set("x-claude-code-agent-id", "agent-1")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	var record map[string]any
	if err := json.Unmarshal(logged.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal log line %q: %v", logged.String(), err)
	}

	want := map[string]any{
		"method":         http.MethodPost,
		"path":           "/v1/messages",
		"model":          "openai-gpt-5.2",
		"upstream_model": "gpt-5.2",
		"provider":       "openai",
		"session":        "session-1",
		"agent":          "agent-1",
	}
	for key, value := range want {
		if record[key] != value {
			t.Errorf("log %s = %v, want %v", key, record[key], value)
		}
	}
}

func TestLoggingOmitsRoutingForUnroutedPaths(t *testing.T) {
	var logged bytes.Buffer
	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
	routes, err := router.New(fallback, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	handler := New(routes, fallback, slog.New(slog.NewJSONHandler(&logged, nil))).Handler()

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodHead, "/api/hello", nil))

	var record map[string]any
	if err := json.Unmarshal(logged.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal log line %q: %v", logged.String(), err)
	}
	if _, ok := record["model"]; ok {
		t.Errorf("log carries a model for an unrouted path: %v", record)
	}
	if got := record["status"]; got != float64(http.StatusTeapot) {
		t.Errorf("log status = %v, want %d", got, http.StatusTeapot)
	}
}
