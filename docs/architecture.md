# OpenTL Architecture

## Overview

OpenTL is structured as a monolithic Go server with cleanly separated internal packages. Each package has a single responsibility and communicates through well-defined interfaces.

```
┌──────────────────────────────────────────────────────┐
│                     CLI (cobra)                       │
│            cmd/opentl: run, list, status, logs        │
└──────────────────┬───────────────────────────────────┘
                   │ HTTP
┌──────────────────▼───────────────────────────────────┐
│                  Server (chi router)                  │
│       REST API + SSE streaming + session mgmt         │
├──────────┬──────────┬──────────┬─────────────────────┤
│  Slack   │ Telegram │  Orch.   │  Sandbox Manager    │
│  Bot     │   Bot    │ Pipeline │  (Docker lifecycle) │
└──────────┴──────────┴──────────┴─────────────────────┘
                   │                    │
         ┌────────▼────────┐    ┌──────▼──────┐
         │  Session Store  │    │   GitHub    │
         │  (SQLite + Bus) │    │   Client    │
         └─────────────────┘    └─────────────┘
```

## Package Responsibilities

### `cmd/opentl`
CLI entry point using Cobra. Subcommands talk to the server over HTTP.

- **main.go** — Root command, global flags
- **serve.go** — Starts the server with graceful shutdown
- **run.go** — Creates a session and streams events via SSE
- **list.go** — Lists all sessions in a table
- **status.go** — Shows session details; `logs` streams/dumps events

### `internal/server`
The central coordinator. Owns the HTTP router, creates sessions, orchestrates the plan/code/review pipeline, and manages container lifecycle.

Key design decisions:
- `CreateAndRunSession` is the shared entry point for HTTP, Slack, and Telegram
- Session execution runs in a background goroutine with a 30-minute timeout
- SSE handler subscribes to the event bus before reading historical events to prevent race conditions

### `internal/session`
Persistence layer (SQLite) and in-memory pub/sub event bus.

- **Store** — CRUD for sessions and events, WAL mode for concurrency
- **EventBus** — Channels-based pub/sub per session ID, with back-pressure (drops events if subscriber is slow)

### `internal/sandbox`
Docker container lifecycle management via `docker` CLI.

- Containers run with resource limits (memory, CPU, PID)
- Logs are streamed by redirecting stderr into stdout at the source
- Communication uses line-prefixed markers (`###OPENTL_STATUS###`, etc.)

### `internal/orchestrator`
The plan-then-code-then-review pipeline. Three sequential LLM calls:

1. **Plan** — Generate a structured plan from the task description
2. **Enrich** — Combine original prompt with the plan for the sandbox agent
3. **Review** — Review the resulting diff, up to N rounds

Supports Anthropic and OpenAI as LLM providers via the `LLMClient` interface.

### `internal/github`
Thin wrapper around `go-github` for PR creation and default branch detection.

### `internal/slack`
Socket Mode bot. Listens for `@mentions`, creates sessions, posts threaded updates, uploads terminal logs as files, and delivers PR links with Block Kit formatting.

### `internal/telegram`
Long-polling bot. Processes messages, creates sessions, sends status updates as replies, uploads terminal logs as documents.

## Data Flow

```
1. User sends task (CLI / Slack / Telegram)
2. Server creates Session (SQLite) + generates branch name
3. Orchestrator.Plan() → LLM generates structured plan
4. Orchestrator.EnrichPrompt() → combines prompt + plan
5. Sandbox.Start() → Docker container with enriched prompt
6. entrypoint.sh → clones repo, installs deps, runs agent
7. Server.StreamLogs() → parses container output markers
8. Events stored in SQLite + pushed to EventBus subscribers
9. Sandbox exits → Server gets diff from container
10. Orchestrator.Review() → LLM reviews diff (up to 2 rounds)
11. GitHub.CreatePR() → opens pull request
12. Session marked complete, "done" event emitted
```

## Security Model

- **Isolation:** All code execution happens inside Docker containers with resource limits
- **Authentication:** GitHub token scoped to repo operations; LLM keys for plan/review only
- **Input validation:** Repo format validated, request body size limited to 1 MB
- **Timeouts:** Session timeout (30 min), LLM API timeout (5 min), HTTP middleware timeout (5 min)

## Database Schema

```sql
sessions (
    id TEXT PRIMARY KEY,
    repo TEXT, prompt TEXT, status TEXT,
    branch TEXT, pr_url TEXT, pr_number INTEGER,
    container_id TEXT, error TEXT,
    created_at DATETIME, updated_at DATETIME
)

session_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT REFERENCES sessions(id),
    type TEXT, data TEXT, created_at DATETIME
)
```
