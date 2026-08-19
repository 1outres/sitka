// Package provider defines the upstream abstraction the gateway routes to.
package provider

import (
	"context"
	"net/http"

	"github.com/1outres/sitka/internal/anthropic"
)

// Provider serves the Anthropic Messages API surface for one upstream.
// Implementations write the whole response themselves, including the status
// code and any streaming body, so that a stream reaches the client unbuffered.
type Provider interface {
	// Name identifies the provider in logs.
	Name() string

	// Messages serves POST /v1/messages. upstreamModel is the requested model
	// with the provider prefix removed, and body is the raw request body.
	Messages(w http.ResponseWriter, r *http.Request, upstreamModel string, body []byte)

	// CountTokens serves POST /v1/messages/count_tokens with the same arguments.
	CountTokens(w http.ResponseWriter, r *http.Request, upstreamModel string, body []byte)

	// Models lists what this provider offers, with ids already carrying the
	// prefix a client must send to reach them.
	Models(ctx context.Context) ([]anthropic.Model, error)
}

// Passthrough is a provider that can also serve arbitrary API paths unchanged.
// The gateway hands it every request it does not handle itself, such as the
// connection-warming probe Claude Code sends at startup.
type Passthrough interface {
	Provider
	http.Handler
}
