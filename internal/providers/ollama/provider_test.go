package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

func newTestProvider(serverURL string) *Provider {
	return New(providers.ProviderConfig{
		Type:    "ollama",
		Name:    "ollama-test",
		BaseURL: serverURL,
		Timeout: 5 * time.Second,
	}, zerolog.Nop())
}

func TestChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/chat", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		// Verify request body
		var reqBody ollamaChatRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.Equal(t, "llama3.2:3b", reqBody.Model)
		assert.False(t, reqBody.Stream)

		resp := ollamaChatResponse{
			Model:           "llama3.2:3b",
			CreatedAt:       time.Now(),
			Message:         ollamaMessage{Role: "assistant", Content: "Hello!"},
			Done:            true,
			PromptEvalCount: 10,
			EvalCount:       5,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "llama3.2:3b",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hello!"}},
	})

	require.NoError(t, err)
	assert.Equal(t, "chat.completion", resp.Object)
	assert.Equal(t, "llama3.2:3b", resp.Model)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "Hello!", resp.Choices[0].Message.Content)
	assert.Equal(t, "assistant", resp.Choices[0].Message.Role)
	assert.Equal(t, 10, resp.Usage.PromptTokens)
	assert.Equal(t, 5, resp.Usage.CompletionTokens)
	assert.Equal(t, 15, resp.Usage.TotalTokens)
}

func TestChat_ProviderError_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "llama3.2:3b",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hi"}},
	})

	require.Error(t, err)
	assert.True(t, providers.IsRetryable(err), "5xx should be retryable")
}

func TestChat_ProviderError_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "llama3.2:3b",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hi"}},
	})

	require.Error(t, err)
	assert.True(t, providers.IsRetryable(err), "429 should be retryable")
}

func TestChat_ProviderError_404_NotRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "nonexistent:model",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hi"}},
	})

	require.Error(t, err)
	assert.False(t, providers.IsRetryable(err), "404 should NOT be retryable")
}

func TestChatStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")

		words := []string{"Hello", " world", "!"}
		for i, word := range words {
			done := i == len(words)-1
			chunk := ollamaChatResponse{
				Model:     "llama3.2:3b",
				CreatedAt: time.Now(),
				Message:   ollamaMessage{Role: "assistant", Content: word},
				Done:      done,
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "%s\n", data)
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	chunks, errc := p.ChatStream(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "llama3.2:3b",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hi"}},
		Stream:   true,
	})

	var received []string
	for chunk := range chunks {
		for _, c := range chunk.Choices {
			received = append(received, c.Delta.Content)
		}
	}

	require.NoError(t, <-errc)
	assert.Equal(t, []string{"Hello", " world", "!"}, received)
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		resp := ollamaTagsResponse{
			Models: []ollamaModelInfo{
				{Name: "llama3.2:3b", ModifiedAt: time.Now(), Size: 1_000_000},
				{Name: "mistral:7b", ModifiedAt: time.Now(), Size: 2_000_000},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	models, err := p.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "llama3.2:3b", models[0].ID)
	assert.Equal(t, "model", models[0].Object)
	assert.Equal(t, "ollama-test", models[0].OwnedBy)
}

func TestHealth_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ollamaTagsResponse{})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	assert.NoError(t, p.Health(context.Background()))
}

func TestHealth_Unhealthy(t *testing.T) {
	// Point at a non-existent server
	p := newTestProvider("http://127.0.0.1:19999")
	err := p.Health(context.Background())
	require.Error(t, err)
	assert.True(t, providers.IsRetryable(err))
}
