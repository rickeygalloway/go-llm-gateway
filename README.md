# Go-LLM-Gateway

A production-grade LLM gateway in Go. Drop-in OpenAI-compatible API that routes requests across multiple backends (Ollama, OpenAI, Anthropic, vLLM) with automatic fallback, SSE streaming, and full observability.

---

## Features

- **OpenAI-compatible API** — existing clients point their `base_url` here, zero code changes
- **Multi-provider routing** — Ollama (local), OpenAI, Anthropic, vLLM behind one endpoint
- **Fallback chain** — if provider A returns 5xx or 429, automatically tries B, then C
- **SSE streaming** — `"stream": true` works across all providers
- **Config-driven** — YAML + env var overrides, no recompile needed
- **Structured logging** — zerolog JSON output, request ID propagation
- **Graceful shutdown** — drains in-flight requests on SIGTERM
- **Health + readiness probes** — `/healthz` and `/readyz` for K8s

**Coming next:** Redis rate limiting · pgvector semantic cache · OpenTelemetry tracing · Kafka async inference · K8s operator

---

## Start

**First time only** — pull a model after the stack is up:
```bash
docker compose up -d
docker exec -it go-llm-gateway-ollama-1 ollama pull llama3.2:3b
```

**Every other time** — one command:
```bash
docker compose up -d
```

Everything has `restart: unless-stopped`, so it also comes back automatically after a reboot.

---

## build.sh cheat sheet

`build.sh` auto-discovers your Go binary — no PATH setup required.

```bash
./build.sh            # compile → bin/gateway
./build.sh test       # run all unit tests
./build.sh check      # vet + build + test (CI-style)
./build.sh run        # run gateway locally on :8080
./build.sh tidy       # go mod tidy
```

`./build.sh run` uses `config.local.yaml` (localhost URLs) automatically when present, so it works alongside the Dockerised infrastructure without any extra config.

---

## Test the running gateway

```bash
# Chat completion
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"llama3.2:3b\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello\"}]}"

# Streaming
curl -N http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"llama3.2:3b\",\"messages\":[{\"role\":\"user\",\"content\":\"Count to 5\"}],\"stream\":true}"

# List models
curl -s http://localhost:8080/v1/models

# Health check
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
```

---

## Configuration

Edit `config.yaml` or override any field with `GATEWAY_<KEY>` env vars.

```yaml
providers:
  - type: ollama          # ollama | openai | anthropic | vllm
    name: ollama
    base_url: "http://ollama:11434"
    priority: 0           # lower = tried first in fallback chain
    timeout: 120s
    models:               # leave empty to query provider dynamically
      - llama3.2:3b
```

| Field | Env var override | Default |
|---|---|---|
| `server.addr` | `GATEWAY_SERVER_ADDR` | `:8080` |
| `log.level` | `GATEWAY_LOG_LEVEL` | `info` |
| `log.format` | `GATEWAY_LOG_FORMAT` | `json` |
| `auth.enabled` | `GATEWAY_AUTH_ENABLED` | `false` |

To add OpenAI as a fallback, uncomment the block in `config.yaml` and set:

```bash
GATEWAY_PROVIDERS_1_APIKEY=sk-...
```

---

## Architecture

```
go-llm-gateway/
├── cmd/gateway/main.go              # HTTP server + graceful shutdown
├── internal/
│   ├── config/config.go             # Viper config loader (YAML + env)
│   ├── providers/
│   │   ├── interface.go             # Provider interface (Chat, ChatStream, ListModels, Health)
│   │   ├── errors.go                # Typed errors + IsRetryable() routing oracle
│   │   ├── registry.go              # Thread-safe provider map
│   │   ├── ollama/provider.go       # Ollama REST adapter
│   │   ├── openai/provider.go       # OpenAI SDK adapter (sashabaranov/go-openai)
│   │   ├── anthropic/provider.go    # Anthropic Messages API adapter (manual REST)
│   │   └── vllm/provider.go         # vLLM adapter (wraps OpenAI adapter)
│   └── gateway/
│       ├── router.go                # Fallback chain: try A → B → C
│       ├── handler.go               # chi routes, SSE streaming, error mapping
│       └── middleware.go            # RequestID, Logger, Recoverer
├── pkg/openai/types.go              # Canonical wire types (all providers translate to/from these)
├── deploy/
│   ├── helm/                        # Helm chart (Deployment, Service, ConfigMap)
│   └── k8s/init.sql                 # Postgres schema (requests log + pgvector cache)
├── Dockerfile                       # Multi-stage: golang:1.23-alpine → distroless
├── docker-compose.yaml              # Full local stack
└── build.sh                         # Zero-config build script
```

The key design decision: **all providers speak the OpenAI wire format**. The `pkg/openai/types.go` types are the single source of truth. Adding a new backend means implementing one interface — nothing else changes.

---

## Roadmap

| Step | Status | Description |
|---|---|---|
| 1 | **Done** | Multi-provider router, fallback chain, streaming, full scaffold |
| 2 | Planned | Redis rate limiting (per API key + per model) |
| 3 | Planned | OpenTelemetry tracing + Prometheus metrics (token latency, $/query) |
| 4 | Planned | Kafka producer/consumer (async batch inference, model-refresh events) |
| 5 | Planned | pgvector semantic cache + request metadata store |
| 6 | Planned | Kubernetes operator (controller-runtime, LLMBackend + LLMRoute CRDs) |

---

## Tests

```bash
./build.sh test     # unit tests (no external deps, httptest mocks)
./build.sh check    # vet + build + test
```

Unit tests use `httptest.NewServer` to mock each provider's HTTP API — no running Ollama or API keys needed. Integration tests (tagged `//go:build integration`) require `docker compose up -d` first.

---

## Development

Pre-commit hooks run automatically on every `git commit`:

| Hook | Action |
|------|--------|
| `go-fmt` | Format Go code |
| `go-vet` | Run go vet |
| `gitleaks` | Block commits containing credentials or high-entropy secrets |
| `detect-private-key` | Block PEM private keys |

Run `pre-commit install` once after cloning.

```bash
# Manual quality checks
make fmt      # gofmt + goimports
make vet      # go vet
make lint     # golangci-lint
make test     # unit tests with race detector
make check    # all of the above
```
