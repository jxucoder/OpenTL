"""SQLite database for session metadata."""

from __future__ import annotations

import sqlite3
from pathlib import Path

SCHEMA = """
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    repo_url TEXT,
    repo_path TEXT,
    workspace_dir TEXT NOT NULL,
    runtime_type TEXT NOT NULL DEFAULT 'claude-code',
    status TEXT NOT NULL DEFAULT 'created',
    branch TEXT,
    prompt TEXT,
    output_summary TEXT,
    test_command TEXT,
    lint_command TEXT,
    test_result TEXT,
    lint_result TEXT,
    pid INTEGER,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS session_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    prompt TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    output_summary TEXT,
    started_at TEXT NOT NULL DEFAULT (datetime('now')),
    finished_at TEXT
);
"""


def init_db(db_path: Path) -> sqlite3.Connection:
    """Initialize the database and return a connection."""
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA foreign_keys=ON")
    conn.executescript(SCHEMA)
    return conn


def get_connection(db_path: Path) -> sqlite3.Connection:
    """Get a database connection."""
    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA foreign_keys=ON")
    return conn
