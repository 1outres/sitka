// Package openaicompat serves the Anthropic Messages API from an
// OpenAI-compatible Chat Completions upstream.
package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/1outres/sitka/internal/config"
	"github.com/1outres/sitka/internal/provider"
)

// defaultPingInterval is how long a stream may stay silent before the gateway
// sends a ping. Claude Code drops a stream that writes nothing for 300
// seconds, and an OpenAI-compatible upstream writes nothing while the model
// reasons, so the gateway makes the bytes itself.
const defaultPingInterval = 15 * time.Second

// Paths of the OpenAI-compatible API, relative to the configured base URL.
const (
	chatCompletionsPath = "/chat/completions"
	modelsPath          = "/models"
)

const (
	dialTimeout           = 10 * time.Second
	keepAlivePeriod       = 30 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	expectContinueTimeout = 1 * time.Second
	idleConnTimeout       = 90 * time.Second
	maxIdleConns          = 32
)

var _ provider.Provider = (*Provider)(nil)

// Provider serves the Anthropic Messages API from an OpenAI-compatible upstream.
type Provider struct {
	id           string
	baseURL      string
	apiKey       string
	headers      map[string]string
	effort       json.RawMessage
	models       map[string]config.Model
	client       *http.Client
	logger       *slog.Logger
	pingInterval time.Duration
}

// New builds a provider for one configured upstream.
func New(cfg config.Provider, logger *slog.Logger) (*Provider, error) {
	if cfg.ID == "" {
		return nil, errors.New("openaicompat: the provider id is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openaicompat: provider %q needs an api key", cfg.ID)
	}
	baseURL, err := cleanBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: provider %q: %w", cfg.ID, err)
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Provider{
		id:           cfg.ID,
		baseURL:      baseURL,
		apiKey:       cfg.APIKey,
		headers:      maps.Clone(cfg.Headers),
		effort:       cfg.Effort,
		models:       maps.Clone(cfg.Models),
		client:       newHTTPClient(),
		logger:       logger.With("provider", cfg.ID),
		pingInterval: defaultPingInterval,
	}, nil
}

// Name returns the provider id, which is also the model prefix that routes to it.
func (p *Provider) Name() string {
	return p.id
}

func cleanBaseURL(rawURL string) (string, error) {
	if rawURL == "" {
		return "", errors.New("the base url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("the base url %q is not a valid URL: %w", rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("the base url %q must use http or https", rawURL)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("the base url %q must have a host", rawURL)
	}
	return strings.TrimRight(rawURL, "/"), nil
}

// newHTTPClient bounds how long the client waits to connect but sets no
// overall timeout, because one streaming reply can stay open for many minutes.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlivePeriod}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   tlsHandshakeTimeout,
			ExpectContinueTimeout: expectContinueTimeout,
			IdleConnTimeout:       idleConnTimeout,
			MaxIdleConns:          maxIdleConns,
			MaxIdleConnsPerHost:   maxIdleConns,
		},
	}
}
