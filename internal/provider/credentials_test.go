package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCredentialsFromRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r.Header.Set("x-api-key", "sk-ant-123")
	r.Header.Set("Authorization", "Bearer oauth-token")

	got := CredentialsFromRequest(r)
	if got.APIKey != "sk-ant-123" {
		t.Errorf("APIKey = %q, want %q", got.APIKey, "sk-ant-123")
	}
	if got.Authorization != "Bearer oauth-token" {
		t.Errorf("Authorization = %q, want %q", got.Authorization, "Bearer oauth-token")
	}
}

func TestCredentialsApplyLeavesUnsetHeadersAbsent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	Credentials{APIKey: "sk-ant-123"}.Apply(r)

	if got := r.Header.Get("x-api-key"); got != "sk-ant-123" {
		t.Errorf("x-api-key = %q, want %q", got, "sk-ant-123")
	}
	if _, ok := r.Header["Authorization"]; ok {
		t.Error("Authorization should stay absent when the client never sent one")
	}
}

func TestCredentialsContextRoundTrip(t *testing.T) {
	want := Credentials{APIKey: "sk-ant-123", Authorization: "Bearer oauth-token"}
	got, ok := CredentialsFrom(ContextWithCredentials(context.Background(), want))
	if !ok {
		t.Fatal("CredentialsFrom = not found, want the stored credentials")
	}
	if got != want {
		t.Errorf("CredentialsFrom = %+v, want %+v", got, want)
	}
}

func TestCredentialsFromEmptyContext(t *testing.T) {
	if _, ok := CredentialsFrom(context.Background()); ok {
		t.Error("CredentialsFrom = found, want not found on a bare context")
	}
}
