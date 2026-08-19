package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ErrNotFlushable reports a response writer that cannot stream.
var ErrNotFlushable = errors.New("anthropic: response writer does not support flushing")

// SSEWriter serializes events onto a streaming HTTP response. It is safe for
// concurrent use so a keep-alive ticker can share it with the event producer.
type SSEWriter struct {
	mu        sync.Mutex
	w         http.ResponseWriter
	flusher   http.Flusher
	lastWrite time.Time
	closed    bool
}

// NewSSEWriter sets the streaming response headers and returns a writer for
// them. It fails when the response writer cannot flush, because a buffered
// stream stalls the client.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrNotFlushable
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &SSEWriter{w: w, flusher: flusher, lastWrite: time.Now()}, nil
}

// Send writes one named event carrying a JSON payload and flushes it.
func (s *SSEWriter) Send(event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("anthropic: marshal %s event: %w", event, err)
	}

	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(event)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if _, err := s.w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("anthropic: write %s event: %w", event, err)
	}
	s.flusher.Flush()
	s.lastWrite = time.Now()
	return nil
}

// PingIfIdle sends a ping when nothing has been written for idle. Claude Code
// aborts a stream that stays byte-silent, and OpenAI-compatible upstreams send
// nothing while a model reasons, so the gateway generates its own pings.
func (s *SSEWriter) PingIfIdle(idle time.Duration) error {
	s.mu.Lock()
	quiet := time.Since(s.lastWrite) < idle
	s.mu.Unlock()
	if quiet {
		return nil
	}
	return s.Send(EventPing, PingEvent{Type: EventPing})
}

// Close stops the writer from emitting further events.
func (s *SSEWriter) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}
