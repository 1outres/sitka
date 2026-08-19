package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootCommandHasEverySubcommand(t *testing.T) {
	want := []string{"serve", "models", "version"}

	got := make(map[string]bool)
	for _, cmd := range NewRootCommand().Commands() {
		got[cmd.Name()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("subcommand %q is missing", name)
		}
	}
}

func TestVersionCommandPrints(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Error("version printed nothing")
	}
}

func TestMissingConfigNamesThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"models", "--config", path})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute = nil error, want a missing config error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want it to name %s", err, path)
	}
}

func TestInvalidLogLevelIsRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	config := "listen: 127.0.0.1:8787\nproviders:\n  - id: openai\n    base_url: https://api.openai.com/v1\n    api_key: sk-test\n"
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"models", "--config", path, "--log-level", "loud"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute = nil error, want an invalid log level error")
	}
	if !strings.Contains(err.Error(), "loud") {
		t.Errorf("error = %v, want it to quote the bad level", err)
	}
}

func TestDefaultPathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")

	opts := &options{}
	if _, err := opts.load(); err == nil {
		t.Fatal("load = nil error, want a missing config error")
	} else if !strings.Contains(err.Error(), filepath.Join("/tmp/xdg", "sitka", "config.yaml")) {
		t.Errorf("error = %v, want it to name the XDG path", err)
	}
}
