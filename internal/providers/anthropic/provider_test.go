package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

func newTestProvider(serverURL string) *Provider {
	return New(providers.ProviderConfig{
		Type:    "anthropic",
		Name:    "anthropic-test",
		BaseURL: serverURL,
		APIKey:  "sk-ant-test",
	}, zerolog.Nop())
}

func TestChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "sk-ant-test", r.Header.Get("x-api-key"))
		assert.Equal(t, apiVersion, r.Header.Get("anthropic-version"))

		// Decode and verify the request body
		var reqBody messageRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.Equal(t, "claude-3-5-sonnet-20241022", reqBody.Model)
		assert.Equal(t, "You are a helpful assistant.", reqBody.System)
		assert.False(t, reqBody.Stream)

		resp := messageResponse{
			ID:         "msg_test123",
			Type:       "message",
			Role:       "assistant",
			Content:    []content{{Type: "text", Text: "Hi there!"}},
			Model:      "claude-3-5-sonnet-20241022",
			StopReason: "end_turn",
			Usage:      usage{InputTokens: 25, OutputTokens: 8},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []openaitypes.Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello!"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "chat.completion", resp.Object)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "Hi there!", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
	assert.Equal(t, 25, resp.Usage.PromptTokens)
	assert.Equal(t, 8, resp.Usage.CompletionTokens)
	assert.Equal(t, 33, resp.Usage.TotalTokens)
}

func TestChat_Retryable_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(apiError{Type: "error", Error: struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}{Type: "overloaded_error", Message: "service overloaded"}})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Chat(context.Background(), &openaitypes.ChatCompletionRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hi"}},
	})

	require.Error(t, err)
	assert.True(t, providers.IsRetryable(err), "5xx should be retryable")
}

func TestTranslateRequest_SystemMessageExtraction(t *testing.T) {
	req := &openaitypes.ChatCompletionRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []openaitypes.Message{
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi"},
			{Role: "user", Content: "How are you?"},
		},
	}

	antReq, err := translateRequest(req)
	require.NoError(t, err)

	assert.Equal(t, "Be concise.", antReq.System)
	assert.Len(t, antReq.Messages, 3) // system stripped, 3 remaining
	assert.Equal(t, "user", antReq.Messages[0].Role)
	assert.Equal(t, "assistant", antReq.Messages[1].Role)
	assert.Equal(t, "user", antReq.Messages[2].Role)
}

func TestTranslateRequest_DefaultMaxTokens(t *testing.T) {
	req := &openaitypes.ChatCompletionRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []openaitypes.Message{{Role: "user", Content: "Hi"}},
		// MaxTokens intentionally not set
	}

	antReq, err := translateRequest(req)
	require.NoError(t, err)
	assert.Equal(t, 4096, antReq.MaxTokens, "should default to 4096 when not specified")
}

func TestStopReasonMapping(t *testing.T) {
	cases := []struct {
		in  string
		out string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"stop_sequence", "stop"},
		{"tool_use", "tool_calls"},
		{"unknown", "stop"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.out, stopReasonToFinishReason(tc.in), "input: %s", tc.in)
	}
}
