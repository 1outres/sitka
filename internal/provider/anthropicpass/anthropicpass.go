// Package anthropicpass serves every model id the gateway does not route
// elsewhere by forwarding the request to the real Anthropic API unchanged.
package anthropicpass

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/1outres/sitka/internal/provider"
)

var _ provider.Passthrough = (*Provider)(nil)

// providerName is how the gateway names this upstream in logs and errors.
const providerName = "anthropic"

const (
	dialTimeout           = 10 * time.Second
	keepAlivePeriod       = 30 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	expectContinueTimeout = 1 * time.Second
	idleConnTimeout       = 90 * time.Second
	maxIdleConns          = 100
	maxIdleConnsPerHost   = 16
	modelsTimeout         = 30 * time.Second
)

// Provider forwards requests to the real Anthropic API unchanged.
type Provider struct {
	baseURL *url.URL
	proxy   *httputil.ReverseProxy
	client  *http.Client
	logger  *slog.Logger
}

// New builds the passthrough for baseURL, which must be an absolute http or
// https URL. A nil logger drops the log output.
func New(baseURL string, logger *slog.Logger) (*Provider, error) {
	target, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	transport := newTransport()
	p := &Provider{
		baseURL: target,
		client:  &http.Client{Transport: transport, Timeout: modelsTimeout},
		logger:  logger,
	}
	p.proxy = &httputil.ReverseProxy{
		Rewrite:   p.rewrite,
		Transport: transport,
		// Flush after every write, so streamed events reach the client as they arrive.
		FlushInterval: -1,
		ErrorHandler:  p.reportTransportFailure,
	}

	return p, nil
}

// Name returns the id this upstream is logged under.
func (p *Provider) Name() string {
	return providerName
}

func parseBaseURL(baseURL string) (*url.URL, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("%s: base URL is required", providerName)
	}

	target, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("%s: base URL %q is not a valid URL: %w", providerName, baseURL, err)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("%s: base URL %q must be absolute and use http or https", providerName, baseURL)
	}
	if target.Host == "" {
		return nil, fmt.Errorf("%s: base URL %q must name a host", providerName, baseURL)
	}

	return target, nil
}

// newTransport limits how long dialing and the TLS handshake may take, but
// never how long a response may take, because a streamed reply can stay open
// for many minutes.
func newTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: keepAlivePeriod,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
	}
}
