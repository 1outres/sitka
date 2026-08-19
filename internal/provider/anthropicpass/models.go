package anthropicpass

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/provider"
)

const (
	apiVersion      = "2023-06-01"
	modelsPageLimit = 1000
	errorBodyLimit  = 4 << 10
)

// Models lists what the Anthropic API offers. The ids come back unchanged,
// because a client already reaches this provider by sending them as they are.
func (p *Provider) Models(ctx context.Context) ([]anthropic.Model, error) {
	credentials, ok := provider.CredentialsFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("%s: listing models needs the client's own API key, because sitka holds no key for the Anthropic upstream", providerName)
	}

	req, err := p.newModelsRequest(ctx, credentials)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: list models: %w", providerName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s: list models: upstream returned %s: %s", providerName, resp.Status, readErrorBody(resp.Body))
	}

	var list anthropic.ModelList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("%s: list models: cannot read the upstream reply: %w", providerName, err)
	}

	return list.Data, nil
}

func (p *Provider) newModelsRequest(ctx context.Context, credentials provider.Credentials) (*http.Request, error) {
	endpoint := p.baseURL.JoinPath("v1", "models")
	endpoint.RawQuery = url.Values{"limit": {strconv.Itoa(modelsPageLimit)}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build the list models request: %w", providerName, err)
	}
	req.Header.Set("anthropic-version", apiVersion)
	credentials.Apply(req)

	return req, nil
}

func readErrorBody(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, errorBodyLimit))
	if err != nil {
		return fmt.Sprintf("the reply body could not be read: %v", err)
	}

	return strings.TrimSpace(string(raw))
}
