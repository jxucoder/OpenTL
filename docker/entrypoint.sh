#!/bin/bash
# TeleCoder Sandbox Entrypoint
#
# This script runs inside the Docker sandbox container. It:
#   1. Clones the repository
#   2. Runs a coding agent (OpenCode or Codex CLI)
#   3. Commits and pushes changes
#   4. Signals completion back to the server
#
# Communication protocol:
#   Lines prefixed with ###TELECODER_STATUS### are status updates
#   Lines prefixed with ###TELECODER_ERROR###  are error messages
#   Lines prefixed with ###TELECODER_DONE###   signal completion
#   All other lines are agent output

set -euo pipefail

# --- Helpers ---
emit_status() { echo "###TELECODER_STATUS### $1"; }
emit_error()  { echo "###TELECODER_ERROR### $1"; }
emit_done()   { echo "###TELECODER_DONE### $1"; }

# --- Validate required environment ---
: "${TELECODER_REPO:?TELECODER_REPO is required}"
: "${TELECODER_PROMPT:?TELECODER_PROMPT is required}"
: "${TELECODER_BRANCH:?TELECODER_BRANCH is required}"
: "${GITHUB_TOKEN:?GITHUB_TOKEN is required}"

# --- Clone repository ---
emit_status "Cloning ${TELECODER_REPO}..."

CLONE_URL="https://x-access-token:${GITHUB_TOKEN}@github.com/${TELECODER_REPO}.git"
git clone --depth=1 "${CLONE_URL}" /workspace/repo 2>&1
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

# --- Select and run coding agent ---
# Agent selection priority:
#   1. ANTHROPIC_API_KEY set → OpenCode (auto-detects model, or override via TELECODER_AGENT_MODEL)
#   2. OPENAI_API_KEY set   → Codex CLI
#   3. Neither              → error

if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
    # --- OpenCode ---
    MODEL_ARGS=""
    if [ -n "${TELECODER_AGENT_MODEL:-}" ]; then
        MODEL_ARGS="-m ${TELECODER_AGENT_MODEL}"
        cat > /workspace/repo/opencode.json <<CFGEOF
{
  "\$schema": "https://opencode.ai/config.json",
  "model": "${TELECODER_AGENT_MODEL}"
}
CFGEOF
        emit_status "Running OpenCode (${TELECODER_AGENT_MODEL})..."
    else
        emit_status "Running OpenCode..."
    fi

    opencode run ${MODEL_ARGS} "${TELECODER_PROMPT}" 2>&1 || {
        EXIT_CODE=$?
        if [ $EXIT_CODE -ne 0 ]; then
            emit_error "OpenCode agent exited with code ${EXIT_CODE}"
            exit $EXIT_CODE
        fi
    }

elif [ -n "${OPENAI_API_KEY:-}" ]; then
    # --- Codex CLI ---
    emit_status "Running Codex CLI..."
    codex exec \
        --full-auto \
        --ephemeral \
        "${TELECODER_PROMPT}" 2>&1 || {
        EXIT_CODE=$?
        if [ $EXIT_CODE -ne 0 ]; then
            emit_error "Codex agent exited with code ${EXIT_CODE}"
            exit $EXIT_CODE
        fi
    }

else
    emit_error "No LLM API key set. Set ANTHROPIC_API_KEY or OPENAI_API_KEY."
    exit 1
fi

emit_status "Agent finished"

# --- Commit changes ---
emit_status "Committing changes..."

git add -A

if git diff --cached --quiet; then
    emit_error "No changes were made by the agent"
    exit 1
fi

# Create a meaningful commit message.
COMMIT_MSG="telecoder: ${TELECODER_PROMPT}"
# Truncate to 72 chars for git subject line.
if [ ${#COMMIT_MSG} -gt 72 ]; then
    COMMIT_MSG="${COMMIT_MSG:0:69}..."
fi

git commit -m "${COMMIT_MSG}" 2>&1

emit_status "Changes committed"

# --- Push branch ---
emit_status "Pushing branch ${TELECODER_BRANCH}..."

git push origin "${TELECODER_BRANCH}" 2>&1

emit_status "Branch pushed successfully"

# --- Signal completion ---
emit_done "${TELECODER_BRANCH}"
