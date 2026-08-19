package openaicompat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/1outres/sitka/internal/anthropic"
)

func TestMessagesStreamsAnthropicEventSequence(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		startEventStream(w)
		sendChunk(w, textChunk("Hello"))
		sendChunk(w, textChunk(" there"))
		sendChunk(w, stopChunk())
		sendChunk(w, usageChunk(7, 2))
		sendDone(w)
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	gateway := startGateway(t, p)

	resp := postMessages(t, gateway.URL, requestBody(t, userRequest(true)))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", got, "text/event-stream")
	}

	events := drainEvents(t, eventStream(resp.Body), 5*time.Second)
	want := []string{
		anthropic.EventMessageStart,
		anthropic.EventContentBlockStart,
		anthropic.EventContentBlockDelta,
		anthropic.EventContentBlockDelta,
		anthropic.EventContentBlockStop,
		anthropic.EventMessageDelta,
		anthropic.EventMessageStop,
	}
	if got := eventNames(events); !slices.Equal(got, want) {
		t.Fatalf("event sequence = %v, want %v", got, want)
	}

	var start anthropic.MessageStartEvent
	if err := json.Unmarshal([]byte(events[0].data), &start); err != nil {
		t.Fatalf("decode the message_start payload: %v", err)
	}
	if start.Message.Model != clientModel {
		t.Errorf("message_start model = %q, want the id the client asked for %q", start.Message.Model, clientModel)
	}

	var delta anthropic.MessageDeltaEvent
	if err := json.Unmarshal([]byte(events[5].data), &delta); err != nil {
		t.Fatalf("decode the message_delta payload: %v", err)
	}
	if delta.Delta.StopReason == nil || *delta.Delta.StopReason != anthropic.StopEndTurn {
		t.Errorf("message_delta stop reason = %v, want %q", delta.Delta.StopReason, anthropic.StopEndTurn)
	}
	if delta.Usage.InputTokens != 7 || delta.Usage.OutputTokens != 2 {
		t.Errorf("message_delta usage = %+v, want 7 input and 2 output tokens", delta.Usage)
	}
}

func TestMessagesStreamsWithoutBuffering(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startEventStream(w)
		sendChunk(w, textChunk("Hello"))
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		sendChunk(w, stopChunk())
		sendChunk(w, usageChunk(7, 2))
		sendDone(w)
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	gateway := startGateway(t, p)

	resp := postMessages(t, gateway.URL, requestBody(t, userRequest(true)))
	defer func() { _ = resp.Body.Close() }()
	events := eventStream(resp.Body)

	seen := waitForEvent(t, events, anthropic.EventContentBlockDelta, 5*time.Second)
	if got := eventNames(seen); !slices.Equal(got, []string{anthropic.EventMessageStart, anthropic.EventContentBlockStart, anthropic.EventContentBlockDelta}) {
		t.Fatalf("events before the upstream continued = %v, want the first three events of a reply", got)
	}

	close(release)
	rest := drainEvents(t, events, 5*time.Second)
	if got := eventNames(rest); !slices.Equal(got, []string{anthropic.EventContentBlockStop, anthropic.EventMessageDelta, anthropic.EventMessageStop}) {
		t.Fatalf("events after the upstream continued = %v, want the closing events of a reply", got)
	}
}

func TestMessagesPingsWhileTheUpstreamIsSilent(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startEventStream(w)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		sendChunk(w, textChunk("Hello"))
		sendDone(w)
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	p.pingInterval = 20 * time.Millisecond
	gateway := startGateway(t, p)

	resp := postMessages(t, gateway.URL, requestBody(t, userRequest(true)))
	defer func() { _ = resp.Body.Close() }()
	events := eventStream(resp.Body)

	waitForEvent(t, events, anthropic.EventPing, 5*time.Second)

	close(release)
	waitForEvent(t, events, anthropic.EventMessageStop, 5*time.Second)
}
