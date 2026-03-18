// Package ollama implements the Provider interface for Ollama backends.
// Rather than importing github.com/ollama/ollama (the full server codebase),
// we speak directly to the Ollama REST API. This keeps the dependency tree
// lean and gives us full control over timeout and error handling.
//
// Ollama REST API reference: https://github.com/ollama/ollama/blob/main/docs/api.md
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

// Provider adapts an Ollama server to the gateway Provider interface.
type Provider struct {
	name    string
	baseURL string
	client  *http.Client
	logger  zerolog.Logger
}

// New creates an Ollama Provider. baseURL should be e.g. "http://ollama:11434".
func New(cfg providers.ProviderConfig, logger zerolog.Logger) *Provider {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &Provider{
		name:    cfg.EffectiveName(),
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		client:  &http.Client{Timeout: timeout},
		logger:  logger.With().Str("provider", cfg.EffectiveName()).Logger(),
	}
}

func (p *Provider) Name() string { return p.name }

// ---- Ollama-native types ---------------------------------------------------

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Model     string        `json:"model"`
	CreatedAt time.Time     `json:"created_at"`
	Message   ollamaMessage `json:"message"`
	Done      bool          `json:"done"`
	// Token stats (only present when done=true)
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}

type ollamaTagsResponse struct {
	Models []ollamaModelInfo `json:"models"`
}

type ollamaModelInfo struct {
	Name       string    `json:"name"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
}

// ---- Provider implementation ----------------------------------------------

func (p *Provider) Chat(ctx context.Context, req *openaitypes.ChatCompletionRequest) (*openaitypes.ChatCompletionResponse, error) {
	ollamaReq := &ollamaChatRequest{
		Model:    req.Model,
		Messages: translateMessages(req.Messages),
		Stream:   false,
	}

	if req.Temperature != 0 || req.TopP != 0 || req.MaxTokens != 0 {
		ollamaReq.Options = map[string]any{}
		if req.Temperature != 0 {
			ollamaReq.Options["temperature"] = req.Temperature
		}
		if req.TopP != 0 {
			ollamaReq.Options["top_p"] = req.TopP
		}
		if req.MaxTokens != 0 {
			ollamaReq.Options["num_predict"] = req.MaxTokens
		}
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("marshalling ollama request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, providers.NewProviderError(p.name, 0, "connection failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, providers.NewProviderError(p.name, resp.StatusCode, string(b), nil)
	}

	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("decoding ollama response: %w", err)
	}

	return &openaitypes.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-ollama-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   ollamaResp.Model,
		Choices: []openaitypes.Choice{
			{
				Index: 0,
				Message: openaitypes.Message{
					Role:    ollamaResp.Message.Role,
					Content: ollamaResp.Message.Content,
				},
				FinishReason: "stop",
			},
		},
		Usage: openaitypes.Usage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}, nil
}

// ChatStream opens a streaming connection to Ollama and forwards incremental
// chunks onto the returned channel. The error channel receives a single value
// (nil on clean EOF, non-nil on error) and is then closed.
func (p *Provider) ChatStream(ctx context.Context, req *openaitypes.ChatCompletionRequest) (<-chan openaitypes.ChatCompletionChunk, <-chan error) {
	chunks := make(chan openaitypes.ChatCompletionChunk, 32)
	errc := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errc)

		ollamaReq := &ollamaChatRequest{
			Model:    req.Model,
			Messages: translateMessages(req.Messages),
			Stream:   true,
		}

		body, err := json.Marshal(ollamaReq)
		if err != nil {
			errc <- fmt.Errorf("marshalling ollama stream request: %w", err)
			return
		}

		// Streaming requests need a context without a hard deadline on the
		// read side — use a detached context that mirrors parent cancellation.
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
		if err != nil {
			errc <- fmt.Errorf("creating stream request: %w", err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		// Use a client without a hard read timeout for streaming
		streamClient := &http.Client{}
		resp, err := streamClient.Do(httpReq)
		if err != nil {
			errc <- providers.NewProviderError(p.name, 0, "stream connection failed", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			errc <- providers.NewProviderError(p.name, resp.StatusCode, string(b), nil)
			return
		}

		id := fmt.Sprintf("chatcmpl-ollama-%d", time.Now().UnixNano())
		scanner := bufio.NewScanner(resp.Body)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var ollamaChunk ollamaChatResponse
			if err := json.Unmarshal(line, &ollamaChunk); err != nil {
				p.logger.Warn().Err(err).Msg("failed to decode ollama stream chunk")
				continue
			}

			finishReason := ""
			if ollamaChunk.Done {
				finishReason = "stop"
			}

			chunk := openaitypes.ChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   ollamaChunk.Model,
				Choices: []openaitypes.ChunkChoice{
					{
						Index: 0,
						Delta: openaitypes.Delta{
							Content: ollamaChunk.Message.Content,
						},
						FinishReason: finishReason,
					},
				},
			}

			select {
			case chunks <- chunk:
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			}

			if ollamaChunk.Done {
				errc <- nil
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errc <- fmt.Errorf("reading ollama stream: %w", err)
			return
		}
		errc <- nil
	}()

	return chunks, errc
}

func (p *Provider) ListModels(ctx context.Context) ([]openaitypes.Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("creating list models request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, providers.NewProviderError(p.name, 0, "connection failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, providers.NewProviderError(p.name, resp.StatusCode, string(b), nil)
	}

	var tagsResp ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("decoding tags response: %w", err)
	}

	models := make([]openaitypes.Model, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		models = append(models, openaitypes.Model{
			ID:      m.Name,
			Object:  "model",
			Created: m.ModifiedAt.Unix(),
			OwnedBy: p.name,
		})
	}
	return models, nil
}

func (p *Provider) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return providers.NewProviderError(p.name, 0, "health check failed", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return providers.NewProviderError(p.name, resp.StatusCode, "health check returned non-200", nil)
	}
	return nil
}

// translateMessages converts OpenAI message format to Ollama format.
// The role mapping is 1:1 (system/user/assistant) so this is mostly
// a struct translation.
func translateMessages(msgs []openaitypes.Message) []ollamaMessage {
	out := make([]ollamaMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, ollamaMessage{Role: m.Role, Content: m.Content})
	}
	return out
}
