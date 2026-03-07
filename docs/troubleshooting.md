# Troubleshooting

## Common Issues

### "Claude Code CLI not found"

Claude Code is not installed or not in PATH.

```bash
# Install it
npm install -g @anthropic-ai/claude-code

# Check it's in PATH
which claude
```

If installed but not found, set the binary path in config:

```toml
[runtime]
binary = "/usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude"
```

### Session starts but produces no output

Check stderr logs:

```bash
telecoder session logs <session-id> --stream stderr
```

Common causes:
- Invalid or missing API key
- Network issues on the VPS
- Runtime binary not working

### "Permission denied" errors

Check file ownership:

```bash
ls -la /var/lib/telecoder/
```

Fix permissions:

```bash
sudo chown -R telecoder:telecoder /var/lib/telecoder/
```

### Session stuck in "running" status

The process may have died. Refresh the status:

```bash
telecoder session inspect <session-id>
```

If the PID is no longer running, the status will update to "completed".

### Git clone fails

Check that the telecoder user has git access:

```bash
sudo -u telecoder git clone <your-repo-url> /tmp/test
```

See the [Git Credentials Guide](git-credentials.md).

### Service won't start

Check the systemd logs:

```bash
sudo journalctl -u telecoder -n 50
```

Common causes:
- Config file not found
- Database directory permissions
- Missing Python dependencies

### Web UI not accessible

Check the service is running:

```bash
sudo systemctl status telecoder
```

Check the port is open:

```bash
curl http://127.0.0.1:7830/
```

If accessing remotely, check your firewall:

```bash
sudo ufw status
sudo ufw allow 7830/tcp
```

## Getting Help

Check logs:

```bash
# Service logs
sudo journalctl -u telecoder -f

# Session stdout
telecoder session logs <id>

# Session stderr
telecoder session logs <id> --stream stderr
```

## Resetting

To start fresh:

```bash
sudo systemctl stop telecoder
sudo rm -rf /var/lib/telecoder/*
telecoder init
sudo systemctl start telecoder
```
