package provider

import (
	"context"
	"net/http"
)

type credentialsContextKey struct{}

// Credentials are the API credentials a client sent to the gateway. The
// Anthropic passthrough holds no key of its own and authenticates every
// upstream call with whatever the client presented, including a claude.ai
// login, so requests that do not carry the original headers must pass the
// credentials another way.
type Credentials struct {
	APIKey        string
	Authorization string
}

// CredentialsFromRequest reads the credential headers of an incoming request.
func CredentialsFromRequest(r *http.Request) Credentials {
	return Credentials{
		APIKey:        r.Header.Get("x-api-key"),
		Authorization: r.Header.Get("Authorization"),
	}
}

// Apply writes the credentials onto an outgoing request, leaving headers that
// were never set absent rather than empty.
func (c Credentials) Apply(r *http.Request) {
	if c.APIKey != "" {
		r.Header.Set("x-api-key", c.APIKey)
	}
	if c.Authorization != "" {
		r.Header.Set("Authorization", c.Authorization)
	}
}

// ContextWithCredentials carries credentials to calls that receive no request,
// such as Provider.Models.
func ContextWithCredentials(ctx context.Context, c Credentials) context.Context {
	return context.WithValue(ctx, credentialsContextKey{}, c)
}

// CredentialsFrom returns the credentials stored in ctx.
func CredentialsFrom(ctx context.Context) (Credentials, bool) {
	c, ok := ctx.Value(credentialsContextKey{}).(Credentials)
	return c, ok
}
