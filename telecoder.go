// Package telecoder is the top-level entry point for the TeleCoder framework.
//
// Use the Builder to compose a custom TeleCoder application:
//
//	app, err := telecoder.NewBuilder().Build()
//	app.Start(ctx)
//
// Or customize every component:
//
//	app, err := telecoder.NewBuilder().
//	    WithStore(myStore).
//	    WithGitProvider(myProvider).
//	    WithSandbox(myRuntime).
//	    Build()
package telecoder

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jxucoder/TeleCoder/internal/engine"
	"github.com/jxucoder/TeleCoder/internal/httpapi"
	"github.com/jxucoder/TeleCoder/pkg/channel"
	"github.com/jxucoder/TeleCoder/pkg/eventbus"
	"github.com/jxucoder/TeleCoder/pkg/gitprovider"
	ghProvider "github.com/jxucoder/TeleCoder/pkg/gitprovider/github"
	"github.com/jxucoder/TeleCoder/pkg/sandbox"
	dockerSandbox "github.com/jxucoder/TeleCoder/pkg/sandbox/docker"
	"github.com/jxucoder/TeleCoder/pkg/store"
	sqliteStore "github.com/jxucoder/TeleCoder/pkg/store/sqlite"
)

// Config holds top-level configuration for a TeleCoder application.
type Config struct {
	// ServerAddr is the address the HTTP server listens on (default ":7080").
	ServerAddr string

	// DataDir is the directory for persistent data (default "~/.telecoder").
	DataDir string

	// DatabasePath is the full path to the SQLite database file.
	DatabasePath string

	// DockerImage is the base sandbox Docker image name (default "telecoder-sandbox").
	DockerImage string

	// DockerNetwork is the Docker network for sandbox containers (default "telecoder-net").
	DockerNetwork string

	// SandboxEnv holds environment variables to pass into sandbox containers.
	SandboxEnv []string

	// MaxRevisions is the max review-revision rounds (default 1).
	MaxRevisions int

	// ChatIdleTimeout is how long a chat sandbox stays alive without messages (default 30m).
	ChatIdleTimeout time.Duration

	// ChatMaxMessages is the max user messages per chat session (default 50).
	ChatMaxMessages int

	// WebhookSecret is the GitHub webhook HMAC secret.
	WebhookSecret string

	// CodingAgent is the coding agent to run inside the sandbox.
	// Valid values: "opencode", "claude-code", "codex", "auto" (default).
	// "auto" selects based on API keys: ANTHROPIC_API_KEY → OpenCode, OPENAI_API_KEY → Codex.
	CodingAgent string

	// MaxSubTasks is the maximum number of sub-tasks the decompose stage may
	// produce for a single task session (default 5, max 15).
	MaxSubTasks int
}

// Builder constructs a TeleCoder App.
type Builder struct {
	config   Config
	store    store.SessionStore
	bus      eventbus.Bus
	sandbox  sandbox.Runtime
	git      gitprovider.Provider
	channels []channel.Channel
}

// NewBuilder creates a new Builder with sensible defaults.
func NewBuilder() *Builder {
	return &Builder{}
}

// WithConfig sets the application configuration.
func (b *Builder) WithConfig(cfg Config) *Builder {
	b.config = cfg
	return b
}

// WithStore sets the session store implementation.
func (b *Builder) WithStore(s store.SessionStore) *Builder {
	b.store = s
	return b
}

// WithBus sets the event bus implementation.
func (b *Builder) WithBus(bus eventbus.Bus) *Builder {
	b.bus = bus
	return b
}

// WithSandbox sets the sandbox runtime implementation.
func (b *Builder) WithSandbox(s sandbox.Runtime) *Builder {
	b.sandbox = s
	return b
}

// WithGitProvider sets the git hosting provider implementation.
func (b *Builder) WithGitProvider(g gitprovider.Provider) *Builder {
	b.git = g
	return b
}

// WithChannel adds a channel (Slack, Telegram, etc.) to the application.
func (b *Builder) WithChannel(ch channel.Channel) *Builder {
	b.channels = append(b.channels, ch)
	return b
}

// Build creates the App. Missing components are filled with defaults.
func (b *Builder) Build() (*App, error) {
	if err := applyDefaults(b); err != nil {
		return nil, err
	}

	eng := engine.New(
		engine.Config{
			DockerImage:     b.config.DockerImage,
			DockerNetwork:   b.config.DockerNetwork,
			SandboxEnv:      b.config.SandboxEnv,
			MaxRevisions:    b.config.MaxRevisions,
			ChatIdleTimeout: b.config.ChatIdleTimeout,
			ChatMaxMessages: b.config.ChatMaxMessages,
			WebhookSecret:   b.config.WebhookSecret,
			CodingAgent:     b.config.CodingAgent,
			MaxSubTasks:     b.config.MaxSubTasks,
		},
		b.store,
		b.bus,
		b.sandbox,
		b.git,
	)

	handler := httpapi.New(eng)

	return &App{
		config:   b.config,
		engine:   eng,
		handler:  handler,
		channels: b.channels,
	}, nil
}

// App is a running TeleCoder application.
type App struct {
	config   Config
	engine   *engine.Engine
	handler  *httpapi.Handler
	channels []channel.Channel
}

// Engine returns the underlying engine for direct access.
func (a *App) Engine() *engine.Engine { return a.engine }

// Start starts the HTTP server and all channels. Blocks until ctx is done.
func (a *App) Start(ctx context.Context) error {
	a.engine.Start(ctx)

	for _, ch := range a.channels {
		ch := ch
		go func() {
			if err := ch.Run(ctx); err != nil {
				log.Printf("%s channel error: %v", ch.Name(), err)
			}
		}()
	}

	srv := &http.Server{
		Addr:    a.config.ServerAddr,
		Handler: a.handler.Router(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("TeleCoder server listening on %s", a.config.ServerAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	a.engine.Stop()
	return a.engine.Store().Close()
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

func applyDefaults(b *Builder) error {
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
	if b.config.MaxSubTasks == 0 {
		b.config.MaxSubTasks = 5
	}

	if err := os.MkdirAll(b.config.DataDir, 0o755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	if b.store == nil {
		st, err := sqliteStore.New(b.config.DatabasePath)
		if err != nil {
			return fmt.Errorf("initializing store: %w", err)
		}
		b.store = st
	}

	if b.bus == nil {
		b.bus = eventbus.NewInMemoryBus()
	}

	if b.sandbox == nil {
		b.sandbox = dockerSandbox.New()
	}

	if b.git == nil {
		token := os.Getenv("GITHUB_TOKEN")
		if token != "" {
			b.git = ghProvider.New(token)
		}
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
