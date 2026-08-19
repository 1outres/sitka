package translate

import (
	"testing"

	"github.com/1outres/sitka/internal/openai"
)

func TestResponse(t *testing.T) {
	tests := []struct {
		name string
		resp *openai.Response
		want string
	}{
		{
			name: "text only",
			resp: &openai.Response{
				ID:    "chatcmpl-1",
				Model: "gpt-5.2",
				Choices: []openai.Choice{{
					Message:      openai.ResponseMessage{Role: openai.RoleAssistant, Content: strPtr("hi there")},
					FinishReason: openai.FinishStop,
				}},
				Usage: &openai.Usage{PromptTokens: 11, CompletionTokens: 3, TotalTokens: 14},
			},
			want: `{"id":"chatcmpl-1","type":"message","role":"assistant","model":"openai-gpt-5.2",
				"content":[{"type":"text","text":"hi there"}],
				"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":11,"output_tokens":3}}`,
		},
		{
			name: "tool calls only",
			resp: &openai.Response{
				ID: "chatcmpl-2",
				Choices: []openai.Choice{{
					Message: openai.ResponseMessage{
						Role: openai.RoleAssistant,
						ToolCalls: []openai.ToolCall{
							{ID: "call_1", Type: "function", Function: openai.FunctionCall{Name: "read", Arguments: `{"path":"/tmp"}`}},
							{ID: "call_2", Type: "function", Function: openai.FunctionCall{Name: "now"}},
						},
					},
					FinishReason: openai.FinishToolCalls,
				}},
			},
			want: `{"id":"chatcmpl-2","type":"message","role":"assistant","model":"openai-gpt-5.2",
				"content":[
					{"type":"tool_use","id":"call_1","name":"read","input":{"path":"/tmp"}},
					{"type":"tool_use","id":"call_2","name":"now","input":{}}
				],
				"stop_reason":"tool_use","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
		{
			name: "text before tool calls",
			resp: &openai.Response{
				ID: "chatcmpl-3",
				Choices: []openai.Choice{{
					Message: openai.ResponseMessage{
						Role:    openai.RoleAssistant,
						Content: strPtr("let me look"),
						ToolCalls: []openai.ToolCall{
							{ID: "call_1", Function: openai.FunctionCall{Name: "read", Arguments: `{}`}},
						},
					},
					FinishReason: openai.FinishToolCalls,
				}},
			},
			want: `{"id":"chatcmpl-3","type":"message","role":"assistant","model":"openai-gpt-5.2",
				"content":[
					{"type":"text","text":"let me look"},
					{"type":"tool_use","id":"call_1","name":"read","input":{}}
				],
				"stop_reason":"tool_use","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
		{
			name: "empty content emits no text block",
			resp: &openai.Response{
				ID: "chatcmpl-4",
				Choices: []openai.Choice{{
					Message:      openai.ResponseMessage{Role: openai.RoleAssistant, Content: strPtr("")},
					FinishReason: openai.FinishStop,
				}},
			},
			want: `{"id":"chatcmpl-4","type":"message","role":"assistant","model":"openai-gpt-5.2",
				"content":[],"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
		{
			name: "nil content emits no text block",
			resp: &openai.Response{
				ID: "chatcmpl-5",
				Choices: []openai.Choice{{
					Message:      openai.ResponseMessage{Role: openai.RoleAssistant},
					FinishReason: openai.FinishLength,
				}},
			},
			want: `{"id":"chatcmpl-5","type":"message","role":"assistant","model":"openai-gpt-5.2",
				"content":[],"stop_reason":"max_tokens","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
		{
			name: "later choices are ignored",
			resp: &openai.Response{
				ID: "chatcmpl-6",
				Choices: []openai.Choice{
					{Message: openai.ResponseMessage{Content: strPtr("first")}, FinishReason: openai.FinishStop},
					{Message: openai.ResponseMessage{Content: strPtr("second")}, FinishReason: openai.FinishStop},
				},
			},
			want: `{"id":"chatcmpl-6","type":"message","role":"assistant","model":"openai-gpt-5.2",
				"content":[{"type":"text","text":"first"}],
				"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Response(tt.resp, "openai-gpt-5.2")
			if err != nil {
				t.Fatalf("Response() error = %v", err)
			}
			assertJSON(t, got, tt.want)
		})
	}
}

func TestResponseStopReason(t *testing.T) {
	tests := []struct {
		finish string
		want   *string
	}{
		{openai.FinishStop, strPtr("end_turn")},
		{openai.FinishLength, strPtr("max_tokens")},
		{openai.FinishToolCalls, strPtr("tool_use")},
		{openai.FinishContentFilter, strPtr("refusal")},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run("finish_"+tt.finish, func(t *testing.T) {
			resp := &openai.Response{
				Choices: []openai.Choice{{FinishReason: tt.finish}},
			}
			got, err := Response(resp, "openai-gpt-5.2")
			if err != nil {
				t.Fatalf("Response() error = %v", err)
			}
			switch {
			case tt.want == nil && got.StopReason != nil:
				t.Fatalf("StopReason = %q, want nil", *got.StopReason)
			case tt.want != nil && got.StopReason == nil:
				t.Fatalf("StopReason = nil, want %q", *tt.want)
			case tt.want != nil && *got.StopReason != *tt.want:
				t.Fatalf("StopReason = %q, want %q", *got.StopReason, *tt.want)
			}
		})
	}
}

func TestResponseErrors(t *testing.T) {
	tests := []struct {
		name string
		resp *openai.Response
	}{
		{name: "nil response"},
		{name: "no choices", resp: &openai.Response{ID: "chatcmpl-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Response(tt.resp, "openai-gpt-5.2")
			if err == nil {
				t.Fatalf("Response() error = nil, want an error")
			}
			if got != nil {
				t.Errorf("Response() = %v, want nil result", got)
			}
		})
	}
}
