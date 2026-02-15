#!/usr/bin/env bash
#
# OpenTL Deploy Script
#
# Run on any fresh Ubuntu/macOS machine:
#   git clone https://github.com/jxucoder/opentl.git
#   cd opentl
#   ./deploy.sh
#
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info()  { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[x]${NC} $1"; exit 1; }

REQUIRED_GO_VERSION="1.25.7"
CONFIG_FILE="$HOME/.opentl/config.env"

# ──────────────────────────────────────────────────────────────
# Helper: compare version strings.  Returns 0 if $1 >= $2.
# ──────────────────────────────────────────────────────────────
version_ge() {
    # Returns 0 (true) if $1 >= $2 using sort -V.
    [ "$(printf '%s\n%s' "$1" "$2" | sort -V | head -n1)" = "$2" ]
}

# ──────────────────────────────────────────────────────────────
# 1. Check / Install Go
# ──────────────────────────────────────────────────────────────
check_go() {
    if command -v go &> /dev/null; then
        local current
        current="$(go version | grep -oP 'go\K[0-9]+\.[0-9]+(\.[0-9]+)?' || true)"
        if [ -n "$current" ] && version_ge "$current" "$REQUIRED_GO_VERSION"; then
            info "Go $current found (>= $REQUIRED_GO_VERSION)"
            return 0
        else
            warn "Go $current found but $REQUIRED_GO_VERSION or later is required"
            return 1
        fi
    else
        warn "Go is not installed"
        return 1
    fi
}

install_go() {
    local go_version="$REQUIRED_GO_VERSION"
    local os_name arch tarball url

    os_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$arch" in
        x86_64)  arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *) error "Unsupported architecture: $arch" ;;
    esac

    case "$os_name" in
        linux|darwin) ;;
        *) error "Unsupported operating system: $os_name" ;;
    esac

    tarball="go${go_version}.${os_name}-${arch}.tar.gz"
    url="https://go.dev/dl/${tarball}"

    info "Downloading Go $go_version for ${os_name}/${arch}..."

    local tmp
    tmp="$(mktemp -d)"
    if ! curl -fsSL -o "${tmp}/${tarball}" "$url"; then
        rm -rf "$tmp"
        error "Failed to download Go from $url"
    fi

    info "Installing Go to /usr/local/go..."
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "${tmp}/${tarball}"
    rm -rf "$tmp"

    # Add to PATH for the rest of this script.
    export PATH="/usr/local/go/bin:$PATH"

    if ! command -v go &> /dev/null; then
        error "Go installation failed — 'go' not found in PATH"
    fi

    local installed
    installed="$(go version | grep -oP 'go\K[0-9]+\.[0-9]+(\.[0-9]+)?' || true)"
    info "Go $installed installed successfully"

    # Advise user to persist PATH if it is not already there.
    if ! grep -q '/usr/local/go/bin' "$HOME/.profile" 2>/dev/null &&
       ! grep -q '/usr/local/go/bin' "$HOME/.bashrc" 2>/dev/null; then
        warn "Add Go to your PATH permanently by adding this to ~/.profile or ~/.bashrc:"
        echo '  export PATH="/usr/local/go/bin:$PATH"'
    fi
}

# ──────────────────────────────────────────────────────────────
# 2. Build the opentl binary
# ──────────────────────────────────────────────────────────────
build_opentl() {
    info "Building opentl binary..."
    go build -o bin/opentl ./cmd/opentl
    if [ ! -x bin/opentl ]; then
        error "Build failed — bin/opentl not found"
    fi
    info "opentl binary built at bin/opentl"
}

# ──────────────────────────────────────────────────────────────
# 3. Run opentl config setup (interactive wizard)
# ──────────────────────────────────────────────────────────────
setup_config() {
    if [ -f "$CONFIG_FILE" ]; then
        info "Existing configuration found at $CONFIG_FILE"
        # Source the config so we can check required values.
        set -a
        source "$CONFIG_FILE"
        set +a

        # Quick validation: do we have the minimum required keys?
        if [ -n "${GITHUB_TOKEN:-}" ] && { [ -n "${ANTHROPIC_API_KEY:-}" ] || [ -n "${OPENAI_API_KEY:-}" ]; }; then
            info "Configuration looks valid. Skipping setup wizard."
            return 0
        else
            warn "Configuration is incomplete. Running setup wizard..."
        fi
    else
        info "No configuration found. Running setup wizard..."
    fi

    ./bin/opentl config setup

    # Verify the config file was created.
    if [ ! -f "$CONFIG_FILE" ]; then
        error "Config setup did not create $CONFIG_FILE. Cannot proceed."
    fi
}

# ──────────────────────────────────────────────────────────────
# 4. Load config into environment (for Docker Compose)
# ──────────────────────────────────────────────────────────────
load_config() {
    if [ -f "$CONFIG_FILE" ]; then
        set -a
        source "$CONFIG_FILE"
        set +a
    fi

    # Final validation.
    if [ -z "${GITHUB_TOKEN:-}" ]; then
        error "GITHUB_TOKEN is not configured. Run: ./bin/opentl config setup"
    fi
    if [ -z "${ANTHROPIC_API_KEY:-}" ] && [ -z "${OPENAI_API_KEY:-}" ]; then
        error "At least one of ANTHROPIC_API_KEY or OPENAI_API_KEY is required. Run: ./bin/opentl config setup"
    fi

    if [ -n "${TELEGRAM_BOT_TOKEN:-}" ]; then
        info "Telegram bot token found"
    else
        warn "No TELEGRAM_BOT_TOKEN set — Telegram bot will be disabled"
    fi
}

# ──────────────────────────────────────────────────────────────
# 5. Install Docker if needed
# ──────────────────────────────────────────────────────────────
check_docker() {
    if ! command -v docker &> /dev/null; then
        info "Installing Docker..."
        curl -fsSL https://get.docker.com | sh
        sudo usermod -aG docker "$USER"
        info "Docker installed. You may need to log out and back in for group changes."
        # Use sudo for the rest of this script since the group change hasn't taken effect.
        DOCKER="sudo docker"
        COMPOSE="sudo docker compose"
    else
        info "Docker already installed"
        DOCKER="docker"
        COMPOSE="docker compose"
    fi

    # Ensure Docker is running.
    if ! $DOCKER info &> /dev/null; then
        info "Starting Docker..."
        sudo systemctl start docker
        sudo systemctl enable docker
    fi
}

# ──────────────────────────────────────────────────────────────
# 6. Build sandbox image & deploy
# ──────────────────────────────────────────────────────────────
deploy_services() {
    info "Building sandbox image (this takes a few minutes on first run)..."
    $DOCKER build -f docker/base.Dockerfile -t opentl-sandbox .

    info "Starting OpenTL..."
    $COMPOSE -f docker/compose.yml up -d --build
}

# ──────────────────────────────────────────────────────────────
# 7. Verify deployment
# ──────────────────────────────────────────────────────────────
verify() {
    echo ""
    info "Waiting for server to start..."
    sleep 3

    if $DOCKER ps | grep -q opentl; then
        echo ""
        info "OpenTL is running!"
        echo ""
        echo "  Server:    http://$(hostname -I 2>/dev/null | awk '{print $1}' || echo 'localhost'):7080"
        echo "  Health:    http://$(hostname -I 2>/dev/null | awk '{print $1}' || echo 'localhost'):7080/health"
        echo ""
        if [ -n "${TELEGRAM_BOT_TOKEN:-}" ]; then
            echo "  Telegram:  Open your bot and send a message!"
            echo ""
        fi
        echo "  Logs:      $COMPOSE -f docker/compose.yml logs -f"
        echo "  Stop:      $COMPOSE -f docker/compose.yml down"
        echo "  Restart:   $COMPOSE -f docker/compose.yml restart"
        echo ""
    else
        warn "Container may not have started. Check logs:"
        echo "  $COMPOSE -f docker/compose.yml logs"
    fi
}

# ══════════════════════════════════════════════════════════════
# Main
# ══════════════════════════════════════════════════════════════

echo ""
echo "  ╔═══════════════════════════════════╗"
echo "  ║   OpenTL Deploy                   ║"
echo "  ║   Send a task, get a PR.          ║"
echo "  ╚═══════════════════════════════════╝"
echo ""

# Step 1: Ensure Go is available.
if ! check_go; then
    install_go
fi

# Step 2: Build the opentl binary.
build_opentl

# Step 3: Run interactive config setup (or skip if already configured).
setup_config

# Step 4: Load config into environment for Docker Compose.
load_config

# Step 5: Ensure Docker is available.
check_docker

# Step 6: Build images and start services.
deploy_services

# Step 7: Verify everything is running.
verify
