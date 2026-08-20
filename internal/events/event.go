// Package events publishes what the gateway routed, so that a watcher can
// follow the traffic while it happens.
package events

import "time"

// Event is one finished request as the gateway saw it. The routing fields stay
// empty for a request that never reached a model, such as the connection probe
// Claude Code sends at startup.
type Event struct {
	Time          time.Time `json:"time"`
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	Status        int       `json:"status"`
	DurationMS    int64     `json:"duration_ms"`
	Model         string    `json:"model,omitempty"`
	Provider      string    `json:"provider,omitempty"`
	UpstreamModel string    `json:"upstream_model,omitempty"`
	Stream        bool      `json:"stream,omitempty"`
	Usage         *Usage    `json:"usage,omitempty"`
	StopReason    string    `json:"stop_reason,omitempty"`
	Session       string    `json:"session,omitempty"`
	Agent         string    `json:"agent,omitempty"`
}

// Usage reports the token counts the reply carried.
type Usage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
}
