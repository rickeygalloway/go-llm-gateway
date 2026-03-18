// Package config handles configuration loading from YAML files and environment
// variables using Viper. Environment variables take precedence over the file.
//
// Env var mapping: GATEWAY_<FIELD> (e.g. GATEWAY_SERVER_ADDR=":9090")
// For slice fields use GATEWAY_PROVIDERS_0_BASEURL, GATEWAY_PROVIDERS_1_APIKEY, etc.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
)

// Config is the top-level configuration struct.
type Config struct {
	Server    ServerConfig      `mapstructure:"server"`
	Providers []ProviderEntry   `mapstructure:"providers"`
	Log       LogConfig         `mapstructure:"log"`
	Auth      AuthConfig        `mapstructure:"auth"`
}

// ServerConfig controls the HTTP server behaviour.
type ServerConfig struct {
	Addr            string        `mapstructure:"addr"`             // default: ":8080"
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`     // default: 30s
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`    // default: 5m (streaming)
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"` // default: 30s
}

// ProviderEntry adds routing metadata on top of the base ProviderConfig.
type ProviderEntry struct {
	providers.ProviderConfig `mapstructure:",squash"`
}

// LogConfig controls zerolog output format and level.
type LogConfig struct {
	Level  string `mapstructure:"level"`  // trace|debug|info|warn|error; default: info
	Format string `mapstructure:"format"` // json|console; default: json
}

// AuthConfig configures API key authentication on the gateway itself.
type AuthConfig struct {
	Enabled bool     `mapstructure:"enabled"` // default: false (open for local dev)
	Keys    []string `mapstructure:"keys"`    // list of valid bearer tokens
}

// Load reads configuration from the given path (YAML).
// If path is empty, it attempts to load "config.yaml" from the current directory.
// Environment variables always override file values.
func Load(path string) (*Config, error) {
	v := viper.New()

	// Bind environment variables with GATEWAY_ prefix
	v.SetEnvPrefix("GATEWAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// File config
	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/go-llm-gateway")
	}

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		// Missing config file is acceptable — we fall through to defaults + env vars
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.addr", ":8080")
	v.SetDefault("server.read_timeout", 30*time.Second)
	v.SetDefault("server.write_timeout", 5*time.Minute) // long for streaming
	v.SetDefault("server.shutdown_timeout", 30*time.Second)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("auth.enabled", false)
}

func validate(cfg *Config) error {
	for i, p := range cfg.Providers {
		if p.Type == "" {
			return fmt.Errorf("providers[%d]: type is required", i)
		}
		validTypes := map[string]bool{"ollama": true, "openai": true, "anthropic": true, "vllm": true}
		if !validTypes[p.Type] {
			return fmt.Errorf("providers[%d]: unknown type %q (valid: ollama, openai, anthropic, vllm)", i, p.Type)
		}
		if p.BaseURL == "" && p.Type != "openai" && p.Type != "anthropic" {
			return fmt.Errorf("providers[%d] (%s): base_url is required", i, p.Type)
		}
	}
	return nil
}
