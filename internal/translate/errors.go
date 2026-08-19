// Package translate converts requests and replies between the Anthropic
// Messages API and the OpenAI Chat Completions API.
package translate

import "fmt"

// UnsupportedError reports a request feature that has no OpenAI equivalent.
type UnsupportedError struct{ Feature string }

// Error returns the message of the unsupported feature.
func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("translate: unsupported feature: %s", e.Feature)
}
