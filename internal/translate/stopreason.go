package translate

import (
	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
)

// stopReason maps an OpenAI finish reason to the Anthropic stop reason. It
// returns nil when the finish reason is empty or unknown, because the API
// reports an unfinished turn as a null stop_reason.
func stopReason(finish string) *string {
	var reason string
	switch finish {
	case openai.FinishStop:
		reason = anthropic.StopEndTurn
	case openai.FinishLength:
		reason = anthropic.StopMaxTokens
	case openai.FinishToolCalls:
		reason = anthropic.StopToolUse
	case openai.FinishContentFilter:
		reason = anthropic.StopRefusal
	default:
		return nil
	}
	return &reason
}
