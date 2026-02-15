#!/bin/bash
# TeleCoder Chat Sandbox Setup
#
# This script is run via `docker exec` inside a persistent chat container.
# It performs the one-time setup: cloning the repo, installing deps, and
# creating the working branch. Unlike entrypoint.sh, it does NOT run the
# coding agent or commit/push — those are handled per-message by the server.
#
# Required environment variables (set by the server at container start):
#   TELECODER_REPO     - "owner/repo"
#   TELECODER_BRANCH   - git branch name
#   GITHUB_TOKEN    - GitHub access token

set -euo pipefail

# --- Helpers ---
emit_status() { echo "###TELECODER_STATUS### $1"; }
emit_error()  { echo "###TELECODER_ERROR### $1"; }

# --- Validate required environment ---
: "${TELECODER_REPO:?TELECODER_REPO is required}"
: "${TELECODER_BRANCH:?TELECODER_BRANCH is required}"
: "${GITHUB_TOKEN:?GITHUB_TOKEN is required}"

# --- Clone repository ---
emit_status "Cloning ${TELECODER_REPO}..."

CLONE_URL="https://x-access-token:${GITHUB_TOKEN}@github.com/${TELECODER_REPO}.git"
git clone --depth=50 "${CLONE_URL}" /workspace/repo 2>&1
cd /workspace/repo

# Configure git identity.
git config user.name "TeleCoder"
git config user.email "telecoder@users.noreply.github.com"

# Create the working branch.
git checkout -b "${TELECODER_BRANCH}"

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
    if [ -n "${TELECODER_AGENT_MODEL:-}" ]; then
        cat > /workspace/repo/opencode.json <<CFGEOF
{
  "\$schema": "https://opencode.ai/config.json",
  "model": "${TELECODER_AGENT_MODEL}"
}
CFGEOF
        emit_status "Agent: OpenCode (${TELECODER_AGENT_MODEL})"
    else
        emit_status "Agent: OpenCode"
    fi
elif [ -n "${OPENAI_API_KEY:-}" ]; then
    emit_status "Agent: Codex CLI"
fi

emit_status "Ready"
