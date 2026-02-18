// Package llm defines the LLM client interface for TeleCoder.
package llm

import "context"

// Client is a minimal interface for making LLM API calls.
// Implementations provide the actual HTTP transport to a specific provider.
type Client interface {
	Complete(ctx context.Context, system, user string) (string, error)
}
