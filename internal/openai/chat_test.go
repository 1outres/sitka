package openai

import (
	"encoding/json"
	"testing"
)

func TestContentMarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		content Content
		want    string
		wantErr bool
	}{
		{
			name:    "text only marshals as a bare string",
			content: TextContent("hello"),
			want:    `"hello"`,
		},
		{
			name:    "empty content marshals as an empty string",
			content: Content{},
			want:    `""`,
		},
		{
			name: "parts marshal as an array",
			content: PartsContent([]ContentPart{
				{Type: PartText, Text: "look"},
				{Type: PartImageURL, ImageURL: &ImageURL{URL: "data:image/png;base64,AAAA"}},
			}),
			want: `[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]`,
		},
		{
			name:    "text and parts together is rejected",
			content: Content{Text: "a", Parts: []ContentPart{{Type: PartText, Text: "b"}}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Marshal = nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestContentUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantText  string
		wantParts int
	}{
		{name: "string", input: `"hi"`, wantText: "hi"},
		{name: "null", input: `null`},
		{name: "parts", input: `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, wantParts: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Content
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tt.wantText)
			}
			if len(got.Parts) != tt.wantParts {
				t.Errorf("len(Parts) = %d, want %d", len(got.Parts), tt.wantParts)
			}
		})
	}
}

func TestContentIsZero(t *testing.T) {
	if !(Content{}).IsZero() {
		t.Error("empty Content should be zero")
	}
	if TextContent("a").IsZero() {
		t.Error("text Content should not be zero")
	}
	if PartsContent([]ContentPart{{Type: PartText}}).IsZero() {
		t.Error("parts Content should not be zero")
	}
}

func TestRequestOmitsUnsetFields(t *testing.T) {
	got, err := json.Marshal(&Request{Model: "gpt-5.2", Messages: []Message{}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"model":"gpt-5.2","messages":[]}`
	if string(got) != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
}
