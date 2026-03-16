# TeleCoder

A self-hosted coding agent that runs on your VPS, keeps working when you disconnect, and acts within boundaries you control.

## What It Does

TeleCoder turns a cheap Linux VPS into a persistent coding agent. You give it a task or set up a watch, close your laptop, and come back to results. Sessions survive disconnections. Watches monitor your CI and PRs while you sleep.

It delegates the actual coding to existing agents (Codex, Claude, OpenCode, etc.) through [acpx](https://github.com/nickarls/acpx), so you get one consistent interface regardless of which model is doing the work.

**Two modes:**

- **Reactive** -- send a prompt, get durable results you can inspect later
- **Proactive** -- set up watches that fire automatically on CI failures or new PRs

## Quick Start

### Prerequisites

- [Bun](https://bun.sh) >= 1.3.5
- Git
- [acpx](https://github.com/nickarls/acpx) installed and on your PATH
- An API key for at least one coding agent (e.g. `OPENAI_API_KEY` for Codex)

### Install

```bash
git clone <your-telecoder-repo-url>
cd telecoder
```

No `bun install` needed -- TeleCoder uses zero npm dependencies. Everything runs on Bun built-ins (SQLite, HTTP server, process spawning).

### Run Your First Task

```bash
# Start the server
bun run serve

# In another terminal, run a task
bun src/cli.ts run --repo /path/to/your/repo "fix the failing test in auth.ts"
```

TeleCoder will:
1. Clone your repo into an isolated workspace
2. Create a `telecoder/<session-id>` branch
3. Run acpx with your prompt
4. Stream events to your terminal
5. Store the result in SQLite for later inspection

### Check Results Later

```bash
# List all sessions
bun run list

# Get details on a specific session
bun run status <session-id>

# See the event stream
bun run events <session-id>

# Rerun a failed or completed session
bun src/cli.ts rerun <session-id>
```

## Using the HTTP API

Start the server and interact via REST:

```bash
bun run serve
# Server starts on http://127.0.0.1:7080

# Create a task
curl -X POST http://localhost:7080/api/sessions \
  -H "Content-Type: application/json" \
  -d '{"repo": "/path/to/repo", "prompt": "refactor the billing module"}'

# List sessions
curl http://localhost:7080/api/sessions

# Get session details
curl http://localhost:7080/api/sessions/<id>

# Stream events (SSE)
curl -H "Accept: text/event-stream" http://localhost:7080/api/sessions/<id>/events

# Health check
curl http://localhost:7080/health
```

## Setting Up Watches

Watches are standing instructions. They fire automatically when conditions are met.

### CI Watch -- React to CI Failures

```bash
# Watch all CI on a repo
bun src/cli.ts watch-add-ci --repo /path/to/repo "investigate the failure and suggest a fix"

# Watch a specific workflow and branch
bun src/cli.ts watch-add-ci --repo /path/to/repo \
  --workflow build --branch main \
  "investigate the failure, run tests locally, suggest a fix"
```

When CI fails, TeleCoder automatically spins up a session that investigates and reports back.

### PR Watch -- React to New/Updated PRs

```bash
# Watch all PRs
bun src/cli.ts watch-add-pr --repo /path/to/repo "review the code changes for correctness and style"

# Watch PRs targeting a specific base branch
bun src/cli.ts watch-add-pr --repo /path/to/repo \
  --base main \
  "review the diff, check for security issues, suggest improvements"
```

### Trigger Watches Manually (for Testing)

```bash
# Simulate a CI failure
bun src/cli.ts trigger-ci \
  --repo /path/to/repo \
  --workflow build --branch main --run-id 12345 \
  --status completed --conclusion failure

# Simulate a PR event
bun src/cli.ts trigger-pr \
  --repo /path/to/repo \
  --pr-number 42 --title "Add auth" --base main --head feature/auth \
  --action opened
```

### Manage Watches

```bash
# List all watches
bun src/cli.ts watch-list

# Filter by kind or status
bun src/cli.ts watch-list --kind ci_failure --status active

# See run history for a watch
bun src/cli.ts watch-runs <watch-id>
```

## Publishing Results as PRs

When a session produces useful changes, publish them:

```bash
bun src/cli.ts publish-session <session-id> --base main

# With custom PR title and body
bun src/cli.ts publish-session <session-id> --base main \
  --title "Fix auth test" --body "Resolved the flaky assertion in auth.test.ts"
```

This commits any pending changes, pushes the session branch, and opens a GitHub PR. Requires `GITHUB_TOKEN`, `GH_TOKEN`, or a logged-in `gh` CLI.

## Configuration

All configuration is through environment variables. Defaults work out of the box for local development.

| Variable | Default | Description |
|----------|---------|-------------|
| `TELECODER_DATA_DIR` | `~/.telecoder` | Where SQLite database and workspaces live |
| `TELECODER_LISTEN_HOST` | `127.0.0.1` | Server bind address |
| `TELECODER_LISTEN_PORT` | `7080` | Server port |
| `TELECODER_DEFAULT_AGENT` | `codex` | Default coding agent |
| `TELECODER_POLICY_MODE` | `observe` | Default trust policy (see below) |
| `TELECODER_ACPX_COMMAND` | `acpx` | Path to acpx binary |
| `TELECODER_PROMPT_TIMEOUT_SECONDS` | `300` | Max execution time per session |
| `TELECODER_SESSION_HEARTBEAT_SECONDS` | `5` | Heartbeat interval for liveness |
| `TELECODER_SESSION_STALE_SECONDS` | `30` | Timeout before marking session abandoned |
| `GITHUB_TOKEN` / `GH_TOKEN` | -- | For PR publishing |

### Per-Agent Commands

Override the command for a specific agent:

```bash
export TELECODER_AGENT_CLAUDE_COMMAND="claude --model sonnet"
export TELECODER_AGENT_OPENCODE_COMMAND="/usr/local/bin/opencode"
```

Use with `--agent claude` or `--agent opencode` in CLI/API calls.

### View Resolved Config

```bash
bun run config
```

## Policy Modes

Every session runs under a policy that controls what the agent is allowed to do:

| Mode | Permissions | Workspace Writes | Max Runtime | Use Case |
|------|------------|-----------------|-------------|----------|
| `locked` | deny-all | blocked | 60s | Read-only investigation |
| `observe` | approve-reads | blocked | 180s | Analysis without changes (default) |
| `standard` | approve-all | contained | 300s | Let the agent make changes in its branch |

Set per-session:

```bash
bun src/cli.ts run --repo /path/to/repo --policy standard "fix the bug and commit"
```

Or set globally:

```bash
export TELECODER_POLICY_MODE=standard
```

## VPS Deployment

For always-on operation, deploy to a Linux VPS.

### Quick Install (Ubuntu)

```bash
# Clone to a stable path
sudo mkdir -p /opt/telecoder
git clone <repo-url> /opt/telecoder/app
cd /opt/telecoder/app

# Run the install script
sudo scripts/install/install-ubuntu-vps.sh
```

This installs Bun, Node 22, acpx, creates a systemd service, and starts TeleCoder.

### Configure

```bash
# Edit the env file
sudo vim /etc/telecoder/telecoder.env

# Add your API keys and preferences:
# OPENAI_API_KEY=sk-...
# ANTHROPIC_API_KEY=sk-ant-...
# TELECODER_POLICY_MODE=observe
# TELECODER_DEFAULT_AGENT=codex

# Restart after changes
sudo systemctl restart telecoder.service
```

### Check Health

```bash
curl http://127.0.0.1:7080/health
sudo systemctl status telecoder.service
sudo journalctl -u telecoder.service -n 50
```

### Upgrade

```bash
cd /opt/telecoder/app
git pull
sudo scripts/install/install-ubuntu-vps.sh --no-start
sudo systemctl restart telecoder.service
```

## Development

```bash
# Run with auto-reload
bun run dev

# Run tests
bun run test

# Show CLI help
bun src/cli.ts help
```

### Project Structure

```
src/
  cli.ts          CLI command dispatcher
  server.ts       HTTP/REST API (Bun.serve)
  engine.ts       Core task orchestration
  store.ts        SQLite persistence and migrations
  types.ts        TypeScript type definitions
  config.ts       Environment-based configuration
  policy.ts       Trust policy resolution
  process.ts      Shell command execution
  workspace.ts    Git workspace preparation
  publish.ts      PR publishing and GitHub integration
  watch.ts        Watch logic, matching, and prompting
  runtime/
    acpx.ts       ACP protocol integration
test/
  *.test.ts       Test suite (Bun test runner)
  fixtures/       Test mocks and helpers
notes/
  vision.md       Product vision
  vps-ubuntu.md   VPS deployment guide
  plans/          Sprint plans and reports
```

## Extending TeleCoder

### Add a New Agent

No code changes required. Set an environment variable:

```bash
export TELECODER_AGENT_MYAGENT_COMMAND="/path/to/my-agent"
```

Then use it:

```bash
bun src/cli.ts run --repo . --agent myagent "do the thing"
```

The agent must work with acpx. See the [acpx docs](https://github.com/nickarls/acpx) for supported runtimes.

### Add a New Watch Type

1. Add your watch kind to `WatchKind` in `src/types.ts`
2. Implement event detection (`isMyEvent`) and matching (`matchesMyWatch`) in `src/watch.ts`
3. Build a prompt constructor (`buildMyWatchPrompt`) in `src/watch.ts`
4. Add trigger logic to `src/engine.ts`
5. Wire up the CLI command in `src/cli.ts` and the HTTP endpoint in `src/server.ts`

The existing CI and PR watches are good templates to follow.

### Customize Policies

Add a new policy mode by extending the `POLICY_DEFAULTS` map in `src/policy.ts`:

```typescript
const POLICY_DEFAULTS: Record<TeleCoderPolicyMode, PolicyDefaults> = {
  // ...existing modes...
  mymode: {
    permissionMode: "approve-reads",
    workspaceWritePolicy: "contained",
    maxRuntimeSeconds: 120,
  },
};
```

Then add `"mymode"` to the `TeleCoderPolicyMode` type in `src/types.ts`.

## API Reference

### Sessions

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/sessions` | Create a task (`{repo, prompt, agent?, policy?}`) |
| `GET` | `/api/sessions` | List sessions (`?status=&agent=&policy=&parent=&lineage=`) |
| `GET` | `/api/sessions/:id` | Get session details |
| `POST` | `/api/sessions/:id/rerun` | Rerun a finished session |
| `GET` | `/api/sessions/:id/events` | Get events (JSON or SSE with `Accept: text/event-stream`) |
| `GET` | `/api/sessions/:id/lineage` | Get session family tree |
| `POST` | `/api/sessions/:id/publish` | Create a PR (`{base, title?, body?}`) |

### Watches

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/watches` | Create a watch (`{kind, repo, instructions, ...}`) |
| `GET` | `/api/watches` | List watches (`?kind=&status=&repo=`) |
| `GET` | `/api/watches/:id` | Get watch details |
| `GET` | `/api/watches/:id/runs` | Get watch run history |

### Webhook Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/watch-events/ci` | Trigger CI watches (`{repo, workflow, branch, runId, ...}`) |
| `POST` | `/api/watch-events/pr` | Trigger PR watches (`{repo, prNumber, title, base, head, ...}`) |

## How It Works

```
You (CLI / API)
  |
  v
TeleCoder Engine
  |-- SQLite (sessions, events, watches)
  |-- Policy resolution
  |-- Workspace isolation (git clone + branch)
  |
  v
acpx exec
  |
  v
Coding Agent (Codex, Claude, OpenCode, ...)
```

Each task gets its own git workspace and branch (`telecoder/<session-id>`). The agent works in isolation. Results are captured, stored, and available through CLI or API. Nothing touches your main branch unless you explicitly publish.

## License

See [LICENSE](LICENSE) for details.
