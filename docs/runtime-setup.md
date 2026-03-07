# Runtime Setup: Claude Code

TeleCoder v1 uses Claude Code as its coding agent runtime.

## Install Claude Code

```bash
npm install -g @anthropic-ai/claude-code
```

## Set API Key

Add your Anthropic API key to TeleCoder config:

```toml
# /etc/telecoder/config.toml
[runtime.env]
ANTHROPIC_API_KEY = "sk-ant-your-key-here"
```

Or export it in the environment for the telecoder user:

```bash
# /var/lib/telecoder/.bashrc
export ANTHROPIC_API_KEY="sk-ant-your-key-here"
```

## Verify Runtime

Test that Claude Code works on the VPS:

```bash
claude --print "say hello"
```

You should see a response from Claude.

## Runtime Configuration

Optional config options:

```toml
[runtime]
type = "claude-code"
# Override the binary path if needed
# binary = "/usr/local/bin/claude"
```

## How TeleCoder Uses Claude Code

When you run a session, TeleCoder:

1. Launches `claude --print --output-format text "<prompt>"` in the session workspace
2. Captures stdout and stderr to log files
3. The process runs in its own session group (survives terminal disconnects)
4. You can stop it at any time with `telecoder session stop`
