package gateway

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

// mockProvider is a test double that implements providers.Provider.
type mockProvider struct {
	name       string
	chatErr    error
	chatResp   *openaitypes.ChatCompletionResponse
	healthErr  error
	callCount  int
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Chat(_ context.Context, req *openaitypes.ChatCompletionRequest) (*openaitypes.ChatCompletionResponse, error) {
	m.callCount++
	if m.chatErr != nil {
		return nil, m.chatErr
	}
	if m.chatResp != nil {
		return m.chatResp, nil
	}
	return &openaitypes.ChatCompletionResponse{
		ID:    "chatcmpl-mock",
		Model: req.Model,
		Choices: []openaitypes.Choice{
			{Message: openaitypes.Message{Role: "assistant", Content: "mock response"}},
		},
	}, nil
}

func (m *mockProvider) ChatStream(_ context.Context, _ *openaitypes.ChatCompletionRequest) (<-chan openaitypes.ChatCompletionChunk, <-chan error) {
	chunks := make(chan openaitypes.ChatCompletionChunk)
	errc := make(chan error, 1)
	close(chunks)
	errc <- nil
	return chunks, errc
}

func (m *mockProvider) ListModels(_ context.Context) ([]openaitypes.Model, error) {
	return []openaitypes.Model{{ID: "mock-model", Object: "model", OwnedBy: m.name}}, nil
}

func (m *mockProvider) Health(_ context.Context) error { return m.healthErr }

func buildRegistry(t *testing.T, ps ...*mockProvider) *providers.Registry {
	t.Helper()
	reg := providers.NewRegistry()
	for _, p := range ps {
		require.NoError(t, reg.Register(p.name, p))
	}
	return reg
}

// ---- Tests -----------------------------------------------------------------

func TestRoute_HappyPath_FirstProviderSucceeds(t *testing.T) {
	p1 := &mockProvider{name: "primary"}
	p2 := &mockProvider{name: "fallback"}
	reg := buildRegistry(t, p1, p2)

	router, err := NewRouter([]RouteConfig{
		{Model: "*", ProviderNames: []string{"primary", "fallback"}},
	}, reg, zerolog.Nop())
	require.NoError(t, err)

	resp, err := router.Route(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "llama3.2:3b",
		Messages: []openaitypes.Message{{Role: "user", Content: "hi"}},
	})

	require.NoError(t, err)
	assert.Equal(t, "chatcmpl-mock", resp.ID)
	assert.Equal(t, 1, p1.callCount, "primary should be called once")
	assert.Equal(t, 0, p2.callCount, "fallback should NOT be called")
}

func TestRoute_Fallback_OnRetryableError(t *testing.T) {
	retryableErr := providers.NewProviderError("primary", 429, "rate limited", nil)
	p1 := &mockProvider{name: "primary", chatErr: retryableErr}
	p2 := &mockProvider{name: "fallback"}
	reg := buildRegistry(t, p1, p2)

	router, err := NewRouter([]RouteConfig{
		{Model: "*", ProviderNames: []string{"primary", "fallback"}},
	}, reg, zerolog.Nop())
	require.NoError(t, err)

	resp, err := router.Route(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "any-model",
		Messages: []openaitypes.Message{{Role: "user", Content: "hi"}},
	})

	require.NoError(t, err)
	assert.Equal(t, "chatcmpl-mock", resp.ID)
	assert.Equal(t, 1, p1.callCount, "primary should be tried once")
	assert.Equal(t, 1, p2.callCount, "fallback should be called")
}

func TestRoute_NoFallback_OnNonRetryableError(t *testing.T) {
	notFoundErr := providers.NewProviderError("primary", 404, "model not found", nil)
	p1 := &mockProvider{name: "primary", chatErr: notFoundErr}
	p2 := &mockProvider{name: "fallback"}
	reg := buildRegistry(t, p1, p2)

	router, err := NewRouter([]RouteConfig{
		{Model: "*", ProviderNames: []string{"primary", "fallback"}},
	}, reg, zerolog.Nop())
	require.NoError(t, err)

	_, err = router.Route(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "nonexistent",
		Messages: []openaitypes.Message{{Role: "user", Content: "hi"}},
	})

	require.Error(t, err)
	assert.False(t, providers.IsRetryable(err))
	assert.Equal(t, 1, p1.callCount, "primary should be tried once")
	assert.Equal(t, 0, p2.callCount, "fallback should NOT be called on 404")
}

func TestRoute_AllProvidersFail(t *testing.T) {
	retryableErr := providers.NewProviderError("p", 500, "internal error", nil)
	p1 := &mockProvider{name: "p1", chatErr: retryableErr}
	p2 := &mockProvider{name: "p2", chatErr: retryableErr}
	reg := buildRegistry(t, p1, p2)

	router, err := NewRouter([]RouteConfig{
		{Model: "*", ProviderNames: []string{"p1", "p2"}},
	}, reg, zerolog.Nop())
	require.NoError(t, err)

	_, err = router.Route(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "any",
		Messages: []openaitypes.Message{{Role: "user", Content: "hi"}},
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, providers.ErrAllProvidersFailed)
	assert.Equal(t, 1, p1.callCount)
	assert.Equal(t, 1, p2.callCount)
}

func TestRoute_NoProvidersForModel(t *testing.T) {
	router, err := NewRouter(nil, providers.NewRegistry(), zerolog.Nop())
	require.NoError(t, err)

	_, err = router.Route(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "unknown-model",
		Messages: []openaitypes.Message{{Role: "user", Content: "hi"}},
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, providers.ErrNoProvidersForModel)
}

func TestRoute_ModelSpecificChainOverridesDefault(t *testing.T) {
	pDefault := &mockProvider{name: "default-provider"}
	pSpecific := &mockProvider{name: "specific-provider"}
	reg := buildRegistry(t, pDefault, pSpecific)

	router, err := NewRouter([]RouteConfig{
		{Model: "*", ProviderNames: []string{"default-provider"}},
		{Model: "gpt-4o", ProviderNames: []string{"specific-provider"}},
	}, reg, zerolog.Nop())
	require.NoError(t, err)

	// gpt-4o should use specific chain
	resp, err := router.Route(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []openaitypes.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, pSpecific.callCount)
	assert.Equal(t, 0, pDefault.callCount)

	// other model should use default chain
	_, err = router.Route(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "llama3.2:3b",
		Messages: []openaitypes.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, pDefault.callCount)
}

func TestNewRouter_UnknownProvider(t *testing.T) {
	reg := providers.NewRegistry()
	_, err := NewRouter([]RouteConfig{
		{Model: "*", ProviderNames: []string{"does-not-exist"}},
	}, reg, zerolog.Nop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}
