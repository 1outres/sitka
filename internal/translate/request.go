package translate

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
)

// ErrNilRequest reports a missing Anthropic request.
var ErrNilRequest = errors.New("translate: request is nil")

const (
	toolTypeFunction = "function"
	emptyToolInput   = "{}"
)

// Tool choice values of the OpenAI API.
const (
	choiceAuto     = "auto"
	choiceRequired = "required"
	choiceNone     = "none"
)

// Request converts an Anthropic Messages request into an OpenAI Chat
// Completions request. upstreamModel replaces the model id.
func Request(req *anthropic.Request, upstreamModel string) (*openai.Request, error) {
	if req == nil {
		return nil, ErrNilRequest
	}

	out := &openai.Request{
		Model:               upstreamModel,
		Messages:            make([]openai.Message, 0, len(req.Messages)+1),
		MaxCompletionTokens: req.MaxTokens,
		Temperature:         req.Temperature,
		TopP:                req.TopP,
		Stop:                req.StopSequences,
		Stream:              req.Stream,
	}
	if req.Stream {
		out.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	}

	system, err := systemMessage(req.System)
	if err != nil {
		return nil, err
	}
	if system != nil {
		out.Messages = append(out.Messages, *system)
	}

	for _, msg := range req.Messages {
		out.Messages, err = appendMessage(out.Messages, msg)
		if err != nil {
			return nil, err
		}
	}

	if out.Tools, err = convertTools(req.Tools); err != nil {
		return nil, err
	}

	if req.ToolChoice != nil {
		choice, err := convertToolChoice(req.ToolChoice)
		if err != nil {
			return nil, err
		}
		out.ToolChoice = choice
		if req.ToolChoice.DisableParallelToolUse {
			parallel := false
			out.ParallelToolCalls = &parallel
		}
	}

	return out, nil
}

func systemMessage(blocks anthropic.Blocks) (*openai.Message, error) {
	for _, block := range blocks {
		if block.Type != anthropic.BlockText {
			return nil, &UnsupportedError{Feature: block.Type}
		}
	}
	text := blocks.Text()
	if text == "" {
		return nil, nil
	}
	return &openai.Message{Role: openai.RoleSystem, Content: openai.TextContent(text)}, nil
}

func appendMessage(dst []openai.Message, msg anthropic.Message) ([]openai.Message, error) {
	var (
		parts        []openai.ContentPart
		toolCalls    []openai.ToolCall
		toolMessages []openai.Message
		hasImage     bool
	)

	for _, block := range msg.Content {
		switch block.Type {
		case anthropic.BlockText:
			parts = append(parts, openai.ContentPart{Type: openai.PartText, Text: block.Text})
		case anthropic.BlockImage:
			url, err := imageURL(block.Source)
			if err != nil {
				return nil, err
			}
			parts = append(parts, openai.ContentPart{
				Type:     openai.PartImageURL,
				ImageURL: &openai.ImageURL{URL: url},
			})
			hasImage = true
		case anthropic.BlockToolUse:
			toolCalls = append(toolCalls, openai.ToolCall{
				ID:   block.ID,
				Type: toolTypeFunction,
				Function: openai.FunctionCall{
					Name:      block.Name,
					Arguments: toolArguments(block.Input),
				},
			})
		case anthropic.BlockToolResult:
			text, err := toolResultText(block.Content)
			if err != nil {
				return nil, err
			}
			toolMessages = append(toolMessages, openai.Message{
				Role:       openai.RoleTool,
				ToolCallID: block.ToolUseID,
				Content:    openai.TextContent(text),
			})
		case anthropic.BlockThinking, anthropic.BlockRedactedThinking:
			// Dropped on purpose: OpenAI has no field that carries reasoning back.
		default:
			return nil, &UnsupportedError{Feature: block.Type}
		}
	}

	// A tool message must directly follow the assistant turn that asked for it,
	// so the tool results of a mixed message go out before its other blocks.
	dst = append(dst, toolMessages...)

	if len(parts) == 0 && len(toolCalls) == 0 {
		return dst, nil
	}

	role := msg.Role
	if len(toolCalls) > 0 {
		role = openai.RoleAssistant
	}
	return append(dst, openai.Message{
		Role:      role,
		Content:   messageContent(parts, hasImage),
		ToolCalls: toolCalls,
	}), nil
}

func messageContent(parts []openai.ContentPart, hasImage bool) openai.Content {
	if hasImage {
		return openai.PartsContent(parts)
	}
	var text strings.Builder
	for _, part := range parts {
		text.WriteString(part.Text)
	}
	return openai.TextContent(text.String())
}

// toolResultText flattens a tool result. An OpenAI tool message can only carry
// text, so anything else in the result has no equivalent and must not be
// dropped without telling the caller.
func toolResultText(blocks anthropic.Blocks) (string, error) {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type != anthropic.BlockText {
			return "", &UnsupportedError{Feature: "tool_result content: " + block.Type}
		}
		text.WriteString(block.Text)
	}
	return text.String(), nil
}

func imageURL(src *anthropic.Source) (string, error) {
	if src == nil {
		return "", &UnsupportedError{Feature: "image without source"}
	}
	switch src.Type {
	case anthropic.SourceBase64:
		return "data:" + src.MediaType + ";base64," + src.Data, nil
	case anthropic.SourceURL:
		return src.URL, nil
	default:
		return "", &UnsupportedError{Feature: "image source: " + src.Type}
	}
}

func toolArguments(input json.RawMessage) string {
	if len(input) == 0 {
		return emptyToolInput
	}
	return string(input)
}

func convertTools(tools []anthropic.Tool) ([]openai.Tool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openai.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "" {
			return nil, &UnsupportedError{Feature: "server tool: " + tool.Type}
		}
		out = append(out, openai.Tool{
			Type: toolTypeFunction,
			Function: openai.FunctionDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return out, nil
}

func convertToolChoice(choice *anthropic.ToolChoice) (any, error) {
	switch choice.Type {
	case anthropic.ToolChoiceAuto:
		return choiceAuto, nil
	case anthropic.ToolChoiceAny:
		return choiceRequired, nil
	case anthropic.ToolChoiceNone:
		return choiceNone, nil
	case anthropic.ToolChoiceTool:
		return openai.NamedToolChoice{
			Type:     toolTypeFunction,
			Function: openai.NamedToolChoiceF{Name: choice.Name},
		}, nil
	default:
		return nil, &UnsupportedError{Feature: "tool_choice: " + choice.Type}
	}
}
