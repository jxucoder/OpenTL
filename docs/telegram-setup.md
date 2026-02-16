# Telegram Bot Setup

The simplest way to use TeleCoder from your phone. Takes about 1 minute.

## Create the Bot

1. Open Telegram and message [@BotFather](https://t.me/BotFather)
2. Send `/newbot`
3. Choose a name (e.g., "TeleCoder")
4. Choose a username (e.g., `my_telecoder_bot`)
5. Copy the token BotFather gives you

## Configure

Set the token as an environment variable:

```bash
export TELEGRAM_BOT_TOKEN=123456789:ABCdefGhIjKlMnOpQrStUvWxYz

# Optional: default repo so you don't need --repo every time
export TELEGRAM_DEFAULT_REPO=your-org/your-repo
```

Or add to your `.env` file for Docker Compose:

```env
TELEGRAM_BOT_TOKEN=123456789:ABCdefGhIjKlMnOpQrStUvWxYz
TELEGRAM_DEFAULT_REPO=your-org/your-repo
```

## Start

```bash
telecoder serve
```

You should see:

```
Telegram bot authorized as @my_telecoder_bot
Telegram bot enabled (long polling)
Telegram bot listening for messages...
```

## Usage

The Telegram bot supports two modes: **chat mode** (multi-turn, persistent sandbox) and **task mode** (one-shot, fire-and-forget).

### Chat Mode (default)

Send a message with a repo and the bot starts a persistent sandbox session. You can then send follow-up messages in the same session.

```
fix the broken login redirect --repo owner/repo
```

Or start explicitly:

```
/new --repo owner/repo
```

Then send follow-up messages naturally:

```
also add tests for the fix
```

```
now update the error messages to be more descriptive
```

When you're happy with the changes:

```
/pr
```

The bot creates a PR from all the accumulated changes.

### Task Mode (one-shot)

For fire-and-forget tasks that don't need follow-ups:

```
/run fix the typo in README.md --repo owner/repo
```

The bot acknowledges, runs the task, and posts the PR link (or text answer) when done.

### Commands

| Command | Description |
|:--------|:------------|
| `/new --repo owner/repo` | Start a fresh chat session |
| `/run <task> --repo owner/repo` | One-shot task mode |
| `/pr` | Create a PR from current session changes |
| `/diff` | Show recent agent output |
| `/status` | Show session info |
| `/stop` | Stop the current session |
| `/help` | Show help message |

If `TELEGRAM_DEFAULT_REPO` is set, you can omit `--repo` from all commands.

### What the Bot Does

1. Acknowledges the task with a status message
2. Sends status updates as replies (planning, sandbox started, agent running, etc.)
3. Sends agent output in code blocks
4. Posts the PR link when done, or displays the text answer

## That's It

No OAuth, no scopes, no app wizard. Just a token and a message.
