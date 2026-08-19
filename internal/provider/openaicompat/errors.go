package openaicompat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
)

// maxErrorBodyBytes bounds how much of a failing reply the gateway reads, so a
// broken upstream cannot make it hold an unbounded body in memory.
const maxErrorBodyBytes = 64 << 10

// upstreamFailure is an upstream error kept as it arrived. The status and the
// message go to the client unchanged, because a remapped error hides what the
// upstream actually said.
type upstreamFailure struct {
	status  int
	message string
}

func (p *Provider) readFailure(resp *http.Response) upstreamFailure {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if err != nil {
		return upstreamFailure{
			status:  resp.StatusCode,
			message: fmt.Sprintf("provider %q returned status %d and its body could not be read: %v", p.id, resp.StatusCode, err),
		}
	}
	return upstreamFailure{status: resp.StatusCode, message: p.failureMessage(resp.StatusCode, body)}
}

func (p *Provider) failureMessage(status int, body []byte) string {
	var upstream openai.ErrorResponse
	if err := json.Unmarshal(body, &upstream); err == nil && upstream.Error.Message != "" {
		return upstream.Error.Message
	}
	if text := strings.TrimSpace(string(body)); text != "" {
		return text
	}
	return fmt.Sprintf("provider %q returned status %d with an empty body", p.id, status)
}

func (p *Provider) writeUpstreamError(w http.ResponseWriter, resp *http.Response) {
	failure := p.readFailure(resp)
	p.logger.Error("upstream returned an error", "status", failure.status, "message", failure.message)
	anthropic.WriteError(w, failure.status, anthropic.ErrorTypeForStatus(failure.status), failure.message)
}
