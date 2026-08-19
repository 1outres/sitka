// Package router picks the upstream provider for a requested model id.
package router

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/1outres/sitka/internal/provider"
)

// prefixSeparator splits a model id into a provider prefix and the model name
// the upstream knows.
const prefixSeparator = "-"

// Router resolves a model id to the provider that serves it.
type Router struct {
	fallback  provider.Provider
	providers []provider.Provider
	byPrefix  map[string]provider.Provider
}

// New builds a router. fallback serves every model id that no provider prefix
// claims. Provider names become the prefixes, so they must be non-empty and
// unique.
func New(fallback provider.Provider, providers []provider.Provider) (*Router, error) {
	if fallback == nil {
		return nil, errors.New("router: fallback provider is required")
	}

	byPrefix := make(map[string]provider.Provider, len(providers))
	var errs []error
	for i, p := range providers {
		if p == nil {
			errs = append(errs, fmt.Errorf("router: provider at index %d is nil", i))
			continue
		}
		name := p.Name()
		if name == "" {
			errs = append(errs, fmt.Errorf("router: provider at index %d has an empty name", i))
			continue
		}
		if _, ok := byPrefix[name]; ok {
			errs = append(errs, fmt.Errorf("router: provider name %q is used more than once", name))
			continue
		}
		byPrefix[name] = p
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return &Router{
		fallback:  fallback,
		providers: slices.Clone(providers),
		byPrefix:  byPrefix,
	}, nil
}

// Route resolves modelID. upstreamModel is the model id with the provider
// prefix removed, or modelID unchanged when the fallback serves it.
func (r *Router) Route(modelID string) (p provider.Provider, upstreamModel string) {
	prefix, rest, found := strings.Cut(modelID, prefixSeparator)
	if !found || rest == "" {
		return r.fallback, modelID
	}
	if matched, ok := r.byPrefix[prefix]; ok {
		return matched, rest
	}
	return r.fallback, modelID
}

// Providers returns the prefixed providers in configuration order. The
// fallback provider is not part of the list.
func (r *Router) Providers() []provider.Provider {
	return slices.Clone(r.providers)
}
