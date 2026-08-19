package translate

import (
	"encoding/json"
	"errors"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
)

// Errors reported when an OpenAI reply carries no usable completion.
var (
	ErrNilResponse = errors.New("translate: response is nil")
	ErrNoChoices   = errors.New("translate: response has no choices")
)

// messageType is the Type field of an Anthropic message reply.
const messageType = "message"

// Response converts a non-streaming OpenAI reply into an Anthropic reply.
// anthropicModel is echoed back as the response model id.
func Response(resp *openai.Response, anthropicModel string) (*anthropic.Response, error) {
	if resp == nil {
		return nil, ErrNilResponse
	}
	if len(resp.Choices) == 0 {
		return nil, ErrNoChoices
	}

	choice := resp.Choices[0]
	out := &anthropic.Response{
		ID:         resp.ID,
		Type:       messageType,
		Role:       anthropic.RoleAssistant,
		Model:      anthropicModel,
		Content:    make([]anthropic.ContentBlock, 0, len(choice.Message.ToolCalls)+1),
		StopReason: stopReason(choice.FinishReason),
	}

	if text := choice.Message.Content; text != nil && *text != "" {
		out.Content = append(out.Content, anthropic.ContentBlock{Type: anthropic.BlockText, Text: *text})
	}
	for _, call := range choice.Message.ToolCalls {
		out.Content = append(out.Content, toolUseBlock(call.ID, call.Function.Name, toolInput(call.Function.Arguments)))
	}

	if resp.Usage != nil {
		out.Usage = anthropic.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}

	return out, nil
}

func toolUseBlock(id, name string, input json.RawMessage) anthropic.ContentBlock {
	return anthropic.ContentBlock{
		Type:  anthropic.BlockToolUse,
		ID:    id,
		Name:  name,
		Input: input,
	}
}

func toolInput(arguments string) json.RawMessage {
	if arguments == "" {
		return json.RawMessage(emptyToolInput)
	}
	return json.RawMessage(arguments)
}
