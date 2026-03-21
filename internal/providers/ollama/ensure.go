package ollama

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/rs/zerolog"
)

// EnsureRunning checks whether the Ollama server at baseURL is reachable.
// If not, it attempts to start it by running "ollama serve" and waits up to
// 30 seconds for it to become ready. Returns nil if Ollama is ready,
// or an error if it could not be reached or started.
func EnsureRunning(ctx context.Context, baseURL string, logger zerolog.Logger) error {
	if ping(baseURL) {
		logger.Debug().Str("base_url", baseURL).Msg("ollama already running")
		return nil
	}

	logger.Info().Str("base_url", baseURL).Msg("ollama not reachable, attempting to start")

	if _, err := exec.LookPath("ollama"); err != nil {
		return fmt.Errorf("ollama not found in PATH (install from https://ollama.com): %w", err)
	}

	cmd := exec.Command("ollama", "serve") //nolint:gosec
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting ollama serve: %w", err)
	}

	logger.Info().Int("pid", cmd.Process.Pid).Msg("ollama process started, waiting for readiness")

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if ping(baseURL) {
			logger.Info().Msg("ollama is ready")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("ollama did not become ready within 30s at %s", baseURL)
}

// ping returns true if the Ollama server at baseURL responds to a health check.
func ping(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/") //nolint:noctx
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
