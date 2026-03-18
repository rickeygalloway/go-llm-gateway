package gateway

import (
	"context"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// contextKey is an unexported type for context keys in this package.
type contextKey int

const requestIDKey contextKey = 1

const requestIDHeader = "X-Request-ID"

// RequestID middleware injects a unique request ID into the context and
// adds it to the response headers. Reads X-Request-ID from the incoming
// request if present (useful when the gateway is behind a load balancer).
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext retrieves the request ID stored by the RequestID middleware.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// Logger middleware logs each request with method, path, status code,
// duration, and the request ID. Uses zerolog for zero-allocation JSON logging.
func Logger(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := newResponseWriter(w)

			next.ServeHTTP(ww, r)

			logger.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.status).
				Int64("latency_ms", time.Since(start).Milliseconds()).
				Str("remote_addr", r.RemoteAddr).
				Str("request_id", w.Header().Get(requestIDHeader)).
				Msg("request")
		})
	}
}

// Recoverer catches panics, logs them with a stack trace, and returns 500.
// This prevents a single bad request from crashing the entire server.
func Recoverer(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rv := recover(); rv != nil {
					logger.Error().
						Interface("panic", rv).
						Bytes("stack", debug.Stack()).
						Str("request_id", w.Header().Get(requestIDHeader)).
						Msg("panic recovered")
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// responseWriter is a minimal wrapper that captures the HTTP status code
// for logging purposes.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
