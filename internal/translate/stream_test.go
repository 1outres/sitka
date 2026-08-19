package translate

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
)

const streamModel = "openai-gpt-5.2"

type sseEvent struct {
	name string
	data string
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil
	}
	var events []sseEvent
	for _, frame := range strings.Split(trimmed, "\n\n") {
		var event sseEvent
		for _, line := range strings.Split(frame, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				event.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				event.data = strings.TrimPrefix(line, "data: ")
			default:
				t.Fatalf("unexpected sse line %q", line)
			}
		}
		events = append(events, event)
	}
	return events
}

func runStream(t *testing.T, chunks string) ([]sseEvent, error) {
	t.Helper()
	recorder := httptest.NewRecorder()
	writer, err := anthropic.NewSSEWriter(recorder)
	if err != nil {
		t.Fatalf("NewSSEWriter() error = %v", err)
	}
	streamErr := Stream(openai.NewStreamReader(strings.NewReader(chunks)), writer, streamModel)
	return parseSSE(t, recorder.Body.String()), streamErr
}

func assertEvents(t *testing.T, got, want []sseEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d\ngot: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].name != want[i].name {
			t.Errorf("event %d name = %q, want %q", i, got[i].name, want[i].name)
			continue
		}
		if !jsonEqual(decodeJSON(t, []byte(got[i].data)), decodeJSON(t, []byte(want[i].data))) {
			t.Errorf("event %d (%s) data\ngot:  %s\nwant: %s", i, got[i].name, got[i].data, want[i].data)
		}
	}
}

func messageStart(id string, inputTokens int) sseEvent {
	return sseEvent{
		name: anthropic.EventMessageStart,
		data: `{"type":"message_start","message":{"id":"` + id + `","type":"message","role":"assistant",
			"model":"` + streamModel + `","content":[],"stop_reason":null,"stop_sequence":null,
			"usage":{"input_tokens":` + strconv.Itoa(inputTokens) + `,"output_tokens":0}}}`,
	}
}

func TestStream(t *testing.T) {
	tests := []struct {
		name   string
		chunks string
		want   []sseEvent
	}{
		{
			name: "text only",
			chunks: `data: {"id":"chatcmpl-1","model":"gpt-5.2","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"Hel"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}

data: [DONE]

`,
			want: []sseEvent{
				messageStart("chatcmpl-1", 0),
				{anthropic.EventContentBlockStart, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
				{anthropic.EventContentBlockDelta, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`},
				{anthropic.EventContentBlockDelta, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`},
				{anthropic.EventContentBlockStop, `{"type":"content_block_stop","index":0}`},
				{anthropic.EventMessageDelta, `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":7,"output_tokens":2}}`},
				{anthropic.EventMessageStop, `{"type":"message_stop"}`},
			},
		},
		{
			name: "tool call only",
			chunks: `data: {"id":"chatcmpl-2","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]}}]}

data: {"id":"chatcmpl-2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\""}}]}}]}

data: {"id":"chatcmpl-2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"/tmp\"}"}}]}}]}

data: {"id":"chatcmpl-2","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`,
			want: []sseEvent{
				messageStart("chatcmpl-2", 0),
				{anthropic.EventContentBlockStart, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"read","input":{}}}`},
				{anthropic.EventContentBlockDelta, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\""}}`},
				{anthropic.EventContentBlockDelta, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":\"/tmp\"}"}}`},
				{anthropic.EventContentBlockStop, `{"type":"content_block_stop","index":0}`},
				{anthropic.EventMessageDelta, `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`},
				{anthropic.EventMessageStop, `{"type":"message_stop"}`},
			},
		},
		{
			name: "text then tool call",
			chunks: `data: {"id":"chatcmpl-3","choices":[{"index":0,"delta":{"content":"Let me read it."}}]}

data: {"id":"chatcmpl-3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]}}]}

data: {"id":"chatcmpl-3","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`,
			want: []sseEvent{
				messageStart("chatcmpl-3", 0),
				{anthropic.EventContentBlockStart, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
				{anthropic.EventContentBlockDelta, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me read it."}}`},
				{anthropic.EventContentBlockStop, `{"type":"content_block_stop","index":0}`},
				{anthropic.EventContentBlockStart, `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"read","input":{}}}`},
				{anthropic.EventContentBlockDelta, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`},
				{anthropic.EventContentBlockStop, `{"type":"content_block_stop","index":1}`},
				{anthropic.EventMessageDelta, `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`},
				{anthropic.EventMessageStop, `{"type":"message_stop"}`},
			},
		},
		{
			name: "two parallel tool calls after text",
			chunks: `data: {"id":"chatcmpl-4","choices":[{"index":0,"delta":{"content":"working"}}]}

data: {"id":"chatcmpl-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":""}}]}}]}

data: {"id":"chatcmpl-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"list","arguments":""}}]}}]}

data: {"id":"chatcmpl-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"p\":1}"}}]}}]}

data: {"id":"chatcmpl-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"q\":2}"}}]}}]}

data: {"id":"chatcmpl-4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`,
			want: []sseEvent{
				messageStart("chatcmpl-4", 0),
				{anthropic.EventContentBlockStart, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
				{anthropic.EventContentBlockDelta, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"working"}}`},
				{anthropic.EventContentBlockStop, `{"type":"content_block_stop","index":0}`},
				{anthropic.EventContentBlockStart, `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_a","name":"read","input":{}}}`},
				{anthropic.EventContentBlockStart, `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_b","name":"list","input":{}}}`},
				{anthropic.EventContentBlockDelta, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"p\":1}"}}`},
				{anthropic.EventContentBlockDelta, `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":2}"}}`},
				{anthropic.EventContentBlockStop, `{"type":"content_block_stop","index":1}`},
				{anthropic.EventContentBlockStop, `{"type":"content_block_stop","index":2}`},
				{anthropic.EventMessageDelta, `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`},
				{anthropic.EventMessageStop, `{"type":"message_stop"}`},
			},
		},
		{
			name:   "empty stream",
			chunks: "data: [DONE]\n\n",
			want: []sseEvent{
				messageStart("", 0),
				{anthropic.EventMessageDelta, `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`},
				{anthropic.EventMessageStop, `{"type":"message_stop"}`},
			},
		},
		{
			name:   "no chunks at all",
			chunks: "",
			want: []sseEvent{
				messageStart("", 0),
				{anthropic.EventMessageDelta, `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`},
				{anthropic.EventMessageStop, `{"type":"message_stop"}`},
			},
		},
		{
			name: "usage only final chunk",
			chunks: `data: {"id":"chatcmpl-6","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}

data: [DONE]

`,
			want: []sseEvent{
				messageStart("chatcmpl-6", 5),
				{anthropic.EventMessageDelta, `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":5,"output_tokens":0}}`},
				{anthropic.EventMessageStop, `{"type":"message_stop"}`},
			},
		},
		{
			name: "empty content deltas open no block",
			chunks: `data: {"id":"chatcmpl-7","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}

data: {"id":"chatcmpl-7","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}

data: [DONE]

`,
			want: []sseEvent{
				messageStart("chatcmpl-7", 0),
				{anthropic.EventMessageDelta, `{"type":"message_delta","delta":{"stop_reason":"max_tokens","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`},
				{anthropic.EventMessageStop, `{"type":"message_stop"}`},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runStream(t, tt.chunks)
			if err != nil {
				t.Fatalf("Stream() error = %v", err)
			}
			assertEvents(t, got, tt.want)
		})
	}
}

func TestStreamUpstreamDecodeError(t *testing.T) {
	chunks := `data: {"id":"chatcmpl-8","choices":[{"index":0,"delta":{"content":"hi"}}]}

data: {"id":

`
	got, err := runStream(t, chunks)
	if err == nil {
		t.Fatal("Stream() error = nil, want a decode error")
	}
	if len(got) == 0 {
		t.Fatal("Stream() wrote no events")
	}
	last := got[len(got)-1]
	if last.name != anthropic.EventError {
		t.Fatalf("last event = %q, want %q", last.name, anthropic.EventError)
	}
	payload, ok := decodeJSON(t, []byte(last.data)).(map[string]any)
	if !ok {
		t.Fatalf("error event payload is not an object: %s", last.data)
	}
	detail, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("error event has no error detail: %s", last.data)
	}
	if detail["type"] != anthropic.ErrAPI {
		t.Errorf("error type = %v, want %q", detail["type"], anthropic.ErrAPI)
	}
	if message, _ := detail["message"].(string); message == "" {
		t.Errorf("error message is empty: %s", last.data)
	}
}

var errClientGone = errors.New("client gone")

// brokenWriter accepts works writes and fails every write after them.
type brokenWriter struct {
	header http.Header
	works  int
	writes int
}

func (w *brokenWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *brokenWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writes > w.works {
		return 0, errClientGone
	}
	return len(data), nil
}

func (w *brokenWriter) WriteHeader(int) {}

func (w *brokenWriter) Flush() {}

func streamToBrokenWriter(t *testing.T, chunks string, works int) error {
	t.Helper()
	writer, err := anthropic.NewSSEWriter(&brokenWriter{works: works})
	if err != nil {
		t.Fatalf("NewSSEWriter() error = %v", err)
	}
	return Stream(openai.NewStreamReader(strings.NewReader(chunks)), writer, streamModel)
}

func TestStreamWriteError(t *testing.T) {
	chunks := `data: {"id":"chatcmpl-9","choices":[{"index":0,"delta":{"content":"hi"}}]}

data: {"id":"chatcmpl-9","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]}}]}

data: {"id":"chatcmpl-9","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	// One case per event of the sequence, so every send failure aborts.
	for works := range 9 {
		t.Run("fails after "+strconv.Itoa(works)+" events", func(t *testing.T) {
			if err := streamToBrokenWriter(t, chunks, works); !errors.Is(err, errClientGone) {
				t.Fatalf("Stream() error = %v, want %v", err, errClientGone)
			}
		})
	}
}

func TestStreamErrorEventWriteError(t *testing.T) {
	err := streamToBrokenWriter(t, "data: {\n\n", 0)
	if !errors.Is(err, errClientGone) {
		t.Errorf("Stream() error = %v, want it to report the write failure", err)
	}
	if !strings.Contains(err.Error(), "decode stream chunk") {
		t.Errorf("Stream() error = %v, want it to report the upstream failure too", err)
	}
}

func TestStreamToolCallWithoutIndex(t *testing.T) {
	chunks := `data: {"id":"chatcmpl-10","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]}}]}

data: [DONE]

`
	_, err := runStream(t, chunks)
	if !errors.Is(err, ErrToolCallIndex) {
		t.Fatalf("Stream() error = %v, want %v", err, ErrToolCallIndex)
	}
}
