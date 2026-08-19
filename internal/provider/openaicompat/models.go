package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
)

// maxModelListBytes bounds how much of a model list the gateway reads, so a
// broken upstream cannot make it hold an unbounded body in memory.
const maxModelListBytes = 4 << 20

const (
	modelType        = "model"
	modelIDSeparator = "-"
)

// Models lists the upstream models, each with the prefixed id a client sends
// to reach it.
func (p *Provider) Models(ctx context.Context) ([]anthropic.Model, error) {
	resp, err := p.send(ctx, http.MethodGet, modelsPath, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxModelListBytes))
	if err != nil {
		return nil, fmt.Errorf("openaicompat: read the model list of provider %q: %w", p.id, err)
	}
	if !isSuccess(resp.StatusCode) {
		return nil, fmt.Errorf("openaicompat: list the models of provider %q: status %d, body %s",
			p.id, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var list openai.ModelList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("openaicompat: decode the model list of provider %q: status %d, body %s: %w",
			p.id, resp.StatusCode, strings.TrimSpace(string(body)), err)
	}

	models := make([]anthropic.Model, 0, len(list.Data))
	for _, entry := range list.Data {
		models = append(models, anthropic.Model{
			ID:          p.id + modelIDSeparator + entry.ID,
			Type:        modelType,
			DisplayName: entry.ID,
		})
	}
	return models, nil
}
