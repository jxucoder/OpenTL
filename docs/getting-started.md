# Getting Started with TeleCoder

This guide walks you through setting up and running TeleCoder locally.

## Prerequisites

- **Go 1.25+**: [Download](https://go.dev/dl/)
- **Docker**: [Install Docker](https://docs.docker.com/get-docker/)
- **GitHub Token**: A personal access token with `repo` scope
- **LLM API Key**: An Anthropic or OpenAI API key

## Installation

### From source (recommended for development)

```bash
git clone https://github.com/jxucoder/TeleCoder.git
cd TeleCoder
make build
```

The binary is at `./bin/telecoder`.

### With `go install`

```bash
go install github.com/jxucoder/TeleCoder/cmd/telecoder@latest
```

## Configuration

Set environment variables:

```bash
# Required: GitHub access
export GITHUB_TOKEN="ghp_your_token_here"

# Required: At least one LLM provider
export ANTHROPIC_API_KEY="sk-ant-..."   # Recommended
# OR
export OPENAI_API_KEY="sk-..."

# Optional: Server settings
export TELECODER_ADDR=":7080"              # Default: :7080
export TELECODER_DATA_DIR="~/.telecoder"   # Default: ~/.telecoder
```

Or use the interactive setup wizard, which writes tokens to `~/.telecoder/config.env`:

```bash
telecoder config setup
```

## Build the Sandbox Image

The sandbox is a Docker image with a full development environment:

```bash
make sandbox-image
```

This builds the `telecoder-sandbox` image with Ubuntu 24.04, Node 22, Python 3.12, Go, and three pre-installed coding agents (OpenCode, Claude Code, Codex CLI). `TELECODER_CODING_AGENT` controls which one runs as the primary agent; the others remain available as CLI tools the agent can invoke.

## Start the Server

```bash
telecoder serve
```

You should see:

```
TeleCoder server listening on :7080
```

## Run Your First Task

In a new terminal:

```bash
# Code task -- produces a PR
telecoder run "fix the typo in README.md" --repo yourorg/yourrepo

# Question -- returns a text answer, no PR
telecoder run "what testing framework does this project use?" --repo yourorg/yourrepo
```

The CLI will:
1. Create a session on the server
2. Stream the agent's output in real-time
3. Show the PR URL when done (or display the text answer)

## CLI Commands

```bash
# Run a task
telecoder run "task description" --repo owner/repo

# Run with a specific coding agent
telecoder run "task" --repo owner/repo --agent claude-code

# List all sessions
telecoder list

# Check session status
telecoder status <session-id>

# Stream session logs
telecoder logs <session-id> --follow

# Connect to a different server
telecoder run "task" --repo owner/repo --server http://remote-server:7080

# Interactive config wizard
telecoder config setup
```

## Docker Compose (All-in-One)

For a fully containerized setup:

```bash
# Create .env file
cat > .env << EOF
GITHUB_TOKEN=ghp_your_token
ANTHROPIC_API_KEY=sk-ant-your_key
EOF

# Build and start
make docker-up

# Run tasks
telecoder run "your task" --repo owner/repo
```

## How It Works

1. **You send a task** via CLI, Slack, Telegram, or Web UI
2. **Server creates a session** and spins up an isolated Docker container
3. **The sandbox clones your repo**, installs dependencies, and runs the coding agent
4. **The agent** reads the prompt and works on the task (the agent decides whether code changes are needed)
5. If code was changed: **changes are committed and pushed** to a new branch, then a **PR is created** on GitHub
6. If no code was changed: a **text answer** is returned directly
7. **You review the PR** and merge it (or read the text answer)

## Troubleshooting

### "Is the server running?"

Make sure `telecoder serve` is running. The CLI defaults to `http://localhost:7080`.

### Docker permission denied

Ensure your user can run Docker commands:
```bash
docker ps  # Should work without sudo
```

### Sandbox build fails

Check that Docker is running and you have internet access for package downloads.

### No changes were made

The agent returned a text result instead of making code changes. This is expected for questions, analysis tasks, or when the agent determines no changes are needed.

## Next Steps

- Read the [Reference](reference.md) for architecture, interfaces, API, and config
- Set up the [Slack bot](slack-setup.md) for team use
- Set up the [Telegram bot](telegram-setup.md) for mobile use
- Set up [Linear](linear-setup.md) or [Jira](jira-setup.md) for ticket-driven automation
- See [User Stories](user-stories.md) for real-world usage scenarios
- Deploy to a VPS with the [Deployment Guide](deploy.md)
