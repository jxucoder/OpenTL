package telecoder

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jxucoder/TeleCoder/eventbus"
	ghProvider "github.com/jxucoder/TeleCoder/gitprovider/github"
	"github.com/jxucoder/TeleCoder/llm"
	llmAnthropic "github.com/jxucoder/TeleCoder/llm/anthropic"
	llmOpenAI "github.com/jxucoder/TeleCoder/llm/openai"
	"github.com/jxucoder/TeleCoder/pipeline"
	dockerSandbox "github.com/jxucoder/TeleCoder/sandbox/docker"
	sqliteStore "github.com/jxucoder/TeleCoder/store/sqlite"
)

// applyDefaults fills in missing fields on the builder with sensible defaults.
func applyDefaults(b *Builder) error {
	// Config defaults.
	if b.config.ServerAddr == "" {
		b.config.ServerAddr = ":7080"
	}
	if b.config.DataDir == "" {
		b.config.DataDir = defaultDataDir()
	}
	if b.config.DatabasePath == "" {
		b.config.DatabasePath = filepath.Join(b.config.DataDir, "telecoder.db")
	}
	if b.config.DockerImage == "" {
		b.config.DockerImage = "telecoder-sandbox"
	}
	if b.config.DockerNetwork == "" {
		b.config.DockerNetwork = "telecoder-net"
	}
	if b.config.MaxRevisions == 0 {
		b.config.MaxRevisions = 1
	}
	if b.config.ChatIdleTimeout == 0 {
		b.config.ChatIdleTimeout = 30 * time.Minute
	}
	if b.config.ChatMaxMessages == 0 {
		b.config.ChatMaxMessages = 50
	}

	// Ensure data dir exists.
	if err := os.MkdirAll(b.config.DataDir, 0o755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	// Store.
	if b.store == nil {
		st, err := sqliteStore.New(b.config.DatabasePath)
		if err != nil {
			return fmt.Errorf("initializing store: %w", err)
		}
		b.store = st
	}

	// Event bus.
	if b.bus == nil {
		b.bus = eventbus.NewInMemoryBus()
	}

	// Sandbox runtime.
	if b.sandbox == nil {
		b.sandbox = dockerSandbox.New()
	}

	// Git provider.
	if b.git == nil {
		token := os.Getenv("GITHUB_TOKEN")
		if token != "" {
			b.git = ghProvider.New(token)
		}
	}

	// LLM + pipeline stages.
	if b.llm == nil {
		b.llm = llmClientFromEnv()
	}

	if b.llm != nil {
		if b.plan == nil {
			b.plan = pipeline.NewPlanStage(b.llm, "")
		}
		if b.review == nil {
			b.review = pipeline.NewReviewStage(b.llm, "")
		}
		if b.decompose == nil {
			b.decompose = pipeline.NewDecomposeStage(b.llm, "")
		}
	}

	return nil
}

// llmClientFromEnv creates an LLM client from environment variables.
// Returns nil if no API key is found.
func llmClientFromEnv() llm.Client {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return llmAnthropic.New(key, os.Getenv("TELECODER_PLANNER_MODEL"))
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return llmOpenAI.New(key, os.Getenv("TELECODER_PLANNER_MODEL"))
	}
	return nil
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".telecoder"
	}
	return filepath.Join(home, ".telecoder")
}
