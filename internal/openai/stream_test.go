package openai

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func collect(t *testing.T, body string) []*Chunk {
	t.Helper()
	reader := NewStreamReader(strings.NewReader(body))

	var chunks []*Chunk
	for {
		chunk, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return chunks
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		chunks = append(chunks, chunk)
	}
}

func TestStreamReaderNext(t *testing.T) {
	body := strings.Join([]string{
		": keep alive comment",
		"",
		`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"He"}}]}`,
		"",
		`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"llo"}}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	chunks := collect(t, body)
	if len(chunks) != 2 {
		t.Fatalf("read %d chunks, want 2", len(chunks))
	}
	if got := *chunks[0].Choices[0].Delta.Content; got != "He" {
		t.Errorf("first delta = %q, want %q", got, "He")
	}
	if got := *chunks[1].Choices[0].Delta.Content; got != "llo" {
		t.Errorf("second delta = %q, want %q", got, "llo")
	}
}

func TestStreamReaderStopsAtDoneMarker(t *testing.T) {
	body := "data: [DONE]\n\ndata: {\"id\":\"after\"}\n\n"
	if chunks := collect(t, body); len(chunks) != 0 {
		t.Fatalf("read %d chunks after [DONE], want 0", len(chunks))
	}
}

func TestStreamReaderEOFWithoutDoneMarker(t *testing.T) {
	body := `data: {"id":"c1","choices":[]}` + "\n\n"
	if chunks := collect(t, body); len(chunks) != 1 {
		t.Fatalf("read %d chunks, want 1", len(chunks))
	}
}

func TestStreamReaderIsIdempotentAfterEOF(t *testing.T) {
	reader := NewStreamReader(strings.NewReader("data: [DONE]\n\n"))
	for i := range 3 {
		if _, err := reader.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("call %d: Next error = %v, want io.EOF", i, err)
		}
	}
}

func TestStreamReaderRejectsMalformedChunk(t *testing.T) {
	reader := NewStreamReader(strings.NewReader("data: {not json}\n\n"))
	if _, err := reader.Next(); err == nil {
		t.Fatal("Next = nil error, want a decode error")
	}
}

func TestStreamReaderReadsToolCallDeltas(t *testing.T) {
	body := `data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]}}]}` + "\n\n"

	chunks := collect(t, body)
	if len(chunks) != 1 {
		t.Fatalf("read %d chunks, want 1", len(chunks))
	}
	call := chunks[0].Choices[0].Delta.ToolCalls[0]
	if call.Index == nil || *call.Index != 0 {
		t.Errorf("tool call Index = %v, want 0", call.Index)
	}
	if call.ID != "call_1" || call.Function.Name != "read" {
		t.Errorf("tool call = %+v, want id call_1 and name read", call)
	}
}

func TestStreamReaderReadsFinalUsageChunk(t *testing.T) {
	body := `data: {"id":"c1","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}` + "\n\n"

	chunks := collect(t, body)
	if len(chunks) != 1 {
		t.Fatalf("read %d chunks, want 1", len(chunks))
	}
	if chunks[0].Usage == nil {
		t.Fatal("Usage = nil, want the final token counts")
	}
	if chunks[0].Usage.PromptTokens != 11 || chunks[0].Usage.CompletionTokens != 7 {
		t.Errorf("Usage = %+v, want 11 prompt and 7 completion tokens", *chunks[0].Usage)
	}
}

func TestStreamReaderHandlesCarriageReturns(t *testing.T) {
	body := "data: {\"id\":\"c1\",\"choices\":[]}\r\n\r\ndata: [DONE]\r\n\r\n"
	if chunks := collect(t, body); len(chunks) != 1 {
		t.Fatalf("read %d chunks, want 1", len(chunks))
	}
}
