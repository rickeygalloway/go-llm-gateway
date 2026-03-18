package providers

import (
	"errors"
	"fmt"
	"net/http"
)

// ProviderError wraps a provider response error with metadata used by the router
// to decide whether to attempt the next provider in the fallback chain.
type ProviderError struct {
	Provider   string // provider name that returned the error
	StatusCode int    // HTTP status code from the upstream provider
	Message    string // human-readable error message
	Retryable  bool   // true → router should try the next provider
	Cause      error  // underlying error, if any
}

func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("provider %q: HTTP %d: %s: %v", e.Provider, e.StatusCode, e.Message, e.Cause)
	}
	return fmt.Sprintf("provider %q: HTTP %d: %s", e.Provider, e.StatusCode, e.Message)
}

func (e *ProviderError) Unwrap() error { return e.Cause }

// NewProviderError constructs a ProviderError, setting Retryable based on StatusCode.
//
// Retry policy:
//   - 429 Too Many Requests → retryable (rate-limited, try next provider)
//   - 5xx Server Error      → retryable (provider unavailable)
//   - 404 Not Found         → NOT retryable (wrong model, fail fast)
//   - 400 Bad Request       → NOT retryable (invalid request, fail fast)
//   - 0   (timeout/network) → retryable (connection failure)
func NewProviderError(provider string, statusCode int, message string, cause error) *ProviderError {
	retryable := false
	switch {
	case statusCode == 0:
		retryable = true // network / timeout
	case statusCode == http.StatusTooManyRequests:
		retryable = true
	case statusCode >= 500:
		retryable = true
	}
	return &ProviderError{
		Provider:   provider,
		StatusCode: statusCode,
		Message:    message,
		Retryable:  retryable,
		Cause:      cause,
	}
}

// IsRetryable reports whether the error indicates the router should try the
// next provider in the fallback chain.
func IsRetryable(err error) bool {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Retryable
	}
	// Unknown errors (e.g. context.DeadlineExceeded from the outer request) are
	// not retried — we don't want to fan out a cancelled request.
	return false
}

// Sentinel errors for common cases. Use errors.Is() to check these.
var (
	// ErrAllProvidersFailed is returned by the router when every provider in the
	// fallback chain has been exhausted.
	ErrAllProvidersFailed = errors.New("all providers in the fallback chain failed")

	// ErrNoProvidersForModel is returned when no provider is configured for the
	// requested model.
	ErrNoProvidersForModel = errors.New("no providers configured for the requested model")

	// ErrStreamingNotSupported is returned when a streaming request is made to a
	// provider that has not implemented ChatStream.
	ErrStreamingNotSupported = errors.New("provider does not support streaming")
)
