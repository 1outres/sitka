package openaicompat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

// send calls one upstream path. The caller closes the response body.
func (p *Provider) send(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	req, err := p.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: call %s of provider %q: %w", p.baseURL+path, p.id, err)
	}
	return resp, nil
}

func (p *Provider) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, payload)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: build the request to %s of provider %q: %w", p.baseURL+path, p.id, err)
	}

	for name, value := range p.headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

func isSuccess(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}
