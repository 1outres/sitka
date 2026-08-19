package server

import (
	"context"
	"net/http"
	"time"
)

type routeContextKey struct{}

// routeRecord carries the routing decision from the handler back to the logging
// middleware, so that one request produces one log line instead of two.
type routeRecord struct {
	model         string
	upstreamModel string
	provider      string
}

func withRoute(ctx context.Context) (context.Context, *routeRecord) {
	record := &routeRecord{}
	return context.WithValue(ctx, routeContextKey{}, record), record
}

func routeFrom(ctx context.Context) *routeRecord {
	record, _ := ctx.Value(routeContextKey{}).(*routeRecord)
	return record
}

// statusRecorder remembers the response status while keeping the streaming
// capabilities of the wrapped writer intact.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	if s.status == 0 {
		s.status = status
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Flush keeps the writer usable for server-sent events. Without it the type
// assertion in anthropic.NewSSEWriter fails and streaming is refused.
func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the original writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, record := withRoute(r.Context())
		recorder := &statusRecorder{ResponseWriter: w}
		start := time.Now()

		next.ServeHTTP(recorder, r.WithContext(ctx))

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration", time.Since(start).Round(time.Millisecond).String(),
		}
		if record.model != "" {
			attrs = append(attrs,
				"model", record.model,
				"upstream_model", record.upstreamModel,
				"provider", record.provider,
			)
		}
		if id := r.Header.Get("x-claude-code-session-id"); id != "" {
			attrs = append(attrs, "session", id)
		}
		if id := r.Header.Get("x-claude-code-agent-id"); id != "" {
			attrs = append(attrs, "agent", id)
		}
		s.logger.Info("request", attrs...)
	})
}
