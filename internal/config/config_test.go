package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.Server.Addr)
	assert.Equal(t, 30*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 5*time.Minute, cfg.Server.WriteTimeout)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
	assert.False(t, cfg.Auth.Enabled)
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("GATEWAY_SERVER_ADDR", ":9090")
	t.Setenv("GATEWAY_LOG_LEVEL", "debug")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.Server.Addr)
	assert.Equal(t, "debug", cfg.Log.Level)
}

func TestLoadFromYAML(t *testing.T) {
	content := `
server:
  addr: ":7777"
log:
  level: "warn"
providers:
  - type: ollama
    base_url: "http://localhost:11434"
    models:
      - llama3.2:3b
    priority: 0
    timeout: 30s
`
	f, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()

	cfg, err := Load(f.Name())
	require.NoError(t, err)

	assert.Equal(t, ":7777", cfg.Server.Addr)
	assert.Equal(t, "warn", cfg.Log.Level)
	require.Len(t, cfg.Providers, 1)
	assert.Equal(t, "ollama", cfg.Providers[0].Type)
	assert.Equal(t, "http://localhost:11434", cfg.Providers[0].BaseURL)
	assert.Equal(t, []string{"llama3.2:3b"}, cfg.Providers[0].Models)
}

func TestValidateUnknownProviderType(t *testing.T) {
	content := `
providers:
  - type: unknown_provider
    base_url: "http://localhost:9999"
`
	f, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()

	_, err = Load(f.Name())
	assert.ErrorContains(t, err, "unknown type")
}

func TestValidateMissingBaseURL(t *testing.T) {
	content := `
providers:
  - type: ollama
`
	f, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()

	_, err = Load(f.Name())
	assert.ErrorContains(t, err, "base_url is required")
}
