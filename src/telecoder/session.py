"""Session engine - create, run, stop, resume, list sessions."""

from __future__ import annotations

import sqlite3
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

from telecoder.config import Config
from telecoder.db import get_connection
from telecoder import runtime


@dataclass
class Session:
    id: str
    repo_url: str | None
    repo_path: str | None
    workspace_dir: str
    runtime_type: str
    status: str
    branch: str | None
    prompt: str | None
    output_summary: str | None
    test_command: str | None
    lint_command: str | None
    test_result: str | None
    lint_result: str | None
    pid: int | None
    created_at: str
    updated_at: str

    @classmethod
    def from_row(cls, row: sqlite3.Row) -> Session:
        return cls(**dict(row))


class SessionEngine:
    def __init__(self, config: Config):
        self.config = config
        self.db_path = config.storage.db_full_path

    def _conn(self) -> sqlite3.Connection:
        return get_connection(self.db_path)

    def create(
        self,
        repo_url: str | None = None,
        repo_path: str | None = None,
        branch: str | None = None,
        test_command: str | None = None,
        lint_command: str | None = None,
    ) -> Session:
        """Create a new session."""
        session_id = uuid.uuid4().hex[:12]
        workspace_dir = str(self.config.storage.workspaces_dir / session_id)
        Path(workspace_dir).mkdir(parents=True, exist_ok=True)

        test_cmd = test_command or self.config.verify.test_command
        lint_cmd = lint_command or self.config.verify.lint_command

        conn = self._conn()
        try:
            conn.execute(
                """INSERT INTO sessions
                   (id, repo_url, repo_path, workspace_dir, runtime_type,
                    status, branch, test_command, lint_command)
                   VALUES (?, ?, ?, ?, ?, 'created', ?, ?, ?)""",
                (
                    session_id,
                    repo_url,
                    repo_path,
                    workspace_dir,
                    self.config.runtime.type,
                    branch,
                    test_cmd,
                    lint_cmd,
                ),
            )
            conn.commit()
            return self.get(session_id)
        finally:
            conn.close()

    def get(self, session_id: str) -> Session:
        """Get a session by ID."""
        conn = self._conn()
        try:
            row = conn.execute(
                "SELECT * FROM sessions WHERE id = ?", (session_id,)
            ).fetchone()
            if not row:
                raise KeyError(f"Session not found: {session_id}")
            return Session.from_row(row)
        finally:
            conn.close()

    def list(self, status: str | None = None) -> list[Session]:
        """List sessions, optionally filtered by status."""
        conn = self._conn()
        try:
            if status:
                rows = conn.execute(
                    "SELECT * FROM sessions WHERE status = ? ORDER BY created_at DESC",
                    (status,),
                ).fetchall()
            else:
                rows = conn.execute(
                    "SELECT * FROM sessions ORDER BY created_at DESC"
                ).fetchall()
            return [Session.from_row(r) for r in rows]
        finally:
            conn.close()

    def run(self, session_id: str, prompt: str) -> Session:
        """Start a runtime process for a session with a prompt."""
        session = self.get(session_id)

        if session.status == "running" and session.pid and runtime.is_running(session.pid):
            raise RuntimeError(f"Session {session_id} is already running (pid {session.pid})")

        workspace = Path(session.workspace_dir)
        workspace.mkdir(parents=True, exist_ok=True)

        logs_dir = self.config.storage.logs_dir
        proc = runtime.launch(
            config=self.config.runtime,
            workspace_dir=workspace,
            logs_dir=logs_dir,
            prompt=prompt,
            session_id=session_id,
        )

        now = datetime.now(timezone.utc).isoformat()
        conn = self._conn()
        try:
            conn.execute(
                """UPDATE sessions
                   SET status='running', pid=?, prompt=?, updated_at=?
                   WHERE id=?""",
                (proc.pid, prompt, now, session_id),
            )
            conn.execute(
                """INSERT INTO session_runs (session_id, prompt, status)
                   VALUES (?, ?, 'running')""",
                (session_id, prompt),
            )
            conn.commit()
            return self.get(session_id)
        finally:
            conn.close()

    def stop(self, session_id: str) -> Session:
        """Stop a running session."""
        session = self.get(session_id)

        if session.pid and runtime.is_running(session.pid):
            runtime.stop(session.pid)

        now = datetime.now(timezone.utc).isoformat()
        conn = self._conn()
        try:
            conn.execute(
                "UPDATE sessions SET status='stopped', updated_at=? WHERE id=?",
                (now, session_id),
            )
            conn.execute(
                """UPDATE session_runs SET status='stopped', finished_at=?
                   WHERE session_id=? AND status='running'""",
                (now, session_id),
            )
            conn.commit()
            return self.get(session_id)
        finally:
            conn.close()

    def refresh_status(self, session_id: str) -> Session:
        """Check if a running session has finished and update status."""
        session = self.get(session_id)

        if session.status != "running":
            return session

        if session.pid and not runtime.is_running(session.pid):
            now = datetime.now(timezone.utc).isoformat()
            conn = self._conn()
            try:
                conn.execute(
                    "UPDATE sessions SET status='completed', updated_at=? WHERE id=?",
                    (now, session_id),
                )
                conn.execute(
                    """UPDATE session_runs SET status='completed', finished_at=?
                       WHERE session_id=? AND status='running'""",
                    (now, session_id),
                )
                conn.commit()
                return self.get(session_id)
            finally:
                conn.close()

        return session

    def get_logs(self, session_id: str, stream: str = "stdout", tail: int = 100) -> str:
        """Read log output for a session."""
        logs_dir = self.config.storage.logs_dir
        log_file = logs_dir / f"{session_id}.{stream}.log"
        if not log_file.exists():
            return ""
        lines = log_file.read_text().splitlines()
        return "\n".join(lines[-tail:])

    def delete(self, session_id: str) -> None:
        """Delete a session and its data."""
        session = self.get(session_id)

        if session.pid and runtime.is_running(session.pid):
            runtime.stop(session.pid)

        conn = self._conn()
        try:
            conn.execute("DELETE FROM session_runs WHERE session_id=?", (session_id,))
            conn.execute("DELETE FROM sessions WHERE id=?", (session_id,))
            conn.commit()
        finally:
            conn.close()
