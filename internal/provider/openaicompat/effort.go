package openaicompat

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/1outres/sitka/internal/anthropic"
)

// effortFields returns the body fields configured for the effort level of one
// request. An entry under the model replaces the provider default outright, so
// a model that spells effort differently does not inherit the wrong shape.
// Nothing configured means nothing to send, and the upstream keeps its own
// default.
func (p *Provider) effortFields(upstreamModel string, output *anthropic.OutputConfig) json.RawMessage {
	if output == nil || output.Effort == "" {
		return nil
	}

	effort := p.effort
	if model, ok := p.models[upstreamModel]; ok && model.Effort != nil {
		effort = model.Effort
	}
	return effort[output.Effort]
}

// mergeFields writes fields over the top level of payload. The merge is
// shallow, so a configured field replaces the whole value the translation
// wrote for that key rather than being merged into it.
func mergeFields(payload, fields json.RawMessage) ([]byte, error) {
	if len(fields) == 0 {
		return payload, nil
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, fmt.Errorf("read back the translated request: %w", err)
	}
	var overrides map[string]json.RawMessage
	if err := json.Unmarshal(fields, &overrides); err != nil {
		return nil, fmt.Errorf("read the configured fields %s: %w", fields, err)
	}

	maps.Copy(body, overrides)
	return json.Marshal(body)
}
