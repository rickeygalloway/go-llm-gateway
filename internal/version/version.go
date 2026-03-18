// Package version exposes build-time version information.
// The Version variable is injected via -ldflags during `go build`:
//
//	go build -ldflags="-X github.com/go-llm-gateway/go-llm-gateway/internal/version.Version=v1.2.3"
package version

// Version is the semantic version string, injected at build time.
// Falls back to "dev" for local builds without ldflags.
var Version = "dev"

// Commit is the git commit SHA, injected at build time.
var Commit = "unknown"

// BuildDate is the ISO 8601 build date, injected at build time.
var BuildDate = "unknown"
