// Package gateway implements the core request routing and HTTP handler logic.
// The Router implements a fallback chain: if provider A fails with a retryable
// error (5xx, 429, network timeout), it tries provider B, then C, until one
// succeeds or all are exhausted.
package gateway

import (
	"context"
	"fmt"
	"sort"

	"github.com/rs/zerolog"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

// RouteConfig maps a model ID pattern to an ordered list of provider names.
// The gateway tries providers in slice order; first success wins.
type RouteConfig struct {
	// Model is the exact model ID this route applies to, e.g. "llama3.2:3b".
	// Use "*" for a catch-all default route.
	Model string

	// ProviderNames is the ordered fallback chain, e.g. ["ollama", "vllm"].
	ProviderNames []string
}

// Router routes incoming chat completion requests to the appropriate provider,
// with automatic fallback on retryable errors.
type Router struct {
	// chains maps a model ID → ordered slice of providers to try.
	chains map[string][]providers.Provider

	// defaultChain is used when no model-specific chain exists.
	defaultChain []providers.Provider

	// registry holds all registered providers (used for /v1/models aggregation).
	registry *providers.Registry

	logger zerolog.Logger
}

// NewRouter builds a Router from a set of RouteConfigs and a Provider Registry.
// Routes are sorted by specificity: exact model matches before wildcards.
func NewRouter(routes []RouteConfig, registry *providers.Registry, logger zerolog.Logger) (*Router, error) {
	r := &Router{
		chains:   make(map[string][]providers.Provider),
		registry: registry,
		logger:   logger,
	}

	for _, route := range routes {
		chain := make([]providers.Provider, 0, len(route.ProviderNames))
		for _, name := range route.ProviderNames {
			p, ok := registry.Get(name)
			if !ok {
				return nil, fmt.Errorf("route for model %q references unknown provider %q", route.Model, name)
			}
			chain = append(chain, p)
		}

		if route.Model == "*" {
			r.defaultChain = chain
		} else {
			r.chains[route.Model] = chain
		}
	}

	return r, nil
}

// NewRouterFromRegistry builds a Router where each provider handles exactly
// the models listed in its ProviderConfig.Models. If Models is empty, that
// provider is added to the default chain.
// Providers are sorted by Priority (ascending) so lower numbers are tried first.
func NewRouterFromRegistry(cfgs []providers.ProviderConfig, registry *providers.Registry, logger zerolog.Logger) (*Router, error) {
	// Sort by priority so lower priority value = tried first
	sorted := make([]providers.ProviderConfig, len(cfgs))
	copy(sorted, cfgs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	r := &Router{
		chains:   make(map[string][]providers.Provider),
		registry: registry,
		logger:   logger,
	}

	for _, cfg := range sorted {
		name := cfg.EffectiveName()
		p, ok := registry.Get(name)
		if !ok {
			logger.Warn().Str("provider", name).Msg("provider in config not found in registry, skipping")
			continue
		}

		if len(cfg.Models) == 0 {
			// No model restriction: add to default chain
			r.defaultChain = append(r.defaultChain, p)
		} else {
			for _, model := range cfg.Models {
				r.chains[model] = append(r.chains[model], p)
			}
		}
	}

	return r, nil
}

// Route dispatches a chat completion request to the appropriate provider chain.
// If the primary provider returns a retryable error, it tries the next provider.
// Non-retryable errors (404 model not found, 400 bad request) are returned immediately.
func (r *Router) Route(ctx context.Context, req *openaitypes.ChatCompletionRequest) (*openaitypes.ChatCompletionResponse, error) {
	chain := r.chainForModel(req.Model)
	if len(chain) == 0 {
		return nil, providers.ErrNoProvidersForModel
	}

	var lastErr error
	for i, p := range chain {
		r.logger.Debug().
			Str("provider", p.Name()).
			Str("model", req.Model).
			Int("attempt", i+1).
			Int("chain_len", len(chain)).
			Msg("routing request")

		resp, err := p.Chat(ctx, req)
		if err == nil {
			if i > 0 {
				r.logger.Info().
					Str("provider", p.Name()).
					Int("fallback_attempt", i+1).
					Msg("fallback provider succeeded")
			}
			return resp, nil
		}

		lastErr = err

		if !providers.IsRetryable(err) {
			r.logger.Debug().
				Str("provider", p.Name()).
				Err(err).
				Msg("non-retryable error, stopping fallback chain")
			return nil, err
		}

		r.logger.Warn().
			Str("provider", p.Name()).
			Str("model", req.Model).
			Int("attempt", i+1).
			Err(err).
			Msg("retryable error, trying next provider")
	}

	return nil, fmt.Errorf("%w: last error: %w", providers.ErrAllProvidersFailed, lastErr)
}

// RouteStream dispatches a streaming chat completion request.
// Falls back synchronously before opening the stream: if a provider's Chat()
// call fails (e.g. health check), we try the next one. Once streaming starts,
// we commit to that provider.
//
// Note: For production, a more sophisticated approach would be to try providers
// in parallel and pick the first to respond — that's a future enhancement.
func (r *Router) RouteStream(ctx context.Context, req *openaitypes.ChatCompletionRequest) (<-chan openaitypes.ChatCompletionChunk, <-chan error, error) {
	chain := r.chainForModel(req.Model)
	if len(chain) == 0 {
		return nil, nil, providers.ErrNoProvidersForModel
	}

	// For streaming, we use Health() to pre-screen providers before committing.
	for _, p := range chain {
		if err := p.Health(ctx); err != nil {
			if !providers.IsRetryable(err) {
				return nil, nil, err
			}
			r.logger.Warn().Str("provider", p.Name()).Err(err).Msg("provider unhealthy, trying next for stream")
			continue
		}

		chunks, errc := p.ChatStream(ctx, req)
		return chunks, errc, nil
	}

	return nil, nil, providers.ErrAllProvidersFailed
}

// AllModels aggregates the model list from all healthy providers.
// Used by the /v1/models endpoint.
func (r *Router) AllModels(ctx context.Context) []openaitypes.Model {
	var all []openaitypes.Model
	seen := make(map[string]bool)

	for _, p := range r.registry.All() {
		models, err := p.ListModels(ctx)
		if err != nil {
			r.logger.Warn().Str("provider", p.Name()).Err(err).Msg("failed to list models from provider")
			continue
		}
		for _, m := range models {
			if !seen[m.ID] {
				seen[m.ID] = true
				all = append(all, m)
			}
		}
	}
	return all
}

// chainForModel returns the provider chain for the given model ID.
// Falls back to defaultChain if no model-specific chain is registered.
func (r *Router) chainForModel(model string) []providers.Provider {
	if chain, ok := r.chains[model]; ok {
		return chain
	}
	return r.defaultChain
}

// HealthStatus reports the health of all registered providers.
// Used by the /readyz endpoint.
type HealthStatus struct {
	Provider string `json:"provider"`
	Healthy  bool   `json:"healthy"`
	Error    string `json:"error,omitempty"`
}

func (r *Router) HealthCheck(ctx context.Context) []HealthStatus {
	all := r.registry.All()
	statuses := make([]HealthStatus, 0, len(all))

	for _, p := range all {
		status := HealthStatus{Provider: p.Name(), Healthy: true}
		if err := p.Health(ctx); err != nil {
			status.Healthy = false
			status.Error = err.Error()
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// IsHealthy returns true if at least one provider is healthy.
func (r *Router) IsHealthy(ctx context.Context) bool {
	for _, s := range r.HealthCheck(ctx) {
		if s.Healthy {
			return true
		}
	}
	return false
}
