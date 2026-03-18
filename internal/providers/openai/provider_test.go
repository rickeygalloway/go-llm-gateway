package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goopenai "github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

func TestChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			resp := goopenai.ChatCompletionResponse{
				ID:      "chatcmpl-test123",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   "gpt-4o",
				Choices: []goopenai.ChatCompletionChoice{
					{
						Index: 0,
						Message: goopenai.ChatCompletionMessage{
							Role:    "assistant",
							Content: "Hello there!",
						},
						FinishReason: "stop",
					},
				},
				Usage: goopenai.Usage{
					PromptTokens:     20,
					CompletionTokens: 5,
					TotalTokens:      25,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	p := New(providers.ProviderConfig{
		Type:    "openai",
		Name:    "openai-test",
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Timeout: 5 * time.Second,
	})

	resp, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hello!"}},
	})

	require.NoError(t, err)
	assert.Equal(t, "chatcmpl-test123", resp.ID)
	assert.Equal(t, "gpt-4o", resp.Model)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "Hello there!", resp.Choices[0].Message.Content)
	assert.Equal(t, 20, resp.Usage.PromptTokens)
	assert.Equal(t, 5, resp.Usage.CompletionTokens)
	assert.Equal(t, 25, resp.Usage.TotalTokens)
}

func TestChat_APIError_5xx_IsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(goopenai.ErrorResponse{
			Error: &goopenai.APIError{
				Message: "service unavailable",
				Type:    "server_error",
				Code:    nil,
			},
		})
	}))
	defer srv.Close()

	p := New(providers.ProviderConfig{
		Type:    "openai",
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Timeout: 5 * time.Second,
	})

	_, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hi"}},
	})

	require.Error(t, err)
	assert.True(t, providers.IsRetryable(err), "5xx should be retryable")
}

func TestChat_RateLimit_IsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(goopenai.ErrorResponse{
			Error: &goopenai.APIError{
				Message: "rate limit exceeded",
				Type:    "rate_limit_error",
			},
		})
	}))
	defer srv.Close()

	p := New(providers.ProviderConfig{
		Type:    "openai",
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Timeout: 5 * time.Second,
	})

	_, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hi"}},
	})

	require.Error(t, err)
	assert.True(t, providers.IsRetryable(err), "429 should be retryable")
}

func TestChat_InvalidRequest_NotRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(goopenai.ErrorResponse{
			Error: &goopenai.APIError{
				Message: "invalid request",
				Type:    "invalid_request_error",
			},
		})
	}))
	defer srv.Close()

	p := New(providers.ProviderConfig{
		Type:    "openai",
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Timeout: 5 * time.Second,
	})

	_, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hi"}},
	})

	require.Error(t, err)
	assert.False(t, providers.IsRetryable(err), "400 should NOT be retryable")
}
