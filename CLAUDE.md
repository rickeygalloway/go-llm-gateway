# go-llm-gateway

## Project Type

Go

## Commands

- **Build**: `./build.sh` or `make build`
- **Test**: `./build.sh test` or `make test`
- **Lint**: `make lint` (golangci-lint)
- **Format**: `make fmt` (gofmt + goimports)
- **Vet**: `make vet`
- **Full check**: `make check` (fmt + vet + lint + test)
- **Run locally**: `./build.sh run` (uses config.local.yaml if present)
- **Tidy**: `./build.sh tidy`

## Conventions

- Go 1.21+
- Module path: `github.com/go-llm-gateway/go-llm-gateway`
- All providers implement the `Provider` interface in `internal/providers/interface.go`
- All providers translate to/from `pkg/openai/types.go` — single source of truth for wire types
- Error handling: always check errors, wrap with `fmt.Errorf("context: %w", err)`
- Logging: zerolog structured JSON via `internal/middleware.go`; never use `log.Print`
- Config: Viper (YAML + env var overrides); never hardcode values
- Never commit `config.local.yaml` — use `config.yaml` as the template (no real keys in it)
- Pre-commit hooks run gofmt, go-vet, gitleaks on commit
- Unit tests use `httptest.NewServer` to mock provider HTTP APIs — no real API keys needed
- Integration tests tagged `//go:build integration` require `docker compose up -d`

## Architecture

```
cmd/gateway/main.go           # HTTP server + graceful shutdown
internal/
  config/config.go            # Viper config loader (YAML + env)
  providers/
    interface.go              # Provider interface (Chat, ChatStream, ListModels, Health)
    errors.go                 # Typed errors + IsRetryable() routing oracle
    registry.go               # Thread-safe provider map
    ollama/provider.go        # Ollama REST adapter
    openai/provider.go        # OpenAI SDK adapter
    anthropic/provider.go     # Anthropic Messages API adapter
    vllm/provider.go          # vLLM adapter (wraps OpenAI)
  gateway/
    router.go                 # Fallback chain: try A → B → C
    handler.go                # chi routes, SSE streaming, error mapping
    middleware.go             # RequestID, Logger, Recoverer
pkg/openai/types.go           # Canonical wire types
```
