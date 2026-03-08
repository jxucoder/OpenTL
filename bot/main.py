# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "python-telegram-bot>=21.0",
# ]
# ///
"""Telegram bot for TeleCoder — relay messages straight to Claude Code."""

import logging
import os

from telegram import Update
from telegram.ext import (
    ApplicationBuilder,
    CommandHandler,
    ContextTypes,
    MessageHandler,
    filters,
)

from bot import telecoder

logging.basicConfig(level=logging.INFO)
log = logging.getLogger("telecoder-bot")

# Optional: restrict to specific Telegram user IDs for security
ALLOWED_USERS: set[int] = set()
_allowed = os.environ.get("TELECODER_TG_ALLOWED_USERS", "")
if _allowed:
    ALLOWED_USERS = {int(uid.strip()) for uid in _allowed.split(",") if uid.strip()}


def _authorized(update: Update) -> bool:
    if not ALLOWED_USERS:
        return True  # no restriction configured
    return update.effective_user and update.effective_user.id in ALLOWED_USERS


# ── State: one active session per Telegram chat ──────────────────────
# chat_id -> session_id
_sessions: dict[int, str] = {}


async def cmd_start(update: Update, ctx: ContextTypes.DEFAULT_TYPE) -> None:
    await update.message.reply_text(
        "Send me what to do. It runs immediately.\n\n"
        "Commands:\n"
        "/new <repo-url> — create a session for a repo\n"
        "/status — check current session\n"
        "/logs — last 30 lines of output\n"
        "/stop — stop current session\n\n"
        "Or just type what you want done."
    )


async def cmd_new(update: Update, ctx: ContextTypes.DEFAULT_TYPE) -> None:
    if not _authorized(update):
        return
    args = ctx.args
    repo_url = args[0] if args else None
    chat_id = update.effective_chat.id

    await update.message.reply_text("Creating session…")
    try:
        sid = telecoder.create(repo_url=repo_url)
        _sessions[chat_id] = sid.strip()
        await update.message.reply_text(f"Session `{sid.strip()}` ready. Send me what to do.", parse_mode="Markdown")
    except Exception as e:
        await update.message.reply_text(f"Failed: {e}")


async def cmd_status(update: Update, ctx: ContextTypes.DEFAULT_TYPE) -> None:
    if not _authorized(update):
        return
    chat_id = update.effective_chat.id
    sid = _sessions.get(chat_id)
    if not sid:
        await update.message.reply_text("No active session. Use /new or just send a message.")
        return
    try:
        info = telecoder.inspect(sid)
        await update.message.reply_text(f"```\n{info}\n```", parse_mode="Markdown")
    except Exception as e:
        await update.message.reply_text(f"Error: {e}")


async def cmd_logs(update: Update, ctx: ContextTypes.DEFAULT_TYPE) -> None:
    if not _authorized(update):
        return
    chat_id = update.effective_chat.id
    sid = _sessions.get(chat_id)
    if not sid:
        await update.message.reply_text("No active session.")
        return
    try:
        output = telecoder.logs(sid)
        # Telegram has a 4096 char limit
        if len(output) > 4000:
            output = "…" + output[-4000:]
        await update.message.reply_text(f"```\n{output}\n```", parse_mode="Markdown")
    except Exception as e:
        await update.message.reply_text(f"Error: {e}")


async def cmd_stop(update: Update, ctx: ContextTypes.DEFAULT_TYPE) -> None:
    if not _authorized(update):
        return
    chat_id = update.effective_chat.id
    sid = _sessions.get(chat_id)
    if not sid:
        await update.message.reply_text("No active session.")
        return
    try:
        telecoder.stop(sid)
        await update.message.reply_text(f"Stopped `{sid}`.", parse_mode="Markdown")
    except Exception as e:
        await update.message.reply_text(f"Error: {e}")


async def handle_message(update: Update, ctx: ContextTypes.DEFAULT_TYPE) -> None:
    """Plain English message → create session if needed → run immediately."""
    if not _authorized(update):
        return

    chat_id = update.effective_chat.id
    text = update.message.text.strip()

    if not text:
        return

    sid = _sessions.get(chat_id)

    # Auto-create a session if none exists
    if not sid:
        try:
            sid = telecoder.create().strip()
            _sessions[chat_id] = sid
        except Exception as e:
            await update.message.reply_text(f"Could not create session: {e}")
            return

    # Send the message as-is to Claude Code
    await update.message.reply_text("On it.")
    try:
        result = telecoder.run(sid, text)
        await update.message.reply_text(result)
    except Exception as e:
        await update.message.reply_text(f"Error: {e}")


def main() -> None:
    token = os.environ.get("TELEGRAM_BOT_TOKEN")
    if not token:
        raise SystemExit("Set TELEGRAM_BOT_TOKEN env var")

    app = ApplicationBuilder().token(token).build()

    app.add_handler(CommandHandler("start", cmd_start))
    app.add_handler(CommandHandler("help", cmd_start))
    app.add_handler(CommandHandler("new", cmd_new))
    app.add_handler(CommandHandler("status", cmd_status))
    app.add_handler(CommandHandler("logs", cmd_logs))
    app.add_handler(CommandHandler("stop", cmd_stop))
    app.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_message))

    log.info("Bot started — polling for messages")
    app.run_polling()


if __name__ == "__main__":
    main()
