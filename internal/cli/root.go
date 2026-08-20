// Package cli builds the sitka command tree.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/1outres/sitka/internal/config"
)

// options are the flags every subcommand shares.
type options struct {
	configPath string
	logLevel   string
}

// NewRootCommand builds the sitka command tree.
func NewRootCommand() *cobra.Command {
	opts := &options{}

	cmd := &cobra.Command{
		Use:           "sitka",
		Short:         "Personal AI gateway for Claude Code",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&opts.configPath, "config", "",
		"path to the config file (default $XDG_CONFIG_HOME/sitka/config.yaml)")
	cmd.PersistentFlags().StringVar(&opts.logLevel, "log-level", "info",
		"log level: debug, info, warn or error")

	cmd.AddCommand(
		newServeCommand(opts),
		newModelsCommand(opts),
		newWatchCommand(opts),
		newVersionCommand(),
	)
	return cmd
}

// Execute runs the command tree and returns the process exit code.
func Execute() int {
	if err := NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "sitka:", err)
		return 1
	}
	return 0
}

func (o *options) load() (*config.Config, error) {
	path := o.configPath
	if path == "" {
		defaultPath, err := config.DefaultPath()
		if err != nil {
			return nil, err
		}
		path = defaultPath
	}
	return config.Load(path)
}

func (o *options) logger() (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(o.logLevel)); err != nil {
		return nil, fmt.Errorf("invalid --log-level %q: %w", o.logLevel, err)
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return slog.New(handler), nil
}
