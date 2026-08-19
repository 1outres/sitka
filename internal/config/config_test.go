package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestDefaultPath(t *testing.T) {
	t.Run("uses XDG_CONFIG_HOME", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", dir)

		got, err := DefaultPath()
		if err != nil {
			t.Fatalf("DefaultPath() error = %v", err)
		}
		want := filepath.Join(dir, "sitka", "config.yaml")
		if got != want {
			t.Errorf("DefaultPath() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to the home directory", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", home)

		got, err := DefaultPath()
		if err != nil {
			t.Fatalf("DefaultPath() error = %v", err)
		}
		want := filepath.Join(home, ".config", "sitka", "config.yaml")
		if got != want {
			t.Errorf("DefaultPath() = %q, want %q", got, want)
		}
	})
}

func TestLoadFullConfig(t *testing.T) {
	path := writeConfig(t, `
listen: 0.0.0.0:9000

anthropic:
  base_url: https://anthropic.example.com/

providers:
  - id: openai
    base_url: https://api.openai.com/v1/
    api_key: sk-test
  - id: openrouter
    base_url: https://openrouter.ai/api/v1
    api_key: sk-or-test
    headers:
      HTTP-Referer: https://github.com/1outres/sitka
      X-Title: sitka
`)

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := &Config{
		Listen:    "0.0.0.0:9000",
		Anthropic: Anthropic{BaseURL: "https://anthropic.example.com"},
		Providers: []Provider{
			{
				ID:      "openai",
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-test",
			},
			{
				ID:      "openrouter",
				BaseURL: "https://openrouter.ai/api/v1",
				APIKey:  "sk-or-test",
				Headers: map[string]string{
					"HTTP-Referer": "https://github.com/1outres/sitka",
					"X-Title":      "sitka",
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeConfig(t, `
providers:
  - id: openai
    base_url: https://api.openai.com/v1
    api_key: sk-test
`)

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Listen != "127.0.0.1:8787" {
		t.Errorf("Listen = %q, want %q", got.Listen, "127.0.0.1:8787")
	}
	if got.Anthropic.BaseURL != "https://api.anthropic.com" {
		t.Errorf("Anthropic.BaseURL = %q, want %q", got.Anthropic.BaseURL, "https://api.anthropic.com")
	}
}

func TestLoadEmptyFileIsAllDefaults(t *testing.T) {
	path := writeConfig(t, "")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := &Config{
		Listen:    "127.0.0.1:8787",
		Anthropic: Anthropic{BaseURL: "https://api.anthropic.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	got, err := Load(path)
	if err == nil {
		t.Fatalf("Load() error = nil, want an error")
	}
	if got != nil {
		t.Errorf("Load() config = %+v, want nil", got)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load() error = %v, want it to wrap fs.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Load() error = %v, want it to name the path %q", err, path)
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "malformed yaml",
			body: "listen: [127.0.0.1:8787\nproviders:\n",
			want: "parse",
		},
		{
			name: "wrong type for a field",
			body: "providers: not-a-list\n",
			want: "parse",
		},
		{
			name: "unknown field",
			body: "listen: 127.0.0.1:8787\nlisten_address: 127.0.0.1:8787\n",
			want: "listen_address",
		},
		{
			name: "invalid config",
			body: "listen: not-a-host-port\n",
			want: "listen",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.body)

			got, err := Load(path)
			if err == nil {
				t.Fatalf("Load() error = nil, want an error")
			}
			if got != nil {
				t.Errorf("Load() config = %+v, want nil", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() error = %v, want it to mention %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("Load() error = %v, want it to name the path %q", err, path)
			}
		})
	}
}

func TestLoadIgnoresEnvironmentAPIKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-env")
	path := writeConfig(t, `
providers:
  - id: openai
    base_url: https://api.openai.com/v1
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error because api_key is only read from the file")
	}
}
