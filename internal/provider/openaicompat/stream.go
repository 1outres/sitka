package openaicompat

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
	"github.com/1outres/sitka/internal/translate"
)

// stream relays an upstream stream to the client as Anthropic events.
func (p *Provider) stream(w http.ResponseWriter, body io.Reader, anthropicModel string) {
	sse, err := anthropic.NewSSEWriter(w)
	if err != nil {
		anthropic.WriteError(w, http.StatusInternalServerError, anthropic.ErrAPI,
			fmt.Sprintf("stream the reply of provider %q: %v", p.id, err))
		return
	}
	defer sse.Close()

	pings := startPings(sse, p.pingInterval, p.logger)
	defer pings.stop()

	if err := translate.Stream(openai.NewStreamReader(body), sse, anthropicModel); err != nil {
		// translate.Stream already sent an error event, and the reply has
		// started, so the client can only be told through the log.
		p.logger.Error("relay the upstream stream", "error", err)
	}
}

// pinger keeps a stream from going byte-silent while the upstream says nothing.
type pinger struct {
	stopped chan struct{}
	done    chan struct{}
}

func startPings(sse *anthropic.SSEWriter, interval time.Duration, logger *slog.Logger) *pinger {
	p := &pinger{stopped: make(chan struct{}), done: make(chan struct{})}
	go p.run(sse, interval, logger)
	return p
}

func (p *pinger) run(sse *anthropic.SSEWriter, interval time.Duration, logger *slog.Logger) {
	defer close(p.done)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopped:
			return
		case <-ticker.C:
			if err := sse.PingIfIdle(interval); err != nil {
				logger.Error("send a keep-alive ping", "error", err)
				return
			}
		}
	}
}

// stop waits for the ticker goroutine to end, so that no ping can reach the
// response after the handler returns.
func (p *pinger) stop() {
	close(p.stopped)
	<-p.done
}
