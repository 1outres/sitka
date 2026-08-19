package translate

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1outres/sitka/internal/anthropic"
)

func TestRequest(t *testing.T) {
	temperature := 0.5
	topP := 0.9
	topK := 40

	tests := []struct {
		name string
		req  *anthropic.Request
		want string
	}{
		{
			name: "plain text",
			req: &anthropic.Request{
				Model:     "openai-gpt-5.2",
				MaxTokens: 1024,
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockText, Text: "hello"},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1024}`,
		},
		{
			name: "system prompt",
			req: &anthropic.Request{
				System: anthropic.Blocks{
					{Type: anthropic.BlockText, Text: "be terse"},
					{Type: anthropic.BlockText, Text: " and kind"},
				},
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockText, Text: "hi"},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"system","content":"be terse and kind"},{"role":"user","content":"hi"}]}`,
		},
		{
			name: "empty system emits no system message",
			req: &anthropic.Request{
				System: anthropic.Blocks{},
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockText, Text: "hi"},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "base64 image",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockText, Text: "look"},
						{Type: anthropic.BlockImage, Source: &anthropic.Source{
							Type:      anthropic.SourceBase64,
							MediaType: "image/png",
							Data:      "QUJD",
						}},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}]}]}`,
		},
		{
			name: "url image",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockImage, Source: &anthropic.Source{
							Type: anthropic.SourceURL,
							URL:  "https://example.com/cat.png",
						}},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}]}]}`,
		},
		{
			name: "assistant tool use",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleAssistant, Content: anthropic.Blocks{
						{Type: anthropic.BlockText, Text: "reading it"},
						{Type: anthropic.BlockToolUse, ID: "call_1", Name: "read", Input: json.RawMessage(`{"path":"/tmp"}`)},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"assistant","content":"reading it","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"/tmp\"}"}}]}]}`,
		},
		{
			name: "tool use without input",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleAssistant, Content: anthropic.Blocks{
						{Type: anthropic.BlockToolUse, ID: "call_1", Name: "now"},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"now","arguments":"{}"}}]}]}`,
		},
		{
			name: "tool result mixed with text",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockToolResult, ToolUseID: "call_1", Content: anthropic.Blocks{
							{Type: anthropic.BlockText, Text: "file body"},
						}},
						{Type: anthropic.BlockText, Text: "what now?"},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"tool","content":"file body","tool_call_id":"call_1"},{"role":"user","content":"what now?"}]}`,
		},
		{
			name: "two tool results only",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockToolResult, ToolUseID: "call_1", Content: anthropic.Blocks{
							{Type: anthropic.BlockText, Text: "one"},
						}},
						{Type: anthropic.BlockToolResult, ToolUseID: "call_2", Content: anthropic.Blocks{
							{Type: anthropic.BlockText, Text: "two"},
						}},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"tool","content":"one","tool_call_id":"call_1"},{"role":"tool","content":"two","tool_call_id":"call_2"}]}`,
		},
		{
			name: "thinking blocks are dropped",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleAssistant, Content: anthropic.Blocks{
						{Type: anthropic.BlockThinking, Thinking: "hmm", Signature: "sig"},
						{Type: anthropic.BlockRedactedThinking, Data: "opaque"},
						{Type: anthropic.BlockText, Text: "the answer"},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[{"role":"assistant","content":"the answer"}]}`,
		},
		{
			name: "message with only thinking is dropped",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleAssistant, Content: anthropic.Blocks{
						{Type: anthropic.BlockThinking, Thinking: "hmm"},
					}},
				},
			},
			want: `{"model":"gpt-5.2","messages":[]}`,
		},
		{
			name: "tools with auto tool choice",
			req: &anthropic.Request{
				Tools: []anthropic.Tool{
					{Name: "read", Description: "read a file", InputSchema: json.RawMessage(`{"type":"object"}`)},
				},
				ToolChoice: &anthropic.ToolChoice{Type: anthropic.ToolChoiceAuto},
			},
			want: `{"model":"gpt-5.2","messages":[],"tools":[{"type":"function","function":{"name":"read","description":"read a file","parameters":{"type":"object"}}}],"tool_choice":"auto"}`,
		},
		{
			name: "any tool choice",
			req: &anthropic.Request{
				ToolChoice: &anthropic.ToolChoice{Type: anthropic.ToolChoiceAny},
			},
			want: `{"model":"gpt-5.2","messages":[],"tool_choice":"required"}`,
		},
		{
			name: "none tool choice",
			req: &anthropic.Request{
				ToolChoice: &anthropic.ToolChoice{Type: anthropic.ToolChoiceNone},
			},
			want: `{"model":"gpt-5.2","messages":[],"tool_choice":"none"}`,
		},
		{
			name: "named tool choice",
			req: &anthropic.Request{
				ToolChoice: &anthropic.ToolChoice{Type: anthropic.ToolChoiceTool, Name: "read"},
			},
			want: `{"model":"gpt-5.2","messages":[],"tool_choice":{"type":"function","function":{"name":"read"}}}`,
		},
		{
			name: "disabled parallel tool use",
			req: &anthropic.Request{
				ToolChoice: &anthropic.ToolChoice{Type: anthropic.ToolChoiceAuto, DisableParallelToolUse: true},
			},
			want: `{"model":"gpt-5.2","messages":[],"tool_choice":"auto","parallel_tool_calls":false}`,
		},
		{
			name: "sampling parameters",
			req: &anthropic.Request{
				Temperature:   &temperature,
				TopP:          &topP,
				TopK:          &topK,
				StopSequences: []string{"STOP", "HALT"},
			},
			want: `{"model":"gpt-5.2","messages":[],"temperature":0.5,"top_p":0.9,"stop":["STOP","HALT"]}`,
		},
		{
			name: "streaming asks for usage",
			req: &anthropic.Request{
				Stream: true,
			},
			want: `{"model":"gpt-5.2","messages":[],"stream":true,"stream_options":{"include_usage":true}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Request(tt.req, "gpt-5.2")
			if err != nil {
				t.Fatalf("Request() error = %v", err)
			}
			assertJSON(t, got, tt.want)
		})
	}
}

func TestRequestUnsupported(t *testing.T) {
	tests := []struct {
		name    string
		req     *anthropic.Request
		feature string
	}{
		{
			name: "unknown block type",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockDocument},
					}},
				},
			},
			feature: "document",
		},
		{
			name: "unknown system block type",
			req: &anthropic.Request{
				System: anthropic.Blocks{{Type: anthropic.BlockImage}},
			},
			feature: "image",
		},
		{
			name: "server tool",
			req: &anthropic.Request{
				Tools: []anthropic.Tool{{Type: "web_search_20250305", Name: "web_search"}},
			},
			feature: "server tool: web_search_20250305",
		},
		{
			name: "image source type",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockImage, Source: &anthropic.Source{Type: anthropic.SourceFile, FileID: "file_1"}},
					}},
				},
			},
			feature: "image source: file",
		},
		{
			name: "image without source",
			req: &anthropic.Request{
				Messages: []anthropic.Message{
					{Role: anthropic.RoleUser, Content: anthropic.Blocks{
						{Type: anthropic.BlockImage},
					}},
				},
			},
			feature: "image without source",
		},
		{
			name: "unknown tool choice",
			req: &anthropic.Request{
				ToolChoice: &anthropic.ToolChoice{Type: "magic"},
			},
			feature: "tool_choice: magic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Request(tt.req, "gpt-5.2")
			if got != nil {
				t.Errorf("Request() = %v, want nil result", got)
			}
			var unsupported *UnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("Request() error = %v, want *UnsupportedError", err)
			}
			if unsupported.Feature != tt.feature {
				t.Errorf("Feature = %q, want %q", unsupported.Feature, tt.feature)
			}
			if !strings.Contains(unsupported.Error(), tt.feature) {
				t.Errorf("Error() = %q, want it to name %q", unsupported.Error(), tt.feature)
			}
		})
	}
}

func TestRequestNil(t *testing.T) {
	got, err := Request(nil, "gpt-5.2")
	if !errors.Is(err, ErrNilRequest) {
		t.Fatalf("Request() error = %v, want %v", err, ErrNilRequest)
	}
	if got != nil {
		t.Errorf("Request() = %v, want nil result", got)
	}
}

func TestRequestRejectsNonTextToolResult(t *testing.T) {
	req := &anthropic.Request{
		Model: "claude-sonnet-5",
		Messages: []anthropic.Message{{
			Role: anthropic.RoleUser,
			Content: anthropic.Blocks{{
				Type:      anthropic.BlockToolResult,
				ToolUseID: "toolu_1",
				Content: anthropic.Blocks{
					{Type: anthropic.BlockText, Text: "here is the file"},
					{Type: anthropic.BlockImage, Source: &anthropic.Source{
						Type: anthropic.SourceBase64, MediaType: "image/png", Data: "AAAA",
					}},
				},
			}},
		}},
	}

	_, err := Request(req, "gpt-5.2")
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Request error = %v, want an *UnsupportedError", err)
	}
	if !strings.Contains(unsupported.Feature, anthropic.BlockImage) {
		t.Errorf("Feature = %q, want it to name the image block", unsupported.Feature)
	}
}
