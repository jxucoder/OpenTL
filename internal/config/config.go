// Package config provides configuration management for OpenTL.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the OpenTL server.
type Config struct {
	// ServerAddr is the address the HTTP server listens on (e.g., ":7080").
	ServerAddr string

	// DataDir is the directory for persistent data (SQLite DB, etc.).
	DataDir string

	// DatabasePath is the full path to the SQLite database file.
	DatabasePath string

	// GitHubToken is the personal access token for GitHub API operations.
	GitHubToken string

	// LLM provider API keys (passed to sandbox as env vars).
	AnthropicAPIKey string
	OpenAIAPIKey    string

	// DockerImage is the base sandbox Docker image name.
	DockerImage string

	// DockerNetwork is the Docker network for sandbox containers.
	DockerNetwork string

	// Slack integration (optional -- Socket Mode).
	// SlackBotToken is the Bot User OAuth Token (xoxb-...).
	SlackBotToken string
	// SlackAppToken is the App-Level Token (xapp-...) required for Socket Mode.
	SlackAppToken string
	// SlackDefaultRepo is the fallback repository when --repo is not specified.
	SlackDefaultRepo string

	// Telegram integration (optional -- long polling, no public URL needed).
	// TelegramBotToken is the token from @BotFather.
	TelegramBotToken string
	// TelegramDefaultRepo is the fallback repository when --repo is not specified.
	TelegramDefaultRepo string

	// MaxRevisions is the maximum number of review-revision rounds before
	// proceeding to PR creation. 0 means no revisions (review only). Default: 1.
	MaxRevisions int

	// ChatIdleTimeout is how long a chat sandbox stays alive without messages
	// before being automatically stopped. Default: 30 minutes.
	ChatIdleTimeout time.Duration

	// ChatMaxMessages is the maximum number of user messages per chat session.
	// Default: 50.
	ChatMaxMessages int
}

// Load creates a Config from the config file and environment variables.
// Values are resolved in order: environment variable > config file > default.
func Load() (*Config, error) {
	// Load config file (~/.opentl/config.env) into the environment.
	// Existing env vars take precedence (loadConfigFile only sets unset vars).
	loadConfigFile()

	dataDir := envOr("OPENTL_DATA_DIR", defaultDataDir())
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	cfg := &Config{
		ServerAddr:      envOr("OPENTL_ADDR", ":7080"),
		DataDir:         dataDir,
		DatabasePath:    filepath.Join(dataDir, "opentl.db"),
		GitHubToken:     os.Getenv("GITHUB_TOKEN"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		DockerImage:      envOr("OPENTL_DOCKER_IMAGE", "opentl-sandbox"),
		DockerNetwork:    envOr("OPENTL_DOCKER_NETWORK", "opentl-net"),
		SlackBotToken:       os.Getenv("SLACK_BOT_TOKEN"),
		SlackAppToken:       os.Getenv("SLACK_APP_TOKEN"),
		SlackDefaultRepo:    os.Getenv("SLACK_DEFAULT_REPO"),
		TelegramBotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramDefaultRepo: os.Getenv("TELEGRAM_DEFAULT_REPO"),
		MaxRevisions:        envOrInt("OPENTL_MAX_REVISIONS", 1),
		ChatIdleTimeout:     envOrDuration("OPENTL_CHAT_IDLE_TIMEOUT", 30*time.Minute),
		ChatMaxMessages:     envOrInt("OPENTL_CHAT_MAX_MESSAGES", 50),
	}

	return cfg, nil
}

// loadConfigFile reads ~/.opentl/config.env and sets any values that are not
// already present in the environment. This ensures env vars always win.
func loadConfigFile() {
	path := filepath.Join(defaultDataDir(), "config.env")
	f, err := os.Open(path)
	if err != nil {
		return // file doesn't exist or can't be read — that's fine
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		// Only set if not already in the environment.
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}

// Validate checks that required configuration is present.
func (c *Config) Validate() error {
	if c.GitHubToken == "" {
		return fmt.Errorf("GITHUB_TOKEN is required")
	}
	if c.AnthropicAPIKey == "" && c.OpenAIAPIKey == "" {
		return fmt.Errorf("at least one of ANTHROPIC_API_KEY or OPENAI_API_KEY is required")
	}
	return nil
}

// SlackEnabled returns true if Slack Socket Mode is configured.
func (c *Config) SlackEnabled() bool {
	return c.SlackBotToken != "" && c.SlackAppToken != ""
}

// TelegramEnabled returns true if the Telegram bot is configured.
func (c *Config) TelegramEnabled() bool {
	return c.TelegramBotToken != ""
}

// SandboxEnv returns environment variables to pass to sandbox containers.
func (c *Config) SandboxEnv() []string {
	env := []string{
		"GITHUB_TOKEN=" + c.GitHubToken,
	}
	if c.AnthropicAPIKey != "" {
		env = append(env, "ANTHROPIC_API_KEY="+c.AnthropicAPIKey)
	}
	if c.OpenAIAPIKey != "" {
		env = append(env, "OPENAI_API_KEY="+c.OpenAIAPIKey)
	}
	return env
}

func envOrDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".opentl"
	}
	return filepath.Join(home, ".opentl")
}
