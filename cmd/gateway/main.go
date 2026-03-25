// Command gateway starts the Go-LLM-Gateway HTTP server.
//
// The startup sequence is:
//  1. Load configuration (YAML + env vars)
//  2. Initialise structured logger (zerolog)
//  3. Build provider adapters from config
//  4. Build provider registry + fallback router
//  5. Start HTTP server (chi)
//  6. Wait for OS signal, gracefully drain in-flight requests
//
// The server is OpenAI API-compatible: existing OpenAI clients can point their
// base URL at this gateway with zero code changes.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/go-llm-gateway/go-llm-gateway/internal/config"
	"github.com/go-llm-gateway/go-llm-gateway/internal/gateway"
	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	anthropicprovider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/anthropic"
	deepseeekprovider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/deepseek"
	grokprovider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/grok"
	ollamaprovider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/ollama"
	openaiprovider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/openai"
	vllmprovider "github.com/go-llm-gateway/go-llm-gateway/internal/providers/vllm"
	"github.com/go-llm-gateway/go-llm-gateway/internal/version"
)

func main() {
	configPath := flag.String("config", "", "path to config.yaml (default: ./config.yaml or /etc/go-llm-gateway/config.yaml)")
	flag.Parse()

	if err := run(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	// --- 1. Load config -------------------------------------------------------
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// --- 2. Set up logger ------------------------------------------------------
	logger := buildLogger(cfg.Log)
	logger.Info().
		Str("version", version.Version).
		Str("commit", version.Commit).
		Str("build_date", version.BuildDate).
		Msg("starting go-llm-gateway")

	// --- 3. Build providers ---------------------------------------------------
	registry := providers.NewRegistry()
	providerCfgs := make([]providers.ProviderConfig, 0, len(cfg.Providers))

	for _, pe := range cfg.Providers {
		pc := pe.ProviderConfig
		name := pc.EffectiveName()

		var p providers.Provider
		switch pc.Type {
		case "ollama":
			p = ollamaprovider.New(pc, logger)
		case "openai":
			p = openaiprovider.New(pc)
		case "anthropic":
			p = anthropicprovider.New(pc, logger)
		case "deepseek":
			p = deepseeekprovider.New(pc, logger)
		case "grok":
			p = grokprovider.New(pc, logger)
		case "vllm":
			p = vllmprovider.New(pc, logger)
		default:
			return fmt.Errorf("unknown provider type %q in config", pc.Type)
		}

		if err := registry.Register(name, p); err != nil {
			return fmt.Errorf("registering provider %q: %w", name, err)
		}
		providerCfgs = append(providerCfgs, pc)

		logger.Info().
			Str("provider", name).
			Str("type", pc.Type).
			Str("base_url", pc.BaseURL).
			Int("priority", pc.Priority).
			Strs("models", pc.Models).
			Msg("registered provider")
	}

	if len(providerCfgs) == 0 {
		logger.Warn().Msg("no providers configured — all requests will return 404")
	}

	// --- 4. Build router ------------------------------------------------------
	router, err := gateway.NewRouterFromRegistry(providerCfgs, registry, logger)
	if err != nil {
		return fmt.Errorf("building router: %w", err)
	}

	// --- 5. Start HTTP server -------------------------------------------------
	handler := gateway.NewHandler(router, logger)
	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Channel to receive server errors
	serverErr := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", cfg.Server.Addr).Msg("HTTP server listening")
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// --- 6. Wait for shutdown signal ------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info().Str("signal", sig.String()).Msg("shutdown signal received")
	case err := <-serverErr:
		return fmt.Errorf("HTTP server error: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	logger.Info().
		Dur("timeout", cfg.Server.ShutdownTimeout).
		Msg("draining in-flight requests...")

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info().Msg("server stopped cleanly")
	return nil
}

func buildLogger(cfg config.LogConfig) zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if cfg.Format == "console" {
		return log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}
	return zerolog.New(os.Stderr).With().Timestamp().Logger()
}
