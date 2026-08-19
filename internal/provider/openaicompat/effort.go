package openaicompat

import (
	"encoding/json"
	"fmt"
	"maps"
)

// effortFields returns the request body fields configured for one model. An
// entry under the model replaces the provider setting outright, so a model that
// spells effort differently does not inherit the wrong shape. Nothing
// configured means nothing to send, and the upstream keeps its own default.
//
// The client's own effort level is not read. Claude Code sets it for the whole
// session, and the levels of an OpenAI-compatible upstream rarely line up with
// it, so the configuration pins each model instead.
func (p *Provider) effortFields(upstreamModel string) json.RawMessage {
	if model, ok := p.models[upstreamModel]; ok && model.Effort != nil {
		return model.Effort
	}
	return p.effort
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
