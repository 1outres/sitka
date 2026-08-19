package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		Listen:    "127.0.0.1:8787",
		Anthropic: Anthropic{BaseURL: "https://api.anthropic.com"},
		Providers: []Provider{
			{
				ID:      "openai",
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-test",
				Headers: map[string]string{"HTTP-Referer": "https://github.com/1outres/sitka"},
			},
		},
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr []string
	}{
		{
			name:   "valid config",
			mutate: func(*Config) {},
		},
		{
			name:   "no providers is valid",
			mutate: func(c *Config) { c.Providers = nil },
		},
		{
			name: "valid effort settings",
			mutate: func(c *Config) {
				c.Providers[0].Effort = json.RawMessage(`{"reasoning_effort":"high"}`)
				c.Providers[0].Models = map[string]Model{
					"gpt-5.2": {Effort: json.RawMessage(`{"reasoning":{"effort":"high"}}`)},
				}
			},
		},
		{
			name:    "effort that is not a mapping",
			mutate:  func(c *Config) { c.Providers[0].Effort = json.RawMessage(`"high"`) },
			wantErr: []string{"providers[0].effort", "mapping"},
		},
		{
			name:    "effort set to null",
			mutate:  func(c *Config) { c.Providers[0].Effort = json.RawMessage(`null`) },
			wantErr: []string{"providers[0].effort", "mapping"},
		},
		{
			name: "model effort that is not a mapping",
			mutate: func(c *Config) {
				c.Providers[0].Models = map[string]Model{"gpt-5.2": {Effort: json.RawMessage(`[1]`)}}
			},
			wantErr: []string{`providers[0].models["gpt-5.2"].effort`, "mapping"},
		},
		{
			name:    "empty model name",
			mutate:  func(c *Config) { c.Providers[0].Models = map[string]Model{"": {}} },
			wantErr: []string{"providers[0].models", "empty"},
		},
		{
			name:    "listen without a port",
			mutate:  func(c *Config) { c.Listen = "127.0.0.1" },
			wantErr: []string{"listen", "127.0.0.1"},
		},
		{
			name:    "listen with a named port",
			mutate:  func(c *Config) { c.Listen = "127.0.0.1:http" },
			wantErr: []string{"listen", "numeric port"},
		},
		{
			name:    "listen with an empty port",
			mutate:  func(c *Config) { c.Listen = "127.0.0.1:" },
			wantErr: []string{"listen", "numeric port"},
		},
		{
			name:    "listen with a port out of range",
			mutate:  func(c *Config) { c.Listen = "127.0.0.1:99999" },
			wantErr: []string{"listen", "numeric port"},
		},
		{
			name:    "empty listen",
			mutate:  func(c *Config) { c.Listen = "" },
			wantErr: []string{"listen"},
		},
		{
			name:    "anthropic base url is empty",
			mutate:  func(c *Config) { c.Anthropic.BaseURL = "" },
			wantErr: []string{"anthropic.base_url"},
		},
		{
			name:    "anthropic base url is relative",
			mutate:  func(c *Config) { c.Anthropic.BaseURL = "/v1" },
			wantErr: []string{"anthropic.base_url", "http"},
		},
		{
			name:    "anthropic base url has a bad scheme",
			mutate:  func(c *Config) { c.Anthropic.BaseURL = "ftp://api.anthropic.com" },
			wantErr: []string{"anthropic.base_url", "http"},
		},
		{
			name:    "anthropic base url is unparsable",
			mutate:  func(c *Config) { c.Anthropic.BaseURL = "https://api.anthropic.com/%zz" },
			wantErr: []string{"anthropic.base_url"},
		},
		{
			name:    "provider id is empty",
			mutate:  func(c *Config) { c.Providers[0].ID = "" },
			wantErr: []string{"providers[0].id", "required"},
		},
		{
			name:    "provider id contains a dash",
			mutate:  func(c *Config) { c.Providers[0].ID = "open-ai" },
			wantErr: []string{"providers[0].id", "open-ai", "first \"-\""},
		},
		{
			name:    "provider id is upper case",
			mutate:  func(c *Config) { c.Providers[0].ID = "OpenAI" },
			wantErr: []string{"providers[0].id", "^[a-z0-9]+$"},
		},
		{
			name:    "provider id has an underscore",
			mutate:  func(c *Config) { c.Providers[0].ID = "open_ai" },
			wantErr: []string{"providers[0].id", "^[a-z0-9]+$"},
		},
		{
			name:    "provider id claude is reserved",
			mutate:  func(c *Config) { c.Providers[0].ID = "claude" },
			wantErr: []string{"providers[0].id", "reserved"},
		},
		{
			name:    "provider id anthropic is reserved",
			mutate:  func(c *Config) { c.Providers[0].ID = "anthropic" },
			wantErr: []string{"providers[0].id", "reserved"},
		},
		{
			name: "duplicate provider ids",
			mutate: func(c *Config) {
				c.Providers = append(c.Providers, Provider{
					ID:      "openai",
					BaseURL: "https://other.example.com/v1",
					APIKey:  "sk-other",
				})
			},
			wantErr: []string{"providers[1].id", "openai", "already"},
		},
		{
			name:    "provider base url is empty",
			mutate:  func(c *Config) { c.Providers[0].BaseURL = "" },
			wantErr: []string{"providers[0].base_url", "required"},
		},
		{
			name:    "provider base url is relative",
			mutate:  func(c *Config) { c.Providers[0].BaseURL = "api.openai.com/v1" },
			wantErr: []string{"providers[0].base_url", "http"},
		},
		{
			name:    "provider base url has a bad scheme",
			mutate:  func(c *Config) { c.Providers[0].BaseURL = "ftp://api.openai.com/v1" },
			wantErr: []string{"providers[0].base_url", "http"},
		},
		{
			name:    "provider api key is empty",
			mutate:  func(c *Config) { c.Providers[0].APIKey = "" },
			wantErr: []string{"providers[0].api_key", "required"},
		},
		{
			name:    "header name has a space",
			mutate:  func(c *Config) { c.Providers[0].Headers = map[string]string{"X Title": "sitka"} },
			wantErr: []string{"providers[0].headers", "X Title"},
		},
		{
			name:    "header name is empty",
			mutate:  func(c *Config) { c.Providers[0].Headers = map[string]string{"": "sitka"} },
			wantErr: []string{"providers[0].headers"},
		},
		{
			name:    "header name is not ascii",
			mutate:  func(c *Config) { c.Providers[0].Headers = map[string]string{"X-Tïtle": "sitka"} },
			wantErr: []string{"providers[0].headers", "X-Tïtle"},
		},
		{
			name:    "header value has a newline",
			mutate:  func(c *Config) { c.Providers[0].Headers = map[string]string{"X-Title": "sitka\ninjected: 1"} },
			wantErr: []string{"providers[0].headers", "X-Title", "line break"},
		},
		{
			name:    "header value has a carriage return",
			mutate:  func(c *Config) { c.Providers[0].Headers = map[string]string{"X-Title": "sitka\rinjected"} },
			wantErr: []string{"providers[0].headers", "X-Title", "line break"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(&c)

			err := c.Validate()
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() error = nil, want an error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Validate() error = %v, want it to mention %q", err, want)
				}
			}
		})
	}
}

func TestConfigValidateReportsEveryProblem(t *testing.T) {
	c := Config{
		Listen:    "127.0.0.1:http",
		Anthropic: Anthropic{BaseURL: "ftp://api.anthropic.com"},
		Providers: []Provider{
			{
				ID:      "open-ai",
				BaseURL: "not a url",
				APIKey:  "",
			},
			{
				ID:      "claude",
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-test",
				Headers: map[string]string{"X Title": "sitka"},
			},
		},
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want an error")
	}

	want := []string{
		"listen",
		"anthropic.base_url",
		"providers[0].id",
		"providers[0].base_url",
		"providers[0].api_key",
		"providers[1].id",
		"providers[1].headers",
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("Validate() error = %v, want it to mention %q", err, w)
		}
	}
}

func TestConfigValidateReportsEveryBadHeaderInOrder(t *testing.T) {
	c := validConfig()
	c.Providers[0].Headers = map[string]string{
		"A Bad":  "ok",
		"Z-Good": "value\n",
		"M Bad":  "ok",
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want an error")
	}

	got := err.Error()
	first := strings.Index(got, "A Bad")
	second := strings.Index(got, "M Bad")
	third := strings.Index(got, "Z-Good")
	if first < 0 || second < 0 || third < 0 {
		t.Fatalf("Validate() error = %v, want it to mention every bad header", err)
	}
	if first >= second || second >= third {
		t.Errorf("Validate() error = %v, want header problems sorted by header name", err)
	}
}
