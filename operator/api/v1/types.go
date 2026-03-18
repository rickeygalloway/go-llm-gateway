// Package v1 defines the custom resource types for the Go-LLM-Gateway operator.
// These CRDs allow platform teams to manage gateway configuration declaratively via kubectl.
//
// Future implementation will use controller-runtime to watch these resources
// and dynamically update the gateway's provider registry and routing rules.
//
// Install CRDs: kubectl apply -f deploy/k8s/crds/
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LLMBackend represents a backend LLM provider (Ollama, OpenAI, etc.)
// that the gateway should route traffic to.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
type LLMBackend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LLMBackendSpec   `json:"spec,omitempty"`
	Status LLMBackendStatus `json:"status,omitempty"`
}

// LLMBackendSpec defines the desired state of an LLMBackend.
type LLMBackendSpec struct {
	// Type is the provider type: ollama | openai | anthropic | vllm
	// +kubebuilder:validation:Enum=ollama;openai;anthropic;vllm
	Type string `json:"type"`

	// BaseURL is the backend endpoint URL.
	BaseURL string `json:"baseURL,omitempty"`

	// APIKeySecretRef references a K8s Secret containing the API key.
	// +optional
	APIKeySecretRef *SecretKeyRef `json:"apiKeySecretRef,omitempty"`

	// Models is the list of model IDs served by this backend.
	// +optional
	Models []string `json:"models,omitempty"`

	// Priority defines the fallback order (lower = higher priority).
	// +kubebuilder:default=10
	Priority int `json:"priority,omitempty"`

	// Timeout is the per-request timeout.
	// +kubebuilder:default="60s"
	Timeout string `json:"timeout,omitempty"`
}

// SecretKeyRef references a key in a Kubernetes Secret.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// LLMBackendStatus reflects the observed state of the backend.
type LLMBackendStatus struct {
	// Conditions reports the readiness of the backend.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// AvailableModels is the last-known list of models from this backend.
	AvailableModels []string `json:"availableModels,omitempty"`

	// LastHealthCheckAt is when the operator last checked this backend.
	LastHealthCheckAt *metav1.Time `json:"lastHealthCheckAt,omitempty"`
}

// LLMBackendList contains a list of LLMBackend.
// +kubebuilder:object:root=true
type LLMBackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LLMBackend `json:"items"`
}

// LLMRoute defines routing rules: which models → which backends → in what order.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type LLMRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LLMRouteSpec   `json:"spec,omitempty"`
	Status LLMRouteStatus `json:"status,omitempty"`
}

// LLMRouteSpec defines the desired routing rules.
type LLMRouteSpec struct {
	// Rules maps model patterns to ordered backend lists.
	Rules []RouteRule `json:"rules"`
}

// RouteRule maps a model pattern to a fallback chain of backends.
type RouteRule struct {
	// Model is the model ID to match. Use "*" for a catch-all.
	Model string `json:"model"`

	// Backends is an ordered list of LLMBackend names to try.
	// The gateway tries them in order, falling back on retryable errors.
	Backends []string `json:"backends"`
}

// LLMRouteStatus reflects the observed state of the route.
type LLMRouteStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// LLMRouteList contains a list of LLMRoute.
// +kubebuilder:object:root=true
type LLMRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LLMRoute `json:"items"`
}
