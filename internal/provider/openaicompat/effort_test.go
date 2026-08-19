package openaicompat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1outres/sitka/internal/config"
)

func withEffort(fields string) func(*config.Provider) {
	return func(cfg *config.Provider) { cfg.Effort = json.RawMessage(fields) }
}

func withModelEffort(model, fields string) func(*config.Provider) {
	return func(cfg *config.Provider) {
		if cfg.Models == nil {
			cfg.Models = map[string]config.Model{}
		}
		cfg.Models[model] = config.Model{Effort: json.RawMessage(fields)}
	}
}

func upstreamField(t *testing.T, body []byte, field string) string {
	t.Helper()

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("decode the upstream body %q: %v", body, err)
	}
	return string(fields[field])
}

func sendPlainRequest(t *testing.T, options ...func(*config.Provider)) capturedRequest {
	t.Helper()

	upstream := &upstreamRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.record(r)
		writeJSON(w, http.StatusOK, chatCompletion("ok", 1, 1))
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL, options...)
	rec := callMessages(t, p, requestBody(t, userRequest(false)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %s", rec.Code, http.StatusOK, rec.Body)
	}
	return upstream.last(t)
}

func TestMessagesAppliesProviderEffort(t *testing.T) {
	got := sendPlainRequest(t, withEffort(`{"reasoning_effort":"high"}`))

	if field, want := upstreamField(t, got.body, "reasoning_effort"), `"high"`; field != want {
		t.Errorf("reasoning_effort = %s, want %s", field, want)
	}
}

func TestMessagesModelEffortReplacesTheProviderSetting(t *testing.T) {
	got := sendPlainRequest(t,
		withEffort(`{"reasoning_effort":"high"}`),
		withModelEffort(upstreamModelID, `{"reasoning":{"effort":"max"}}`),
	)

	if field, want := upstreamField(t, got.body, "reasoning"), `{"effort":"max"}`; field != want {
		t.Errorf("reasoning = %s, want %s", field, want)
	}
	if field := upstreamField(t, got.body, "reasoning_effort"); field != "" {
		t.Errorf("reasoning_effort = %s, want it absent because the model entry replaces the provider setting", field)
	}
}

func TestMessagesModelEffortDoesNotLeakToOtherModels(t *testing.T) {
	got := sendPlainRequest(t,
		withEffort(`{"reasoning_effort":"high"}`),
		withModelEffort("some-other-model", `{"reasoning_effort":"low"}`),
	)

	if field, want := upstreamField(t, got.body, "reasoning_effort"), `"high"`; field != want {
		t.Errorf("reasoning_effort = %s, want %s", field, want)
	}
}

func TestMessagesSendsNothingWhenNoEffortIsConfigured(t *testing.T) {
	got := sendPlainRequest(t)

	if field := upstreamField(t, got.body, "reasoning_effort"); field != "" {
		t.Errorf("reasoning_effort = %s, want it absent", field)
	}
}
