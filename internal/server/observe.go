package server

import (
	"context"
	"net/http"
	"time"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/events"
)

// Headers Claude Code sends so that one gateway can tell its sessions and
// subagents apart.
const (
	sessionHeader = "x-claude-code-session-id"
	agentHeader   = "x-claude-code-agent-id"
)

type routeContextKey struct{}

// routeRecord carries the routing decision from the handler back to the
// observing middleware, so that one request produces one record instead of two.
type routeRecord struct {
	model         string
	upstreamModel string
	provider      string
	stream        bool
}

func withRoute(ctx context.Context) (context.Context, *routeRecord) {
	record := &routeRecord{}
	return context.WithValue(ctx, routeContextKey{}, record), record
}

func routeFrom(ctx context.Context) *routeRecord {
	record, _ := ctx.Value(routeContextKey{}).(*routeRecord)
	return record
}

// replyRecorder remembers what the reply said about itself while keeping the
// streaming capabilities of the wrapped writer intact.
type replyRecorder struct {
	http.ResponseWriter
	route   *routeRecord
	status  int
	scanner *anthropic.UsageScanner
	started bool
}

func (s *replyRecorder) WriteHeader(status int) {
	if s.status == 0 {
		s.status = status
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *replyRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	s.scan(b)
	return s.ResponseWriter.Write(b)
}

// scan feeds a copy of the reply to a usage scanner. Only a request that
// reached a model is scanned, because no other path answers with token counts.
// The routing decision is already made by the time the first byte is written.
func (s *replyRecorder) scan(b []byte) {
	if !s.started {
		s.started = true
		if s.route.model != "" {
			s.scanner = anthropic.NewUsageScanner(s.Header().Get("Content-Type"))
		}
	}
	if s.scanner != nil {
		_, _ = s.scanner.Write(b)
	}
}

// statusCode names the status the client saw. A handler that writes nothing
// leaves it unset, and net/http then sends 200.
func (s *replyRecorder) statusCode() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}

// Flush keeps the writer usable for server-sent events. Without it the type
// assertion in anthropic.NewSSEWriter fails and streaming is refused.
func (s *replyRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the original writer.
func (s *replyRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// observe turns every request into one event, which goes to the log and to
// whoever is watching.
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, route := withRoute(r.Context())
		recorder := &replyRecorder{ResponseWriter: w, route: route}
		start := time.Now()

		next.ServeHTTP(recorder, r.WithContext(ctx))

		event := newEvent(r, recorder, start)
		s.events.Publish(event)
		s.log(event)
	})
}

func newEvent(r *http.Request, recorder *replyRecorder, start time.Time) events.Event {
	event := events.Event{
		Time:          start,
		Method:        r.Method,
		Path:          r.URL.Path,
		Status:        recorder.statusCode(),
		DurationMS:    time.Since(start).Milliseconds(),
		Model:         recorder.route.model,
		Provider:      recorder.route.provider,
		UpstreamModel: recorder.route.upstreamModel,
		Stream:        recorder.route.stream,
		Session:       r.Header.Get(sessionHeader),
		Agent:         r.Header.Get(agentHeader),
	}
	if recorder.scanner == nil {
		return event
	}

	usage, stopReason := recorder.scanner.Report()
	event.StopReason = stopReason
	if usage != (anthropic.Usage{}) {
		event.Usage = &events.Usage{
			InputTokens:         usage.InputTokens,
			OutputTokens:        usage.OutputTokens,
			CacheReadTokens:     usage.CacheReadInputTokens,
			CacheCreationTokens: usage.CacheCreationInputTokens,
		}
	}
	return event
}

func (s *Server) log(event events.Event) {
	attrs := []any{
		"method", event.Method,
		"path", event.Path,
		"status", event.Status,
		"duration", (time.Duration(event.DurationMS) * time.Millisecond).String(),
	}
	if event.Model != "" {
		attrs = append(attrs,
			"model", event.Model,
			"upstream_model", event.UpstreamModel,
			"provider", event.Provider,
		)
	}
	if event.Usage != nil {
		attrs = append(attrs,
			"input_tokens", event.Usage.InputTokens,
			"output_tokens", event.Usage.OutputTokens,
		)
	}
	if event.Session != "" {
		attrs = append(attrs, "session", event.Session)
	}
	if event.Agent != "" {
		attrs = append(attrs, "agent", event.Agent)
	}
	s.logger.Info("request", attrs...)
}
