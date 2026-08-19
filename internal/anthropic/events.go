package anthropic

// Server-sent event names of a streaming POST /v1/messages reply.
const (
	EventMessageStart      = "message_start"
	EventContentBlockStart = "content_block_start"
	EventContentBlockDelta = "content_block_delta"
	EventContentBlockStop  = "content_block_stop"
	EventMessageDelta      = "message_delta"
	EventMessageStop       = "message_stop"
	EventPing              = "ping"
	EventError             = "error"
)

// Delta type discriminators of a content_block_delta event.
const (
	DeltaText      = "text_delta"
	DeltaInputJSON = "input_json_delta"
	DeltaThinking  = "thinking_delta"
	DeltaSignature = "signature_delta"
)

// MessageStartEvent opens a streaming reply with the message shell.
type MessageStartEvent struct {
	Type    string   `json:"type"`
	Message Response `json:"message"`
}

// ContentBlockStartEvent opens one content block.
type ContentBlockStartEvent struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// Delta carries the incremental payload of a content_block_delta event.
type Delta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

// ContentBlockDeltaEvent appends to an open content block.
type ContentBlockDeltaEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta Delta  `json:"delta"`
}

// ContentBlockStopEvent closes one content block.
type ContentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// MessageDeltaEvent reports the stop reason and the final output token count.
type MessageDeltaEvent struct {
	Type  string           `json:"type"`
	Delta MessageDeltaBody `json:"delta"`
	Usage Usage            `json:"usage"`
}

// MessageDeltaBody is the delta payload of a message_delta event.
type MessageDeltaBody struct {
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

// MessageStopEvent closes a streaming reply.
type MessageStopEvent struct {
	Type string `json:"type"`
}

// PingEvent keeps a stream from going silent.
type PingEvent struct {
	Type string `json:"type"`
}

// ErrorEvent reports a mid-stream failure.
type ErrorEvent struct {
	Type  string      `json:"type"`
	Error ErrorDetail `json:"error"`
}
