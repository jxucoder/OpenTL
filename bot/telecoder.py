"""Thin wrapper that shells out to the telecoder CLI."""

import subprocess


def _run(*args: str) -> str:
    """Run a telecoder command and return its stdout."""
    result = subprocess.run(
        ["telecoder", *args],
        capture_output=True,
        text=True,
        timeout=30,
    )
    output = result.stdout.strip()
    if result.returncode != 0:
        err = result.stderr.strip()
        raise RuntimeError(err or f"telecoder exited {result.returncode}")
    return output


def create(
    *,
    repo_url: str | None = None,
    repo_path: str | None = None,
    branch: str | None = None,
    test_cmd: str | None = None,
    lint_cmd: str | None = None,
) -> str:
    """Create a session, return the session ID."""
    args = ["create"]
    if repo_url:
        args += ["--repo-url", repo_url]
    if repo_path:
        args += ["--repo-path", repo_path]
    if branch:
        args += ["--branch", branch]
    if test_cmd:
        args += ["--test-cmd", test_cmd]
    if lint_cmd:
        args += ["--lint-cmd", lint_cmd]
    return _run(*args)


def run(session_id: str, prompt: str) -> str:
    """Run a prompt in a session."""
    return _run("run", session_id, prompt)


def stop(session_id: str) -> str:
    return _run("stop", session_id)


def inspect(session_id: str) -> str:
    return _run("inspect", session_id)


def logs(session_id: str, stream: str = "stdout") -> str:
    return _run("logs", session_id, "--stream", stream)


def verify(session_id: str) -> str:
    return _run("verify", session_id)


def delete(session_id: str) -> str:
    return _run("delete", session_id)


def list_sessions(status: str | None = None) -> str:
    args = ["list"]
    if status:
        args += ["--status", status]
    return _run(*args)
