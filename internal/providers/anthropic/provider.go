package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

// Provider adapts Anthropic's Messages API to the gateway Provider interface.
// Key translation notes:
//   - OpenAI "system" role messages → top-level "system" field in Anthropic request
//   - OpenAI "tool" role messages are not yet translated (future work)
//   - Model names: gateway receives "claude-3-5-sonnet-20241022" and passes it through
type Provider struct {
	name   string
	client *httpClient
	logger zerolog.Logger
}

// New creates an Anthropic Provider.
func New(cfg providers.ProviderConfig, logger zerolog.Logger) *Provider {
	return &Provider{
		name:   cfg.EffectiveName(),
		client: newHTTPClient(cfg.BaseURL, cfg.APIKey, cfg.Timeout),
		logger: logger.With().Str("provider", cfg.EffectiveName()).Logger(),
	}
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Chat(ctx context.Context, req *openaitypes.ChatCompletionRequest) (*openaitypes.ChatCompletionResponse, error) {
	antReq, err := translateRequest(req)
	if err != nil {
		return nil, fmt.Errorf("translating request: %w", err)
	}
	antReq.Stream = false

	resp, err := p.client.do(ctx, "/v1/messages", antReq)
	if err != nil {
		return nil, providers.NewProviderError(p.name, 0, "connection failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := readAPIError(resp.Body)
		return nil, providers.NewProviderError(p.name, resp.StatusCode, msg, nil)
	}

	var antResp messageResponse
	if err := json.NewDecoder(resp.Body).Decode(&antResp); err != nil {
		return nil, fmt.Errorf("decoding anthropic response: %w", err)
	}

	return fromMessageResponse(antResp, req.Model), nil
}

func (p *Provider) ChatStream(ctx context.Context, req *openaitypes.ChatCompletionRequest) (<-chan openaitypes.ChatCompletionChunk, <-chan error) {
	chunks := make(chan openaitypes.ChatCompletionChunk, 32)
	errc := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errc)

		antReq, err := translateRequest(req)
		if err != nil {
			errc <- fmt.Errorf("translating request: %w", err)
			return
		}
		antReq.Stream = true

		resp, err := p.client.doStream(ctx, "/v1/messages", antReq)
		if err != nil {
			errc <- providers.NewProviderError(p.name, 0, "stream connection failed", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			msg := readAPIError(resp.Body)
			errc <- providers.NewProviderError(p.name, resp.StatusCode, msg, nil)
			return
		}

		id := fmt.Sprintf("chatcmpl-anthropic-%d", time.Now().UnixNano())
		scanner := bufio.NewScanner(resp.Body)

		for scanner.Scan() {
			line := scanner.Text()

			// SSE format: "event: <type>\n" and "data: <json>\n"
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				if data == "[DONE]" {
					errc <- nil
					return
				}

				// Parse event type from data
				var event streamEvent
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					continue
				}

				switch event.Type {
				case "content_block_delta":
					var cbd contentBlockDelta
					if err := json.Unmarshal([]byte(data), &cbd); err != nil {
						p.logger.Warn().Err(err).Msg("failed to decode content_block_delta")
						continue
					}
					if cbd.Delta.Type == "text_delta" && cbd.Delta.Text != "" {
						chunk := openaitypes.ChatCompletionChunk{
							ID:      id,
							Object:  "chat.completion.chunk",
							Created: time.Now().Unix(),
							Model:   req.Model,
							Choices: []openaitypes.ChunkChoice{
								{
									Index: cbd.Index,
									Delta: openaitypes.Delta{Content: cbd.Delta.Text},
								},
							},
						}
						select {
						case chunks <- chunk:
						case <-ctx.Done():
							errc <- ctx.Err()
							return
						}
					}

				case "message_stop":
					// Final chunk with empty delta and finish_reason
					chunk := openaitypes.ChatCompletionChunk{
						ID:      id,
						Object:  "chat.completion.chunk",
						Created: time.Now().Unix(),
						Model:   req.Model,
						Choices: []openaitypes.ChunkChoice{
							{Index: 0, Delta: openaitypes.Delta{}, FinishReason: "stop"},
						},
					}
					select {
					case chunks <- chunk:
					case <-ctx.Done():
					}
					errc <- nil
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errc <- fmt.Errorf("reading anthropic stream: %w", err)
			return
		}
		errc <- nil
	}()

	return chunks, errc
}

// ListModels returns the hardcoded list of Claude models.
// Anthropic does not have a public /models endpoint as of writing.
func (p *Provider) ListModels(_ context.Context) ([]openaitypes.Model, error) {
	now := time.Now().Unix()
	models := []openaitypes.Model{
		{ID: "claude-opus-4-6", Object: "model", Created: now, OwnedBy: "anthropic"},
		{ID: "claude-sonnet-4-6", Object: "model", Created: now, OwnedBy: "anthropic"},
		{ID: "claude-haiku-4-5-20251001", Object: "model", Created: now, OwnedBy: "anthropic"},
		{ID: "claude-3-5-sonnet-20241022", Object: "model", Created: now, OwnedBy: "anthropic"},
		{ID: "claude-3-haiku-20240307", Object: "model", Created: now, OwnedBy: "anthropic"},
	}
	return models, nil
}

func (p *Provider) Health(ctx context.Context) error {
	// Anthropic doesn't have a free /health endpoint. We do a minimal chat to
	// verify connectivity. In production, replace with a dedicated health token.
	req := &messageRequest{
		Model:     "claude-3-haiku-20240307",
		Messages:  []message{{Role: "user", Content: json.RawMessage(`"ping"`)}},
		MaxTokens: 1,
	}
	resp, err := p.client.do(ctx, "/v1/messages", req)
	if err != nil {
		return providers.NewProviderError(p.name, 0, "health check failed", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return providers.NewProviderError(p.name, resp.StatusCode, "invalid API key", nil)
	}
	// Any non-5xx is considered healthy (rate limits, etc. are transient)
	if resp.StatusCode >= 500 {
		return providers.NewProviderError(p.name, resp.StatusCode, "provider unhealthy", nil)
	}
	return nil
}

// ---- Translation helpers ---------------------------------------------------

// translateRequest converts an OpenAI ChatCompletionRequest to Anthropic's format.
// Critical: Anthropic does not support a "system" role in messages[]. System
// prompts must be extracted and placed in the top-level system field.
func translateRequest(req *openaitypes.ChatCompletionRequest) (*messageRequest, error) { //nolint:unparam
	var systemPrompt string
	msgs := make([]message, 0, len(req.Messages))

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			// Concatenate multiple system messages (rare but valid)
			if systemPrompt != "" {
				systemPrompt += "\n\n"
			}
			systemPrompt += m.Content

		case "user", "assistant":
			content, err := json.Marshal(m.Content)
			if err != nil {
				return nil, fmt.Errorf("marshalling message content: %w", err)
			}
			msgs = append(msgs, message{
				Role:    m.Role,
				Content: content,
			})

		default:
			// Skip tool/function messages for now (future work: translate tool calls)
			// The caller's logger should track this; we silently drop here.
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096 // Anthropic requires max_tokens; set a sensible default
	}

	return &messageRequest{
		Model:       req.Model,
		Messages:    msgs,
		System:      systemPrompt,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		StopSeqs:    req.Stop,
	}, nil
}

func fromMessageResponse(r messageResponse, model string) *openaitypes.ChatCompletionResponse {
	// Concatenate all text content blocks into a single message
	var text strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}

	return &openaitypes.ChatCompletionResponse{
		ID:      r.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openaitypes.Choice{
			{
				Index: 0,
				Message: openaitypes.Message{
					Role:    "assistant",
					Content: text.String(),
				},
				FinishReason: stopReasonToFinishReason(r.StopReason),
			},
		},
		Usage: openaitypes.Usage{
			PromptTokens:     r.Usage.InputTokens,
			CompletionTokens: r.Usage.OutputTokens,
			TotalTokens:      r.Usage.InputTokens + r.Usage.OutputTokens,
		},
	}
}
