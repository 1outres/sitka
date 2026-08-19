package anthropic

import (
	"encoding/json"
	"testing"
)

func TestBlocksUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Blocks
		wantErr bool
	}{
		{
			name:  "bare string becomes one text block",
			input: `"hello"`,
			want:  Blocks{{Type: BlockText, Text: "hello"}},
		},
		{
			name:  "empty string becomes one empty text block",
			input: `""`,
			want:  Blocks{{Type: BlockText, Text: ""}},
		},
		{
			name:  "null becomes nil",
			input: `null`,
			want:  nil,
		},
		{
			name:  "empty array stays empty",
			input: `[]`,
			want:  Blocks{},
		},
		{
			name:  "array of blocks is kept as is",
			input: `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`,
			want:  Blocks{{Type: BlockText, Text: "a"}, {Type: BlockText, Text: "b"}},
		},
		{
			name:    "number is rejected",
			input:   `42`,
			wantErr: true,
		},
		{
			name:    "object is rejected",
			input:   `{"type":"text"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Blocks
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) = nil error, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s) = %v", tt.input, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d blocks, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Type != tt.want[i].Type || got[i].Text != tt.want[i].Text {
					t.Errorf("block %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBlocksText(t *testing.T) {
	blocks := Blocks{
		{Type: BlockText, Text: "one "},
		{Type: BlockToolUse, Name: "read"},
		{Type: BlockText, Text: "two"},
	}
	if got, want := blocks.Text(), "one two"; got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
}

func TestContentBlockRoundTrip(t *testing.T) {
	input := `{"type":"tool_result","tool_use_id":"toolu_1","content":"done","is_error":true}`

	var block ContentBlock
	if err := json.Unmarshal([]byte(input), &block); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if block.ToolUseID != "toolu_1" {
		t.Errorf("ToolUseID = %q, want %q", block.ToolUseID, "toolu_1")
	}
	if got, want := block.Content.Text(), "done"; got != want {
		t.Errorf("Content.Text() = %q, want %q", got, want)
	}
	if !block.IsError {
		t.Error("IsError = false, want true")
	}
}

func TestRequestUnmarshalSystemForms(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "string system",
			input: `{"model":"m","system":"be brief","messages":[]}`,
			want:  "be brief",
		},
		{
			name:  "block array system",
			input: `{"model":"m","system":[{"type":"text","text":"be "},{"type":"text","text":"brief"}],"messages":[]}`,
			want:  "be brief",
		},
		{
			name:  "absent system",
			input: `{"model":"m","messages":[]}`,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req Request
			if err := json.Unmarshal([]byte(tt.input), &req); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got := req.System.Text(); got != tt.want {
				t.Errorf("System.Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentBlockMarshalKeepsEmptyPayloadFields(t *testing.T) {
	tests := []struct {
		name  string
		block ContentBlock
		want  string
	}{
		{
			name:  "text block keeps an empty text field",
			block: ContentBlock{Type: BlockText},
			want:  `{"type":"text","text":""}`,
		},
		{
			name:  "text block keeps its text",
			block: ContentBlock{Type: BlockText, Text: "hi"},
			want:  `{"type":"text","text":"hi"}`,
		},
		{
			name:  "tool_use block keeps an empty input object",
			block: ContentBlock{Type: BlockToolUse, ID: "toolu_1", Name: "read"},
			want:  `{"type":"tool_use","id":"toolu_1","name":"read","input":{}}`,
		},
		{
			name:  "tool_use block keeps its input",
			block: ContentBlock{Type: BlockToolUse, ID: "toolu_1", Name: "read", Input: json.RawMessage(`{"path":"a"}`)},
			want:  `{"type":"tool_use","id":"toolu_1","name":"read","input":{"path":"a"}}`,
		},
		{
			name:  "other block types stay lean",
			block: ContentBlock{Type: BlockToolResult, ToolUseID: "toolu_1"},
			want:  `{"type":"tool_result","tool_use_id":"toolu_1"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.block)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal = %s, want %s", got, tt.want)
			}
		})
	}
}
