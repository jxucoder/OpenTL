# TeleCoder Sprints

## Tech Choices

- **Language**: Python 3.11+
- **CLI**: Click
- **Web**: FastAPI + Jinja2 templates
- **Database**: SQLite via sqlite3 stdlib
- **Config**: TOML (tomllib stdlib)
- **Runtime**: subprocess management of Claude Code CLI
- **Git**: subprocess calls to git CLI
- **Package**: pyproject.toml with pip-installable package

## Sprint 1: Bootstrap & Foundation

**Goal**: Project skeleton that builds, installs, and runs.

Deliverables:
- pyproject.toml with dependencies
- src/telecoder/ package structure
- Config format (TOML) with config.example.toml
- SQLite schema for sessions
- Database initialization module
- Service entrypoint (main.py)
- systemd unit template
- .gitignore updates

## Sprint 2: Runtime Adapter & Session Engine

**Goal**: Can create a session, launch Claude Code, capture output, stop it.

Deliverables:
- Runtime adapter: launch/stop/status for Claude Code CLI
- stdout/stderr capture to log files
- Session engine: create/start/stop/resume/list
- Session state persistence in SQLite
- Session workspace directory management

## Sprint 3: Git Workspace & Verification Layer

**Goal**: Sessions work with real git repos and can run tests.

Deliverables:
- Clone repo into workspace
- Open existing local repo
- Create/select working branch
- Preserve workspace between runs
- Optional test command execution
- Optional lint command execution
- Result capture and pass/fail summary

## Sprint 4: CLI

**Goal**: Full CLI that covers all v1 user stories.

Deliverables:
- telecoder init (first-time setup)
- telecoder config (view/set credentials)
- telecoder session create
- telecoder session list
- telecoder session inspect
- telecoder session run (send prompt)
- telecoder session stop
- telecoder session resume
- telecoder session logs

## Sprint 5: Web UI & Install Story

**Goal**: Basic web viewer and one-command install.

Deliverables:
- FastAPI app with session list page
- Session detail page (status, logs, outputs)
- Log streaming view
- install.sh script
- systemd service registration
- Upgrade and uninstall paths

## Sprint 6: Documentation

**Goal**: A fresh user can set up TeleCoder without help.

Deliverables:
- README with quick start
- VPS setup guide
- Runtime setup guide (Claude Code)
- Git credential guide
- Troubleshooting guide
