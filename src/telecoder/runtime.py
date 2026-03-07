"""Runtime adapter for launching and managing coding agent processes."""

from __future__ import annotations

import os
import signal
import shutil
import subprocess
from dataclasses import dataclass
from pathlib import Path

from telecoder.config import RuntimeConfig


@dataclass
class RuntimeProcess:
    pid: int
    log_stdout: Path
    log_stderr: Path


def find_binary(config: RuntimeConfig) -> str:
    """Find the runtime binary path."""
    if config.binary:
        return config.binary

    if config.type == "claude-code":
        found = shutil.which("claude")
        if found:
            return found
        raise FileNotFoundError(
            "Claude Code CLI not found. Install it or set runtime.binary in config."
        )

    raise ValueError(f"Unknown runtime type: {config.type}")


def launch(
    config: RuntimeConfig,
    workspace_dir: Path,
    logs_dir: Path,
    prompt: str,
    session_id: str,
    env_extra: dict[str, str] | None = None,
) -> RuntimeProcess:
    """Launch a runtime process for a session.

    Returns the RuntimeProcess with PID and log file paths.
    """
    binary = find_binary(config)

    log_stdout = logs_dir / f"{session_id}.stdout.log"
    log_stderr = logs_dir / f"{session_id}.stderr.log"
    logs_dir.mkdir(parents=True, exist_ok=True)

    env = os.environ.copy()
    env.update(config.env)
    if env_extra:
        env.update(env_extra)

    if config.type == "claude-code":
        cmd = [binary, "--print", "--output-format", "text", prompt]
    else:
        raise ValueError(f"Unknown runtime type: {config.type}")

    stdout_f = open(log_stdout, "a")
    stderr_f = open(log_stderr, "a")

    proc = subprocess.Popen(
        cmd,
        cwd=str(workspace_dir),
        env=env,
        stdout=stdout_f,
        stderr=stderr_f,
        stdin=subprocess.DEVNULL,
        start_new_session=True,
    )

    return RuntimeProcess(
        pid=proc.pid,
        log_stdout=log_stdout,
        log_stderr=log_stderr,
    )


def is_running(pid: int) -> bool:
    """Check if a process is still running."""
    try:
        os.kill(pid, 0)
        return True
    except (OSError, ProcessLookupError):
        return False


def stop(pid: int) -> bool:
    """Stop a running process. Returns True if it was stopped."""
    if not is_running(pid):
        return False
    try:
        os.killpg(os.getpgid(pid), signal.SIGTERM)
        return True
    except (OSError, ProcessLookupError):
        return False
