package anthropicpass

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"

	"github.com/1outres/sitka/internal/anthropic"
)

// ServeHTTP forwards any request to the Anthropic API and streams the reply
// back, including the paths the gateway does not handle itself such as the
// connection probe Claude Code sends at startup.
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.proxy.ServeHTTP(w, r)
}

// Messages serves POST /v1/messages. The routed model name is ignored, because
// body already carries the model id and the API must read it exactly as the
// client wrote it.
func (p *Provider) Messages(w http.ResponseWriter, r *http.Request, _ string, body []byte) {
	p.forward(w, r, body)
}

// CountTokens serves POST /v1/messages/count_tokens. It ignores the routed
// model name for the same reason as Messages.
func (p *Provider) CountTokens(w http.ResponseWriter, r *http.Request, _ string, body []byte) {
	p.forward(w, r, body)
}

// forward restores the body the gateway already read while routing, then
// proxies the request.
func (p *Provider) forward(w http.ResponseWriter, r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	p.proxy.ServeHTTP(w, r)
}

func (p *Provider) rewrite(pr *httputil.ProxyRequest) {
	// No SetXForwarded call: the client runs on localhost and the API has no use
	// for its address.
	pr.SetURL(p.baseURL)
}

// reportTransportFailure answers only when the connection itself failed. An
// error the API returned is relayed untouched instead, because Claude Code
// retries on the wording of the upstream message.
func (p *Provider) reportTransportFailure(w http.ResponseWriter, r *http.Request, err error) {
	p.logger.Error("anthropic passthrough could not reach the upstream",
		"method", r.Method,
		"path", r.URL.Path,
		"error", err,
	)
	anthropic.WriteError(w, http.StatusBadGateway, anthropic.ErrAPI,
		fmt.Sprintf("sitka could not reach the Anthropic API: %v", err))
}
