# Deploy TeleCoder

Get a VPS, run one script, message your bot from your phone.

## Quick Start

### 1. Get a Server

Any VPS with 2GB+ RAM running Ubuntu 22.04+:

| Provider | Spec | Price | Link |
|---|---|---|---|
| Hetzner | 2 vCPU, 4GB RAM | ~$4.5/mo | [hetzner.com/cloud](https://www.hetzner.com/cloud) |
| DigitalOcean | 1 vCPU, 2GB RAM | $12/mo | [digitalocean.com](https://www.digitalocean.com) |
| Vultr | 1 vCPU, 2GB RAM | $10/mo | [vultr.com](https://www.vultr.com) |

When creating the server, choose **Ubuntu 24.04**. Add your SSH key.

### 2. SSH In and Deploy

```bash
ssh root@YOUR_SERVER_IP

git clone https://github.com/jxucoder/TeleCoder.git
cd TeleCoder
./deploy.sh
```

That's it. The script handles everything:
- Waits for Ubuntu's auto-updates to finish (common on fresh servers)
- Installs Docker and Go
- Builds the TeleCoder binary
- Runs interactive token setup (`telecoder config setup`)
- Builds the sandbox Docker image
- Starts the server

### 3. Use It

**From the server:**
```bash
./bin/telecoder run "add rate limiting" --repo your-org/your-repo
```

**From your laptop:**
```bash
telecoder --server http://YOUR_SERVER_IP:7080 run "add rate limiting" --repo your-org/your-repo
```

**From Telegram:** message your bot directly (if configured during setup).

**From Slack:** `@TeleCoder add rate limiting --repo your-org/your-repo`

## What You Need Before Deploying

Gather these tokens (~5 minutes total):

1. **GitHub Token** (required) -- [github.com/settings/tokens](https://github.com/settings/tokens) (select `repo` scope)
2. **Anthropic API Key** or **OpenAI API Key** (at least one) -- [console.anthropic.com](https://console.anthropic.com/settings/keys) or [platform.openai.com](https://platform.openai.com/api-keys)
3. **Telegram Bot Token** (optional) -- message [@BotFather](https://t.me/BotFather) on Telegram, send `/newbot`, copy the token

The deploy script will prompt you for each one interactively.

## CI / Non-Interactive Deploy

For automated deployments, set tokens via environment variables:

```bash
export GITHUB_TOKEN=ghp_xxx
export ANTHROPIC_API_KEY=sk-ant-xxx
export TELEGRAM_BOT_TOKEN=123:ABC
./deploy.sh --non-interactive
```

## Managing the Server

```bash
# View logs
docker compose -f docker/compose.yml logs -f

# Restart
docker compose -f docker/compose.yml restart

# Stop
docker compose -f docker/compose.yml down

# Update (pull latest code and redeploy)
git pull && ./deploy.sh
```

## How Much RAM Do I Need?

Each coding task spins up a Docker container. Rough sizing:

| Concurrent Tasks | RAM Needed |
|---|---|
| 1 at a time | 2GB |
| 2-3 at a time | 4-8GB |
| 4+ at a time | 16GB |

For personal use (one task at a time from Telegram), a 2GB server is fine.

## Security Notes

- The server listens on port 7080 (HTTP API). Firewall it if you don't need remote web access.
- The Telegram bot uses **outbound** long-polling -- no inbound ports needed.
- The Slack bot uses **outbound** Socket Mode -- no inbound ports needed.
- Your tokens are stored in `~/.telecoder/config.env` with `0600` permissions.
- The `.env` file is already in `.gitignore`.

### Firewall (optional, if you only use Telegram/Slack)

```bash
ufw allow 22        # SSH
ufw deny 7080       # Block web API from outside
ufw enable
```
