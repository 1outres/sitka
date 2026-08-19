package cli

import (
	"context"
	"errors"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// listTimeout bounds how long the whole listing may take.
const listTimeout = 30 * time.Second

func newModelsCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List the model ids the configured providers offer",
		Long: "List the model ids the configured providers offer. Use one of these ids in the " +
			"model field of a Claude Code subagent to run it on that provider. Models of the " +
			"Anthropic passthrough are not listed, because sitka holds no key for it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.load()
			if err != nil {
				return err
			}
			logger, err := opts.logger()
			if err != nil {
				return err
			}
			_, providers, err := build(cfg, logger)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), listTimeout)
			defer cancel()

			out := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			var failures []error
			for _, p := range providers {
				models, err := p.Models(ctx)
				if err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", p.Name(), err))
					continue
				}
				for _, model := range models {
					_, _ = fmt.Fprintf(out, "%s\t%s\n", model.ID, model.DisplayName)
				}
			}
			if err := out.Flush(); err != nil {
				return err
			}
			return errors.Join(failures...)
		},
	}
}
