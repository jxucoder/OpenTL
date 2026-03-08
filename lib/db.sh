#!/usr/bin/env bash
# Database helpers — thin wrappers around sqlite3 CLI.
# All user input goes through parameterized queries to avoid injection.

TC_DB="${TELECODER_DATA}/telecoder.db"

db_init() {
    sqlite3 "$TC_DB" <<'SQL'
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    repo_url    TEXT,
    repo_path   TEXT,
    workspace   TEXT NOT NULL,
    branch      TEXT,
    status      TEXT NOT NULL DEFAULT 'created',
    prompt      TEXT,
    test_cmd    TEXT,
    lint_cmd    TEXT,
    pid         INTEGER,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
SQL
}

db_query() {
    sqlite3 -separator '|' "$TC_DB" "$1"
}

# Safe insert/update: write values via .param to avoid SQL injection.
# Usage: db_param_exec "INSERT INTO t (a,b) VALUES (:a,:b)" a "val1" b "val2"
db_param_exec() {
    local sql="$1"; shift
    local param_cmds=""
    while [ $# -ge 2 ]; do
        local name="$1" value="$2"; shift 2
        # .param set binds a named parameter safely (no escaping needed)
        param_cmds="${param_cmds}.param set :${name} '$(printf '%s' "$value" | sed "s/'/''/g")'
"
    done
    sqlite3 "$TC_DB" <<EOF
${param_cmds}${sql};
EOF
}

db_exec() {
    sqlite3 "$TC_DB" "$1"
}
