# TeleCoder v2 Design

> A remote agent that does things for your team.

## What It Is

TeleCoder gives your team a remote agent — always running on a server,
reachable from anywhere, doing real work autonomously.

Each task runs in its own Docker sandbox — a fresh clone of your repo,
isolated from the host and other tasks. The agent reads code, executes
commands, calls external APIs (Jira, Buildkite, GitHub, whatever the task
needs), edits files, and produces concrete results. A task can come from a Slack
message, a GitHub issue, a PagerDuty alert, a failing CI build, a cron
schedule, or any webhook. The result can be a pull request, a bug fix, a
debug report, a migration plan, or a dependency update.

Configure the agent once per repo:

```
my-repo/
├── AGENTS.md       ← who the agent is and how it works
└── src/
```

---

## Architecture

```
  Any event source          ┌──────────────────────────────┐
                            │          TeleCoder            │
  Slack / Telegram          │                              │
  GitHub / Linear / Jira ──▶│  Dispatcher → Engine         │
  PagerDuty / Sentry        │      ↕            ↕          │
  CI failure / Cron         │    Store       Sandbox        │
  Agent output   ◀──────────│   (SQLite)    (Docker)        │
                            └─────────────┬────────────────┘
                                          │
                                ┌─────────▼──────────┐
                                │  Docker Container   │
                                │  [coding engine]    │
                                │   reads AGENTS.md   │
                                │   emits events      │
                                └─────────────────────┘
                                          │
                                          ▼
                                 PR / text / event
```

**Dispatcher** — LLM-powered router. Reads any incoming event and decides:
ignore, reply, or spawn an agent session. No keyword matching.

**Engine** — session orchestration. Creates containers, streams events,
manages state, handles timeouts, creates PRs.

**Coding engine** — runs inside each container. Pluggable: Pi, Claude Code,
OpenCode, Codex, or any future agent. TeleCoder never calls an LLM.

**Store** — SQLite. Every session, event, result, and chain persisted.

**Sandbox** — Docker. Isolated, ephemeral, pre-warmed pool.

---

## The Dispatcher

The dispatcher is what makes TeleCoder more than a job queue.

**LLM-powered routing.** Instead of `if label == "telecoder" then run`,
a lightweight LLM reads every incoming event and outputs a decision:

```json
{ "action": "spawn" | "reply" | "ignore", "repo": "owner/repo", "prompt": "..." }
```

A Sentry alert becomes an investigation task. A vague Slack message gets
a clarifying question. A standup question is ignored. No configuration
required beyond a system prompt per channel.

**Agent chains.** A session emits events that spawn new sessions. Defined
in `AGENTS.md`:

```markdown
After opening a PR, request a code review from the review agent.
If the review finds issues, fix them before considering the task done.
```

The agent codes → review agent reviews → fix agent patches → no human
re-prompting between steps.

**Human-in-the-loop.** An agent pauses mid-task, posts a question to the
user's channel, waits for a reply, and resumes with the answer.

**Cross-session memory.** A vector store of past sessions per repo.
Relevant context is injected when a new session starts. TeleCoder's
value compounds over time.

**Proactive monitoring.** A heartbeat polls CI dashboards, Dependabot
alerts, and log aggregators on a schedule. The dispatcher decides whether
to act — no human trigger needed.

---

## Pluggable Engines

Any headless coding agent that runs in Docker.

| Engine | Command | Models | License |
|--------|---------|--------|---------|
| **Pi** (default) | `pi -p "…" --mode json` | 15+ providers | MIT |
| **Claude Code** | `claude -p "…" --output-format stream-json` | Anthropic only | Proprietary |
| **OpenCode** | `opencode -p "…"` | 15+ providers | MIT |
| **Codex** | `codex exec --full-auto "…"` | OpenAI only | Apache 2.0 |

Pi is the default: model-agnostic, MIT, richest JSONL output, and no
existing orchestration layer competing with TeleCoder.

Adding an engine: ~50 lines implementing one interface.

```go
type Engine interface {
    Name() string
    Command(prompt string) string
    ParseEvent(line string) *model.Event
}
```

---

## Session Modes

| Mode | Description |
|------|-------------|
| **Task** | Single prompt → engine runs → PR or text. Container destroyed. |
| **Chat** | Persistent container. Multiple messages. PR on demand. |
| **Autonomous** | Dispatcher-spawned. Engine runs, emits event, may chain. |
| **Batch/cron** | Scheduled job: same prompt across many repos, or sequential steps against one. Each step is a task-mode session. |

Batch jobs are defined as YAML in `.telecoder/jobs/`:

```yaml
schedule: "0 9 * * MON"
repos: [org/api, org/frontend]
prompt: "Audit for outdated deps and TODOs older than 90 days."
```

---

## Agent Configuration

All engines read `AGENTS.md` from the repo root. It defines both how the
agent works and what it does next — making chains declarative:

```markdown
# AGENTS.md
You are a senior backend engineer. Always run `make test` before finishing.
Never modify generated files in `pkg/api/gen/`.

After opening a PR, request a code review. Fix any issues before done.
```

No `AGENTS.md`? Engine uses defaults. TeleCoder injects: "run tests if
a test suite exists."

---

## Getting Started

```bash
curl -fsSL https://telecoder.dev/install | bash
telecoder setup    # GitHub token, LLM key, engine, channels, Docker check
telecoder serve    # team server on :7080
```

---

## Package Layout

```
pkg/agent/        CodingAgent interface + Pi, Claude Code, OpenCode, Codex implementations
pkg/dispatcher/   LLM-powered event router + agent chain evaluator
pkg/memory/       Cross-session vector store (embedder interface + cosine similarity)
pkg/scheduler/    Batch/cron job scheduler (YAML job definitions)
pkg/store/        SQLite persistence (sessions, messages, events)
pkg/sandbox/      Docker sandbox runtime, SSH remote, verify commands
pkg/gitprovider/  GitHub PR creation + webhooks
pkg/eventbus/     Real-time pub/sub (in-memory)
pkg/channel/      Slack, Telegram, Linear, Jira, GitHub Issues
pkg/model/        Core domain types (Session, Event, SubTask, etc.)
internal/engine/  Session orchestration (uses pkg/agent interface)
internal/httpapi/ HTTP API + SSE streaming
cmd/telecoder/    CLI: serve, setup, run, list, status, logs, config
```

Removed from v1: `pkg/llm/`, `pkg/pipeline/` (~1,500 lines — engines
handle this internally).

---

## Implementation Plan

No backward compatibility. Delete freely. Each task has an eval.

### Task 1: Delete v1 pipeline and LLM packages

Delete `pkg/llm/`, `pkg/pipeline/prompts.go`, `pkg/gitprovider/github/indexer.go`.
Move `SubTaskStatus` + progress helpers from `pkg/pipeline/pipeline.go` to
`pkg/model/`. Move `verify.go` to `pkg/sandbox/verify.go`. Delete remainder
of `pkg/pipeline/`. Remove all pipeline/LLM references from `telecoder.go`,
`internal/engine/engine.go`, and Builder/Config.

**Eval:** `go build ./...` succeeds. `go test ./...` passes (update/remove
tests that depend on deleted code). No import of `pkg/llm` or `pkg/pipeline`
anywhere.

### Task 2: Engine interface and implementations

Create `pkg/agent/` with `Engine` interface:

```go
type Engine interface {
    Name() string
    Command(prompt string) string
    ParseEvent(line string) *model.Event
}
```

Implement for Pi, Claude Code, OpenCode, Codex. Each ~50-100 lines.
Update engine.go to use `Engine.Command()` instead of the hardcoded
`chatAgentCommand` switch. Wire engine selection via config
(`TELECODER_CODING_AGENT` env var).

**Eval:** `go test ./pkg/agent/...` — unit tests for each engine's
`Command()` and `ParseEvent()` with sample output lines. Engine.go
compiles with no direct references to "pi", "claude", "opencode",
"codex" — only through the interface.

### Task 3: JSONL event parser

Replace `dispatchLogLine` (the `###TELECODER_` marker parser) with a
call to `Engine.ParseEvent()`. Each engine normalizes its output format
to TeleCoder's `model.Event`. The entrypoint's final line
(`{"telecoder":"done",...}`) is parsed as a completion event.

**Eval:** integration test — feed a sample Pi JSONL session transcript
and a sample Claude Code stream-json transcript through the parser.
Verify events are emitted correctly. Feed the old `###TELECODER_DONE###`
format — verify it's no longer recognized (backward compat broken).

### Task 4: New entrypoint

Replace `docker/entrypoint.sh` with the engine-aware version (case switch
on `TELECODER_CODING_AGENT`). Delete old entrypoint.

**Eval:** build the Docker image (`make sandbox-image`). Run the
entrypoint in a container with `TELECODER_CODING_AGENT=pi` and a mock
prompt — verify it attempts to run `pi -p "..." --mode json`. Repeat
for `claude-code`, `opencode`, `codex`.

### Task 5: Simplify engine.go

Gut `runSubTask`, `runSandboxRound`, `runSandboxRoundWithAgent` (the
decompose/plan/review/revision loop). Replace with:

1. Start container
2. Read stdout via `Engine.ParseEvent()` per line
3. On completion: check for code changes, commit, push if needed
4. Create PR via gitprovider

Keep: `runVerify` (moved to sandbox), `MaxRevisions` as a bounded
retry count for verify failures only, `runSessionMultiStep` simplified
to sequential prompts in a persistent container, chat mode,
`CreatePRFromChat`, `CreatePRCommentSession`, idle reaper.

**Eval:** `go test ./internal/engine/...` passes. E2E test
(`e2e/e2e_test.go`) updated and passes — creates a session, runs a
stub engine, gets a result. Session lifecycle: pending → running →
complete works.

### Task 6: Setup wizard

Create `cmd/telecoder/setup.go`. Interactive prompts using
[huh](https://github.com/charmbracelet/huh):

1. GitHub token (validate with API call)
2. LLM API key (detect provider from key prefix)
3. Engine selection (pi/claude-code/opencode/codex)
4. Channels (Slack/Telegram/none)
5. Docker check (`docker info`)
6. Write `~/.telecoder/config.env`

**Eval:** `go build ./cmd/telecoder/` succeeds. Run
`telecoder setup --non-interactive --github-token=test --engine=pi`
in test — verify it writes a valid config file.

### Task 7: Dispatcher (LLM-powered routing)

Create `pkg/dispatcher/`. Takes raw event text + channel metadata,
calls a lightweight LLM (configurable model), returns structured
decision: `{action, repo, prompt, agent}`.

Channels call the dispatcher instead of parsing keywords/labels
directly. Dispatcher has a system prompt per channel type.

**Eval:** unit test with mock LLM — send 5 sample events (Slack task,
Slack question, GitHub issue, Sentry alert, irrelevant message). Verify
dispatcher returns correct action for each. Test that `action: "ignore"`
produces no session.

### Task 8: Agent chains

After a session completes, engine checks if the session's result should
trigger a follow-up. The dispatcher evaluates the result event and may
spawn a new session.

Chain depth limit (default 3) to prevent loops. Chain ID links related
sessions.

**Eval:** integration test — create a session that completes with
`type: pr`. Verify the dispatcher is called with the completion event.
With a mock dispatcher that returns `action: spawn`, verify a child
session is created with the correct `chain_id`. Verify chain depth 4
is rejected.

### Task 9: Batch/cron scheduler

Create `pkg/scheduler/`. Reads `.telecoder/jobs/*.yaml` from a
configurable directory. Parses schedule (cron syntax), repos, prompts.
Uses `robfig/cron` for scheduling. Each trigger creates task-mode
sessions via the engine.

**Eval:** unit test — parse a sample job YAML, verify schedule, repos,
prompt extracted correctly. Integration test — register a job with
`@every 1s` schedule, verify a session is created within 2 seconds.

### Task 10: Cross-session memory

Create `pkg/memory/`. Uses SQLite with `sqlite-vec` extension for
vector search. After each session completes, store a summary embedding.
Before a new session starts, retrieve top-3 relevant past sessions for
the same repo. Inject as context in the prompt.

**Eval:** unit test — store 10 session summaries, query with a related
prompt, verify the returned sessions are semantically relevant (cosine
similarity > 0.7). Test that sessions from a different repo are not
returned.

### Task 11: Update web dashboard

Update `web/` to show:
- Engine name per session
- Agent chain visualization (linked sessions)
- Batch job status
- Session event stream from new JSONL format

**Eval:** `cd web && npm run build` succeeds. Manual verification that
the dashboard renders sessions with engine labels.

### Task 12: Update CLAUDE.md

Rewrite `CLAUDE.md` to match v2 architecture. Remove references to
`pkg/llm/`, `pkg/pipeline/`, old marker protocol. Add `pkg/agent/`,
`pkg/dispatcher/`, `pkg/memory/`, `pkg/scheduler/`.

**Eval:** every package mentioned in CLAUDE.md exists. Every deleted
package is not mentioned.

---

## Open Questions

1. Use Rivet's Sandbox Agent SDK instead of our own engine abstraction?
   It already normalizes Pi, OpenCode, Claude Code, Codex, and Amp.
2. Dispatcher model: small (Haiku, GPT-4o-mini) vs configurable?
3. Loop prevention: how does the dispatcher know when a chain is done?
4. Memory scope: per-repo or per-team? Cross-repo has privacy implications.
5. Per-engine slim Docker images vs one image with all engines?
