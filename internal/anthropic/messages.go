package anthropic

import "encoding/json"

// Message roles.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Request is the body of POST /v1/messages.
type Request struct {
	Model         string      `json:"model"`
	Messages      []Message   `json:"messages"`
	MaxTokens     int         `json:"max_tokens,omitempty"`
	System        Blocks      `json:"system,omitempty"`
	Tools         []Tool      `json:"tools,omitempty"`
	ToolChoice    *ToolChoice `json:"tool_choice,omitempty"`
	Temperature   *float64    `json:"temperature,omitempty"`
	TopP          *float64    `json:"top_p,omitempty"`
	TopK          *int        `json:"top_k,omitempty"`
	StopSequences []string    `json:"stop_sequences,omitempty"`
	Stream        bool        `json:"stream,omitempty"`

	OutputConfig *OutputConfig `json:"output_config,omitempty"`
}

// OutputConfig carries the output settings of one request. The gateway reads
// only the effort level; the rest of the field has no OpenAI equivalent.
type OutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

// Message is one turn of the conversation.
type Message struct {
	Role    string `json:"role"`
	Content Blocks `json:"content"`
}

// Tool declares a client tool the model may call. A tool with DeferLoading set
// stays out of the model's context until a tool_reference names it.
type Tool struct {
	Type         string          `json:"type,omitempty"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	DeferLoading bool            `json:"defer_loading,omitempty"`
}

// Tool choice discriminators.
const (
	ToolChoiceAuto = "auto"
	ToolChoiceAny  = "any"
	ToolChoiceTool = "tool"
	ToolChoiceNone = "none"
)

// ToolChoice constrains which tool the model may call.
type ToolChoice struct {
	Type                   string `json:"type"`
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

// Stop reasons reported when a turn ends.
const (
	StopEndTurn      = "end_turn"
	StopMaxTokens    = "max_tokens"
	StopStopSequence = "stop_sequence"
	StopToolUse      = "tool_use"
	StopRefusal      = "refusal"
)

// Response is the body of a non-streaming POST /v1/messages reply.
type Response struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	StopReason   *string        `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

// Usage reports the token counts of one request.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// CountTokensResponse is the body of POST /v1/messages/count_tokens.
type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// Model is one entry of GET /v1/models.
type Model struct {
	ID          string `json:"id"`
	Type        string `json:"type,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// ModelList is the body of GET /v1/models.
type ModelList struct {
	Data    []Model `json:"data"`
	HasMore bool    `json:"has_more"`
	FirstID *string `json:"first_id"`
	LastID  *string `json:"last_id"`
}
