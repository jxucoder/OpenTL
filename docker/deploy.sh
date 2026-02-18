#!/usr/bin/env bash
#
# TeleCoder Deploy Script
#
# One-command deployment for any fresh Ubuntu server (22.04+):
#
#   git clone https://github.com/jxucoder/TeleCoder.git
#   cd TeleCoder
#   ./deploy.sh
#
# The script will:
#   1. Wait for apt locks to clear (fresh server auto-updates)
#   2. Install Docker and Go if needed
#   3. Build the telecoder CLI
#   4. Run interactive token setup (or read from existing config)
#   5. Build the sandbox Docker image
#   6. Start the server via Docker Compose
#
# For CI / non-interactive use, set tokens before running:
#   export GITHUB_TOKEN=ghp_xxx
#   export ANTHROPIC_API_KEY=sk-ant-xxx
#   ./deploy.sh --non-interactive
#
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[x]${NC} $1"; exit 1; }
step()  { echo -e "\n${BOLD}── $1${NC}"; }

INTERACTIVE=true
if [[ "${1:-}" == "--non-interactive" ]] || [[ "${1:-}" == "-y" ]]; then
    INTERACTIVE=false
fi

GO_VERSION="1.25.7"

echo ""
echo "  ╔═══════════════════════════════════╗"
echo "  ║   TeleCoder Deploy                   ║"
echo "  ║   Send a task, get a PR.          ║"
echo "  ╚═══════════════════════════════════╝"
echo ""

# --- 1. Wait for apt locks (fresh Ubuntu servers run auto-updates) ---

step "Step 1/6 — Checking system"

# Wait up to 120 seconds for apt locks to clear.
WAIT_SECS=0
while fuser /var/lib/dpkg/lock-frontend &>/dev/null 2>&1 || \
      fuser /var/lib/apt/lists/lock &>/dev/null 2>&1; do
    if [ $WAIT_SECS -eq 0 ]; then
        info "Waiting for system updates to finish (this is normal on fresh servers)..."
    fi
    sleep 5
    WAIT_SECS=$((WAIT_SECS + 5))
    if [ $WAIT_SECS -ge 120 ]; then
        warn "apt lock held for over 2 minutes. Continuing anyway..."
        break
    fi
done

if [ $WAIT_SECS -gt 0 ]; then
    info "System ready (waited ${WAIT_SECS}s)"
else
    info "System ready"
fi

# --- 2. Install Docker ---

step "Step 2/6 — Docker"

if command -v docker &>/dev/null; then
    info "Docker already installed ($(docker --version | head -1))"
else
    info "Installing Docker..."
    curl -fsSL https://get.docker.com | sh
    info "Docker installed"
fi

# Make sure Docker daemon is running.
if ! docker info &>/dev/null 2>&1; then
    if command -v systemctl &>/dev/null; then
        info "Starting Docker daemon..."
        sudo systemctl start docker
        sudo systemctl enable docker
    else
        error "Docker is installed but the daemon is not running. Start it manually."
    fi
fi

# --- 3. Install Go and build ---

step "Step 3/6 — Building TeleCoder"

if ! command -v go &>/dev/null; then
    ARCH=$(dpkg --print-architecture 2>/dev/null || uname -m)
    case "$ARCH" in
        amd64|x86_64) GOARCH="amd64" ;;
        arm64|aarch64) GOARCH="arm64" ;;
        *) error "Unsupported architecture: $ARCH" ;;
    esac

    info "Installing Go ${GO_VERSION} (${GOARCH})..."
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" | tar -C /usr/local -xzf -
    export PATH="/usr/local/go/bin:$PATH"

    # Make it permanent.
    if ! grep -q '/usr/local/go/bin' ~/.bashrc 2>/dev/null; then
        echo 'export PATH="/usr/local/go/bin:$PATH"' >> ~/.bashrc
    fi
    info "Go installed"
else
    info "Go already installed ($(go version))"
fi

# Install make if missing.
if ! command -v make &>/dev/null; then
    info "Installing make..."
    apt-get update -qq && apt-get install -y -qq make >/dev/null 2>&1
fi

info "Building telecoder binary..."
make build
info "Build complete"

# --- 4. Configure tokens ---

step "Step 4/6 — Token Setup"

if [ "$INTERACTIVE" = true ]; then
    # Check if tokens are already configured.
    if ./bin/telecoder config show 2>/dev/null | grep -q "GITHUB_TOKEN.*not set"; then
        info "Running interactive setup..."
        ./bin/telecoder config setup
    else
        info "Tokens already configured"
        ./bin/telecoder config show
        echo ""
        read -p "  Re-run setup? [y/N] " -r
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            ./bin/telecoder config setup
        fi
    fi
else
    # Non-interactive: tokens must come from environment or existing config.
    # Write env vars to config file if they're set.
    if [ -n "${GITHUB_TOKEN:-}" ]; then
        ./bin/telecoder config set GITHUB_TOKEN "$GITHUB_TOKEN"
    fi
    if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
        ./bin/telecoder config set ANTHROPIC_API_KEY "$ANTHROPIC_API_KEY"
    fi
    if [ -n "${OPENAI_API_KEY:-}" ]; then
        ./bin/telecoder config set OPENAI_API_KEY "$OPENAI_API_KEY"
    fi
    if [ -n "${TELEGRAM_BOT_TOKEN:-}" ]; then
        ./bin/telecoder config set TELEGRAM_BOT_TOKEN "$TELEGRAM_BOT_TOKEN"
    fi
    if [ -n "${SLACK_BOT_TOKEN:-}" ]; then
        ./bin/telecoder config set SLACK_BOT_TOKEN "$SLACK_BOT_TOKEN"
    fi
    if [ -n "${SLACK_APP_TOKEN:-}" ]; then
        ./bin/telecoder config set SLACK_APP_TOKEN "$SLACK_APP_TOKEN"
    fi
    info "Tokens configured from environment"
fi

# Validate that minimum tokens are present.
if ./bin/telecoder config show 2>/dev/null | grep -q "GITHUB_TOKEN.*not set"; then
    error "GITHUB_TOKEN is required. Run: ./bin/telecoder config setup"
fi

# Generate .env for Docker Compose (reads from ~/.telecoder/config.env).
CONFIG_FILE="$HOME/.telecoder/config.env"
if [ -f "$CONFIG_FILE" ]; then
    cp "$CONFIG_FILE" .env
    info "Generated .env from config"
fi

# --- 5. Build sandbox image ---

step "Step 5/6 — Building Sandbox Image"

info "This takes 3-5 minutes on first run..."
make sandbox-image
info "Sandbox image built"

# --- 6. Start server ---

step "Step 6/6 — Starting Server"

docker compose -f docker/compose.yml --env-file .env up -d --build

# Wait for server to start.
info "Waiting for server..."
for i in $(seq 1 15); do
    if curl -sf http://localhost:7080/api/sessions >/dev/null 2>&1; then
        break
    fi
    sleep 2
done

# --- Done ---

SERVER_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "localhost")

if curl -sf http://localhost:7080/api/sessions >/dev/null 2>&1; then
    echo ""
    echo -e "  ${GREEN}${BOLD}TeleCoder is running!${NC}"
    echo ""
    echo "  Server:   http://${SERVER_IP}:7080"
    echo ""
    echo "  Run a task from this server:"
    echo "    ./bin/telecoder run \"your task\" --repo owner/repo"
    echo ""
    echo "  Run a task from your laptop:"
    echo "    telecoder --server http://${SERVER_IP}:7080 run \"your task\" --repo owner/repo"
    echo ""

    # Check for bot integrations.
    if grep -q "TELEGRAM_BOT_TOKEN=" .env 2>/dev/null && \
       ! grep -q "TELEGRAM_BOT_TOKEN=$" .env 2>/dev/null; then
        echo "  Telegram:  Message your bot from your phone!"
    fi
    if grep -q "SLACK_BOT_TOKEN=" .env 2>/dev/null && \
       ! grep -q "SLACK_BOT_TOKEN=$" .env 2>/dev/null; then
        echo "  Slack:     @TeleCoder in any channel"
    fi

    echo ""
    echo "  Manage:"
    echo "    Logs:     docker compose -f docker/compose.yml logs -f"
    echo "    Stop:     docker compose -f docker/compose.yml down"
    echo "    Update:   git pull && ./deploy.sh"
    echo ""
else
    warn "Server may not have started. Check logs:"
    echo "    docker compose -f docker/compose.yml logs"
fi
