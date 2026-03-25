// Package grok implements the Provider interface for xAI Grok backends.
// Grok serves an OpenAI-compatible API, so this is a thin wrapper around
// the openai provider pointed at api.x.ai. No separate translation logic
// is needed — model names are passed through as-is.
//
// API reference: https://docs.x.ai/api
// Default base URL: https://api.x.ai/v1
package grok

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	oaiprovider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/openai"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

const defaultBaseURL = "https://api.x.ai/v1"

// Provider wraps the OpenAI provider adapter pointed at the xAI Grok API.
// All OpenAI-compatible logic (streaming, error translation) is inherited.
type Provider struct {
	inner *oaiprovider.Provider
	name  string
}

// New creates a Grok Provider.
// cfg.APIKey must be set to a valid xAI API key.
// cfg.BaseURL defaults to https://api.x.ai/v1 if empty.
func New(cfg providers.ProviderConfig, _ zerolog.Logger) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return &Provider{
		inner: oaiprovider.New(cfg),
		name:  cfg.EffectiveName(),
	}
}

func (p *Provider) Name() string { return p.name }

// Chat performs a non-streaming chat completion via the Grok API.
func (p *Provider) Chat(ctx context.Context, req *openaitypes.ChatCompletionRequest) (*openaitypes.ChatCompletionResponse, error) {
	return p.inner.Chat(ctx, req)
}

// ChatStream opens a streaming chat completion via the Grok API.
func (p *Provider) ChatStream(ctx context.Context, req *openaitypes.ChatCompletionRequest) (<-chan openaitypes.ChatCompletionChunk, <-chan error) {
	return p.inner.ChatStream(ctx, req)
}

// ListModels returns models available from Grok.
func (p *Provider) ListModels(ctx context.Context) ([]openaitypes.Model, error) {
	models, err := p.inner.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	for i := range models {
		models[i].OwnedBy = p.name
	}
	return models, nil
}

// Health performs a lightweight liveness check against the Grok API.
func (p *Provider) Health(ctx context.Context) error {
	return p.inner.Health(ctx)
}
