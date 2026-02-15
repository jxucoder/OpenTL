package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	telecoder "github.com/jxucoder/TeleCoder"
	channelSlack "github.com/jxucoder/TeleCoder/channel/slack"
	channelTelegram "github.com/jxucoder/TeleCoder/channel/telegram"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the TeleCoder server",
	Long:  "Start the TeleCoder API server that manages sandbox sessions and creates PRs.",
	RunE:  runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	// Load config file into environment (non-destructive).
	loadConfigFileIntoEnv()

	// Validate required env vars.
	if os.Getenv("GITHUB_TOKEN") == "" {
		return fmt.Errorf("GITHUB_TOKEN is required")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
		return fmt.Errorf("at least one of ANTHROPIC_API_KEY or OPENAI_API_KEY is required")
	}

	// Build sandbox env vars.
	var sandboxEnv []string
	sandboxEnv = append(sandboxEnv, "GITHUB_TOKEN="+os.Getenv("GITHUB_TOKEN"))
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		sandboxEnv = append(sandboxEnv, "ANTHROPIC_API_KEY="+v)
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		sandboxEnv = append(sandboxEnv, "OPENAI_API_KEY="+v)
	}

	cfg := telecoder.Config{
		ServerAddr:      envOrDefault("TELECODER_ADDR", ":7080"),
		DataDir:         envOrDefault("TELECODER_DATA_DIR", ""),
		DockerImage:     envOrDefault("TELECODER_DOCKER_IMAGE", "telecoder-sandbox"),
		DockerNetwork:   envOrDefault("TELECODER_DOCKER_NETWORK", "telecoder-net"),
		SandboxEnv:      sandboxEnv,
		MaxRevisions:    envOrIntDefault("TELECODER_MAX_REVISIONS", 1),
		ChatIdleTimeout: envOrDurationDefault("TELECODER_CHAT_IDLE_TIMEOUT", 30*time.Minute),
		ChatMaxMessages: envOrIntDefault("TELECODER_CHAT_MAX_MESSAGES", 50),
		WebhookSecret:   os.Getenv("GITHUB_WEBHOOK_SECRET"),
	}

	builder := telecoder.NewBuilder().WithConfig(cfg)

	// Build the app first, then add channels that need the engine.
	app, err := builder.Build()
	if err != nil {
		return fmt.Errorf("building app: %w", err)
	}

	// Add Slack channel if configured.
	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")
	slackAppToken := os.Getenv("SLACK_APP_TOKEN")
	if slackBotToken != "" && slackAppToken != "" {
		slackBot := channelSlack.NewBot(
			slackBotToken,
			slackAppToken,
			os.Getenv("SLACK_DEFAULT_REPO"),
			app.Engine().Store(),
			app.Engine().Bus(),
			app.Engine(),
		)
		builder.WithChannel(slackBot)
		fmt.Println("Slack bot enabled (Socket Mode)")
	}

	// Add Telegram channel if configured.
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if telegramToken != "" {
		tgBot, err := channelTelegram.NewBot(
			telegramToken,
			os.Getenv("TELEGRAM_DEFAULT_REPO"),
			app.Engine().Store(),
			app.Engine().Bus(),
			app.Engine(),
		)
		if err != nil {
			fmt.Printf("Warning: failed to initialize Telegram bot: %v\n", err)
		} else {
			builder.WithChannel(tgBot)
			fmt.Println("Telegram bot enabled (long polling)")
		}
	}

	// Rebuild with channels added.
	app, err = builder.Build()
	if err != nil {
		return fmt.Errorf("rebuilding app with channels: %w", err)
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nShutting down...")
		cancel()
	}()

	return app.Start(ctx)
}

// loadConfigFileIntoEnv reads ~/.telecoder/config.env and sets any values not
// already present in the environment.
func loadConfigFileIntoEnv() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".telecoder", "config.env")
	f, err := os.Open(path)
	if err != nil {
		return
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
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrIntDefault(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envOrDurationDefault(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
