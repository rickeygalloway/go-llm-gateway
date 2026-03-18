// Package providers defines the Provider interface and shared configuration types.
// Each backend (Ollama, OpenAI, Anthropic, vLLM) implements this interface,
// allowing the gateway router to treat all backends uniformly.
//
// Design rationale: this is the classic "repository pattern" applied to LLM backends.
// All I/O is behind an interface → easy mocking in tests, easy swapping of backends.
package providers

import (
	"context"
	"time"

	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

// Provider is the core interface every backend must implement.
// It maps directly onto the OpenAI API surface so the gateway can be
// OpenAI-compatible without any translation at the handler layer.
type Provider interface {
	// Name returns the provider identifier, e.g. "ollama", "openai".
	Name() string

	// Chat performs a non-streaming chat completion.
	// Implementors must respect ctx cancellation and deadline.
	Chat(ctx context.Context, req *openaitypes.ChatCompletionRequest) (*openaitypes.ChatCompletionResponse, error)

	// ChatStream starts a streaming chat completion.
	// Returns a channel of chunks; the channel is closed when the stream ends or errors.
	// The caller must drain or stop reading from the channel to avoid goroutine leaks.
	// A second return channel carries a terminal error (nil on clean completion).
	ChatStream(ctx context.Context, req *openaitypes.ChatCompletionRequest) (<-chan openaitypes.ChatCompletionChunk, <-chan error)

	// ListModels returns models available from this provider.
	// Used to aggregate the /v1/models endpoint response.
	ListModels(ctx context.Context) ([]openaitypes.Model, error)

	// Health performs a lightweight liveness check.
	// Used by /readyz and the router's circuit-breaker logic.
	Health(ctx context.Context) error
}

// ProviderConfig holds per-provider configuration loaded from config.yaml / env.
type ProviderConfig struct {
	// Type identifies the adapter: "ollama" | "openai" | "anthropic" | "vllm"
	Type string `mapstructure:"type"`

	// Name is an optional human label. Defaults to Type if unset.
	Name string `mapstructure:"name"`

	// BaseURL is the backend endpoint, e.g. "http://ollama:11434"
	BaseURL string `mapstructure:"base_url"`

	// APIKey is used for providers that require authentication.
	APIKey string `mapstructure:"api_key"`

	// Models is the explicit list of model IDs this provider serves.
	// If empty, the router will query ListModels() dynamically.
	Models []string `mapstructure:"models"`

	// Priority defines fallback order: lower value = tried first.
	// Two providers with the same priority are tried in config order.
	Priority int `mapstructure:"priority"`

	// Timeout is the per-request timeout for non-streaming calls.
	// Streaming calls use a separate write deadline at the transport level.
	Timeout time.Duration `mapstructure:"timeout"`

	// MaxRetries is the number of retries on transient errors before failing over.
	MaxRetries int `mapstructure:"max_retries"`
}

// EffectiveName returns the provider's display name, falling back to Type.
func (c ProviderConfig) EffectiveName() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Type
}
