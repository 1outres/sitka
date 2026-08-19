package openaicompat

import (
	"fmt"
	"net/http"

	"github.com/1outres/sitka/internal/anthropic"
)

// CountTokens reports that this provider counts no tokens. An OpenAI-compatible
// API has no token counting endpoint, and a guessed number would make the
// client plan against a budget that is not real, so Claude Code is told to
// count through the messages endpoint instead.
func (p *Provider) CountTokens(w http.ResponseWriter, _ *http.Request, _ string, _ []byte) {
	anthropic.WriteError(w, http.StatusNotFound, anthropic.ErrNotFound,
		fmt.Sprintf("provider %q has no token counting endpoint, so count tokens through the messages endpoint instead", p.id))
}
