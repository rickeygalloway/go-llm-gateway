#!/usr/bin/env bash
# build.sh — zero-config build script for go-llm-gateway
#
# Usage:
#   ./build.sh           — build bin/gateway (default)
#   ./build.sh test      — run all unit tests
#   ./build.sh run       — run the gateway (uses config.local.yaml if present, else config.yaml)
#   ./build.sh tidy      — go mod tidy
#   ./build.sh check     — vet + build + test
#
# Automatically finds the Go binary so you don't need Go in your PATH.

set -euo pipefail

# ---- Locate Go binary -------------------------------------------------------
find_go() {
  # 1. Respect GOROOT if already set
  if [[ -n "${GOROOT:-}" && -x "$GOROOT/bin/go" ]]; then
    echo "$GOROOT/bin/go"; return
  fi

  # 2. pre-commit / goenv cache (matches this machine's known location)
  local user="${USERNAME:-$(whoami)}"
  for path in /c/Users/"$user"/.cache/pre-commit/*/golangenv-default/.go/bin/go.exe; do
    if [[ -x "$path" ]]; then echo "$path"; return; fi
  done

  # 3. Standard Windows installer
  if [[ -x "/c/Program Files/Go/bin/go.exe" ]]; then
    echo "/c/Program Files/Go/bin/go.exe"; return
  fi

  # 4. ~/sdk/go* (gotip / goenv)
  for path in ~/sdk/go*/bin/go.exe ~/sdk/go*/bin/go; do
    if [[ -x "$path" ]]; then echo "$path"; return; fi
  done

  # 5. PATH fallback
  if command -v go &>/dev/null; then
    command -v go; return
  fi

  echo "" # not found
}

GO=$(find_go)
if [[ -z "$GO" ]]; then
  echo "error: could not find a Go installation." >&2
  echo "       Install Go from https://go.dev/dl/ or set GOROOT." >&2
  exit 1
fi

# Add the Go bin dir to PATH so sub-commands (go test, go run, etc.) also work
export PATH="$(dirname "$GO"):$PATH"
export GO

echo "Using Go: $($GO version)"

# ---- Build metadata ---------------------------------------------------------
MODULE="github.com/go-llm-gateway/go-llm-gateway"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")
LDFLAGS="-w -s \
  -X ${MODULE}/internal/version.Version=${VERSION} \
  -X ${MODULE}/internal/version.Commit=${COMMIT} \
  -X ${MODULE}/internal/version.BuildDate=${DATE}"

# ---- Dispatch ---------------------------------------------------------------
CMD="${1:-build}"

case "$CMD" in
  build)
    echo "Building bin/gateway..."
    mkdir -p bin
    "$GO" build -ldflags="$LDFLAGS" -o bin/gateway ./cmd/gateway
    echo "Done → bin/gateway"
    ;;

  test)
    echo "Running tests..."
    "$GO" test ./... -count=1 -timeout=60s
    ;;

  run)
    # Prefer config.local.yaml (localhost URLs) over config.yaml (Docker hostnames)
    CONFIG="config.yaml"
    if [[ -f "config.local.yaml" ]]; then
      CONFIG="config.local.yaml"
    fi
    echo "Starting gateway with $CONFIG (Ctrl+C to stop)..."
    "$GO" run -ldflags="$LDFLAGS" ./cmd/gateway -config "$CONFIG"
    ;;

  tidy)
    echo "Running go mod tidy..."
    "$GO" mod tidy
    echo "Done."
    ;;

  check)
    echo "==> go vet"
    "$GO" vet ./...
    echo "==> go build"
    mkdir -p bin
    "$GO" build -ldflags="$LDFLAGS" -o bin/gateway ./cmd/gateway
    echo "==> go test"
    "$GO" test ./... -count=1 -timeout=60s
    echo "All checks passed."
    ;;

  *)
    echo "Unknown command: $CMD"
    echo "Usage: $0 [build|test|run|tidy|check]"
    exit 1
    ;;
esac
