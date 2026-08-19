// Package anthropic defines the wire types of the Anthropic Messages API.
// It holds no translation logic; it only mirrors the API contract.
package anthropic

import (
	"encoding/json"
	"fmt"
)

// Block type discriminators used in a content array.
const (
	BlockText             = "text"
	BlockImage            = "image"
	BlockDocument         = "document"
	BlockToolUse          = "tool_use"
	BlockToolResult       = "tool_result"
	BlockToolReference    = "tool_reference"
	BlockThinking         = "thinking"
	BlockRedactedThinking = "redacted_thinking"
)

// ContentBlock is one entry of a content array. The API models content as a
// tagged union, so Type selects which of the remaining fields carry a value.
type ContentBlock struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"`

	Source *Source `json:"source,omitempty"`

	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	ToolUseID string `json:"tool_use_id,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	Content   Blocks `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`

	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// Source carries the payload of an image or document block.
type Source struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
}

// Source type discriminators.
const (
	SourceBase64 = "base64"
	SourceURL    = "url"
	SourceText   = "text"
	SourceFile   = "file"
)

// CacheControl marks a prompt prefix boundary for caching.
type CacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

// MarshalJSON keeps the payload field a block type always carries, even when it
// is empty. The API opens a stream with `"text": ""` on a text block and
// `"input": {}` on a tool_use block, and clients append onto those fields, so
// omitting them makes a client build its value on top of nothing.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	type plain ContentBlock
	shadow := struct {
		plain
		Text  *string          `json:"text,omitempty"`
		Input *json.RawMessage `json:"input,omitempty"`
	}{plain: plain(b)}

	if b.Type == BlockText || b.Text != "" {
		shadow.Text = &b.Text
	}

	input := b.Input
	if b.Type == BlockToolUse && len(input) == 0 {
		input = json.RawMessage("{}")
	}
	if len(input) > 0 {
		shadow.Input = &input
	}

	return json.Marshal(shadow)
}

// Blocks is a content field that the API accepts either as a bare string or as
// an array of blocks. A bare string is normalized into a single text block.
type Blocks []ContentBlock

// UnmarshalJSON accepts both the string and the array form.
func (b *Blocks) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*b = nil
		return nil
	}
	switch data[0] {
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*b = Blocks{{Type: BlockText, Text: s}}
		return nil
	case '[':
		var blocks []ContentBlock
		if err := json.Unmarshal(data, &blocks); err != nil {
			return err
		}
		*b = blocks
		return nil
	default:
		return fmt.Errorf("anthropic: content must be a string or an array, got %s", firstToken(data))
	}
}

// Text concatenates the text of every text block, ignoring other block types.
func (b Blocks) Text() string {
	var out string
	for _, block := range b {
		if block.Type == BlockText {
			out += block.Text
		}
	}
	return out
}

func firstToken(data []byte) string {
	const limit = 16
	if len(data) > limit {
		return string(data[:limit]) + "..."
	}
	return string(data)
}
