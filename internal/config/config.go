package config

import (
	"os"
	"path/filepath"
)

// Config holds TeleCoder configuration.
type Config struct {
	// DataDir is where TeleCoder stores sessions, logs, and workspaces.
	DataDir string

	// AgentCommand is the command to launch the coding agent in ACP mode.
	// Default: "claude --acp"
	AgentCommand string

	// ListenAddr is the HTTP server listen address. Default: ":7080"
	ListenAddr string

	// VerifyCommand is an optional command to run after the agent finishes (e.g. "npm test").
	VerifyCommand string

	// LintCommand is an optional lint command to run after the agent finishes.
	LintCommand string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	c := &Config{
		DataDir:      envOr("TELECODER_DATA_DIR", defaultDataDir()),
		AgentCommand: envOr("TELECODER_AGENT_COMMAND", "claude --acp"),
		ListenAddr:   envOr("TELECODER_LISTEN_ADDR", ":7080"),
		VerifyCommand: os.Getenv("TELECODER_VERIFY_COMMAND"),
		LintCommand:   os.Getenv("TELECODER_LINT_COMMAND"),
	}
	return c
}

// WorkspacesDir returns the directory where session workspaces are stored.
func (c *Config) WorkspacesDir() string {
	return filepath.Join(c.DataDir, "workspaces")
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/var/lib/telecoder"
	}
	return filepath.Join(home, ".telecoder")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
