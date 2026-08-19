package translate

import (
	"encoding/json"
	"testing"
)

func decodeJSON(t *testing.T, data []byte) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return out
}

func assertJSON(t *testing.T, got any, want string) {
	t.Helper()
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	gotJSON := decodeJSON(t, data)
	wantJSON := decodeJSON(t, []byte(want))
	if !jsonEqual(gotJSON, wantJSON) {
		t.Errorf("json mismatch\ngot:  %s\nwant: %s", data, want)
	}
}

func jsonEqual(a, b any) bool {
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(x) == string(y)
}

func strPtr(s string) *string {
	return &s
}
