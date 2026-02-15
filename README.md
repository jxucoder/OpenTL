<div align="center">

# OpenTL

**Open Tech Lead** — a background coding agent for engineering teams.

Send a task. Get a PR.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Required-2496ED?style=flat&logo=docker&logoColor=white)](https://docs.docker.com/get-docker/)

</div>

---

```
opentl run "add rate limiting to /api/users" --repo myorg/myapp
# ...agent works in background...
# -> PR #142 opened: https://github.com/myorg/myapp/pull/142
```

## How It Works

1. You send a task — via **CLI**, **Slack**, or **Telegram**
2. OpenTL spins up an **isolated Docker sandbox** with your repo
3. A coding agent works on the task — [OpenCode](https://opencode.ai/) (Anthropic key) or [Codex](https://openai.com/index/codex/) (OpenAI key)
4. Changes are committed, pushed, and a **PR is opened**
5. You review the PR

```mermaid
graph LR
    CLI["CLI"]
    Slack["Slack"]
    TG["Telegram"]

    subgraph server ["OpenTL Server"]
        Sessions["Sessions"]
        Events["Event Stream"]
        Sandbox["Docker Sandbox\n(Agent)"]
    end

    GitHub["GitHub PR"]

    CLI --> server
    Slack --> server
    TG --> server
    server --> GitHub
```

## Quick Start

### Prerequisites

- [Go](https://go.dev/dl/) (version compatible with `go.mod`)
- [Docker](https://docs.docker.com/get-docker/)
- A GitHub personal access token ([create one](https://github.com/settings/tokens) with `repo` scope)
- An LLM API key — `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`

### Install

```bash
go install github.com/jxucoder/TeleCoder/cmd/opentl@latest
```

Or build from source:

```bash
git clone https://github.com/jxucoder/TeleCoder.git
cd opentl
make build
```

### Setup

```bash
# Set required environment variables
export GITHUB_TOKEN="ghp_..."
export ANTHROPIC_API_KEY="sk-ant-..."   # or OPENAI_API_KEY

# Build the sandbox Docker image
make sandbox-image
```

### Run

```bash
# Start the server
opentl serve

# In another terminal — run a task
opentl run "fix the typo in README.md" --repo yourorg/yourrepo

# List sessions
opentl list

# Check a session's status
opentl status <session-id>

# Stream logs
opentl logs <session-id> --follow
```

### Docker Compose (fully containerized)

```bash
# Configure tokens
cp .env.example .env
# Edit .env with your tokens

# Start everything
make docker-up

# Run tasks against the server
opentl run "your task" --repo owner/repo --server http://localhost:7080
```

> See [docs/deploy.md](docs/deploy.md) for a full VPS deployment guide.

## Architecture

OpenTL is a single binary — `opentl serve` runs the server, `opentl run` talks to it.

| Component | Description |
|:----------|:------------|
| **Server** | Go HTTP server. Manages sessions, streams events via SSE, and creates PRs. |
| **Orchestrator** | Plan -> code -> review pipeline with optional task decomposition and revision rounds. |
| **GitHub Context Indexer** | Fetches repo metadata/tree/key files to give the planner real codebase context. |
| **Sandbox** | One Docker container per task (or persistent container for chat mode). Clones repo, runs coding agent (OpenCode/Codex), and pushes a branch. |
| **CLI** | Creates sessions, streams logs, checks status. |
| **Slack / Telegram** | Bot integrations — send tasks from chat, get PR links back. |
| **Web UI** | React + Vite dashboard for monitoring sessions. |

### API

| Method | Endpoint | Description |
|:-------|:---------|:------------|
| `POST` | `/api/sessions` | Create a session (`mode=task` or `mode=chat`) |
| `GET` | `/api/sessions` | List sessions |
| `GET` | `/api/sessions/:id` | Get session details |
| `GET` | `/api/sessions/:id/events` | SSE stream of events |
| `GET` | `/api/sessions/:id/messages` | List chat messages |
| `POST` | `/api/sessions/:id/messages` | Send chat message |
| `POST` | `/api/sessions/:id/pr` | Create PR from chat session |
| `POST` | `/api/sessions/:id/stop` | Stop a session |
| `GET` | `/health` | Health check |

## Configuration

All configuration is via environment variables:

| Variable | Required | Default | Description |
|:---------|:---------|:--------|:------------|
| `GITHUB_TOKEN` | Yes | — | GitHub personal access token |
| `ANTHROPIC_API_KEY` | One of these | — | Anthropic API key |
| `OPENAI_API_KEY` | One of these | — | OpenAI API key |
| `OPENTL_ADDR` | No | `:7080` | Server listen address |
| `OPENTL_DATA_DIR` | No | `~/.opentl` | Data directory for SQLite DB |
| `OPENTL_DOCKER_IMAGE` | No | `opentl-sandbox` | Sandbox Docker image |
| `OPENTL_DOCKER_NETWORK` | No | `opentl-net` | Docker network name |
| `OPENTL_MAX_REVISIONS` | No | `1` | Max review/revision rounds per sub-task |
| `OPENTL_CHAT_IDLE_TIMEOUT` | No | `30m` | Idle timeout for persistent chat sandboxes |
| `OPENTL_CHAT_MAX_MESSAGES` | No | `50` | Max user messages per chat session |
| `OPENTL_SERVER` | No | `http://localhost:7080` | Server URL (for CLI) |

## Project Structure

```
OpenTL/
├── cmd/opentl/           CLI + server entry point
├── internal/
│   ├── config/           Configuration from environment
│   ├── github/           GitHub API + repo context indexing
│   ├── orchestrator/     LLM planning, review, and decomposition
│   ├── sandbox/          Docker container lifecycle
│   ├── server/           HTTP API, session orchestration, SSE
│   ├── session/          Session model, SQLite store, event bus
│   ├── slack/            Slack bot integration
│   └── telegram/         Telegram bot integration
├── docker/
│   ├── base.Dockerfile   Sandbox image (Ubuntu + Node + Python + Go + agents)
│   ├── server.Dockerfile Server image (minimal Alpine)
│   ├── compose.yml       Docker Compose for local dev
│   ├── entrypoint.sh     Sandbox entrypoint script
│   └── setup.sh          Sandbox setup script
├── web/                  React + Vite dashboard
└── docs/                 Deployment & setup guides
```

## Roadmap

### Phase 1 — MVP (current)

- [x] Server with REST API and SSE streaming
- [x] Docker sandbox with pluggable agents (OpenCode, Codex)
- [x] CLI (`run`, `list`, `status`, `logs`)
- [x] GitHub PR creation
- [x] Slack bot integration
- [x] Telegram bot integration
- [x] Web UI (React + Vite)

### Phase 2 — Intelligence

- [x] Plan-then-code-then-review prompt chain
- [x] Repo-aware context indexing
- [x] Multi-step task decomposition
- [x] Review-revision loop with configurable max rounds

### Phase 3 — Scale

- [ ] Sandbox pre-warming and caching
- [ ] Modal / cloud sandbox provider
- [ ] Multiplayer sessions
- [ ] Kubernetes Helm chart

## Documentation

| Guide | Description |
|:------|:------------|
| [Getting Started](docs/getting-started.md) | First-time setup walkthrough |
| [Deployment](docs/deploy.md) | VPS deployment with Docker Compose |
| [Slack Setup](docs/slack-setup.md) | Connect your Slack workspace |
| [Telegram Setup](docs/telegram-setup.md) | Set up the Telegram bot |

## License

Apache 2.0 — see [LICENSE](LICENSE).
