package events

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// flushCounter stands in for an HTTP response writer, so a test can prove the
// encoder pushes each event out instead of leaving it in a buffer.
type flushCounter struct {
	bytes.Buffer
	flushes int
}

func (f *flushCounter) Flush() { f.flushes++ }

func TestEncodeDecodeRoundTrip(t *testing.T) {
	want := Event{
		Time:          time.Date(2026, 8, 20, 15, 4, 5, 0, time.UTC),
		Method:        "POST",
		Path:          "/v1/messages",
		Status:        200,
		DurationMS:    3412,
		Model:         "openai-gpt-5.2",
		Provider:      "openai",
		UpstreamModel: "gpt-5.2",
		Stream:        true,
		Usage:         &Usage{InputTokens: 1234, OutputTokens: 567, CacheReadTokens: 89},
		StopReason:    "tool_use",
		Session:       "session-1",
		Agent:         "agent-1",
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(want); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoder := NewDecoder(&buf)
	got, err := decoder.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("event = %+v, want %+v", got, want)
	}
	if _, err := decoder.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("second Next = %v, want io.EOF", err)
	}
}

func TestEncoderFlushesEveryEvent(t *testing.T) {
	writer := &flushCounter{}
	encoder := NewEncoder(writer)

	if err := encoder.Encode(Event{Model: "openai-gpt-5.2"}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := encoder.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if writer.flushes != 2 {
		t.Errorf("flushes = %d, want 2", writer.flushes)
	}
	if !strings.HasSuffix(writer.String(), "\n\n") {
		t.Errorf("stream = %q, want it to end with a blank line", writer.String())
	}
}

func TestDecoderSkipsPingsAndBlankLines(t *testing.T) {
	stream := ": ping\n\n\nevent: request\ndata: {\"model\":\"openai-gpt-5.2\"}\n\n"

	got, err := NewDecoder(strings.NewReader(stream)).Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Model != "openai-gpt-5.2" {
		t.Errorf("model = %q, want %q", got.Model, "openai-gpt-5.2")
	}
}

func TestDecoderReportsABrokenPayload(t *testing.T) {
	_, err := NewDecoder(strings.NewReader("data: {\n\n")).Next()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("Next = %v, want a decode error", err)
	}
}
