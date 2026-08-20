package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
)

// maxScannedBody bounds what a scanner holds while it looks for token counts.
// The gateway writes every reply through a scanner, so its memory must not grow
// with the model's output.
const maxScannedBody = 4 << 20

// Keys a payload must mention before it is worth decoding. Content deltas make
// up nearly all of a stream and carry none of them.
var (
	usageKey       = []byte(`"usage"`)
	stopReasonKey  = []byte(`"stop_reason"`)
	inputTokensKey = []byte(`"input_tokens"`)
)

// UsageScanner reads a copy of a Messages API reply and picks out the token
// counts and the stop reason. Every provider answers in the Anthropic format,
// so one scanner covers all of them.
type UsageScanner struct {
	stream bool
	buf    bytes.Buffer
	// skipLine drops the rest of a stream line that grew past the bound.
	skipLine bool
	// tooLarge marks a single reply the scanner had to give up on.
	tooLarge   bool
	usage      Usage
	stopReason string
}

// NewUsageScanner builds a scanner for a reply of the given content type. A
// text/event-stream reply is read frame by frame, anything else as one body.
func NewUsageScanner(contentType string) *UsageScanner {
	return &UsageScanner{stream: strings.HasPrefix(contentType, "text/event-stream")}
}

// Write consumes a chunk of the reply. It never reports an error, so a caller
// can tee the response into it and pass the bytes on unchanged.
func (u *UsageScanner) Write(p []byte) (int, error) {
	if u.stream {
		u.writeStream(p)
		return len(p), nil
	}
	u.writeBody(p)
	return len(p), nil
}

// Report returns what the reply told about itself. A reply that reported
// nothing comes back empty.
func (u *UsageScanner) Report() (Usage, string) {
	if !u.stream && u.buf.Len() > 0 {
		u.merge(u.buf.Bytes())
		u.buf.Reset()
	}
	return u.usage, u.stopReason
}

func (u *UsageScanner) writeStream(p []byte) {
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		if end < 0 {
			u.hold(p)
			return
		}
		u.hold(p[:end])
		u.endLine()
		p = p[end+1:]
	}
}

func (u *UsageScanner) writeBody(p []byte) {
	if u.tooLarge {
		return
	}
	if u.buf.Len()+len(p) > maxScannedBody {
		u.tooLarge = true
		u.buf.Reset()
		return
	}
	u.buf.Write(p)
}

func (u *UsageScanner) hold(p []byte) {
	if u.skipLine {
		return
	}
	if u.buf.Len()+len(p) > maxScannedBody {
		u.skipLine = true
		u.buf.Reset()
		return
	}
	u.buf.Write(p)
}

func (u *UsageScanner) endLine() {
	dropped := u.skipLine
	u.skipLine = false
	defer u.buf.Reset()

	if dropped {
		return
	}
	payload, ok := bytes.CutPrefix(bytes.TrimRight(u.buf.Bytes(), "\r"), []byte("data:"))
	if !ok {
		return
	}
	u.merge(bytes.TrimSpace(payload))
}

// usageFrame is the part of a reply the scanner reads. The same shape covers a
// whole reply, a message_start event, a message_delta event and a token count.
type usageFrame struct {
	Message *struct {
		Usage Usage `json:"usage"`
	} `json:"message"`
	Usage *Usage `json:"usage"`
	Delta *struct {
		StopReason *string `json:"stop_reason"`
	} `json:"delta"`
	StopReason  *string `json:"stop_reason"`
	InputTokens *int    `json:"input_tokens"`
}

func (u *UsageScanner) merge(payload []byte) {
	if !bytes.Contains(payload, usageKey) &&
		!bytes.Contains(payload, stopReasonKey) &&
		!bytes.Contains(payload, inputTokensKey) {
		return
	}

	var frame usageFrame
	if err := json.Unmarshal(payload, &frame); err != nil {
		return
	}

	if frame.Message != nil {
		u.mergeUsage(frame.Message.Usage)
	}
	if frame.Usage != nil {
		u.mergeUsage(*frame.Usage)
	}
	if frame.InputTokens != nil {
		u.mergeUsage(Usage{InputTokens: *frame.InputTokens})
	}
	if frame.Delta != nil && frame.Delta.StopReason != nil {
		u.stopReason = *frame.Delta.StopReason
	}
	if frame.StopReason != nil {
		u.stopReason = *frame.StopReason
	}
}

// mergeUsage keeps the highest count each field reported, because a stream
// spreads them over several events and repeats some of them.
func (u *UsageScanner) mergeUsage(in Usage) {
	if in.InputTokens > u.usage.InputTokens {
		u.usage.InputTokens = in.InputTokens
	}
	if in.OutputTokens > u.usage.OutputTokens {
		u.usage.OutputTokens = in.OutputTokens
	}
	if in.CacheReadInputTokens > u.usage.CacheReadInputTokens {
		u.usage.CacheReadInputTokens = in.CacheReadInputTokens
	}
	if in.CacheCreationInputTokens > u.usage.CacheCreationInputTokens {
		u.usage.CacheCreationInputTokens = in.CacheCreationInputTokens
	}
}
