# TeleCoder Reference

This page collects technical details that were intentionally kept out of the main `README.md`.

## Builder Usage

TeleCoder is designed as a **pluggable Go framework**. Import it as a library, swap any component via interfaces, and compose a custom application in ~15 lines.

### Minimal Usage

```go
package main

import (
    "context"
    telecoder "github.com/jxucoder/TeleCoder"
)

func main() {
    app, err := telecoder.NewBuilder().Build()
    if err != nil {
        panic(err)
    }
    app.Start(context.Background())
}
```

### Custom Usage

Swap out any component: store, sandbox, git provider, LLM, pipeline stages, channels.

```go
app, err := telecoder.NewBuilder().
    WithConfig(telecoder.Config{ServerAddr: ":8080", MaxRevisions: 2}).
    WithStore(myPostgresStore).
    WithGitProvider(myGitLabProvider).
    WithSandbox(myK8sRuntime).
    WithLLM(myLLMClient).
    WithChannel(myDiscordBot).
    Build()
```

## Core Interfaces

| Interface | Purpose | Built-in |
|:----------|:--------|:---------|
| `llm.Client` | LLM provider | Anthropic, OpenAI |
| `store.SessionStore` | Persistence | SQLite |
| `sandbox.Runtime` | Sandbox lifecycle | Docker, SSH (remote VPS) |
| `gitprovider.Provider` | Git hosting | GitHub |
| `eventbus.Bus` | Real-time event pub/sub | In-memory |
| `pipeline.Stage` | Orchestration stages | Plan, Review, Decompose, Verify |
| `channel.Channel` | Input/output transport | Slack, Telegram, Linear, Jira |

## Architecture

TeleCoder is a single binary: `telecoder serve` runs the server, and `telecoder run` talks to it. Every component is swappable via interfaces.

| Component | Package | Description |
|:----------|:--------|:------------|
| **Builder** | `telecoder` | Entry point. Composes all components via `NewBuilder().Build()`. |
| **Engine** | `engine/` | Session orchestration: creates sessions, manages sandbox lifecycle, runs review/revision loops. |
| **Pipeline** | `pipeline/` | Plan -> code -> review pipeline with optional task decomposition and configurable stages. |
| **HTTP API** | `httpapi/` | Chi router. REST API + SSE streaming. Delegates logic to engine. |
| **Store** | `store/sqlite/` | SQLite persistence (WAL mode) for sessions, messages, and events. |
| **Sandbox** | `sandbox/docker/` | One Docker container per task (or persistent container for chat mode). |
| **Sandbox (SSH)** | `sandbox/ssh/` | Remote sandbox via SSH -- run Docker on any VPS or cloud host. |
| **Sandbox Pool** | `sandbox/` | Pre-warming pool that wraps any Runtime for near-instant startup. |
| **Git Provider** | `gitprovider/github/` | GitHub API: PR creation, repo indexing, webhook handling. |
| **Event Bus** | `eventbus/` | In-memory pub/sub for real-time SSE events. |
| **Channels** | `channel/slack/`, `channel/telegram/` | Bot integrations: send tasks from chat, get PR links back. |
| **Channels** | `channel/linear/`, `channel/jira/` | Webhook integrations: label an issue, get a PR back. |
| **CLI** | `cmd/telecoder/` | Reference implementation. Creates sessions, streams logs, checks status. |
| **Web UI** | `web/` | React + Vite dashboard for monitoring sessions. |

## API Endpoints

| Method | Endpoint | Description |
|:-------|:---------|:------------|
| `POST` | `/api/sessions` | Create a session (`mode=task` or `mode=chat`, optional `agent` field) |
| `GET` | `/api/sessions` | List sessions |
| `GET` | `/api/sessions/:id` | Get session details |
| `GET` | `/api/sessions/:id/events` | SSE stream of events |
| `GET` | `/api/sessions/:id/messages` | List chat messages |
| `POST` | `/api/sessions/:id/messages` | Send chat message |
| `POST` | `/api/sessions/:id/pr` | Create PR from chat session |
| `POST` | `/api/sessions/:id/stop` | Stop a session |
| `POST` | `/api/webhooks/github` | GitHub webhook handler (PR comment events) |
| `GET` | `/health` | Health check |

### Webhook Endpoints (Channel-Specific Servers)

These run on their own ports, separate from the main API server:

| Method | Endpoint | Port | Description |
|:-------|:---------|:-----|:------------|
| `POST` | `/api/webhooks/linear` | `:7090` | Linear webhook handler (issue label events) |
| `POST` | `/api/webhooks/jira` | `:7091` | Jira webhook handler (issue label events) |

## Configuration

All configuration is via environment variables.

### Required

| Variable | Description |
|:---------|:------------|
| `GITHUB_TOKEN` | GitHub personal access token with `repo` scope |
| `ANTHROPIC_API_KEY` | Anthropic API key (at least one LLM key required) |
| `OPENAI_API_KEY` | OpenAI API key (at least one LLM key required) |

### Server

| Variable | Default | Description |
|:---------|:--------|:------------|
| `TELECODER_ADDR` | `:7080` | Server listen address |
| `TELECODER_DATA_DIR` | `~/.telecoder` | Data directory for SQLite DB |
| `TELECODER_DOCKER_IMAGE` | `telecoder-sandbox` | Sandbox Docker image |
| `TELECODER_DOCKER_NETWORK` | `telecoder-net` | Docker network name |
| `TELECODER_MAX_REVISIONS` | `1` | Max review/revision rounds per sub-task |
| `TELECODER_CHAT_IDLE_TIMEOUT` | `30m` | Idle timeout for persistent chat sandboxes |
| `TELECODER_CHAT_MAX_MESSAGES` | `50` | Max user messages per chat session |
| `TELECODER_CODING_AGENT` | `auto` | Coding agent: `opencode`, `claude-code`, `codex`, or `auto` |
| `TELECODER_CODING_AGENT_MODEL` | — | Override the model used by the coding agent |
| `TELECODER_LLM_MODEL` | — | Override LLM model for pipeline stages (auto-detects from API key) |
| `TELECODER_SERVER` | `http://localhost:7080` | Server URL (for CLI) |

### Slack

| Variable | Default | Description |
|:---------|:--------|:------------|
| `SLACK_BOT_TOKEN` | — | Slack bot token (`xoxb-...`) |
| `SLACK_APP_TOKEN` | — | Slack app-level token (`xapp-...`) |
| `SLACK_DEFAULT_REPO` | — | Default repo for Slack commands |

### Telegram

| Variable | Default | Description |
|:---------|:--------|:------------|
| `TELEGRAM_BOT_TOKEN` | — | Telegram bot token |
| `TELEGRAM_DEFAULT_REPO` | — | Default repo for Telegram commands |

### Linear

| Variable | Default | Description |
|:---------|:--------|:------------|
| `LINEAR_API_KEY` | — | Linear API key (enables Linear channel) |
| `LINEAR_WEBHOOK_SECRET` | — | HMAC secret for webhook verification |
| `LINEAR_TRIGGER_LABEL` | `telecoder` | Label that triggers TeleCoder |
| `LINEAR_DEFAULT_REPO` | — | Default repo for Linear issues |
| `LINEAR_WEBHOOK_ADDR` | `:7090` | Listen address for Linear webhook server |

### Jira

| Variable | Default | Description |
|:---------|:--------|:------------|
| `JIRA_BASE_URL` | — | Jira instance URL (e.g. `https://yourcompany.atlassian.net`) |
| `JIRA_USER_EMAIL` | — | Jira user email for API auth |
| `JIRA_API_TOKEN` | — | Jira API token (all three JIRA vars required to enable) |
| `JIRA_WEBHOOK_SECRET` | — | HMAC secret for webhook verification |
| `JIRA_TRIGGER_LABEL` | `telecoder` | Label that triggers TeleCoder |
| `JIRA_DEFAULT_REPO` | — | Default repo for Jira issues |
| `JIRA_WEBHOOK_ADDR` | `:7091` | Listen address for Jira webhook server |

### GitHub Webhooks

| Variable | Default | Description |
|:---------|:--------|:------------|
| `GITHUB_WEBHOOK_SECRET` | — | HMAC secret for GitHub webhook verification |

## Project Structure

```text
TeleCoder/
|-- telecoder.go              Builder, App, Config - top-level framework entry point
|-- defaults.go               Default wiring logic for Build()
|-- model/                    Foundation types (Session, Message, Event)
|-- llm/                      LLM Client interface
|   |-- anthropic/            Anthropic implementation
|   `-- openai/               OpenAI implementation
|-- store/                    SessionStore interface
|   `-- sqlite/               SQLite implementation
|-- sandbox/                  Runtime interface + pre-warming Pool
|   |-- docker/               Docker implementation
|   `-- ssh/                  SSH remote implementation
|-- gitprovider/              Provider interface
|   `-- github/               GitHub implementation (client, indexer, webhook)
|-- eventbus/                 Bus interface + InMemoryBus
|-- pipeline/                 Stage/Pipeline interfaces + built-in stages
|-- engine/                   Session orchestration logic
|-- httpapi/                  HTTP API handler (chi router, SSE)
|-- channel/                  Channel interface
|   |-- slack/                Slack bot (Socket Mode)
|   |-- telegram/             Telegram bot (long polling)
|   |-- linear/               Linear webhook channel (label-triggered)
|   `-- jira/                 Jira webhook channel (label-triggered)
|-- cmd/telecoder/            Reference CLI implementation
|-- web/                      React + Vite dashboard
|-- docker/
|   |-- base.Dockerfile       Sandbox image (Ubuntu + Node + Python + Go + agents)
|   |-- server.Dockerfile     Server image (minimal Alpine)
|   |-- compose.yml           Docker Compose for local dev
|   |-- entrypoint.sh         Sandbox entrypoint script
|   `-- setup.sh              Sandbox setup script (chat mode)
|-- _examples/minimal/        Minimal framework usage example
`-- docs/                     Deployment and setup guides
```

## Roadmap

### Phase 1 - MVP

- [x] Server with REST API and SSE streaming
- [x] Docker sandbox with pluggable agents (OpenCode, Codex)
- [x] CLI (`run`, `list`, `status`, `logs`)
- [x] GitHub PR creation
- [x] Slack bot integration
- [x] Telegram bot integration
- [x] Web UI (React + Vite)

### Phase 2 - Intelligence

- [x] Plan-then-code-then-review prompt chain
- [x] Repo-aware context indexing
- [x] Multi-step task decomposition
- [x] Review-revision loop with configurable max rounds

### Phase 3 - Extensible Framework

- [x] Interface-based architecture with 7 pluggable components
- [x] Builder API for composing custom applications
- [x] Extracted engine, pipeline, HTTP API into independent packages
- [x] Framework importable as a Go library

### Phase 4 - Quality and Speed

- [x] Built-in test/lint pipeline stage -- verify agent output before creating PR
- [x] Sandbox pre-warming pool for near-instant startup
- [x] Remote sandbox provider -- run sandboxes on a VPS or cloud Docker host via SSH

### Phase 5 - Agent Selection

- [x] Claude Code support in sandbox image
- [x] `TELECODER_CODING_AGENT` explicit agent selection in entrypoint (`opencode`, `claude-code`, `codex`, `auto`)
- [x] Per-session agent override via API and CLI (`--agent` flag)

### Phase 6 - Project Management Integrations

- [x] Linear webhook channel -- label an issue, get a PR back
- [x] Jira webhook channel -- label an issue, get a PR back
