package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// streamBufferSize bounds one SSE line. Tool call argument fragments can be
// large, so this is well above bufio's 64 KiB default.
const streamBufferSize = 8 << 20

// doneMarker ends an OpenAI stream.
const doneMarker = "[DONE]"

// Chunk is one server-sent event of a streaming reply.
type Chunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ChunkChoice is one completion within a chunk.
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// ChunkDelta is the incremental payload of a chunk.
type ChunkDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   *string    `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Refusal   *string    `json:"refusal,omitempty"`
}

// StreamReader decodes an OpenAI server-sent event stream.
type StreamReader struct {
	scanner *bufio.Scanner
	done    bool
}

// NewStreamReader reads chunks from an SSE body.
func NewStreamReader(r io.Reader) *StreamReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), streamBufferSize)
	return &StreamReader{scanner: scanner}
}

// Next returns the next chunk. It returns io.EOF once the stream ends, either
// at the done marker or when the body closes.
func (s *StreamReader) Next() (*Chunk, error) {
	if s.done {
		return nil, io.EOF
	}
	for s.scanner.Scan() {
		line := bytes.TrimRight(s.scanner.Bytes(), "\r")
		if len(line) == 0 || line[0] == ':' {
			continue
		}
		payload, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue
		}
		payload = bytes.TrimSpace(payload)
		if string(payload) == doneMarker {
			s.done = true
			return nil, io.EOF
		}
		if len(payload) == 0 {
			continue
		}
		var chunk Chunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return nil, fmt.Errorf("openai: decode stream chunk: %w", err)
		}
		return &chunk, nil
	}
	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("openai: read stream: %w", err)
	}
	s.done = true
	return nil, io.EOF
}
