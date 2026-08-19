package anthropic

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type unflushableWriter struct{ header http.Header }

func (u *unflushableWriter) Header() http.Header         { return u.header }
func (u *unflushableWriter) Write(b []byte) (int, error) { return len(b), nil }
func (u *unflushableWriter) WriteHeader(int)             {}

func TestNewSSEWriterSetsStreamingHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	if _, err := NewSSEWriter(rec); err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	want := map[string]string{
		"Content-Type":  "text/event-stream",
		"Cache-Control": "no-cache",
	}
	for key, value := range want {
		if got := rec.Header().Get(key); got != value {
			t.Errorf("header %s = %q, want %q", key, got, value)
		}
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestNewSSEWriterRejectsUnflushableWriter(t *testing.T) {
	_, err := NewSSEWriter(&unflushableWriter{header: http.Header{}})
	if !errors.Is(err, ErrNotFlushable) {
		t.Fatalf("NewSSEWriter error = %v, want %v", err, ErrNotFlushable)
	}
}

func TestSSEWriterSendWireFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	if err := w.Send(EventMessageStop, MessageStopEvent{Type: EventMessageStop}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	want := "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestSSEWriterSendFlushesEachEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	rec.Flushed = false
	if err := w.Send(EventPing, PingEvent{Type: EventPing}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !rec.Flushed {
		t.Error("Send did not flush; a buffered stream stalls the client")
	}
}

func TestSSEWriterPingIfIdle(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}
	rec.Body.Reset()

	if err := w.PingIfIdle(time.Hour); err != nil {
		t.Fatalf("PingIfIdle: %v", err)
	}
	if got := rec.Body.String(); got != "" {
		t.Errorf("a recent write should suppress the ping, got %q", got)
	}

	if err := w.PingIfIdle(0); err != nil {
		t.Fatalf("PingIfIdle: %v", err)
	}
	if !strings.Contains(rec.Body.String(), "event: ping") {
		t.Errorf("an idle writer should ping, got %q", rec.Body.String())
	}
}

func TestSSEWriterCloseStopsFurtherEvents(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	w.Close()
	rec.Body.Reset()
	if err := w.Send(EventPing, PingEvent{Type: EventPing}); err != nil {
		t.Fatalf("Send after Close: %v", err)
	}
	if got := rec.Body.String(); got != "" {
		t.Errorf("body after Close = %q, want empty", got)
	}
}

func TestSSEWriterConcurrentSend(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Send(EventPing, PingEvent{Type: EventPing}); err != nil {
				t.Errorf("Send: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := strings.Count(rec.Body.String(), "event: ping"); got != 32 {
		t.Errorf("wrote %d ping events, want 32", got)
	}
}
