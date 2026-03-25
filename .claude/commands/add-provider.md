Add a new LLM provider to go-llm-gateway. The provider name is: $ARGUMENTS

Follow these steps in order:

## 1. Create the provider package

Create `internal/providers/<name>/provider.go`.

If the provider has an OpenAI-compatible API (like DeepSeek, Grok, Azure), copy the vLLM pattern — wrap the OpenAI adapter with a default base URL:

```go
package <name>

import (
    "context"
    "github.com/rs/zerolog"
    "github.com/go-llm-gateway/go-llm-gateway/internal/providers"
    oaiprovider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/openai"
    openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

const defaultBaseURL = "https://api.<name>.com/v1"

type Provider struct {
    inner *oaiprovider.Provider
    name  string
}

func New(cfg providers.ProviderConfig, _ zerolog.Logger) *Provider {
    if cfg.BaseURL == "" {
        cfg.BaseURL = defaultBaseURL
    }
    return &Provider{inner: oaiprovider.New(cfg), name: cfg.EffectiveName()}
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
    for i := range models {
        models[i].OwnedBy = p.name
    }
    return models, nil
}
func (p *Provider) Health(ctx context.Context) error { return p.inner.Health(ctx) }
```

If the provider has a custom API (like Ollama or Anthropic), implement the full Provider interface from scratch following the existing pattern in `internal/providers/ollama/provider.go`.

## 2. Register in config validation

In `internal/config/config.go`, add `"<name>": true` to the `validTypes` map, and add `&& p.Type != "<name>"` to the base_url required check if the provider has a known default base URL.

## 3. Wire into main.go

In `cmd/gateway/main.go`:
- Add the import: `<name>provider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/<name>"`
- Add a case to the switch: `case "<name>": p = <name>provider.New(pc, logger)`

## 4. Update config.yaml

Add a commented-out block in the cloud fallbacks section:
```yaml
# - type: <name>
#   name: <name>
#   priority: <next available>
```

## 5. Update config.local.yaml.example

Add an optional commented-out example with the provider's models.

## 6. Update README.md

Add `<name>` to the provider types comment in the Configuration section.

## 7. Build and test

Run `./build.sh check` and confirm all checks pass before finishing.
