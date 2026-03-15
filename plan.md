# TeleCoder Plan

## Goal

Build the first useful version of TeleCoder:

**a simple remote coding session product for individuals and very small teams**

The product should let a user:

1. set up TeleCoder on a Linux VPS quickly
2. connect a repo and a coding runtime
3. start a remote session
4. leave the laptop
5. come back later to logs, outputs, and verification results

## Product Standard

The plan should protect one core promise:

**TeleCoder is the simplest way to run a coding agent on your own VPS.**

If a feature weakens that promise, it should move out of v1.

## Hard Choices For V1

V1 should make explicit decisions instead of staying abstract.

### User

- individual developer first
- very small team second

### Platform

- Ubuntu VPS first
- one-machine deployment
- one long-running TeleCoder service

### Runtime

- support one runtime well
- default target: Claude Code first
- communicate with the runtime via ACP (Agent Client Protocol)
- use the Go SDK: `github.com/coder/acp-go-sdk`

### Persistence

- SQLite for session metadata
- local disk for logs and artifacts
- one workspace or worktree per session

### Isolation

- one process per session
- one workspace per session
- no container requirement in v1

### Interfaces

- CLI first
- simple web UI second

### Git

- clone and push with credentials available on the VPS
- PR creation is optional in v1

## Non-Goals For V1

Do not build these into the first version:

- enterprise auth
- org-wide collaboration
- Jira integration
- Buildkite integration
- metrics dashboards
- Slack or Telegram entry points
- multiple runtime support
- container orchestration
- multi-machine deployment

## V1 User Stories

### Core Stories

- As a user, I can install TeleCoder on a fresh Ubuntu VPS with one install path.
- As a user, I can configure my model credentials and git credentials.
- As a user, I can point TeleCoder at a repository.
- As a user, I can start a remote coding session from the CLI.
- As a user, I can see whether the session is running, completed, failed, or stopped.
- As a user, I can stop a running session.
- As a user, I can resume a prior session and continue from the same remote state.
- As a user, I can inspect logs, changed files, and test results after a run.

### Nice-to-Have Stories

- As a user, I can open a lightweight web page to inspect sessions.
- As a user, I can push a generated branch to my git remote.
- As a user, I can get a PR-ready summary even if PR creation itself is manual.

## System Shape

V1 system layout:

```
[CLI]
  |
  v
[TeleCoder Service]
 - config
 - session manager
 - ACP client
 - SQLite
 - local artifacts
  |
  +-----------------------------+
  |                             |
  v                             v
[Session Workspace]        [Session Workspace]
 - repo                    - repo
 - agent process (ACP)     - agent process (ACP)
 - JSON-RPC over stdio     - JSON-RPC over stdio
 - logs                    - logs
 - outputs                 - outputs
```

Optional in v1.1:

```
[Web UI]
  |
  v
[TeleCoder Service API]
```

## Workstreams

## 1. Bootstrap

Deliver:

- repo structure
- local development workflow
- config format
- service entrypoint
- SQLite schema for sessions

Suggested outputs:

- `src/` application skeleton
- `config.example`
- `data/` directory structure
- `systemd` unit template

## 2. Runtime Adapter (ACP Client)

Deliver:

- ACP client using `github.com/coder/acp-go-sdk`
- spawn agent process with ACP mode (e.g. `claude --acp`)
- JSON-RPC 2.0 over stdin/stdout
- Initialize handshake, session creation, prompt execution
- streaming SessionUpdate events mapped to TeleCoder events
- agent process lifecycle management (start, stop, cleanup)
- session working directory management

The runtime adapter is an ACP client, not a shell command wrapper.
TeleCoder talks to agents using the Agent Client Protocol — a standard
JSON-RPC 2.0 protocol over stdio, similar to LSP.

This means:

- no custom marker protocol for event parsing
- no per-agent adapter code
- any ACP-compatible agent works without code changes
- chat mode is native (multi-turn prompts over one connection)

Important rule:

Start with Claude Code as the first ACP agent.
Do not build a fallback shell adapter until a real need exists.

## 3. Session Engine

Deliver:

- create session (also creates ACP session via `NewSession`)
- attach repo + working state
- start run (sends `Prompt` over ACP, streams `SessionUpdate` events)
- stop run (sends ACP cancellation)
- persist session metadata
- list sessions
- resume session (reconnects ACP or creates new session with context)

Core session fields:

- id
- acp session id
- repo path or repo URL
- working directory
- runtime type
- status
- created at
- updated at
- latest prompt
- latest output summary

## 4. Git Workspace Handling

Deliver:

- clone repo into VPS
- open existing repo
- create or select working branch
- preserve workspace state between runs

Keep this simple:

- one session -> one workspace
- worktree support can be added after the base flow works

## 5. Verification Layer

Deliver:

- optional test command
- optional lint command
- result capture
- pass/fail summary

The point is not perfect verification.
The point is to make the output more useful than raw model text.

## 6. CLI

Deliver:

- install/init command
- configure credentials
- create session
- run prompt
- list sessions
- inspect session
- stop session
- resume session

CLI is the real product surface for v1.

## 7. Web UI

Deliver:

- basic session list
- session detail view
- logs/output view
- run status

Web UI should be intentionally small.
It is a viewer and controller, not a full app platform.

## 8. Install Story

Deliver:

- one documented install path
- one documented upgrade path
- one documented uninstall path

This is product-critical.

Possible install path:

1. run one shell script
2. install TeleCoder binaries/deps
3. create config/data directories
4. register systemd service
5. print next-step commands

## 9. Documentation

Deliver:

- quick start
- VPS setup guide
- runtime setup guide
- git credential guide
- troubleshooting guide

Good docs are part of the product.

## Milestones

## Milestone 0: Feasibility

Goal:

Prove TeleCoder can communicate with Claude Code over ACP on a VPS.

Exit criteria:

- Claude Code launches in ACP mode on target VPS
- ACP Initialize handshake succeeds
- a prompt can be sent and a response received over ACP
- SessionUpdate events stream correctly
- the agent process can be stopped and restarted

## Milestone 1: Session Core

Goal:

Ship local CLI-only TeleCoder with persistent sessions.

Exit criteria:

- create/list/stop/resume sessions
- one session workspace per task
- SQLite-backed state
- local log and artifact files

## Milestone 2: Repo Workflow

Goal:

Support real repo work.

Exit criteria:

- clone repo
- open repo
- create/select branch
- run coding task in workspace
- capture changed files and diff summary

## Milestone 3: Verification

Goal:

Make results trustworthy enough to be useful.

Exit criteria:

- optional test run
- optional lint run
- clear pass/fail summary
- failure output visible to user

## Milestone 4: VPS Productization

Goal:

Make setup actually simple.

Exit criteria:

- one install path
- one config path
- one systemd service
- quick-start docs good enough for a fresh user

## Milestone 5: Web Viewer

Goal:

Basic remote visibility from browser.

Exit criteria:

- session list
- session detail
- logs view
- current status

## Recommended Build Order

1. ~~Choose the exact v1 runtime.~~ **Done: Claude Code via ACP.**
2. Prove ACP works: spawn Claude Code in ACP mode, complete a handshake,
   send a prompt, receive a response. Do this on the target Ubuntu VPS.
3. Build session persistence with SQLite.
4. Build workspace management for one repo per session.
5. Build the ACP client adapter that maps ACP events to TeleCoder events.
6. Build CLI for create/run/stop/resume.
7. Add verification commands and result summaries.
8. Build the install script and systemd service.
9. Add the small web UI.

## Open Decisions

These should be resolved early:

- ~~What exact runtime is v1 using?~~ **Resolved: Claude Code via ACP.**
- What exact install path do we ship?
- How does the runtime get credentials?
- Do we support branch push in v1 by default or only when configured?
- Is the web UI served by the same service or a separate lightweight frontend?
- What is the exact CLI flag to launch Claude Code in ACP mode?

## Risks

### Runtime Risk

The chosen runtime may be harder to automate on a VPS than expected.

Mitigation:

- prove runtime feasibility first

### Setup Risk

The install path may become too fragile or too manual.

Mitigation:

- optimize packaging early
- test from a clean VPS repeatedly

### Scope Risk

It will be tempting to add integrations and workflow automation too early.

Mitigation:

- protect the v1 promise
- refuse features that do not improve the core session product

### Reliability Risk

Long-lived sessions may break on disconnects, crashes, or partial writes.

Mitigation:

- store state locally and simply
- prefer restartable processes
- keep session state explicit

## Acceptance Criteria

The product is ready for a real first user when this is true:

- a fresh user can install it on an Ubuntu VPS without hand-holding
- the user can connect a repo and a runtime
- the user can start a session from CLI
- the session survives the user closing the laptop
- the user can return later and inspect outputs
- the user can stop and resume sessions
- the user can see test or lint results when configured

## Final Rule

When in doubt, choose the option that makes TeleCoder:

- easier to install
- easier to explain
- easier to trust
- easier to resume

That is the product.
