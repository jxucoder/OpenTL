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

## Use

Open Telegram on your phone, find your bot, and send a message:

```
fix the broken login redirect --repo owner/repo
```

Or if you set `TELEGRAM_DEFAULT_REPO`:

```
add rate limiting to the users API
```

The bot will:
1. Acknowledge the task
2. Send status updates as replies
3. Upload the full terminal output as a `.log` file
4. Send the PR link when done

## That's It

No OAuth, no scopes, no app wizard. Just a token and a message.
