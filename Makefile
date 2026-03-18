# Go-LLM-Gateway Makefile
#
# Usage:
#   make build       — compile all binaries
#   make test        — run unit tests with race detector
#   make test-int    — run integration tests (requires docker compose up)
#   make lint        — run golangci-lint
#   make docker-up   — start all services
#   make docker-down — stop all services
#   make run         — run gateway locally (requires config.yaml)
#   make clean       — remove build artifacts

MODULE   := github.com/go-llm-gateway/go-llm-gateway
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -w -s \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.BuildDate=$(DATE)

# ---- Build targets ----------------------------------------------------------

.PHONY: build
build:
	go build -ldflags="$(LDFLAGS)" -o bin/gateway ./cmd/gateway

.PHONY: build-operator
build-operator:
	go build -ldflags="$(LDFLAGS)" -o bin/operator ./cmd/operator

.PHONY: build-all
build-all: build build-operator

# ---- Test targets -----------------------------------------------------------

.PHONY: test
test:
	go test ./... -race -count=1 -timeout=60s

.PHONY: test-verbose
test-verbose:
	go test ./... -v -race -count=1 -timeout=60s

.PHONY: test-int
test-int:
	go test ./... -tags=integration -race -count=1 -timeout=120s

.PHONY: test-cover
test-cover:
	go test ./... -race -count=1 -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# ---- Code quality -----------------------------------------------------------

.PHONY: lint
lint:
	golangci-lint run --timeout=5m

.PHONY: fmt
fmt:
	gofmt -s -w .
	goimports -w .

.PHONY: vet
vet:
	go vet ./...

.PHONY: tidy
tidy:
	go mod tidy

# ---- Docker targets ---------------------------------------------------------

.PHONY: docker-build
docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(DATE) \
		-t go-llm-gateway:$(VERSION) \
		-t go-llm-gateway:latest \
		.

.PHONY: docker-up
docker-up:
	docker compose up -d
	@echo ""
	@echo "Services started. Gateway will be available at http://localhost:8080"
	@echo "Pull a model with: docker exec -it go-llm-gateway-ollama-1 ollama pull llama3.2:3b"

.PHONY: docker-down
docker-down:
	docker compose down

.PHONY: docker-logs
docker-logs:
	docker compose logs -f gateway

.PHONY: docker-restart
docker-restart:
	docker compose restart gateway

# ---- Run locally ------------------------------------------------------------

.PHONY: run
run:
	go run -ldflags="$(LDFLAGS)" ./cmd/gateway -config config.yaml

.PHONY: run-dev
run-dev:
	GATEWAY_LOG_FORMAT=console GATEWAY_LOG_LEVEL=debug \
	go run -ldflags="$(LDFLAGS)" ./cmd/gateway -config config.yaml

# ---- Kubernetes / Helm ------------------------------------------------------

.PHONY: helm-lint
helm-lint:
	helm lint ./deploy/helm

.PHONY: helm-template
helm-template:
	helm template go-llm-gateway ./deploy/helm --values ./deploy/helm/values.yaml

.PHONY: helm-install
helm-install:
	helm upgrade --install go-llm-gateway ./deploy/helm \
		--namespace go-llm-gateway \
		--create-namespace \
		--values ./deploy/helm/values.yaml

# ---- Misc -------------------------------------------------------------------

.PHONY: generate
generate:
	go generate ./...

.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html

.PHONY: check
check: fmt vet lint test

.DEFAULT_GOAL := build
