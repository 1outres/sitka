package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
	"github.com/1outres/sitka/internal/translate"
)

// Messages serves POST /v1/messages by translating the request for the
// upstream and translating its reply back.
func (p *Provider) Messages(w http.ResponseWriter, r *http.Request, upstreamModel string, body []byte) {
	var req anthropic.Request
	if err := json.Unmarshal(body, &req); err != nil {
		anthropic.WriteError(w, http.StatusBadRequest, anthropic.ErrInvalidRequest,
			fmt.Sprintf("parse the request body: %v", err))
		return
	}

	upstreamReq, err := translate.Request(&req, upstreamModel)
	if err != nil {
		p.writeTranslateError(w, err)
		return
	}

	payload, err := json.Marshal(upstreamReq)
	if err != nil {
		anthropic.WriteError(w, http.StatusInternalServerError, anthropic.ErrAPI,
			fmt.Sprintf("encode the request for provider %q: %v", p.id, err))
		return
	}

	payload, err = mergeFields(payload, p.effortFields(upstreamModel))
	if err != nil {
		anthropic.WriteError(w, http.StatusInternalServerError, anthropic.ErrAPI,
			fmt.Sprintf("apply the effort settings of provider %q: %v", p.id, err))
		return
	}

	resp, err := p.send(r.Context(), http.MethodPost, chatCompletionsPath, payload)
	if err != nil {
		p.logger.Error("upstream request failed", "error", err)
		anthropic.WriteError(w, http.StatusBadGateway, anthropic.ErrAPI, err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if !isSuccess(resp.StatusCode) {
		p.writeUpstreamError(w, resp)
		return
	}

	if req.Stream {
		p.stream(w, resp.Body, req.Model)
		return
	}
	p.writeMessage(w, resp.Body, req.Model)
}

func (p *Provider) writeTranslateError(w http.ResponseWriter, err error) {
	var unsupported *translate.UnsupportedError
	if errors.As(err, &unsupported) {
		anthropic.WriteError(w, http.StatusBadRequest, anthropic.ErrInvalidRequest,
			fmt.Sprintf("provider %q cannot serve this request because an OpenAI-compatible API has no equivalent of %s", p.id, unsupported.Feature))
		return
	}
	anthropic.WriteError(w, http.StatusBadRequest, anthropic.ErrInvalidRequest,
		fmt.Sprintf("translate the request for provider %q: %v", p.id, err))
}

// writeMessage relays a single upstream reply. anthropicModel is the model id
// the client asked for, which the reply echoes back.
func (p *Provider) writeMessage(w http.ResponseWriter, body io.Reader, anthropicModel string) {
	var upstream openai.Response
	if err := json.NewDecoder(body).Decode(&upstream); err != nil {
		anthropic.WriteError(w, http.StatusBadGateway, anthropic.ErrAPI,
			fmt.Sprintf("decode the reply of provider %q: %v", p.id, err))
		return
	}

	out, err := translate.Response(&upstream, anthropicModel)
	if err != nil {
		anthropic.WriteError(w, http.StatusBadGateway, anthropic.ErrAPI,
			fmt.Sprintf("translate the reply of provider %q: %v", p.id, err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		p.logger.Error("write the reply to the client", "error", err)
	}
}
