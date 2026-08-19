package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
)

func TestMessagesSendsTranslatedRequestUpstream(t *testing.T) {
	recorder := &upstreamRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		writeJSON(w, http.StatusOK, chatCompletion("Hi", 5, 3))
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL, withHeader("HTTP-Referer", "https://github.com/1outres/sitka"))
	rec := callMessages(t, p, requestBody(t, userRequest(false)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %s", rec.Code, http.StatusOK, rec.Body)
	}

	got := recorder.last(t)
	if got.method != http.MethodPost {
		t.Errorf("method = %q, want %q", got.method, http.MethodPost)
	}
	if got.path != "/chat/completions" {
		t.Errorf("path = %q, want %q", got.path, "/chat/completions")
	}
	if want := "Bearer " + testAPIKey; got.header.Get("Authorization") != want {
		t.Errorf("Authorization = %q, want %q", got.header.Get("Authorization"), want)
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got.header.Get("Content-Type"), "application/json")
	}
	if want := "https://github.com/1outres/sitka"; got.header.Get("HTTP-Referer") != want {
		t.Errorf("HTTP-Referer = %q, want %q", got.header.Get("HTTP-Referer"), want)
	}

	var sent openai.Request
	if err := json.Unmarshal(got.body, &sent); err != nil {
		t.Fatalf("decode the upstream request %q: %v", got.body, err)
	}
	if sent.Model != upstreamModelID {
		t.Errorf("upstream model = %q, want %q", sent.Model, upstreamModelID)
	}
	if sent.Stream {
		t.Error("upstream request asked for a stream, want a single reply")
	}
	if len(sent.Messages) != 1 || sent.Messages[0].Content.Text != "Hello" {
		t.Errorf("upstream messages = %+v, want one user message saying Hello", sent.Messages)
	}
}

func TestMessagesNonStreamingReply(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, chatCompletion("Hi there", 11, 4))
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	rec := callMessages(t, p, requestBody(t, userRequest(false)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %s", rec.Code, http.StatusOK, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	got := decodeMessage(t, rec.Body.Bytes())
	if got.Model != clientModel {
		t.Errorf("model = %q, want the id the client asked for %q", got.Model, clientModel)
	}
	if got.Role != anthropic.RoleAssistant || got.Type != "message" {
		t.Errorf("role = %q type = %q, want assistant message", got.Role, got.Type)
	}
	if len(got.Content) != 1 || got.Content[0].Type != anthropic.BlockText || got.Content[0].Text != "Hi there" {
		t.Fatalf("content = %+v, want one text block saying Hi there", got.Content)
	}
	if got.Usage.InputTokens != 11 || got.Usage.OutputTokens != 4 {
		t.Errorf("usage = %+v, want 11 input and 4 output tokens", got.Usage)
	}
	if got.StopReason == nil || *got.StopReason != anthropic.StopEndTurn {
		t.Errorf("stop reason = %v, want %q", got.StopReason, anthropic.StopEndTurn)
	}
}

func TestMessagesKeepsUpstreamErrorStatusAndMessage(t *testing.T) {
	const upstreamMessage = "Rate limit reached for gpt-5.2 in organization org-1"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": map[string]any{"message": upstreamMessage, "type": "rate_limit_exceeded"},
		})
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	rec := callMessages(t, p, requestBody(t, userRequest(false)))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	got := decodeError(t, rec.Body.Bytes())
	if got.Error.Type != anthropic.ErrRateLimit {
		t.Errorf("error type = %q, want %q", got.Error.Type, anthropic.ErrRateLimit)
	}
	if got.Error.Message != upstreamMessage {
		t.Errorf("error message = %q, want the upstream text %q", got.Error.Message, upstreamMessage)
	}
}

func TestMessagesKeepsNonJSONUpstreamError(t *testing.T) {
	const upstreamBody = "upstream exploded"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(upstreamBody))
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	rec := callMessages(t, p, requestBody(t, userRequest(false)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	got := decodeError(t, rec.Body.Bytes())
	if got.Error.Type != anthropic.ErrAPI {
		t.Errorf("error type = %q, want %q", got.Error.Type, anthropic.ErrAPI)
	}
	if got.Error.Message != upstreamBody {
		t.Errorf("error message = %q, want the raw upstream text %q", got.Error.Message, upstreamBody)
	}
}

func TestMessagesRejectsInvalidJSONBody(t *testing.T) {
	recorder := &upstreamRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		writeJSON(w, http.StatusOK, chatCompletion("Hi", 1, 1))
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	rec := callMessages(t, p, []byte("{not json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := decodeError(t, rec.Body.Bytes()); got.Error.Type != anthropic.ErrInvalidRequest {
		t.Errorf("error type = %q, want %q", got.Error.Type, anthropic.ErrInvalidRequest)
	}
	if recorder.count() != 0 {
		t.Errorf("the upstream received %d requests, want none", recorder.count())
	}
}

func TestMessagesRejectsUnsupportedFeature(t *testing.T) {
	recorder := &upstreamRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		writeJSON(w, http.StatusOK, chatCompletion("Hi", 1, 1))
	}))
	defer upstream.Close()

	body := []byte(`{"model":"openai-gpt-5.2","max_tokens":64,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0="}}]}]}`)

	p := newTestProvider(t, upstream.URL)
	rec := callMessages(t, p, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body %s", rec.Code, http.StatusBadRequest, rec.Body)
	}

	got := decodeError(t, rec.Body.Bytes())
	if got.Error.Type != anthropic.ErrInvalidRequest {
		t.Errorf("error type = %q, want %q", got.Error.Type, anthropic.ErrInvalidRequest)
	}
	if !strings.Contains(got.Error.Message, anthropic.BlockDocument) {
		t.Errorf("error message = %q, want it to name the %q feature", got.Error.Message, anthropic.BlockDocument)
	}
	if recorder.count() != 0 {
		t.Errorf("the upstream received %d requests, want none", recorder.count())
	}
}

func TestMessagesReportsTransportFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := upstream.URL
	upstream.Close()

	p := newTestProvider(t, baseURL)
	rec := callMessages(t, p, requestBody(t, userRequest(false)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if got := decodeError(t, rec.Body.Bytes()); got.Error.Type != anthropic.ErrAPI {
		t.Errorf("error type = %q, want %q", got.Error.Type, anthropic.ErrAPI)
	}
}

func TestMessagesCancelsUpstreamWhenClientDisconnects(t *testing.T) {
	started := make(chan struct{})
	upstreamErr := make(chan error, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// The server only watches the connection once the body is drained, so
		// it learns about the disconnect only after this read.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		upstreamErr <- r.Context().Err()
	}))
	defer upstream.Close()

	p := newTestProvider(t, upstream.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	body := requestBody(t, userRequest(false))
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", http.NoBody).WithContext(ctx)

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		p.Messages(rec, r, upstreamModelID, body)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the upstream never received the request")
	}
	cancel()

	select {
	case err := <-upstreamErr:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("upstream context error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the upstream request was not cancelled with the client")
	}

	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Messages did not return after the client disconnected")
	}
}
