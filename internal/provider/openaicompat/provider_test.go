package openaicompat

import (
	"log/slog"
	"testing"
	"time"

	"github.com/1outres/sitka/internal/config"
)

func TestNewRejectsIncompleteConfig(t *testing.T) {
	valid := config.Provider{ID: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "sk-test"}

	tests := []struct {
		name    string
		cfg     config.Provider
		wantErr bool
	}{
		{name: "valid", cfg: valid},
		{name: "empty id", cfg: config.Provider{BaseURL: valid.BaseURL, APIKey: valid.APIKey}, wantErr: true},
		{name: "empty api key", cfg: config.Provider{ID: valid.ID, BaseURL: valid.BaseURL}, wantErr: true},
		{name: "empty base url", cfg: config.Provider{ID: valid.ID, APIKey: valid.APIKey}, wantErr: true},
		{name: "relative base url", cfg: config.Provider{ID: valid.ID, BaseURL: "/v1", APIKey: valid.APIKey}, wantErr: true},
		{name: "base url without a scheme", cfg: config.Provider{ID: valid.ID, BaseURL: "api.openai.com/v1", APIKey: valid.APIKey}, wantErr: true},
		{name: "base url with another scheme", cfg: config.Provider{ID: valid.ID, BaseURL: "ftp://api.openai.com/v1", APIKey: valid.APIKey}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.cfg, slog.New(slog.DiscardHandler))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("New(%+v) returned no error, want one", tt.cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%+v): %v", tt.cfg, err)
			}
			if p.Name() != tt.cfg.ID {
				t.Errorf("Name() = %q, want %q", p.Name(), tt.cfg.ID)
			}
			if p.pingInterval != defaultPingInterval {
				t.Errorf("pingInterval = %v, want %v", p.pingInterval, defaultPingInterval)
			}
			if p.client.Timeout != 0 {
				t.Errorf("client timeout = %v, want none so a long stream can stay open", p.client.Timeout)
			}
		})
	}
}

func TestNewTrimsTrailingSlashesFromTheBaseURL(t *testing.T) {
	p, err := New(config.Provider{ID: "openai", BaseURL: "https://api.openai.com/v1//", APIKey: "sk-test"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if want := "https://api.openai.com/v1"; p.baseURL != want {
		t.Errorf("baseURL = %q, want %q", p.baseURL, want)
	}
}

func TestDefaultPingIntervalLeavesRoomBeforeTheClientGivesUp(t *testing.T) {
	if defaultPingInterval != 15*time.Second {
		t.Errorf("defaultPingInterval = %v, want %v", defaultPingInterval, 15*time.Second)
	}
}
