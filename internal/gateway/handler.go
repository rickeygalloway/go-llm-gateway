package gateway

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/go-llm-gateway/go-llm-gateway/internal/providers"
	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

// Handler holds all HTTP handlers for the gateway and is wired to a chi Router.
type Handler struct {
	router *Router
	logger zerolog.Logger
}

// NewHandler creates a Handler and returns the configured chi.Mux.
// Mount this on your http.Server.
func NewHandler(router *Router, logger zerolog.Logger) http.Handler {
	h := &Handler{router: router, logger: logger}

	r := chi.NewRouter()

	// Core middleware (applied to all routes)
	r.Use(chimiddleware.RealIP)
	r.Use(RequestID)
	r.Use(Logger(logger))
	r.Use(Recoverer(logger))

	// OpenAI-compatible API routes
	r.Post("/v1/chat/completions", h.ChatCompletions)
	r.Get("/v1/models", h.Models)

	// Operational endpoints (no auth required)
	r.Get("/healthz", h.Healthz)
	r.Get("/readyz", h.Readyz)

	return r
}

// ChatCompletions handles POST /v1/chat/completions.
// Detects stream:true and routes to SSE streaming or non-streaming response.
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req openaitypes.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("could not parse request body: %v", err))
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "field 'model' is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "field 'messages' must not be empty")
		return
	}

	if req.Stream {
		h.chatCompletionsStream(w, r, &req)
		return
	}

	resp, err := h.router.Route(r.Context(), &req)
	if err != nil {
		h.writeProviderError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// chatCompletionsStream handles streaming requests using Server-Sent Events.
// The response format matches the OpenAI streaming spec:
//
//	data: {"id":"...","object":"chat.completion.chunk",...}\n\n
//	data: [DONE]\n\n
func (h *Handler) chatCompletionsStream(w http.ResponseWriter, r *http.Request, req *openaitypes.ChatCompletionRequest) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "server_error", "streaming not supported by this server")
		return
	}

	chunks, errc, err := h.router.RouteStream(r.Context(), req)
	if err != nil {
		h.writeProviderError(w, err)
		return
	}

	bw := bufio.NewWriter(w)

	for chunk := range chunks {
		data, err := json.Marshal(chunk)
		if err != nil {
			h.logger.Error().Err(err).Msg("failed to marshal stream chunk")
			continue
		}
		fmt.Fprintf(bw, "data: %s\n\n", data)
		bw.Flush()
		flusher.Flush()
	}

	// Check for stream error
	if streamErr := <-errc; streamErr != nil {
		h.logger.Error().Err(streamErr).Msg("stream error")
		// Can't change status code after streaming started; send an error event.
		fmt.Fprintf(bw, "data: {\"error\":{\"message\":\"%v\",\"type\":\"server_error\"}}\n\n", streamErr)
		bw.Flush()
		flusher.Flush()
		return
	}

	fmt.Fprintf(bw, "data: [DONE]\n\n")
	bw.Flush()
	flusher.Flush()
}

// Models handles GET /v1/models.
// Aggregates models from all healthy providers.
func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	models := h.router.AllModels(r.Context())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openaitypes.ModelList{
		Object: "list",
		Data:   models,
	})
}

// Healthz handles GET /healthz — a lightweight liveness probe.
// Returns 200 immediately without checking providers.
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// Readyz handles GET /readyz — a readiness probe that checks provider health.
// Returns 200 if at least one provider is healthy, 503 otherwise.
func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	statuses := h.router.HealthCheck(r.Context())

	healthy := false
	for _, s := range statuses {
		if s.Healthy {
			healthy = true
			break
		}
	}

	statusCode := http.StatusOK
	readyStatus := "ready"
	if !healthy {
		statusCode = http.StatusServiceUnavailable
		readyStatus = "not ready"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]any{
		"status":    readyStatus,
		"providers": statuses,
	})
}

// ---- Error helpers ---------------------------------------------------------

func writeError(w http.ResponseWriter, statusCode int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(openaitypes.ErrorResponse{
		Error: openaitypes.APIError{
			Type:    errType,
			Message: message,
		},
	})
}

// writeProviderError translates a provider error into an appropriate HTTP response.
func (h *Handler) writeProviderError(w http.ResponseWriter, err error) {
	h.logger.Error().Err(err).Msg("provider error")

	switch {
	case errors.Is(err, providers.ErrNoProvidersForModel):
		writeError(w, http.StatusNotFound, "invalid_request_error", err.Error())
	case errors.Is(err, providers.ErrAllProvidersFailed):
		writeError(w, http.StatusBadGateway, "server_error", "all upstream providers are unavailable")
	default:
		// Check if it's a non-retryable ProviderError (e.g. 400, 404 from upstream)
		var pe *providers.ProviderError
		if errors.As(err, &pe) {
			if pe.StatusCode == http.StatusNotFound {
				writeError(w, http.StatusNotFound, "invalid_request_error", pe.Message)
				return
			}
			if pe.StatusCode >= 400 && pe.StatusCode < 500 {
				writeError(w, pe.StatusCode, "invalid_request_error", pe.Message)
				return
			}
		}
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
	}
}
