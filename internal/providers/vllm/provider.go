// Package vllm implements the Provider interface for vLLM backends.
// vLLM serves an OpenAI-compatible API, so this is a thin wrapper around
// the openai provider that sets a custom BaseURL. No separate translation
// logic is needed — model names are passed through as-is.
//
// Deploy vLLM with: docker run --gpus all vllm/vllm-openai:latest \
//
//	--model meta-llama/Meta-Llama-3-8B-Instruct
package vllm

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	oaiprovider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/openai"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

// Provider wraps the OpenAI provider adapter with a custom base URL.
// All OpenAI-compatible logic (streaming, error translation) is inherited.
type Provider struct {
	inner *oaiprovider.Provider
	name  string
}

// New creates a vLLM Provider pointing at the given base URL.
// Typically: cfg.BaseURL = "http://vllm-service:8000/v1"
func New(cfg providers.ProviderConfig, _ zerolog.Logger) *Provider {
	// vLLM doesn't require an API key by default, but supports one for auth.
	inner := oaiprovider.New(cfg)
	return &Provider{
		inner: inner,
		name:  cfg.EffectiveName(),
	}
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Chat(ctx context.Context, req *openaitypes.ChatCompletionRequest) (*openaitypes.ChatCompletionResponse, error) {
	return p.inner.Chat(ctx, req)
}

func (p *Provider) ChatStream(ctx context.Context, req *openaitypes.ChatCompletionRequest) (<-chan openaitypes.ChatCompletionChunk, <-chan error) {
	return p.inner.ChatStream(ctx, req)
}

func (p *Provider) ListModels(ctx context.Context) ([]openaitypes.Model, error) {
	models, err := p.inner.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	// Override OwnedBy to "vllm" so the /v1/models response is informative
	for i := range models {
		models[i].OwnedBy = p.name
	}
	return models, nil
}

func (p *Provider) Health(ctx context.Context) error {
	return p.inner.Health(ctx)
}
