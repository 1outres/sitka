package openaicompat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/config"
)

const (
	testProviderID  = "openai"
	testAPIKey      = "sk-test-key"
	clientModel     = "openai-gpt-5.2"
	upstreamModelID = "gpt-5.2"
)

func newTestProvider(t *testing.T, baseURL string, options ...func(*config.Provider)) *Provider {
	t.Helper()

	cfg := config.Provider{ID: testProviderID, BaseURL: baseURL, APIKey: testAPIKey}
	for _, option := range options {
		option(&cfg)
	}
	p, err := New(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func withHeader(name, value string) func(*config.Provider) {
	return func(cfg *config.Provider) {
		if cfg.Headers == nil {
			cfg.Headers = map[string]string{}
		}
		cfg.Headers[name] = value
	}
}

func userRequest(stream bool) anthropic.Request {
	return anthropic.Request{
		Model:     clientModel,
		MaxTokens: 128,
		Messages: []anthropic.Message{{
			Role:    anthropic.RoleUser,
			Content: anthropic.Blocks{{Type: anthropic.BlockText, Text: "Hello"}},
		}},
		Stream: stream,
	}
}

func requestBody(t *testing.T, req anthropic.Request) []byte {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal the request: %v", err)
	}
	return body
}

func callMessages(t *testing.T, p *Provider, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	p.Messages(rec, r, upstreamModelID, body)
	return rec
}

func decodeMessage(t *testing.T, body []byte) anthropic.Response {
	t.Helper()

	var out anthropic.Response
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode the message reply %q: %v", body, err)
	}
	return out
}

func decodeError(t *testing.T, body []byte) anthropic.ErrorResponse {
	t.Helper()

	var out anthropic.ErrorResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode the error reply %q: %v", body, err)
	}
	if out.Type != "error" {
		t.Errorf("envelope type = %q, want %q", out.Type, "error")
	}
	return out
}

// startGateway serves the provider over a real connection, which the streaming
// tests need so they can read events as they arrive.
func startGateway(t *testing.T, p *Provider) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.Messages(w, r, upstreamModelID, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func postMessages(t *testing.T, gatewayURL string, body []byte) *http.Response {
	t.Helper()

	resp, err := http.Post(gatewayURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post to the gateway: %v", err)
	}
	return resp
}

type capturedRequest struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// upstreamRecorder keeps what the upstream received. The handler and the test
// run on different goroutines, so every field goes through the mutex.
type upstreamRecorder struct {
	mu       sync.Mutex
	requests []capturedRequest
}

func (u *upstreamRecorder) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests = append(u.requests, capturedRequest{
		method: r.Method,
		path:   r.URL.Path,
		header: r.Header.Clone(),
		body:   body,
	})
}

func (u *upstreamRecorder) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.requests)
}

func (u *upstreamRecorder) last(t *testing.T) capturedRequest {
	t.Helper()

	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.requests) == 0 {
		t.Fatal("the upstream received no request")
	}
	return u.requests[len(u.requests)-1]
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func chatCompletion(text string, promptTokens, completionTokens int) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-1",
		"object":  "chat.completion",
		"created": 1,
		"model":   upstreamModelID,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": text},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	}
}

func textChunk(text string) string {
	return fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":%q,"choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`, upstreamModelID, text)
}

func stopChunk() string {
	return fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":%q,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, upstreamModelID)
}

func usageChunk(promptTokens, completionTokens int) string {
	return fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":%q,"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}}`,
		upstreamModelID, promptTokens, completionTokens, promptTokens+completionTokens)
}

func startEventStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flush(w)
}

func sendChunk(w http.ResponseWriter, payload string) {
	_, _ = io.WriteString(w, "data: "+payload+"\n\n")
	flush(w)
}

func sendDone(w http.ResponseWriter) {
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flush(w)
}

func flush(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

type sseEvent struct {
	name string
	data string
}

// eventStream decodes the events of an SSE body as they arrive, so a test can
// assert on the first events before the body ends.
func eventStream(r io.Reader) <-chan sseEvent {
	out := make(chan sseEvent, 256)
	go func() {
		defer close(out)

		scanner := bufio.NewScanner(r)
		var event sseEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				event.data = strings.TrimPrefix(line, "data: ")
			case line == "":
				if event.name != "" {
					out <- event
				}
				event = sseEvent{}
			}
		}
	}()
	return out
}

func eventNames(events []sseEvent) []string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.name)
	}
	return names
}

func waitForEvent(t *testing.T, events <-chan sseEvent, name string, timeout time.Duration) []sseEvent {
	t.Helper()

	deadline := time.After(timeout)
	var seen []sseEvent
	for {
		select {
		case event, open := <-events:
			if !open {
				t.Fatalf("the stream ended before the %s event, saw %v", name, eventNames(seen))
			}
			seen = append(seen, event)
			if event.name == name {
				return seen
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the %s event, saw %v", name, eventNames(seen))
		}
	}
}

func drainEvents(t *testing.T, events <-chan sseEvent, timeout time.Duration) []sseEvent {
	t.Helper()

	deadline := time.After(timeout)
	var seen []sseEvent
	for {
		select {
		case event, open := <-events:
			if !open {
				return seen
			}
			seen = append(seen, event)
		case <-deadline:
			t.Fatalf("timed out reading the stream, saw %v", eventNames(seen))
		}
	}
}
