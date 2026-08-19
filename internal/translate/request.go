package translate

import (
	"encoding/json"
	"errors"
	"fmt"
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

// toolSearchPrefix starts the type of every tool search server tool, such as
// tool_search_tool_regex_20251119 and tool_search_tool_bm25_20251119.
const toolSearchPrefix = "tool_search_tool_"

// attributionPrefix starts the block Claude Code puts first in the system
// prompt. The Anthropic API drops that block by position, so an upstream
// without the same rule would read it as part of the prompt. Dropping it here
// instead of at the client keeps it on the requests that go to Anthropic,
// where auto mode needs it to recognize its own classifier calls.
const attributionPrefix = "x-anthropic-billing-header: "

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

	catalog := newToolCatalog(req.Tools)
	for _, msg := range req.Messages {
		out.Messages, err = appendMessage(out.Messages, msg, catalog)
		if err != nil {
			return nil, err
		}
	}

	if out.Tools, err = convertTools(req.Tools, loadedTools(req.Messages)); err != nil {
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
	blocks = withoutAttribution(blocks)
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

// withoutAttribution drops a leading attribution block. Only the first block
// can be one, and a block that starts with the marker counts as attribution in
// whole, which is how the Anthropic API reads it too.
func withoutAttribution(blocks anthropic.Blocks) anthropic.Blocks {
	if len(blocks) == 0 {
		return blocks
	}
	first := blocks[0]
	if first.Type != anthropic.BlockText || !strings.HasPrefix(first.Text, attributionPrefix) {
		return blocks
	}
	return blocks[1:]
}

func appendMessage(dst []openai.Message, msg anthropic.Message, catalog toolCatalog) ([]openai.Message, error) {
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
			text, err := toolResultText(block.Content, catalog)
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

// toolCatalog indexes the tools of one request by name, so that a tool_reference
// can be resolved to the definition it points at.
type toolCatalog map[string]anthropic.Tool

func newToolCatalog(tools []anthropic.Tool) toolCatalog {
	catalog := make(toolCatalog, len(tools))
	for _, tool := range tools {
		if tool.Name == "" {
			continue
		}
		catalog[tool.Name] = tool
	}
	return catalog
}

// toolResultText flattens a tool result. An OpenAI tool message can only carry
// text, so anything else in the result has no equivalent and must not be
// dropped without telling the caller.
func toolResultText(blocks anthropic.Blocks, catalog toolCatalog) (string, error) {
	var text strings.Builder
	var referenced []anthropic.Tool
	for _, block := range blocks {
		switch block.Type {
		case anthropic.BlockText:
			text.WriteString(block.Text)
		case anthropic.BlockToolReference:
			tool, ok := catalog[block.ToolName]
			if !ok {
				return "", fmt.Errorf("translate: tool_reference names %q, which this request does not define", block.ToolName)
			}
			referenced = append(referenced, tool)
		default:
			return "", &UnsupportedError{Feature: "tool_result content: " + block.Type}
		}
	}

	if len(referenced) == 0 {
		return text.String(), nil
	}
	block, err := functionsBlock(referenced)
	if err != nil {
		return "", err
	}
	return text.String() + block, nil
}

// functionsBlock writes tool definitions the way the tool search tool of the
// client tells the model to expect them. The Anthropic API turns a
// tool_reference into a usable definition itself; an OpenAI-compatible API has
// no such step, so the gateway spells the definition out instead.
func functionsBlock(tools []anthropic.Tool) (string, error) {
	var out strings.Builder
	out.WriteString("<functions>\n")
	for _, tool := range tools {
		line, err := json.Marshal(functionDoc{
			Description: tool.Description,
			Name:        tool.Name,
			Parameters:  tool.InputSchema,
		})
		if err != nil {
			return "", fmt.Errorf("translate: write the definition of tool %q: %w", tool.Name, err)
		}
		out.WriteString("<function>")
		out.Write(line)
		out.WriteString("</function>\n")
	}
	out.WriteString("</functions>")
	return out.String(), nil
}

// functionDoc is one entry of a functions block. The field order is the one the
// tool search tool documents, so it must stay as written.
type functionDoc struct {
	Description string          `json:"description"`
	Name        string          `json:"name"`
	Parameters  json.RawMessage `json:"parameters"`
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

// loadedTools collects the tools a tool_reference has already named. The
// Anthropic API keeps a deferred tool out of the model's context until that
// happens, so the gateway holds it back for the same turns.
func loadedTools(messages []anthropic.Message) map[string]struct{} {
	loaded := make(map[string]struct{})
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type != anthropic.BlockToolResult {
				continue
			}
			for _, inner := range block.Content {
				if inner.Type == anthropic.BlockToolReference && inner.ToolName != "" {
					loaded[inner.ToolName] = struct{}{}
				}
			}
		}
	}
	return loaded
}

func convertTools(tools []anthropic.Tool, loaded map[string]struct{}) ([]openai.Tool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openai.Tool, 0, len(tools))
	for _, tool := range tools {
		// A tool search tool has no OpenAI equivalent, and dropping it costs
		// nothing: a search only reveals tools this request already carries.
		if strings.HasPrefix(tool.Type, toolSearchPrefix) {
			continue
		}
		if tool.Type != "" {
			return nil, &UnsupportedError{Feature: "server tool: " + tool.Type}
		}
		if _, discovered := loaded[tool.Name]; tool.DeferLoading && !discovered {
			continue
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
