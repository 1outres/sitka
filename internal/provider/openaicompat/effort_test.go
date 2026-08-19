package openaicompat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/config"
)

func withEffort(effort config.Effort) func(*config.Provider) {
	return func(cfg *config.Provider) { cfg.Effort = effort }
}

func withModelEffort(model string, effort config.Effort) func(*config.Provider) {
	return func(cfg *config.Provider) {
		if cfg.Models == nil {
			cfg.Models = map[string]config.Model{}
		}
		cfg.Models[model] = config.Model{Effort: effort}
	}
}

func effortRequest(level string) anthropic.Request {
	req := userRequest(false)
	req.OutputConfig = &anthropic.OutputConfig{Effort: level}
	return req
}

func upstreamField(t *testing.T, body []byte, field string) string {
	t.Helper()

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("decode the upstream body %q: %v", body, err)
	}
	return string(fields[field])
}

func sendEffortRequest(t *testing.T, level string, options ...func(*config.Provider)) capturedRequest {
	t.Helper()

	upstream := &upstreamRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.record(r)
		writeJSON(w, http.StatusOK, chatCompletion("ok", 1, 1))
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL, options...)
	rec := callMessages(t, p, requestBody(t, effortRequest(level)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %s", rec.Code, http.StatusOK, rec.Body)
	}
	return upstream.last(t)
}

func TestMessagesAppliesProviderEffort(t *testing.T) {
	got := sendEffortRequest(t, "high", withEffort(config.Effort{
		"high": json.RawMessage(`{"reasoning_effort":"high"}`),
	}))

	if field, want := upstreamField(t, got.body, "reasoning_effort"), `"high"`; field != want {
		t.Errorf("reasoning_effort = %s, want %s", field, want)
	}
}

func TestMessagesModelEffortReplacesTheProviderDefault(t *testing.T) {
	got := sendEffortRequest(t, "max",
		withEffort(config.Effort{"max": json.RawMessage(`{"reasoning_effort":"high"}`)}),
		withModelEffort(upstreamModelID, config.Effort{"max": json.RawMessage(`{"reasoning":{"effort":"high"}}`)}),
	)

	if field, want := upstreamField(t, got.body, "reasoning"), `{"effort":"high"}`; field != want {
		t.Errorf("reasoning = %s, want %s", field, want)
	}
	if field := upstreamField(t, got.body, "reasoning_effort"); field != "" {
		t.Errorf("reasoning_effort = %s, want it absent because the model entry replaces the provider default", field)
	}
}

func TestMessagesSendsNothingForAnUnconfiguredLevel(t *testing.T) {
	got := sendEffortRequest(t, "max", withEffort(config.Effort{
		"high": json.RawMessage(`{"reasoning_effort":"high"}`),
	}))

	if field := upstreamField(t, got.body, "reasoning_effort"); field != "" {
		t.Errorf("reasoning_effort = %s, want it absent", field)
	}
}

func TestMessagesSendsNothingWhenNoEffortIsConfigured(t *testing.T) {
	got := sendEffortRequest(t, "high")

	if field := upstreamField(t, got.body, "reasoning_effort"); field != "" {
		t.Errorf("reasoning_effort = %s, want it absent", field)
	}
}

func TestMessagesModelEffortDoesNotLeakToOtherModels(t *testing.T) {
	got := sendEffortRequest(t, "high",
		withEffort(config.Effort{"high": json.RawMessage(`{"reasoning_effort":"high"}`)}),
		withModelEffort("some-other-model", config.Effort{"high": json.RawMessage(`{"reasoning":{"effort":"low"}}`)}),
	)

	if field, want := upstreamField(t, got.body, "reasoning_effort"), `"high"`; field != want {
		t.Errorf("reasoning_effort = %s, want %s", field, want)
	}
}
