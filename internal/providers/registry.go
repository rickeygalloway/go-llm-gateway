package providers

import (
	"fmt"
	"sync"
)

// Registry is a thread-safe map of provider name → Provider instance.
// The gateway builds one Registry at startup and passes it to the router.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register adds a provider under the given name.
// Returns an error if the name is already registered.
func (r *Registry) Register(name string, p Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}
	r.providers[name] = p
	return nil
}

// MustRegister is like Register but panics on duplicate names.
// Intended for use in main() where a duplicate is a programming error.
func (r *Registry) MustRegister(name string, p Provider) {
	if err := r.Register(name, p); err != nil {
		panic(err)
	}
}

// Get retrieves a provider by name. Returns nil, false if not found.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// All returns a snapshot of all registered providers.
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	return out
}

// Names returns the registered provider names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	return names
}
