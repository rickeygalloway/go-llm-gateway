package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpClient is a thin wrapper around net/http.Client that adds Anthropic
// authentication headers and handles error response bodies.
type httpClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func newHTTPClient(baseURL, apiKey string, timeout time.Duration) *httpClient {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &httpClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: timeout},
	}
}

// do sends a POST request to path with body serialised as JSON.
// On non-2xx responses it returns a structured error.
func (c *httpClient) do(ctx context.Context, path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	return c.http.Do(req)
}

// doStream is identical to do but uses a fresh http.Client without a read
// deadline so streaming responses can flow indefinitely.
func (c *httpClient) doStream(ctx context.Context, path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("Accept", "text/event-stream")

	// No timeout on the streaming client — the caller's context controls lifetime.
	streamHTTP := &http.Client{}
	return streamHTTP.Do(req)
}

// apiError is the Anthropic error response shape.
type apiError struct {
	Type  string `json:"type"` // "error"
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// readAPIError reads and parses an Anthropic error response body.
func readAPIError(r io.Reader) string {
	var ae apiError
	if err := json.NewDecoder(r).Decode(&ae); err != nil {
		return "unknown anthropic error"
	}
	return ae.Error.Message
}
