// Package config loads and validates the sitka configuration file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	defaultListen           = "127.0.0.1:8787"
	defaultAnthropicBaseURL = "https://api.anthropic.com"

	configDirName  = "sitka"
	configFileName = "config.yaml"
)

// Config is the whole configuration file.
type Config struct {
	Listen    string     `json:"listen,omitempty"`
	Anthropic Anthropic  `json:"anthropic,omitempty"`
	Providers []Provider `json:"providers,omitempty"`
}

// Anthropic configures the upstream that serves every model id no provider
// prefix claims.
type Anthropic struct {
	BaseURL string `json:"base_url,omitempty"`
}

// Provider configures one OpenAI-compatible upstream. ID is the prefix a
// client puts in front of the upstream model name, as in "openai-gpt-5.2".
type Provider struct {
	ID      string            `json:"id"`
	BaseURL string            `json:"base_url"`
	APIKey  string            `json:"api_key"`
	Headers map[string]string `json:"headers,omitempty"`
}

// DefaultPath returns the config path from XDG_CONFIG_HOME, falling back to
// ~/.config/sitka/config.yaml.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, configDirName, configFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory: %w", err)
	}
	return filepath.Join(home, ".config", configDirName, configFileName), nil
}

// Load reads and validates the config at path, applying defaults. API keys
// come from the file only, never from the environment.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no config file at %s: %w", path, err)
		}
		return nil, fmt.Errorf("read the config file %s: %w", path, err)
	}

	var c Config
	if err := yaml.UnmarshalStrict(data, &c); err != nil {
		return nil, fmt.Errorf("parse the config file %s: %w", path, err)
	}

	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config file %s: %w", path, err)
	}
	c.normalize()

	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = defaultListen
	}
	if c.Anthropic.BaseURL == "" {
		c.Anthropic.BaseURL = defaultAnthropicBaseURL
	}
}

// normalize runs after validation so that error messages quote what the user
// wrote.
func (c *Config) normalize() {
	c.Anthropic.BaseURL = trimTrailingSlashes(c.Anthropic.BaseURL)
	for i := range c.Providers {
		c.Providers[i].BaseURL = trimTrailingSlashes(c.Providers[i].BaseURL)
	}
}

func trimTrailingSlashes(rawURL string) string {
	return strings.TrimRight(rawURL, "/")
}
