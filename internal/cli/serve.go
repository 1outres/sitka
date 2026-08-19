package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/1outres/sitka/internal/config"
	"github.com/1outres/sitka/internal/provider"
	"github.com/1outres/sitka/internal/provider/anthropicpass"
	"github.com/1outres/sitka/internal/provider/openaicompat"
	"github.com/1outres/sitka/internal/router"
	"github.com/1outres/sitka/internal/server"
)

// shutdownTimeout bounds how long in-flight streams may finish after a signal.
const shutdownTimeout = 10 * time.Second

func newServeCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the gateway",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.load()
			if err != nil {
				return err
			}
			logger, err := opts.logger()
			if err != nil {
				return err
			}
			return serve(cmd.Context(), cfg, logger)
		},
	}
}

func serve(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	fallback, providers, err := build(cfg, logger)
	if err != nil {
		return err
	}
	routes, err := router.New(fallback, providers)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.New(routes, fallback, logger).Handler(),
		ReadHeaderTimeout: 30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "address", cfg.Listen, "providers", providerNames(providers))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		stop()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

// build turns the config into the passthrough upstream and one provider per
// configured entry.
func build(cfg *config.Config, logger *slog.Logger) (provider.Passthrough, []provider.Provider, error) {
	fallback, err := anthropicpass.New(cfg.Anthropic.BaseURL, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic upstream: %w", err)
	}

	providers := make([]provider.Provider, 0, len(cfg.Providers))
	for _, entry := range cfg.Providers {
		p, err := openaicompat.New(entry, logger)
		if err != nil {
			return nil, nil, fmt.Errorf("provider %q: %w", entry.ID, err)
		}
		providers = append(providers, p)
	}
	return fallback, providers, nil
}

func providerNames(providers []provider.Provider) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	return names
}
