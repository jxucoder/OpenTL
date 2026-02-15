# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

TeleCoder is an extensible open-source background coding agent framework for engineering teams. Users send a task and get a PR back. It runs AI coding agents (OpenCode/Codex) inside Docker sandboxes that clone a repo, apply changes, push a branch, and create a GitHub pull request.

TeleCoder is designed as a **pluggable framework**: developers import it as a Go library and compose a custom application by swapping any component via interfaces (store, sandbox, git provider, LLM, pipeline stages, channels).

**Module:** `github.com/jxucoder/TeleCoder`
**Language:** Go 1.25.7 (backend), TypeScript/React 19 (web UI)

## Build & Development Commands

```bash
# Build the CLI binary
make build                    # outputs to ./bin/telecoder

# Run all Go tests
make test                     # equivalent to: go test ./...

# Run a single test
go test ./pipeline/ -run TestPlan

# Lint
make lint                     # equivalent to: golangci-lint run ./...

# Build Docker images
make sandbox-image            # builds telecoder-sandbox from docker/base.Dockerfile
make server-image             # builds telecoder-server from docker/server.Dockerfile

# Docker Compose (requires .env file)
make docker-up                # builds sandbox-image + starts compose
make docker-down              # stops compose

# Clean
make clean

# Web UI (from web/ directory)
cd web && npm install
cd web && npm run dev          # Vite dev server, proxies /api to :7080
cd web && npm run build        # tsc + vite build
```

## Architecture

### Framework Design

TeleCoder is built around 7 core interfaces. Every component is swappable:

1. **`llm.Client`** — LLM provider (Anthropic, OpenAI, or custom)
2. **`store.SessionStore`** — Persistence (SQLite or custom)
3. **`sandbox.Runtime`** — Sandbox lifecycle (Docker or custom)
4. **`gitprovider.Provider`** — Git hosting (GitHub or custom)
5. **`eventbus.Bus`** — Real-time event pub/sub
6. **`pipeline.Stage`** — Orchestration stages (plan, review, decompose, or custom)
7. **`channel.Channel`** — Input/output transport (Slack, Telegram, or custom)

### Builder API

Minimal usage (~10 lines):
```go
app, err := telecoder.NewBuilder().Build()
app.Start(ctx)
```

Custom usage (swap any component):
```go
app, err := telecoder.NewBuilder().
    WithConfig(telecoder.Config{ServerAddr: ":8080"}).
    WithStore(myStore).
    WithGitProvider(myProvider).
    WithSandbox(myRuntime).
    Build()
```

### Request Flow

```
User (CLI/Slack/Telegram/Web) → HTTP API → Engine → Pipeline (Plan→Code→Review) → Docker Sandbox → GitHub PR
```

1. User submits a task with a target repo
2. Engine creates a session (stored via SessionStore)
3. Pipeline optionally generates a plan via LLM, enriches the prompt
4. Engine launches a sandbox container via Runtime
5. Sandbox clones the repo, installs deps, runs the AI agent (OpenCode or Codex)
6. Agent modifies code, sandbox commits and pushes a branch
7. Pipeline optionally reviews the diff; may request revisions
8. Engine creates a GitHub PR and marks the session complete
9. Real-time events streamed to clients via SSE

### Package Layout

```
telecoder.go              # Builder, App, Config — top-level entry point
defaults.go               # Default wiring logic for Build()

model/                    # Foundation: Session, Message, Event types (zero deps)
llm/                      # LLM Client interface
llm/anthropic/            # Anthropic implementation
llm/openai/               # OpenAI implementation
store/                    # SessionStore interface
store/sqlite/             # SQLite implementation
sandbox/                  # Runtime interface + StartOptions
sandbox/docker/           # Docker implementation
gitprovider/              # Provider interface + PROptions, RepoContext
gitprovider/github/       # GitHub implementation (client, indexer, webhook)
eventbus/                 # Bus interface + InMemoryBus
pipeline/                 # Pipeline/Stage interfaces + built-in stages
engine/                   # Session orchestration logic
httpapi/                  # HTTP API handler (chi router, SSE streaming)
channel/                  # Channel interface
channel/slack/            # Slack bot (Socket Mode)
channel/telegram/         # Telegram bot (long polling)

cmd/telecoder/               # Reference CLI implementation (uses Builder)
web/                      # React + Vite + Tailwind web UI
_examples/minimal/        # Minimal framework usage example
```

**Dependency flow:** `model` → `llm/store/sandbox/gitprovider/eventbus` → `pipeline/engine` → `httpapi/channel/*` → `telecoder` → `cmd/telecoder`

### Key Packages

- **`telecoder.go`** — Builder pattern entry point. `NewBuilder().Build()` wires all components.
- **`defaults.go`** — Auto-detects LLM keys, creates default store/bus/sandbox/pipeline.
- **`engine/`** — Session orchestration: CreateAndRunSession, CreateChatSession, SendChatMessage, CreatePRFromChat, sandbox lifecycle, review/revision loops.
- **`httpapi/`** — HTTP API handler using Chi router, delegates all logic to engine.
- **`pipeline/`** — LLM pipeline with Plan/Review/Decompose stages. System prompts are configurable.
- **`store/sqlite/`** — SQLite persistence with WAL mode.
- **`sandbox/docker/`** — Docker container lifecycle. Container naming: `telecoder-{session-id}`.
- **`gitprovider/github/`** — GitHub API: PR creation, repo indexing, webhook parsing.
- **`eventbus/`** — In-memory pub/sub for real-time SSE events.
- **`channel/slack/`** — Slack bot (Socket Mode).
- **`channel/telegram/`** — Telegram bot (long polling).
- **`cmd/telecoder/`** — Reference CLI using Cobra. Commands: `serve`, `run`, `list`, `status`, `logs`, `config`.
- **`web/`** — React + Vite + Tailwind web UI for session monitoring.

### Docker Sandbox

The sandbox image (`docker/base.Dockerfile`) is Ubuntu 24.04 with Node 22, Python 3.12, Go 1.23.4, and pre-installed AI agents (OpenCode, Codex CLI). The entrypoint (`docker/entrypoint.sh`) handles repo cloning, dependency installation, agent selection based on available API keys, and git push. Communication with the server uses marker-based protocols in stdout:

- `###TELECODER_STATUS### message` — status update
- `###TELECODER_ERROR### message` — error
- `###TELECODER_DONE### branch-name` — completion signal

### Session Model

Sessions have two modes: `task` (one-shot execution) and `chat` (persistent sandbox with back-and-forth messaging). Status progression: `pending` → `running` → `complete`/`error`. Chat sessions can go `idle` and are reaped after a configurable timeout.

### Configuration

Required env vars: `GITHUB_TOKEN`, plus `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`. Key optional vars: `TELECODER_ADDR` (default `:7080`), `TELECODER_DATA_DIR` (default `~/.telecoder`), `TELECODER_DOCKER_IMAGE` (default `telecoder-sandbox`), `TELECODER_MAX_REVISIONS` (default `1`). See `.env.example` for full list.

## Testing Patterns

- Tests use real SQLite databases in temp directories (cleaned up via `t.Cleanup`)
- Pipeline tests use fake LLM clients that return canned responses
- Test files: `pipeline/pipeline_test.go`, `store/sqlite/sqlite_test.go`, `eventbus/eventbus_test.go`

## API Endpoints

- `POST /api/sessions` — create session (task or chat mode)
- `GET /api/sessions` — list sessions
- `GET /api/sessions/:id` — get session
- `GET /api/sessions/:id/events` — SSE event stream
- `GET/POST /api/sessions/:id/messages` — chat messages
- `POST /api/sessions/:id/pr` — create PR from chat session
- `POST /api/sessions/:id/stop` — stop session
- `GET /health` — health check
