package anthropic

import (
	"strings"
	"testing"
)

func scan(t *testing.T, contentType string, chunks ...string) (Usage, string) {
	t.Helper()
	scanner := NewUsageScanner(contentType)
	for _, chunk := range chunks {
		if _, err := scanner.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	return scanner.Report()
}

func TestUsageScannerReadsAStream(t *testing.T) {
	start := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":1234,"cache_read_input_tokens":890,"cache_creation_input_tokens":12}}}

`
	delta := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":567}}

event: message_stop
data: {"type":"message_stop"}

`

	usage, stopReason := scan(t, "text/event-stream", start, delta)

	want := Usage{InputTokens: 1234, OutputTokens: 567, CacheReadInputTokens: 890, CacheCreationInputTokens: 12}
	if usage != want {
		t.Errorf("usage = %+v, want %+v", usage, want)
	}
	if stopReason != "tool_use" {
		t.Errorf("stopReason = %q, want %q", stopReason, "tool_use")
	}
}

// TestUsageScannerReadsAStreamSplitMidLine proves the scanner works on the
// arbitrary chunks a proxy writes, not only on whole events.
func TestUsageScannerReadsAStreamSplitMidLine(t *testing.T) {
	stream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n"

	for _, cut := range []int{1, 20, len(stream) - 3} {
		usage, _ := scan(t, "text/event-stream", stream[:cut], stream[cut:])
		if usage.InputTokens != 10 {
			t.Errorf("cut at %d: input tokens = %d, want 10", cut, usage.InputTokens)
		}
	}
}

func TestUsageScannerReadsASingleReply(t *testing.T) {
	body := `{"id":"msg_1","type":"message","role":"assistant","content":[],"stop_reason":"end_turn","usage":{"input_tokens":42,"output_tokens":7}}`

	usage, stopReason := scan(t, "application/json", body)

	want := Usage{InputTokens: 42, OutputTokens: 7}
	if usage != want {
		t.Errorf("usage = %+v, want %+v", usage, want)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want %q", stopReason, "end_turn")
	}
}

func TestUsageScannerReadsACountTokensReply(t *testing.T) {
	usage, _ := scan(t, "application/json", `{"input_tokens":515}`)

	if usage.InputTokens != 515 {
		t.Errorf("input tokens = %d, want 515", usage.InputTokens)
	}
}

func TestUsageScannerReportsNothingForAnErrorReply(t *testing.T) {
	body := `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`

	usage, stopReason := scan(t, "application/json", body)

	if usage != (Usage{}) || stopReason != "" {
		t.Errorf("usage = %+v, stopReason = %q, want both empty", usage, stopReason)
	}
}

// TestUsageScannerBoundsWhatItHolds proves the scanner cannot grow with the
// reply, because it sits in front of every response the gateway writes.
func TestUsageScannerBoundsWhatItHolds(t *testing.T) {
	huge := strings.Repeat("x", maxScannedBody+1)

	usage, _ := scan(t, "application/json", `{"usage":{"input_tokens":1},"padding":"`+huge+`"}`)

	if usage != (Usage{}) {
		t.Errorf("usage = %+v, want nothing for a reply that is too large to hold", usage)
	}
}

func TestUsageScannerSkipsAnOversizedStreamLine(t *testing.T) {
	huge := strings.Repeat("x", maxScannedBody+1)
	stream := "event: content_block_delta\ndata: {\"text\":\"" + huge + "\"}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":9}}\n\n"

	usage, stopReason := scan(t, "text/event-stream", stream)

	if usage.OutputTokens != 9 {
		t.Errorf("output tokens = %d, want 9 after a line the scanner had to drop", usage.OutputTokens)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want %q", stopReason, "end_turn")
	}
}
