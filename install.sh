#!/usr/bin/env bash
set -euo pipefail

# TeleCoder Installer
# Usage: curl -fsSL https://your-domain/install.sh | bash

TELECODER_USER="telecoder"
TELECODER_DATA="/var/lib/telecoder"
TELECODER_CONFIG="/etc/telecoder"

echo "=== TeleCoder Installer ==="
echo ""

# Check for root
if [ "$(id -u)" -ne 0 ]; then
    echo "Please run as root (or with sudo)."
    exit 1
fi

# Check for Python 3.11+
if ! command -v python3 &>/dev/null; then
    echo "Python 3 not found. Installing..."
    apt-get update -qq && apt-get install -y -qq python3 python3-pip python3-venv git
fi

PYTHON_VERSION=$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')
PYTHON_MAJOR=$(echo "$PYTHON_VERSION" | cut -d. -f1)
PYTHON_MINOR=$(echo "$PYTHON_VERSION" | cut -d. -f2)

if [ "$PYTHON_MAJOR" -lt 3 ] || { [ "$PYTHON_MAJOR" -eq 3 ] && [ "$PYTHON_MINOR" -lt 11 ]; }; then
    echo "Python 3.11+ required. Found: $PYTHON_VERSION"
    exit 1
fi

echo "Python $PYTHON_VERSION found."

# Create telecoder user if not exists
if ! id "$TELECODER_USER" &>/dev/null; then
    useradd --system --home-dir "$TELECODER_DATA" --shell /usr/sbin/nologin "$TELECODER_USER"
    echo "Created user: $TELECODER_USER"
fi

# Create directories
mkdir -p "$TELECODER_DATA"/{workspaces,logs}
mkdir -p "$TELECODER_CONFIG"

# Install TeleCoder
echo "Installing TeleCoder..."
pip3 install --quiet telecoder 2>/dev/null || {
    # If not published to PyPI, install from local source
    if [ -f "pyproject.toml" ]; then
        pip3 install --quiet .
    else
        echo "Could not install telecoder. Ensure pyproject.toml is present or the package is on PyPI."
        exit 1
    fi
}

# Copy example config if no config exists
if [ ! -f "$TELECODER_CONFIG/config.toml" ]; then
    if [ -f "config.example.toml" ]; then
        cp config.example.toml "$TELECODER_CONFIG/config.toml"
    else
        # Write a minimal config
        cat > "$TELECODER_CONFIG/config.toml" <<'TOML'
[service]
host = "127.0.0.1"
port = 7830

[runtime]
type = "claude-code"

[runtime.env]
# ANTHROPIC_API_KEY = "sk-ant-..."

[storage]
data_dir = "/var/lib/telecoder"
db_path = "telecoder.db"

[git]
branch_prefix = "telecoder/"
auto_push = false
TOML
    fi
    echo "Config written to $TELECODER_CONFIG/config.toml"
fi

# Set permissions
chown -R "$TELECODER_USER:$TELECODER_USER" "$TELECODER_DATA"
chown -R "$TELECODER_USER:$TELECODER_USER" "$TELECODER_CONFIG"

# Install systemd service
if [ -d /etc/systemd/system ]; then
    cat > /etc/systemd/system/telecoder.service <<EOF
[Unit]
Description=TeleCoder - Remote Coding Agent Service
After=network.target

[Service]
Type=simple
User=$TELECODER_USER
Group=$TELECODER_USER
WorkingDirectory=$TELECODER_DATA
ExecStart=$(command -v telecoder) --config $TELECODER_CONFIG/config.toml service start
Restart=on-failure
RestartSec=5
Environment=TELECODER_CONFIG=$TELECODER_CONFIG/config.toml

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    echo "Systemd service installed."
fi

echo ""
echo "=== TeleCoder installed ==="
echo ""
echo "Next steps:"
echo "  1. Edit $TELECODER_CONFIG/config.toml"
echo "     - Add your ANTHROPIC_API_KEY in [runtime.env]"
echo "  2. Start the service:"
echo "     sudo systemctl start telecoder"
echo "     sudo systemctl enable telecoder"
echo "  3. Create your first session:"
echo "     telecoder session create --repo-url https://github.com/you/repo"
echo "     telecoder session run <session-id> 'fix the failing tests'"
echo ""
