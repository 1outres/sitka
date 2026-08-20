package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1outres/sitka/internal/events"
)

func routedEvent() events.Event {
	return events.Event{
		Time:          time.Date(2026, 8, 20, 15, 4, 5, 0, time.Local),
		Method:        http.MethodPost,
		Path:          "/v1/messages",
		Status:        http.StatusOK,
		DurationMS:    3412,
		Model:         "openai-gpt-5.2",
		Provider:      "openai",
		UpstreamModel: "gpt-5.2",
		Stream:        true,
		Usage:         &events.Usage{InputTokens: 1234, OutputTokens: 567, CacheReadTokens: 45000},
		StopReason:    "tool_use",
		Session:       "9f8e7d6c-0000-0000-0000-000000000000",
		Agent:         "1a2b3c4d-5e6f-7890-abcd-ef1234567890",
	}
}

func TestFormatEventShowsTheRouteAndTheTokens(t *testing.T) {
	line := formatEvent(routedEvent(), plainPalette)

	want := []string{
		"15:04:05", "200", "openai-gpt-5.2 → openai/gpt-5.2",
		"3.4s", "in=1.2k", "out=567", "cache_r=45.0k", "tool_use", "agent=1a2b3c4d",
	}
	for _, part := range want {
		if !strings.Contains(line, part) {
			t.Errorf("line %q is missing %q", line, part)
		}
	}
	if strings.Contains(line, "\x1b[") {
		t.Errorf("line %q carries colors, but they are turned off", line)
	}
}

func TestFormatEventNamesThePassthroughOnce(t *testing.T) {
	event := routedEvent()
	event.Model = "claude-opus-5"
	event.Provider = "anthropic"
	event.UpstreamModel = "claude-opus-5"

	line := formatEvent(event, plainPalette)

	if !strings.Contains(line, "claude-opus-5 → anthropic") {
		t.Errorf("line %q does not name the route", line)
	}
	if strings.Contains(line, "anthropic/") {
		t.Errorf("line %q repeats the model the upstream already knows", line)
	}
}

func TestFormatEventKeepsTheDurationInMilliseconds(t *testing.T) {
	event := routedEvent()
	event.DurationMS = 0

	if line := formatEvent(event, plainPalette); !strings.Contains(line, "0ms") {
		t.Errorf("line %q does not report a request that took under a millisecond", line)
	}
}

func TestFormatEventShowsThePathOfAnUnroutedRequest(t *testing.T) {
	event := events.Event{Method: http.MethodGet, Path: "/api/hello", Status: 404, DurationMS: 12}

	line := formatEvent(event, plainPalette)

	for _, part := range []string{"404", "GET /api/hello", "12ms"} {
		if !strings.Contains(line, part) {
			t.Errorf("line %q is missing %q", line, part)
		}
	}
}

func TestFormatEventNamesAnEndpointOtherThanMessages(t *testing.T) {
	event := routedEvent()
	event.Path = "/v1/messages/count_tokens"

	if line := formatEvent(event, plainPalette); !strings.Contains(line, "count_tokens") {
		t.Errorf("line %q does not name the endpoint", line)
	}
}

func TestFormatEventFallsBackToTheSessionWhenNoAgentRan(t *testing.T) {
	event := routedEvent()
	event.Agent = ""

	if line := formatEvent(event, plainPalette); !strings.Contains(line, "session=9f8e7d6c") {
		t.Errorf("line %q does not name the session", line)
	}
}

func TestFormatEventColorsATerminal(t *testing.T) {
	event := routedEvent()
	event.Status = http.StatusInternalServerError

	if line := formatEvent(event, colorPalette); !strings.Contains(line, colorPalette.bad) {
		t.Errorf("line %q does not color a failed request", line)
	}
}

// eventStream answers one watch request with a single event and then ends,
// standing in for a gateway that shuts down.
func eventStream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != watchPath {
			t.Errorf("watch asked for %q, want %q", r.URL.Path, watchPath)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if err := events.NewEncoder(w).Encode(routedEvent()); err != nil {
			t.Errorf("Encode: %v", err)
		}
	}))
}

func TestWatchPrintsEveryEvent(t *testing.T) {
	stream := eventStream(t)
	defer stream.Close()

	var out bytes.Buffer
	err := watch(context.Background(), strings.TrimPrefix(stream.URL, "http://"), &out, false)

	if err == nil {
		t.Error("watch = nil error, want it to report the stream that ended")
	}
	if !strings.Contains(out.String(), "openai-gpt-5.2 → openai/gpt-5.2") {
		t.Errorf("output = %q, want the routed line", out.String())
	}
}

func TestWatchCanPrintJSON(t *testing.T) {
	stream := eventStream(t)
	defer stream.Close()

	var out bytes.Buffer
	_ = watch(context.Background(), stream.URL, &out, true)

	if !strings.Contains(out.String(), `"model":"openai-gpt-5.2"`) {
		t.Errorf("output = %q, want one JSON object per event", out.String())
	}
}

func TestWatchReportsAGatewayThatIsNotThere(t *testing.T) {
	err := watch(context.Background(), "127.0.0.1:1", &bytes.Buffer{}, false)

	if err == nil {
		t.Fatal("watch = nil error, want it to report the unreachable gateway")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error = %v, want it to name the address", err)
	}
}
