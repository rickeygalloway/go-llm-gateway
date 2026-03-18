package vllm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goopenai "github.com/sashabaranov/go-openai"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

func TestChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			resp := goopenai.ChatCompletionResponse{
				ID:      "chatcmpl-vllm-test",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   "meta-llama/Meta-Llama-3-8B-Instruct",
				Choices: []goopenai.ChatCompletionChoice{
					{
						Index: 0,
						Message: goopenai.ChatCompletionMessage{
							Role:    "assistant",
							Content: "42",
						},
						FinishReason: "stop",
					},
				},
				Usage: goopenai.Usage{PromptTokens: 15, CompletionTokens: 2, TotalTokens: 17},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	p := New(providers.ProviderConfig{
		Type:    "vllm",
		Name:    "vllm-gpu",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	}, zerolog.Nop())

	resp, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "meta-llama/Meta-Llama-3-8B-Instruct",
		Messages: []openaitypes.Message{{Role: "user", Content: "What is 6*7?"}},
	})

	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "42", resp.Choices[0].Message.Content)
}

func TestListModels_OwnedByIsVLLM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			resp := goopenai.ModelsList{
				Models: []goopenai.Model{
					{ID: "meta-llama/Meta-Llama-3-8B-Instruct", Object: "model", CreatedAt: time.Now().Unix(), OwnedBy: "vllm"},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	p := New(providers.ProviderConfig{
		Type:    "vllm",
		Name:    "vllm-gpu",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	}, zerolog.Nop())

	models, err := p.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "vllm-gpu", models[0].OwnedBy)
}
