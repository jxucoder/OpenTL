# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

TeleCoder is an extensible open-source background coding agent framework for engineering teams. Users send a task and get a PR back. It runs AI coding agents (OpenCode, Claude Code, Codex) inside Docker sandboxes that clone a repo, apply changes, push a branch, and create a GitHub pull request.

TeleCoder is designed as a **pluggable framework**: developers import it as a Go library and compose a custom application by swapping any component via interfaces (store, sandbox, git provider, LLM, pipeline stages, channels).

**Module:** `github.com/jxucoder/TeleCoder`
**Language:** Go 1.25.7 (backend), TypeScript/React 19 (web UI)

## Build & Development Commands

```bash
# Build the CLI binary
make build                    # outputs to ./bin/telecoder

# Install to $GOPATH/bin
make install

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
3. **`sandbox.Runtime`** — Sandbox lifecycle (Docker, SSH remote, or custom)
4. **`gitprovider.Provider`** — Git hosting (GitHub or custom)
5. **`eventbus.Bus`** — Real-time event pub/sub
6. **`pipeline.Stage`** — Orchestration stages (plan, review, decompose, verify, or custom)
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
    WithVerifyStage(myVerifier).
    Build()
```

### Request Flow

```
User (CLI/Slack/Telegram/Web) → HTTP API → Engine → Pipeline (Decompose→Plan→Code→Verify→Review) → Sandbox → GitHub PR
```

1. User submits a task with a target repo
2. Engine creates a session (stored via SessionStore)
3. Pipeline decomposes task into sub-tasks (single or multi-step)
4. For each sub-task: pipeline generates a plan via LLM, enriches the prompt
5. Engine launches a sandbox container via Runtime
6. Sandbox clones the repo, installs deps, runs the AI agent (OpenCode or Codex)
7. Agent modifies code, sandbox commits and pushes a branch
8. Pipeline runs tests/linting (verify stage); failures trigger revisions
9. Pipeline reviews the diff; may request revisions (up to MaxRevisions rounds)
10. Engine creates a GitHub PR and marks the session complete
11. Real-time events streamed to clients via SSE

### Package Layout

```
telecoder.go              # Builder, App, Config — top-level entry point
defaults.go               # Default wiring logic for Build()

model/                    # Foundation: Session, Message, Event types (zero deps)
llm/                      # LLM Client interface
llm/anthropic/            # Anthropic implementation (default: claude-sonnet-4-20250514)
llm/openai/               # OpenAI implementation (default: gpt-4o)
store/                    # SessionStore interface
store/sqlite/             # SQLite implementation (WAL mode)
sandbox/                  # Runtime interface + StartOptions + LineScanner
sandbox/docker/           # Docker implementation (local daemon via CLI)
sandbox/ssh/              # SSH remote implementation (runs Docker on a VPS via SSH)
sandbox/pool.go           # Pre-warming pool (wraps any Runtime, maintains warm containers)
gitprovider/              # Provider interface + PROptions, RepoContext, WebhookEvent
gitprovider/github/       # GitHub implementation (client, indexer, webhook)
eventbus/                 # Bus interface + InMemoryBus
pipeline/                 # Pipeline/Stage interfaces + built-in stages + prompts
engine/                   # Session orchestration logic
httpapi/                  # HTTP API handler (chi router, SSE streaming)
channel/                  # Channel interface
channel/slack/            # Slack bot (Socket Mode)
channel/telegram/         # Telegram bot (long polling)

cmd/telecoder/            # Reference CLI (Cobra): serve, run, list, status, logs, config
web/                      # React + Vite + Tailwind web UI
docs/                     # Documentation (getting-started, deploy, slack/telegram setup, reference, user-stories)
_examples/minimal/        # Minimal framework usage example
```

**Dependency flow:** `model` → `llm/store/sandbox/gitprovider/eventbus` → `pipeline/engine` → `httpapi/channel/*` → `telecoder` → `cmd/telecoder`

### Key Packages

- **`telecoder.go`** — Builder pattern entry point. `NewBuilder().Build()` wires all components. Config struct holds ServerAddr, DataDir, DatabasePath, DockerImage, DockerNetwork, SandboxEnv, MaxRevisions, ChatIdleTimeout, ChatMaxMessages, WebhookSecret, Agent.
- **`defaults.go`** — Auto-detects LLM keys (prioritizes Anthropic over OpenAI), creates default store/bus/sandbox/pipeline stages including verify.
- **`engine/`** — Session orchestration: CreateAndRunSession, CreateAndRunSessionWithAgent, CreateChatSession, SendChatMessage, CreatePRFromChat, CreatePRCommentSession, sandbox lifecycle, decompose→plan→code→verify→review loops with revision rounds. `resolveAgentName()` resolves per-session agent overrides.
- **`httpapi/`** — HTTP API handler using Chi router, delegates all logic to engine. Includes GitHub webhook handler.
- **`pipeline/`** — LLM pipeline stages:
  - **PlanStage** — Generates structured plan from task + codebase context
  - **ReviewStage** — Reviews diff against plan (called directly by engine, not via pipeline.Run)
  - **DecomposeStage** — Breaks task into ordered sub-tasks (single for simple, 2-5 for complex)
  - **VerifyStage** (`verify.go`) — Runs tests/linting and analyzes output via LLM. `DetectVerifyCommands()` auto-detects test/lint commands for Go, Node, Python, Rust, and Makefile projects.
  - System prompts defined in `prompts.go` (DefaultPlannerPrompt, DefaultReviewerPrompt, DefaultDecomposerPrompt, DefaultVerifyPrompt)
  - Utility functions: `EnrichPrompt()`, `RevisePrompt()`, `parseSubTasks()`
- **`store/sqlite/`** — SQLite persistence with WAL mode. Tables: sessions, session_events, messages.
- **`sandbox/docker/`** — Docker container lifecycle via CLI. Container naming: `telecoder-{session-id}`.
- **`sandbox/ssh/`** — Remote sandbox runtime. Runs Docker commands on a VPS via SSH for cloud deployments.
- **`sandbox/pool.go`** — Pre-warming pool. Wraps any Runtime and maintains N warm containers (default 2) for near-instant startup. Refills periodically (default 10s). Reconfigures warm containers with session-specific env before claiming.
- **`gitprovider/github/`** — GitHub API: PR creation, repo indexing (file tree, language stats, key files), webhook parsing for PR comment events, reply to PR comments.
- **`eventbus/`** — In-memory pub/sub for real-time SSE events. Non-blocking publish.
- **`channel/slack/`** — Slack bot (Socket Mode). Listens for DMs and slash commands.
- **`channel/telegram/`** — Telegram bot (long polling). Commands: /start, /chat, /run, /status, /pr. Supports multi-turn chat sessions.
- **`cmd/telecoder/`** — Reference CLI using Cobra. Commands: `serve`, `run`, `list`, `status`, `logs` (with `--follow`), `config` (subcommands: `setup`, `set`, `show`, `path`).
- **`web/`** — React 19 + Vite 6 + Tailwind CSS 3.4 web UI for session monitoring. SSE event streaming support.

### Docker Sandbox

The sandbox image (`docker/base.Dockerfile`) is Ubuntu 24.04 with Node 22, Python 3.12, Go 1.23.4, and pre-installed AI agents (OpenCode, Claude Code, Codex CLI). The entrypoint (`docker/entrypoint.sh`) handles:

1. Validates environment (TELECODER_REPO, TELECODER_PROMPT, TELECODER_BRANCH, GITHUB_TOKEN)
2. Clones repo with `--depth=1`
3. Configures git identity
4. Creates feature branch
5. Auto-detects and installs dependencies (npm/pnpm/yarn/pip/go)
6. **Agent selection:** `TELECODER_AGENT` selects the agent (`opencode`, `claude-code`, `codex`). `auto` (default) falls back to API-key-based detection: ANTHROPIC_API_KEY → OpenCode, OPENAI_API_KEY → Codex CLI. Optional `TELECODER_AGENT_MODEL` overrides the model.
7. Commits, pushes, and signals completion

Communication with the server uses marker-based protocols in stdout:

- `###TELECODER_STATUS### message` — status update
- `###TELECODER_ERROR### message` — error
- `###TELECODER_DONE### branch-name` — completion signal

Chat-mode sandboxes use `docker/setup.sh` for environment preparation instead of the full entrypoint.

### Session Model

Sessions have two modes:

- **`task`** (fire-and-forget): `pending` → `running` → `complete`/`error`. Single prompt execution, creates PR automatically.
- **`chat`** (persistent sandbox): `pending` → `idle` ↔ `running` → `complete`/`error`. Multiple messages in a single session, sandbox stays alive between messages. User explicitly creates PR when ready. Idle timeout reaper stops inactive sessions.

### Configuration

Required env vars: `GITHUB_TOKEN`, plus `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`.

Key optional vars:
- `TELECODER_ADDR` — Server listen address (default `:7080`)
- `TELECODER_DATA_DIR` — Data directory (default `~/.telecoder`)
- `TELECODER_DOCKER_IMAGE` — Sandbox image name (default `telecoder-sandbox`)
- `TELECODER_DOCKER_NETWORK` — Docker network (default `telecoder-net`)
- `TELECODER_MAX_REVISIONS` — Review/revision rounds (default `1`)
- `TELECODER_CHAT_IDLE_TIMEOUT` — Chat inactivity timeout (default `30m`)
- `TELECODER_CHAT_MAX_MESSAGES` — Max user messages per chat (default `50`)
- `TELECODER_PLANNER_MODEL` — Override LLM model for planning stages
- `TELECODER_AGENT` — Coding agent: `opencode`, `claude-code`, `codex`, `auto` (default)
- `TELECODER_AGENT_MODEL` — Override agent model inside sandbox
- `GITHUB_WEBHOOK_SECRET` — HMAC secret for webhook verification
- `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN` — Slack integration
- `SLACK_DEFAULT_REPO` — Default repo for Slack commands
- `TELEGRAM_BOT_TOKEN` — Telegram bot token
- `TELEGRAM_DEFAULT_REPO` — Default repo for Telegram commands

Config file: `~/.telecoder/config.env` (loaded by `serve` command).

## Testing Patterns

- Tests use real SQLite databases in temp directories (cleaned up via `t.Cleanup`)
- Pipeline tests use fake LLM clients (`fakeLLM`) that return canned responses
- Sandbox pool tests use a mock Runtime to verify pre-warming, claiming, and refilling behavior
- Engine tests use stubs for sandbox, git, and LLM to verify session lifecycle, agent selection, and event dispatch
- HTTP API tests use stub implementations for engine dependencies to test request validation, routing, and response formatting
- E2E tests wire up a real HTTP router, SQLite store, event bus, and simulated sandbox/git provider to test full session lifecycle
- Test files: `e2e_test.go`, `pipeline/pipeline_test.go`, `store/sqlite/sqlite_test.go`, `eventbus/eventbus_test.go`, `sandbox/pool_test.go`, `engine/engine_test.go`, `httpapi/httpapi_test.go`, `model/model_test.go`

## API Endpoints

- `POST /api/sessions` — create session (task or chat mode, optional `agent` field for per-session override)
- `GET /api/sessions` — list sessions
- `GET /api/sessions/{id}` — get session
- `GET /api/sessions/{id}/events` — SSE event stream
- `GET /api/sessions/{id}/messages` — get chat messages
- `POST /api/sessions/{id}/messages` — send chat message
- `POST /api/sessions/{id}/pr` — create PR from chat session
- `POST /api/sessions/{id}/stop` — stop session
- `POST /api/webhooks/github` — GitHub webhook handler (PR comment events)
- `GET /health` — health check
