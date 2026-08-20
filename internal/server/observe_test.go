package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1outres/sitka/internal/events"
)

// streamedReply is what a provider writes for a streaming request, cut down to
// the events that carry token counts.
const streamedReply = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":120,\"cache_read_input_tokens\":900}}}\n\n" +
	"event: message_delta\n" +
	"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":34}}\n\n"

func writeStreamedReply(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(streamedReply))
}

func waitForEvent(t *testing.T, ch <-chan events.Event) events.Event {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("no event was published")
		return events.Event{}
	}
}

func TestEventReportsTheRouteAndTheTokens(t *testing.T) {
	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
	openai := &stubProvider{name: "openai", handler: writeStreamedReply}
	gateway := newTestGateway(t, fallback, openai)

	watched, cancel := gateway.events.Subscribe()
	defer cancel()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"openai-gpt-5.2","stream":true,"messages":[]}`))
	req.Header.Set("x-claude-code-session-id", "session-1")
	req.Header.Set("x-claude-code-agent-id", "agent-1")
	gateway.Handler().ServeHTTP(httptest.NewRecorder(), req)

	event := waitForEvent(t, watched)

	if event.Model != "openai-gpt-5.2" || event.Provider != "openai" || event.UpstreamModel != "gpt-5.2" {
		t.Errorf("route = %s -> %s/%s, want openai-gpt-5.2 -> openai/gpt-5.2",
			event.Model, event.Provider, event.UpstreamModel)
	}
	if !event.Stream {
		t.Error("Stream = false, want true for a streamed request")
	}
	if event.Status != http.StatusOK {
		t.Errorf("Status = %d, want %d", event.Status, http.StatusOK)
	}
	if event.Session != "session-1" || event.Agent != "agent-1" {
		t.Errorf("session = %q, agent = %q, want the header values", event.Session, event.Agent)
	}
	if event.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", event.StopReason, "end_turn")
	}
	want := events.Usage{InputTokens: 120, OutputTokens: 34, CacheReadTokens: 900}
	if event.Usage == nil || *event.Usage != want {
		t.Errorf("Usage = %+v, want %+v", event.Usage, want)
	}
}

func TestEventReportsTheTokensOfASingleReply(t *testing.T) {
	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic", handler: func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","stop_reason":"tool_use","usage":{"input_tokens":42,"output_tokens":7}}`))
	}}}
	gateway := newTestGateway(t, fallback)

	watched, cancel := gateway.events.Subscribe()
	defer cancel()

	post(t, gateway.Handler(), "/v1/messages", `{"model":"claude-opus-5","messages":[]}`)

	event := waitForEvent(t, watched)
	want := events.Usage{InputTokens: 42, OutputTokens: 7}
	if event.Usage == nil || *event.Usage != want {
		t.Errorf("Usage = %+v, want %+v", event.Usage, want)
	}
	if event.Stream {
		t.Error("Stream = true, want false for a request that did not ask for one")
	}
}

func TestEventForAnUnroutedPathCarriesNoModel(t *testing.T) {
	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
	gateway := newTestGateway(t, fallback)

	watched, cancel := gateway.events.Subscribe()
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	gateway.Handler().ServeHTTP(httptest.NewRecorder(), req)

	event := waitForEvent(t, watched)
	if event.Model != "" || event.Provider != "" {
		t.Errorf("route = %q/%q, want it empty for a path that reached no model", event.Model, event.Provider)
	}
	if event.Path != "/api/hello" || event.Method != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/hello", event.Method, event.Path)
	}
	if event.Status != http.StatusTeapot {
		t.Errorf("Status = %d, want %d", event.Status, http.StatusTeapot)
	}
}

// TestWatchStreamsEventsAsTheyHappen proves a watcher started before a request
// sees it without waiting for anything else.
func TestWatchStreamsEventsAsTheyHappen(t *testing.T) {
	fallback := &stubPassthrough{stubProvider: stubProvider{name: "anthropic"}}
	openai := &stubProvider{name: "openai", handler: writeStreamedReply}
	gateway := newTestGateway(t, fallback, openai)

	upstream := httptest.NewServer(gateway.Handler())
	defer upstream.Close()

	watch, err := http.Get(upstream.URL + watchPath)
	if err != nil {
		t.Fatalf("Get %s: %v", watchPath, err)
	}
	defer func() { _ = watch.Body.Close() }()
	if watch.StatusCode != http.StatusOK {
		t.Fatalf("watch status = %d, want %d", watch.StatusCode, http.StatusOK)
	}
	if got := watch.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("Content-Type = %q, want an event stream", got)
	}

	reply, err := http.Post(upstream.URL+"/v1/messages", "application/json",
		strings.NewReader(`{"model":"openai-gpt-5.2","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	_ = reply.Body.Close()

	decoded := make(chan events.Event, 1)
	go func() {
		event, err := events.NewDecoder(watch.Body).Next()
		if err != nil {
			t.Errorf("Next: %v", err)
			return
		}
		decoded <- event
	}()

	select {
	case event := <-decoded:
		if event.Model != "openai-gpt-5.2" {
			t.Errorf("model = %q, want %q", event.Model, "openai-gpt-5.2")
		}
		if event.Usage == nil || event.Usage.OutputTokens != 34 {
			t.Errorf("Usage = %+v, want the token counts of the reply", event.Usage)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the watcher received no event")
	}
}
