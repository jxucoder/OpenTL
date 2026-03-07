# Quick Start

Get TeleCoder running on your VPS in under 5 minutes.

## Prerequisites

- Ubuntu 22.04+ VPS (2GB+ RAM recommended)
- Python 3.11+
- Git
- Claude Code CLI installed (`npm install -g @anthropic-ai/claude-code`)
- An Anthropic API key

## Install

SSH into your VPS and run:

```bash
sudo bash install.sh
```

Or install manually:

```bash
pip3 install telecoder
telecoder init
```

## Configure

Edit `/etc/telecoder/config.toml`:

```toml
[runtime.env]
ANTHROPIC_API_KEY = "sk-ant-your-key-here"
```

## Start the service

```bash
sudo systemctl start telecoder
sudo systemctl enable telecoder
```

## Create your first session

```bash
# Point TeleCoder at a repo
telecoder session create --repo-url https://github.com/you/your-repo

# Run a task
telecoder session run <session-id> "fix the failing tests in src/auth.py"

# Check status
telecoder session list

# View output
telecoder session logs <session-id>

# See full details
telecoder session inspect <session-id>
```

## Close your laptop

The session keeps running on the VPS. Come back later and check the results:

```bash
telecoder session inspect <session-id>
```

## Stop or resume

```bash
# Stop a running session
telecoder session stop <session-id>

# Resume with a new prompt
telecoder session resume <session-id> "now run the linter and fix warnings"
```

## Web UI

Visit `http://your-vps-ip:7830` to see sessions in a browser.
