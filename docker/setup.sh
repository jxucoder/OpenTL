#!/bin/bash
# OpenTL Chat Sandbox Setup
#
# This script is run via `docker exec` inside a persistent chat container.
# It performs the one-time setup: cloning the repo, installing deps, and
# creating the working branch. Unlike entrypoint.sh, it does NOT run the
# coding agent or commit/push — those are handled per-message by the server.
#
# Required environment variables (set by the server at container start):
#   OPENTL_REPO     - "owner/repo"
#   OPENTL_BRANCH   - git branch name
#   GITHUB_TOKEN    - GitHub access token

set -euo pipefail

# --- Helpers ---
emit_status() { echo "###OPENTL_STATUS### $1"; }
emit_error()  { echo "###OPENTL_ERROR### $1"; }

# --- Validate required environment ---
: "${OPENTL_REPO:?OPENTL_REPO is required}"
: "${OPENTL_BRANCH:?OPENTL_BRANCH is required}"
: "${GITHUB_TOKEN:?GITHUB_TOKEN is required}"

# --- Clone repository ---
emit_status "Cloning ${OPENTL_REPO}..."

CLONE_URL="https://x-access-token:${GITHUB_TOKEN}@github.com/${OPENTL_REPO}.git"
git clone --depth=50 "${CLONE_URL}" /workspace/repo 2>&1
cd /workspace/repo

# Configure git identity.
git config user.name "OpenTL"
git config user.email "opentl@users.noreply.github.com"

# Create the working branch.
git checkout -b "${OPENTL_BRANCH}"

emit_status "Repository cloned successfully"

# --- Install project dependencies (best-effort) ---
emit_status "Detecting and installing dependencies..."

if [ -f "package-lock.json" ]; then
    npm ci 2>&1 || npm install 2>&1 || true
elif [ -f "pnpm-lock.yaml" ]; then
    pnpm install --frozen-lockfile 2>&1 || pnpm install 2>&1 || true
elif [ -f "yarn.lock" ]; then
    if command -v yarn >/dev/null 2>&1; then
        yarn install --frozen-lockfile 2>&1 || true
    else
        npm install 2>&1 || true
    fi
elif [ -f "requirements.txt" ]; then
    pip install -r requirements.txt 2>&1 || true
elif [ -f "go.mod" ]; then
    go mod download 2>&1 || true
fi

emit_status "Dependencies installed"

# --- Configure coding agent ---
emit_status "Configuring coding agent..."

if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
    # OpenCode + Claude Opus 4.6 config.
    cat > /workspace/repo/opencode.json <<CFGEOF
{
  "\$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-opus-4-6"
}
CFGEOF
    emit_status "Agent: OpenCode (Claude Opus 4.6)"
elif [ -n "${OPENAI_API_KEY:-}" ]; then
    emit_status "Agent: Codex CLI"
fi

emit_status "Ready"
