"""TeleCoder CLI - the main user interface."""

from __future__ import annotations

import json
import sys
from pathlib import Path

import click

from telecoder import __version__
from telecoder.config import Config, load_config
from telecoder.db import init_db
from telecoder.session import SessionEngine
from telecoder import git_workspace
from telecoder import verify


def get_config(ctx: click.Context) -> Config:
    return ctx.obj["config"]


def get_engine(ctx: click.Context) -> SessionEngine:
    return ctx.obj["engine"]


@click.group()
@click.option("--config", "config_path", type=click.Path(exists=False), default=None,
              help="Path to config file.")
@click.version_option(version=__version__)
@click.pass_context
def main(ctx, config_path):
    """TeleCoder - run a coding agent on your own VPS."""
    config = load_config(Path(config_path) if config_path else None)
    init_db(config.storage.db_full_path)
    ctx.ensure_object(dict)
    ctx.obj["config"] = config
    ctx.obj["engine"] = SessionEngine(config)


# --- init ---

@main.command()
@click.option("--data-dir", type=click.Path(), default=None,
              help="Data directory (default: /var/lib/telecoder).")
@click.pass_context
def init(ctx, data_dir):
    """Initialize TeleCoder (first-time setup)."""
    config = get_config(ctx)
    if data_dir:
        config.storage.data_dir = Path(data_dir)

    dirs = [
        config.storage.data_dir,
        config.storage.workspaces_dir,
        config.storage.logs_dir,
    ]
    for d in dirs:
        d.mkdir(parents=True, exist_ok=True)
        click.echo(f"  Created {d}")

    init_db(config.storage.db_full_path)
    click.echo(f"  Database at {config.storage.db_full_path}")

    config_dest = Path.home() / ".config" / "telecoder" / "config.toml"
    if not config_dest.exists():
        config_dest.parent.mkdir(parents=True, exist_ok=True)
        src = Path(__file__).parent.parent.parent / "config.example.toml"
        if src.exists():
            config_dest.write_text(src.read_text())
            click.echo(f"  Config template at {config_dest}")
        else:
            click.echo(f"  No config template found. Create {config_dest} manually.")

    click.echo("\nTeleCoder initialized. Edit your config to add credentials.")


# --- session group ---

@main.group()
def session():
    """Manage coding sessions."""
    pass


@session.command("create")
@click.option("--repo-url", default=None, help="Git repo URL to clone.")
@click.option("--repo-path", default=None, help="Local repo path.")
@click.option("--branch", default=None, help="Branch name to create/use.")
@click.option("--test-command", default=None, help="Test command to run.")
@click.option("--lint-command", default=None, help="Lint command to run.")
@click.pass_context
def session_create(ctx, repo_url, repo_path, branch, test_command, lint_command):
    """Create a new session."""
    engine = get_engine(ctx)
    config = get_config(ctx)
    s = engine.create(
        repo_url=repo_url,
        repo_path=repo_path,
        branch=branch,
        test_command=test_command,
        lint_command=lint_command,
    )

    # Set up git workspace
    workspace = Path(s.workspace_dir)
    git_workspace.setup_workspace(workspace, repo_url=repo_url, repo_path=repo_path)

    if branch:
        git_dir = workspace / ".git"
        if git_dir.exists():
            actual_branch = config.git.branch_prefix + branch if "/" not in branch else branch
            git_workspace.create_branch(workspace, actual_branch)

    click.echo(f"Session created: {s.id}")
    click.echo(f"  Workspace: {s.workspace_dir}")
    click.echo(f"  Status:    {s.status}")


@session.command("list")
@click.option("--status", default=None, help="Filter by status.")
@click.pass_context
def session_list(ctx, status):
    """List sessions."""
    engine = get_engine(ctx)
    sessions = engine.list(status=status)

    if not sessions:
        click.echo("No sessions found.")
        return

    click.echo(f"{'ID':<14} {'STATUS':<12} {'REPO':<30} {'CREATED':<20}")
    click.echo("-" * 76)
    for s in sessions:
        repo = s.repo_url or s.repo_path or "-"
        if len(repo) > 28:
            repo = "..." + repo[-25:]
        click.echo(f"{s.id:<14} {s.status:<12} {repo:<30} {s.created_at:<20}")


@session.command("inspect")
@click.argument("session_id")
@click.pass_context
def session_inspect(ctx, session_id):
    """Show detailed session info."""
    engine = get_engine(ctx)
    s = engine.refresh_status(session_id)

    click.echo(f"Session:    {s.id}")
    click.echo(f"Status:     {s.status}")
    click.echo(f"Runtime:    {s.runtime_type}")
    click.echo(f"Workspace:  {s.workspace_dir}")
    click.echo(f"Repo URL:   {s.repo_url or '-'}")
    click.echo(f"Repo Path:  {s.repo_path or '-'}")
    click.echo(f"Branch:     {s.branch or '-'}")
    click.echo(f"PID:        {s.pid or '-'}")
    click.echo(f"Created:    {s.created_at}")
    click.echo(f"Updated:    {s.updated_at}")

    if s.prompt:
        click.echo(f"\nLast Prompt:\n  {s.prompt}")
    if s.output_summary:
        click.echo(f"\nOutput Summary:\n  {s.output_summary}")

    # Show changed files if workspace has a git repo
    workspace = Path(s.workspace_dir)
    if (workspace / ".git").exists():
        changed = git_workspace.get_changed_files(workspace)
        if changed:
            click.echo(f"\nChanged Files ({len(changed)}):")
            for f in changed:
                click.echo(f"  {f}")
        diff = git_workspace.get_diff_summary(workspace)
        if diff:
            click.echo(f"\nDiff Summary:\n{diff}")

    if s.test_result:
        click.echo(f"\nTest Result: {s.test_result}")
    if s.lint_result:
        click.echo(f"\nLint Result: {s.lint_result}")


@session.command("run")
@click.argument("session_id")
@click.argument("prompt")
@click.pass_context
def session_run(ctx, session_id, prompt):
    """Run a prompt in a session."""
    engine = get_engine(ctx)
    s = engine.run(session_id, prompt)
    click.echo(f"Session {s.id} is now running (pid {s.pid}).")
    click.echo(f"Prompt: {prompt}")
    click.echo(f"\nView logs: telecoder session logs {s.id}")


@session.command("stop")
@click.argument("session_id")
@click.pass_context
def session_stop(ctx, session_id):
    """Stop a running session."""
    engine = get_engine(ctx)
    s = engine.stop(session_id)
    click.echo(f"Session {s.id} stopped.")


@session.command("resume")
@click.argument("session_id")
@click.argument("prompt")
@click.pass_context
def session_resume(ctx, session_id, prompt):
    """Resume a session with a new prompt."""
    engine = get_engine(ctx)
    s = engine.get(session_id)

    if s.status == "running":
        click.echo(f"Session {session_id} is already running. Stop it first.")
        return

    s = engine.run(session_id, prompt)
    click.echo(f"Session {s.id} resumed (pid {s.pid}).")
    click.echo(f"Prompt: {prompt}")


@session.command("logs")
@click.argument("session_id")
@click.option("--stream", type=click.Choice(["stdout", "stderr"]), default="stdout")
@click.option("--tail", default=100, help="Number of lines to show.")
@click.pass_context
def session_logs(ctx, session_id, stream, tail):
    """View session logs."""
    engine = get_engine(ctx)
    output = engine.get_logs(session_id, stream=stream, tail=tail)
    if output:
        click.echo(output)
    else:
        click.echo("No logs available yet.")


@session.command("verify")
@click.argument("session_id")
@click.pass_context
def session_verify(ctx, session_id):
    """Run verification (tests/lint) for a session."""
    engine = get_engine(ctx)
    s = engine.get(session_id)

    workspace = Path(s.workspace_dir)
    results = verify.verify_session(
        workspace,
        test_command=s.test_command,
        lint_command=s.lint_command,
    )
    click.echo(verify.format_results(results))


@session.command("delete")
@click.argument("session_id")
@click.confirmation_option(prompt="Are you sure you want to delete this session?")
@click.pass_context
def session_delete(ctx, session_id):
    """Delete a session."""
    engine = get_engine(ctx)
    engine.delete(session_id)
    click.echo(f"Session {session_id} deleted.")


# --- service group ---

@main.group()
def service():
    """Manage the TeleCoder service."""
    pass


@service.command("start")
@click.pass_context
def service_start(ctx):
    """Start the TeleCoder web service."""
    config = get_config(ctx)
    try:
        import uvicorn
        from telecoder.web.app import create_app

        app = create_app(config)
        uvicorn.run(app, host=config.service.host, port=config.service.port)
    except ImportError:
        click.echo("Web dependencies not installed. Run: pip install telecoder[web]")
        sys.exit(1)
