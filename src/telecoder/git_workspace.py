"""Git workspace handling - clone, branch, and workspace management."""

from __future__ import annotations

import subprocess
from pathlib import Path

from telecoder.config import GitConfig


def _run_git(args: list[str], cwd: Path | None = None) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git"] + args,
        cwd=str(cwd) if cwd else None,
        capture_output=True,
        text=True,
    )


def clone_repo(repo_url: str, workspace_dir: Path) -> Path:
    """Clone a repo into the workspace directory."""
    workspace_dir.mkdir(parents=True, exist_ok=True)
    result = _run_git(["clone", repo_url, str(workspace_dir)])
    if result.returncode != 0:
        raise RuntimeError(f"git clone failed: {result.stderr.strip()}")
    return workspace_dir


def open_local_repo(repo_path: Path, workspace_dir: Path) -> Path:
    """Copy/link a local repo into the workspace. Uses git clone --local."""
    workspace_dir.mkdir(parents=True, exist_ok=True)
    result = _run_git(["clone", "--local", str(repo_path), str(workspace_dir)])
    if result.returncode != 0:
        raise RuntimeError(f"git clone --local failed: {result.stderr.strip()}")
    return workspace_dir


def setup_workspace(
    workspace_dir: Path,
    repo_url: str | None = None,
    repo_path: str | None = None,
) -> Path:
    """Set up a workspace from a repo URL or local path.

    If the workspace already contains a git repo, reuses it.
    """
    git_dir = workspace_dir / ".git"
    if git_dir.exists():
        return workspace_dir

    if repo_url:
        return clone_repo(repo_url, workspace_dir)
    elif repo_path:
        return open_local_repo(Path(repo_path), workspace_dir)
    else:
        # No repo - just create the directory
        workspace_dir.mkdir(parents=True, exist_ok=True)
        return workspace_dir


def create_branch(workspace_dir: Path, branch_name: str) -> str:
    """Create and checkout a new branch."""
    result = _run_git(["checkout", "-b", branch_name], cwd=workspace_dir)
    if result.returncode != 0:
        # Branch might already exist, try switching to it
        result = _run_git(["checkout", branch_name], cwd=workspace_dir)
        if result.returncode != 0:
            raise RuntimeError(f"git checkout failed: {result.stderr.strip()}")
    return branch_name


def get_current_branch(workspace_dir: Path) -> str | None:
    """Get the current branch name."""
    result = _run_git(["rev-parse", "--abbrev-ref", "HEAD"], cwd=workspace_dir)
    if result.returncode != 0:
        return None
    return result.stdout.strip()


def get_diff_summary(workspace_dir: Path) -> str:
    """Get a summary of changed files."""
    result = _run_git(["diff", "--stat"], cwd=workspace_dir)
    if result.returncode != 0:
        return ""
    return result.stdout.strip()


def get_changed_files(workspace_dir: Path) -> list[str]:
    """Get list of changed files (staged and unstaged)."""
    result = _run_git(["status", "--porcelain"], cwd=workspace_dir)
    if result.returncode != 0:
        return []
    return [
        line[3:] for line in result.stdout.strip().splitlines() if line.strip()
    ]


def push_branch(workspace_dir: Path, branch: str, remote: str = "origin") -> bool:
    """Push a branch to the remote. Returns True on success."""
    result = _run_git(["push", "-u", remote, branch], cwd=workspace_dir)
    return result.returncode == 0
