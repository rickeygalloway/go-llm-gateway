// Package openai implements the Provider interface for OpenAI and OpenAI-compatible
// backends. It uses github.com/sashabaranov/go-openai, the de-facto standard
// Go OpenAI client, and translates between the gateway's canonical types and
// the SDK types (which are close but not identical).
package openai

import (
	"context"
	"errors"
	"io"
	"net/http"

	goopenai "github.com/sashabaranov/go-openai"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

// Provider adapts the OpenAI API to the gateway Provider interface.
// It can also target any OpenAI-compatible endpoint (e.g. Azure OpenAI,
// self-hosted vLLM) by setting BaseURL in the config.
type Provider struct {
	name   string
	client *goopenai.Client
}

// New creates an OpenAI Provider.
// If cfg.BaseURL is empty, it targets api.openai.com.
func New(cfg providers.ProviderConfig) *Provider {
	config := goopenai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
	}
	if cfg.Timeout > 0 {
		config.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}
	return &Provider{
		name:   cfg.EffectiveName(),
		client: goopenai.NewClientWithConfig(config),
	}
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Chat(ctx context.Context, req *openaitypes.ChatCompletionRequest) (*openaitypes.ChatCompletionResponse, error) {
	sdkResp, err := p.client.CreateChatCompletion(ctx, toSDKRequest(req))
	if err != nil {
		return nil, translateError(p.name, err)
	}
	return fromSDKResponse(sdkResp), nil
}

// ChatStream opens a streaming chat completion and forwards chunks to the caller.
func (p *Provider) ChatStream(ctx context.Context, req *openaitypes.ChatCompletionRequest) (<-chan openaitypes.ChatCompletionChunk, <-chan error) {
	chunks := make(chan openaitypes.ChatCompletionChunk, 32)
	errc := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errc)

		stream, err := p.client.CreateChatCompletionStream(ctx, toSDKRequest(req))
		if err != nil {
			errc <- translateError(p.name, err)
			return
		}
		defer stream.Close()

		for {
			sdkChunk, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				errc <- nil
				return
			}
			if err != nil {
				errc <- translateError(p.name, err)
				return
			}

			select {
			case chunks <- fromSDKChunk(sdkChunk):
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			}
		}
	}()

	return chunks, errc
}

func (p *Provider) ListModels(ctx context.Context) ([]openaitypes.Model, error) {
	list, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, translateError(p.name, err)
	}
	out := make([]openaitypes.Model, 0, len(list.Models))
	for _, m := range list.Models {
		out = append(out, openaitypes.Model{
			ID:      m.ID,
			Object:  "model",
			Created: m.CreatedAt,
			OwnedBy: m.OwnedBy,
		})
	}
	return out, nil
}

func (p *Provider) Health(ctx context.Context) error {
	_, err := p.client.ListModels(ctx)
	if err != nil {
		return translateError(p.name, err)
	}
	return nil
}

// ---- Type translation ------------------------------------------------------

func toSDKRequest(req *openaitypes.ChatCompletionRequest) goopenai.ChatCompletionRequest {
	msgs := make([]goopenai.ChatCompletionMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, goopenai.ChatCompletionMessage{
			Role:    m.Role,
			Content: m.Content,
			Name:    m.Name,
		})
	}
	return goopenai.ChatCompletionRequest{
		Model:            req.Model,
		Messages:         msgs,
		MaxTokens:        req.MaxTokens,
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		N:                req.N,
		Stream:           req.Stream,
		Stop:             req.Stop,
		PresencePenalty:  req.PresencePenalty,
		FrequencyPenalty: req.FrequencyPenalty,
		User:             req.User,
	}
}

func fromSDKResponse(r goopenai.ChatCompletionResponse) *openaitypes.ChatCompletionResponse {
	choices := make([]openaitypes.Choice, 0, len(r.Choices))
	for _, c := range r.Choices {
		choices = append(choices, openaitypes.Choice{
			Index: c.Index,
			Message: openaitypes.Message{
				Role:    c.Message.Role,
				Content: c.Message.Content,
			},
			FinishReason: string(c.FinishReason),
		})
	}
	return &openaitypes.ChatCompletionResponse{
		ID:      r.ID,
		Object:  r.Object,
		Created: r.Created,
		Model:   r.Model,
		Choices: choices,
		Usage: openaitypes.Usage{
			PromptTokens:     r.Usage.PromptTokens,
			CompletionTokens: r.Usage.CompletionTokens,
			TotalTokens:      r.Usage.TotalTokens,
		},
	}
}

func fromSDKChunk(c goopenai.ChatCompletionStreamResponse) openaitypes.ChatCompletionChunk {
	choices := make([]openaitypes.ChunkChoice, 0, len(c.Choices))
	for _, ch := range c.Choices {
		choices = append(choices, openaitypes.ChunkChoice{
			Index: ch.Index,
			Delta: openaitypes.Delta{
				Role:    ch.Delta.Role,
				Content: ch.Delta.Content,
			},
			FinishReason: string(ch.FinishReason),
		})
	}
	return openaitypes.ChatCompletionChunk{
		ID:      c.ID,
		Object:  c.Object,
		Created: c.Created,
		Model:   c.Model,
		Choices: choices,
	}
}

// translateError maps sashabaranov/go-openai errors to ProviderErrors
// so the router can make informed fallback decisions.
func translateError(providerName string, err error) error {
	var apiErr *goopenai.APIError
	if errors.As(err, &apiErr) {
		return providers.NewProviderError(providerName, apiErr.HTTPStatusCode, apiErr.Message, err)
	}
	// Network or timeout error
	return providers.NewProviderError(providerName, 0, err.Error(), err)
}
