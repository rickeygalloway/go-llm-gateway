// Command operator is the Kubernetes controller for the Go-LLM-Gateway CRDs.
// It watches LLMBackend and LLMRoute custom resources and reconciles gateway
// configuration accordingly.
//
// This is a stub for Step 1. The full controller-runtime implementation
// (LLMBackend + LLMRoute reconcilers) will be added in a later step.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "operator: not yet implemented — coming in step 6 (K8s operator)")
	os.Exit(1)
}
