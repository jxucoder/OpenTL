# VPS Setup Guide

## Recommended VPS

- **Provider**: Any (DigitalOcean, Hetzner, Linode, AWS Lightsail, etc.)
- **OS**: Ubuntu 22.04 or 24.04
- **RAM**: 2GB minimum, 4GB recommended
- **Storage**: 20GB+ (depends on repo sizes)

## Initial VPS Setup

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install dependencies
sudo apt install -y python3 python3-pip python3-venv git curl

# Verify Python version (needs 3.11+)
python3 --version
```

## Install Node.js (for Claude Code)

```bash
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
sudo apt install -y nodejs
```

## Install Claude Code CLI

```bash
npm install -g @anthropic-ai/claude-code
```

Verify it works:

```bash
claude --version
```

## Install TeleCoder

```bash
cd /opt
git clone https://github.com/your-org/telecoder.git
cd telecoder
sudo bash install.sh
```

## Firewall

If you want to use the web UI remotely:

```bash
sudo ufw allow 7830/tcp
```

For production, put it behind a reverse proxy with HTTPS.

## Reverse Proxy (Optional)

Example nginx config:

```nginx
server {
    listen 443 ssl;
    server_name telecoder.yourdomain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:7830;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```
