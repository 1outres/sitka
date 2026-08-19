// Package openai defines the wire types of the OpenAI Chat Completions API.
// It holds no translation logic; it only mirrors the API contract.
package openai

import (
	"encoding/json"
	"errors"
)

// Message roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Request is the body of POST /chat/completions.
type Request struct {
	Model               string         `json:"model"`
	Messages            []Message      `json:"messages"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	TopP                *float64       `json:"top_p,omitempty"`
	Stop                []string       `json:"stop,omitempty"`
	Stream              bool           `json:"stream,omitempty"`
	StreamOptions       *StreamOptions `json:"stream_options,omitempty"`
	Tools               []Tool         `json:"tools,omitempty"`
	ToolChoice          any            `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool          `json:"parallel_tool_calls,omitempty"`
}

// StreamOptions asks the upstream for extra data in the stream.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// Message is one turn of the conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    Content    `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// Content part discriminators.
const (
	PartText     = "text"
	PartImageURL = "image_url"
)

// Content is a message body that the API accepts either as a bare string or as
// an array of parts. It marshals as a string when it holds only text.
type Content struct {
	Text  string
	Parts []ContentPart
}

// TextContent builds a plain text message body.
func TextContent(text string) Content {
	return Content{Text: text}
}

// PartsContent builds a multimodal message body.
func PartsContent(parts []ContentPart) Content {
	return Content{Parts: parts}
}

// IsZero reports whether the body carries nothing.
func (c Content) IsZero() bool {
	return c.Text == "" && len(c.Parts) == 0
}

// MarshalJSON emits the string form when the body holds only text.
func (c Content) MarshalJSON() ([]byte, error) {
	if len(c.Parts) == 0 {
		return json.Marshal(c.Text)
	}
	if c.Text != "" {
		return nil, errors.New("openai: content cannot hold both text and parts")
	}
	return json.Marshal(c.Parts)
}

// UnmarshalJSON accepts both the string and the array form.
func (c *Content) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*c = Content{}
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*c = Content{Text: s}
		return nil
	}
	var parts []ContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	*c = Content{Parts: parts}
	return nil
}

// ContentPart is one entry of a multimodal message body.
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL carries an image as a URL or a data URI.
type ImageURL struct {
	URL string `json:"url"`
}

// ToolCall is a function call the model requested. In a streaming delta only
// Index is always present, and the other fields arrive piecewise.
type ToolCall struct {
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the callee and its JSON-encoded arguments.
type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// Tool declares a function the model may call.
type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef is the schema of a callable function.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// NamedToolChoice pins the model to one function.
type NamedToolChoice struct {
	Type     string           `json:"type"`
	Function NamedToolChoiceF `json:"function"`
}

// NamedToolChoiceF names the pinned function.
type NamedToolChoiceF struct {
	Name string `json:"name"`
}

// Finish reasons reported when a turn ends.
const (
	FinishStop          = "stop"
	FinishLength        = "length"
	FinishToolCalls     = "tool_calls"
	FinishContentFilter = "content_filter"
)

// Response is the body of a non-streaming reply.
type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice is one completion of a non-streaming reply.
type Choice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ResponseMessage is the assistant turn of a non-streaming reply.
type ResponseMessage struct {
	Role      string     `json:"role"`
	Content   *string    `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Refusal   *string    `json:"refusal,omitempty"`
}

// Usage reports the token counts of one request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ModelList is the body of GET /models.
type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// Model is one entry of GET /models.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	Created int64  `json:"created,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// ErrorResponse is the API error envelope.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes one upstream failure.
type ErrorDetail struct {
	Message string          `json:"message"`
	Type    string          `json:"type,omitempty"`
	Param   *string         `json:"param,omitempty"`
	Code    json.RawMessage `json:"code,omitempty"`
}
