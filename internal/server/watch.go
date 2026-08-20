package server

import (
	"net/http"
	"time"

	"github.com/1outres/sitka/internal/events"
)

// watchPath is where a watcher subscribes. It sits outside /v1 so that it can
// never collide with a path the gateway forwards to the Anthropic API.
const watchPath = "/_sitka/events"

// watchPingInterval keeps the stream from going byte-silent, so that a watcher
// notices a gateway that went away.
const watchPingInterval = 30 * time.Second

// handleWatch streams every request the gateway serves, as it finishes.
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	// Subscribe before the first byte, so that a watcher which has read the
	// response header misses nothing that happens next.
	watched, cancel := s.events.Subscribe()
	defer cancel()

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	encoder := events.NewEncoder(w)
	if err := encoder.Ping(); err != nil {
		return
	}

	ticker := time.NewTicker(watchPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-watched:
			if err := encoder.Encode(event); err != nil {
				s.logger.Debug("the watch stream ended", "error", err)
				return
			}
		case <-ticker.C:
			if err := encoder.Ping(); err != nil {
				return
			}
		}
	}
}
