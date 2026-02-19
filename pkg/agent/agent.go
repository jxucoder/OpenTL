// Package agent defines the pluggable coding engine interface for TeleCoder.
// Each implementation wraps a headless coding agent that runs inside a Docker sandbox.
package agent

import (
	"fmt"

	"github.com/jxucoder/TeleCoder/pkg/model"
)

// CodingAgent is the interface for a pluggable coding engine.
// Implementations wrap headless coding agents (Pi, Claude Code, OpenCode, Codex)
// that run inside Docker sandbox containers.
type CodingAgent interface {
	// Name returns the agent identifier (e.g. "pi", "claude-code", "opencode", "codex").
	Name() string

	// Command returns the shell command to execute the agent with the given prompt.
	// The command is run inside /workspace/repo in the sandbox container.
	Command(prompt string) string

	// ParseEvent parses a single line of agent stdout into a TeleCoder event.
	// Returns nil if the line is not a recognized event (treat as regular output).
	ParseEvent(line string) *model.Event
}

// Registry holds named CodingAgent implementations.
var registry = map[string]CodingAgent{}

// Register adds a CodingAgent to the global registry.
func Register(a CodingAgent) {
	registry[a.Name()] = a
}

// Get returns a CodingAgent by name, or an error if not found.
func Get(name string) (CodingAgent, error) {
	if a, ok := registry[name]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("unknown coding agent: %q", name)
}

// Default returns the default CodingAgent (OpenCode).
func Default() CodingAgent {
	return registry["opencode"]
}

// Resolve returns the CodingAgent for the given name.
// Empty string or "auto" returns the default agent.
func Resolve(name string) CodingAgent {
	if name == "" || name == "auto" {
		return Default()
	}
	if a, ok := registry[name]; ok {
		return a
	}
	return Default()
}

// Names returns all registered agent names.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

func init() {
	Register(&OpenCode{})
	Register(&ClaudeCode{})
	Register(&Codex{})
	Register(&Pi{})
}
