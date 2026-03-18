# Multi-stage build:
#   Stage 1 (builder): compile the Go binary with all dependencies cached.
#   Stage 2 (final):   distroless image — no shell, no package manager, minimal attack surface.
#
# Build: docker build -t go-llm-gateway:dev .
# Run:   docker run -p 8080:8080 -v $(pwd)/config.yaml:/etc/go-llm-gateway/config.yaml go-llm-gateway:dev

# ---- Stage 1: Build ---------------------------------------------------------
FROM golang:1.23-alpine AS builder

# Install git (needed for go mod download with private deps)
RUN apk add --no-cache git

WORKDIR /app

# Copy dependency manifest first for layer caching:
# As long as go.mod/go.sum don't change, this layer is reused even when source changes.
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build flags:
#   CGO_ENABLED=0   → fully static binary (required for distroless)
#   -w -s           → strip debug info and symbol table (reduces binary size ~30%)
#   -X              → inject version info at build time
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-w -s \
        -X github.com/go-llm-gateway/go-llm-gateway/internal/version.Version=${VERSION} \
        -X github.com/go-llm-gateway/go-llm-gateway/internal/version.Commit=${COMMIT} \
        -X github.com/go-llm-gateway/go-llm-gateway/internal/version.BuildDate=${BUILD_DATE}" \
    -o /gateway \
    ./cmd/gateway

# ---- Stage 2: Final ---------------------------------------------------------
# gcr.io/distroless/static-debian12:
#   - No shell, no apt, no user management tools
#   - Contains only: CA certificates, tzdata, /etc/passwd, libc
#   - ~2MB image (vs ~300MB golang:alpine)
FROM gcr.io/distroless/static-debian12

# Copy the compiled binary from the builder stage
COPY --from=builder /gateway /gateway

# Configuration file (mount via -v or K8s ConfigMap)
COPY --from=builder /app/config.yaml /etc/go-llm-gateway/config.yaml

EXPOSE 8080

# Distroless images run as uid 65532 (nonroot) by default — no need for USER directive
ENTRYPOINT ["/gateway"]
