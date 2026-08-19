package anthropicpass

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1outres/sitka/internal/anthropic"
)

const streamTimeout = 5 * time.Second

func TestServeHTTPForwardsTheRequestUnchanged(t *testing.T) {
	cases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{"messages", http.MethodPost, "/v1/messages", `{"model":"claude-sonnet-5","max_tokens":1024}`},
		{"count tokens", http.MethodPost, "/v1/messages/count_tokens", `{"model":"claude-sonnet-5"}`},
		{"model discovery", http.MethodGet, "/v1/models?limit=1000", ""},
		{"escaped query", http.MethodGet, "/v1/models?after_id=claude%2Fsonnet&limit=1000", ""},
		{"unknown future path", http.MethodPost, "/v1/somethingnew?alpha=1&alpha=2", `{"anything":true}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream, calls := recordingUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			p := newProvider(t, upstream.URL)

			p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body)))

			wantPath, wantQuery, _ := strings.Cut(tc.target, "?")
			got := <-calls
			if got.method != tc.method {
				t.Errorf("upstream method = %q, want %q", got.method, tc.method)
			}
			if got.path != wantPath {
				t.Errorf("upstream path = %q, want %q", got.path, wantPath)
			}
			if got.rawQuery != wantQuery {
				t.Errorf("upstream raw query = %q, want %q", got.rawQuery, wantQuery)
			}
			if string(got.body) != tc.body {
				t.Errorf("upstream body = %q, want %q", got.body, tc.body)
			}
		})
	}
}

func TestServeHTTPForwardsHeadersUnchanged(t *testing.T) {
	sent := map[string]string{
		"anthropic-version":      "2023-06-01",
		"anthropic-beta":         "oauth-2025-04-20,interleaved-thinking-2025-05-14,context-1m-2025-08-07",
		"anthropic-future-thing": "a value sitka has never heard of",
		"x-api-key":              "sk-ant-test",
		"Authorization":          "Bearer oauth-token",
		"Content-Type":           "application/json",
		"User-Agent":             "claude-cli/2.0.0 (external, cli)",
	}

	upstream, calls := recordingUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	p := newProvider(t, upstream.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
	for name, value := range sent {
		req.Header.Set(name, value)
	}
	req.Header.Set("X-Forwarded-For", "203.0.113.7")

	p.ServeHTTP(httptest.NewRecorder(), req)

	got := <-calls
	for name, want := range sent {
		if value := got.header.Get(name); value != want {
			t.Errorf("upstream %s = %q, want %q", name, value, want)
		}
	}
	if value, ok := got.header["X-Forwarded-For"]; ok {
		t.Errorf("upstream X-Forwarded-For = %q, want the header to be absent", value)
	}
}

func TestServeHTTPForwardsTheConnectionProbe(t *testing.T) {
	upstream, calls := recordingUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	p := newProvider(t, upstream.URL)

	recorder := httptest.NewRecorder()
	p.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/api/hello", nil))

	got := <-calls
	if got.method != http.MethodHead {
		t.Errorf("upstream method = %q, want %q", got.method, http.MethodHead)
	}
	if got.path != "/api/hello" {
		t.Errorf("upstream path = %q, want %q", got.path, "/api/hello")
	}
	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestServeHTTPRelaysUpstreamErrorsUnmodified(t *testing.T) {
	const errorBody = `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: 200000 > 64000, which is the maximum allowed for claude-sonnet-5"}}`

	upstream, _ := recordingUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("request-id", "req_011CQdemo")
		w.WriteHeader(http.StatusBadRequest)
		if _, err := io.WriteString(w, errorBody); err != nil {
			t.Errorf("upstream could not write the error body: %v", err)
		}
	})
	p := newProvider(t, upstream.URL)

	recorder := httptest.NewRecorder()
	p.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}")))

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if got := recorder.Body.String(); got != errorBody {
		t.Errorf("body = %q, want the upstream bytes %q", got, errorBody)
	}
	if got := recorder.Header().Get("request-id"); got != "req_011CQdemo" {
		t.Errorf("request-id = %q, want %q", got, "req_011CQdemo")
	}
}

func TestServeHTTPReportsATransportFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	baseURL := upstream.URL
	upstream.Close()

	p := newProvider(t, baseURL)
	recorder := httptest.NewRecorder()
	p.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}")))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	var got anthropic.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not an error envelope: %v", recorder.Body.String(), err)
	}
	if got.Type != "error" {
		t.Errorf("envelope type = %q, want %q", got.Type, "error")
	}
	if got.Error.Type != anthropic.ErrAPI {
		t.Errorf("error type = %q, want %q", got.Error.Type, anthropic.ErrAPI)
	}
	if got.Error.Message == "" {
		t.Error("error message is empty, want the cause of the failure")
	}
}

func TestServeHTTPStreamsWithoutBuffering(t *testing.T) {
	const (
		firstEvent  = "event: ping\ndata: {\"type\": \"ping\"}\n\n"
		secondEvent = "event: message_stop\ndata: {\"type\": \"message_stop\"}\n\n"
	)

	cases := []struct {
		name        string
		contentType string
		knownLength bool
	}{
		{"server sent events", "text/event-stream", false},
		{"body of a known length", "application/json", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			releaseOnce := sync.OnceFunc(func() { close(release) })

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				flusher, ok := w.(http.Flusher)
				if !ok {
					t.Error("the upstream response writer cannot flush")
					return
				}
				w.Header().Set("Content-Type", tc.contentType)
				if tc.knownLength {
					w.Header().Set("Content-Length", strconv.Itoa(len(firstEvent)+len(secondEvent)))
				}
				if _, err := io.WriteString(w, firstEvent); err != nil {
					t.Errorf("upstream could not write the first event: %v", err)
					return
				}
				flusher.Flush()

				select {
				case <-release:
				case <-time.After(streamTimeout):
					t.Error("the client never read the first event, so the gateway buffered it")
					return
				}
				if _, err := io.WriteString(w, secondEvent); err != nil {
					t.Errorf("upstream could not write the second event: %v", err)
				}
				flusher.Flush()
			}))
			t.Cleanup(upstream.Close)
			t.Cleanup(releaseOnce)

			gateway := httptest.NewServer(newProvider(t, upstream.URL))
			t.Cleanup(gateway.Close)

			resp, err := http.Get(gateway.URL + "/v1/messages")
			if err != nil {
				t.Fatalf("GET through the gateway failed: %v", err)
			}
			t.Cleanup(func() { _ = resp.Body.Close() })

			events := readEvents(resp.Body)
			if got := receiveEvent(t, events); got != firstEvent {
				t.Fatalf("first event = %q, want %q", got, firstEvent)
			}
			releaseOnce()
			if got := receiveEvent(t, events); got != secondEvent {
				t.Fatalf("second event = %q, want %q", got, secondEvent)
			}
		})
	}
}

func TestReplaysTheBodyTheGatewayConsumed(t *testing.T) {
	cases := []struct {
		name   string
		target string
		serve  func(p *Provider, w http.ResponseWriter, r *http.Request, upstreamModel string, body []byte)
	}{
		{"messages", "/v1/messages", (*Provider).Messages},
		{"count tokens", "/v1/messages/count_tokens", (*Provider).CountTokens},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"claude-sonnet-5","messages":[{"role":"user","content":"hi"}]}`)

			upstream, calls := recordingUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			p := newProvider(t, upstream.URL)

			req := httptest.NewRequest(http.MethodPost, tc.target, bytes.NewReader(body))
			drained, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("the gateway could not read the request body: %v", err)
			}
			if !bytes.Equal(drained, body) {
				t.Fatalf("the gateway read %q, want %q", drained, body)
			}

			tc.serve(p, httptest.NewRecorder(), req, "claude-sonnet-5", body)

			got := <-calls
			if !bytes.Equal(got.body, body) {
				t.Errorf("upstream body = %q, want %q", got.body, body)
			}
			if got.contentLength != int64(len(body)) {
				t.Errorf("upstream Content-Length = %d, want %d", got.contentLength, len(body))
			}
			if got.path != tc.target {
				t.Errorf("upstream path = %q, want %q", got.path, tc.target)
			}
		})
	}
}

// readEvents splits an event stream into events, so the test can wait for one
// event at a time instead of for the whole body.
func readEvents(body io.Reader) <-chan string {
	events := make(chan string, 2)

	go func() {
		defer close(events)

		reader := bufio.NewReader(body)
		var event strings.Builder
		for {
			line, err := reader.ReadString('\n')
			event.WriteString(line)
			if line == "\n" {
				events <- event.String()
				event.Reset()
			}
			if err != nil {
				return
			}
		}
	}()

	return events
}

func receiveEvent(t *testing.T, events <-chan string) string {
	t.Helper()

	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("the stream ended before the event arrived")
		}
		return event
	case <-time.After(streamTimeout):
		t.Fatal("timed out waiting for an event, so the gateway buffered the stream")
	}
	return ""
}
