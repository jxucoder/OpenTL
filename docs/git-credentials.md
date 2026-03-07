# Git Credential Guide

TeleCoder needs git access to clone and optionally push to your repos.

## SSH Keys (Recommended)

Generate an SSH key for the telecoder user:

```bash
sudo -u telecoder ssh-keygen -t ed25519 -C "telecoder@your-vps"
```

Add the public key to your GitHub/GitLab account as a deploy key.

View the public key:

```bash
sudo cat /var/lib/telecoder/.ssh/id_ed25519.pub
```

## HTTPS with Token

Use a personal access token for HTTPS cloning:

```bash
# Configure git to use the token
sudo -u telecoder git config --global credential.helper store
```

Then create the credential file:

```bash
# /var/lib/telecoder/.git-credentials
https://your-token@github.com
```

## GitHub CLI (Optional)

For future PR creation support:

```bash
sudo -u telecoder gh auth login --with-token < token.txt
```

## Testing Access

Verify git works from the telecoder user:

```bash
sudo -u telecoder git clone https://github.com/you/your-repo /tmp/test-clone
rm -rf /tmp/test-clone
```

## Auto-Push

To automatically push branches after sessions complete:

```toml
# /etc/telecoder/config.toml
[git]
auto_push = true
branch_prefix = "telecoder/"
```
