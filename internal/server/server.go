// Package server wires the gateway's HTTP surface onto the router.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/events"
	"github.com/1outres/sitka/internal/provider"
	"github.com/1outres/sitka/internal/router"
)

// maxRequestBody bounds one request body. Claude Code posts whole conversations,
// so the limit is set high enough that normal use never reaches it.
const maxRequestBody = 128 << 20

// modelsTimeout bounds the aggregated model listing. Claude Code gives its
// startup discovery request three seconds, so a slow upstream must not hold the
// response open much longer than that.
const modelsTimeout = 10 * time.Second

// Server serves the Anthropic Messages API surface on localhost.
type Server struct {
	router   *router.Router
	fallback provider.Passthrough
	events   *events.Broker
	logger   *slog.Logger
}

// New builds a server. fallback receives every path the gateway does not
// handle itself, unchanged. broker receives one event per request.
func New(rt *router.Router, fallback provider.Passthrough, broker *events.Broker, logger *slog.Logger) *Server {
	return &Server{router: rt, fallback: fallback, events: broker, logger: logger}
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", s.handleMessages)
	mux.HandleFunc("POST /v1/messages/count_tokens", s.handleCountTokens)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	mux.HandleFunc("GET "+watchPath, s.handleWatch)
	mux.Handle("/", s.fallback)
	return s.observe(mux)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	s.dispatch(w, r, func(p provider.Provider, upstreamModel string, body []byte) {
		p.Messages(w, r, upstreamModel, body)
	})
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	s.dispatch(w, r, func(p provider.Provider, upstreamModel string, body []byte) {
		p.CountTokens(w, r, upstreamModel, body)
	})
}

// dispatch reads the body, resolves the model to a provider, and hands both to
// call. The body is read in full because routing needs the model id, and the
// original bytes are passed on so a passthrough provider can replay them.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, call func(provider.Provider, string, []byte)) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		anthropic.WriteError(w, http.StatusRequestEntityTooLarge, anthropic.ErrRequestTooLarge,
			fmt.Sprintf("could not read the request body: %v", err))
		return
	}

	peek, err := peekRequest(body)
	if err != nil {
		anthropic.WriteError(w, http.StatusBadRequest, anthropic.ErrInvalidRequest, err.Error())
		return
	}

	p, upstreamModel := s.router.Route(peek.Model)
	if route := routeFrom(r.Context()); route != nil {
		route.model = peek.Model
		route.upstreamModel = upstreamModel
		route.provider = p.Name()
		route.stream = peek.Stream
	}

	call(p, upstreamModel, body)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), modelsTimeout)
	defer cancel()
	ctx = provider.ContextWithCredentials(ctx, provider.CredentialsFromRequest(r))

	providers := append([]provider.Provider{s.fallback}, s.router.Providers()...)
	lists := make([][]anthropic.Model, len(providers))
	errs := make([]error, len(providers))

	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			models, err := p.Models(ctx)
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", p.Name(), err)
				return
			}
			lists[i] = models
		}()
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		anthropic.WriteError(w, http.StatusBadGateway, anthropic.ErrAPI, err.Error())
		return
	}

	models := make([]anthropic.Model, 0)
	for _, list := range lists {
		models = append(models, list...)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(newModelList(models)); err != nil {
		s.logger.Error("write model list", "error", err)
	}
}

func newModelList(models []anthropic.Model) anthropic.ModelList {
	list := anthropic.ModelList{Data: models}
	if len(models) > 0 {
		list.FirstID = &models[0].ID
		list.LastID = &models[len(models)-1].ID
	}
	return list
}

// requestPeek reads only the fields the gateway routes on and reports, so the
// rest of the body reaches the upstream exactly as Claude Code wrote it.
type requestPeek struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func peekRequest(body []byte) (requestPeek, error) {
	var peek requestPeek
	if err := json.Unmarshal(body, &peek); err != nil {
		return peek, fmt.Errorf("request body is not valid JSON: %w", err)
	}
	if peek.Model == "" {
		return peek, errors.New("request body is missing the model field")
	}
	return peek, nil
}
