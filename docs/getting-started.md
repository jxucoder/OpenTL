# Getting Started with OpenTL

This guide walks you through setting up and running OpenTL locally.

## Prerequisites

- **Go 1.22+**: [Download](https://go.dev/dl/)
- **Docker**: [Install Docker](https://docs.docker.com/get-docker/)
- **GitHub Token**: A personal access token with `repo` scope
- **LLM API Key**: An Anthropic or OpenAI API key

## Installation

### From source (recommended for development)

```bash
git clone https://github.com/jxucoder/TeleCoder.git
cd opentl
make build
```

The binary is at `./bin/opentl`.

### With `go install`

```bash
go install github.com/jxucoder/TeleCoder/cmd/opentl@latest
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
export OPENTL_ADDR=":7080"              # Default: :7080
export OPENTL_DATA_DIR="~/.opentl"      # Default: ~/.opentl
```

## Build the Sandbox Image

The sandbox is a Docker image with a full development environment:

```bash
make sandbox-image
```

This builds the `opentl-sandbox` image with Ubuntu, Node.js, Python, Go, and OpenCode.

## Start the Server

```bash
opentl serve
```

You should see:

```
OpenTL server listening on :7080
```

## Run Your First Task

In a new terminal:

```bash
opentl run "fix the typo in README.md" --repo yourorg/yourrepo
```

The CLI will:
1. Create a session on the server
2. Stream the agent's output in real-time
3. Show the PR URL when done

## CLI Commands

```bash
# Run a task
opentl run "task description" --repo owner/repo

# List all sessions
opentl list

# Check session status
opentl status <session-id>

# Stream session logs
opentl logs <session-id> --follow

# Connect to a different server
opentl run "task" --repo owner/repo --server http://remote-server:7080
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
opentl run "your task" --repo owner/repo
```

## How It Works

1. **You send a task** via CLI (or Slack/Web in Phase 2)
2. **Server creates a session** and spins up an isolated Docker container
3. **The sandbox clones your repo**, installs dependencies, and runs OpenCode
4. **OpenCode (the AI agent)** reads the prompt and modifies the codebase
5. **Changes are committed and pushed** to a new branch
6. **Server creates a PR** on GitHub with a summary
7. **You review the PR** and merge it

## Troubleshooting

### "Is the server running?"

Make sure `opentl serve` is running. The CLI defaults to `http://localhost:7080`.

### Docker permission denied

Ensure your user can run Docker commands:
```bash
docker ps  # Should work without sudo
```

### Sandbox build fails

Check that Docker is running and you have internet access for package downloads.

### No changes were made

The AI agent couldn't find anything to change. Try a more specific prompt.

## Next Steps

- Read the [API documentation](../README.md#api)
- Set up the [Web UI](../web/README.md) (Phase 2)
- Configure the [Slack bot](../internal/slack/README.md) (Phase 2)
