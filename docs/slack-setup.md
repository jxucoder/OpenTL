# Slack Bot Setup

TeleCoder includes a Slack bot that lets you trigger coding tasks by mentioning `@TeleCoder` in any channel. The bot uses **Socket Mode** (WebSocket), so no public URL is needed -- it works behind firewalls, NAT, or localhost.

## How It Works

1. You `@mention` the bot with a task description and target repo
2. The bot creates a session, spins up a Docker sandbox, and runs a coding agent
3. Status updates appear as thread replies in Slack
4. When done, the full terminal output is uploaded as a `.log` file
5. The PR link is posted in the thread

## Create the Slack App

### 1. Create a New App

1. Go to [https://api.slack.com/apps](https://api.slack.com/apps)
2. Click **Create New App** > **From scratch**
3. Name it `TeleCoder` (or whatever you prefer)
4. Select your workspace
5. Click **Create App**

### 2. Enable Socket Mode

1. In the left sidebar, go to **Settings > Socket Mode**
2. Toggle **Enable Socket Mode** to ON
3. You'll be prompted to generate an **App-Level Token**
   - Name it `socket-mode`
   - Add the scope `connections:write`
   - Click **Generate**
4. **Copy the `xapp-...` token** -- this is your `SLACK_APP_TOKEN`

### 3. Configure Bot Token Scopes

1. In the left sidebar, go to **Features > OAuth & Permissions**
2. Scroll to **Scopes > Bot Token Scopes**
3. Add the following scopes:
   - `app_mentions:read` -- receive @mention events
   - `chat:write` -- post messages
   - `files:write` -- upload terminal log files
   - `files:read` -- required for file uploads

### 4. Enable Event Subscriptions

1. In the left sidebar, go to **Features > Event Subscriptions**
2. Toggle **Enable Events** to ON
3. Under **Subscribe to bot events**, click **Add Bot User Event**
4. Add: `app_mention`
5. Click **Save Changes**

### 5. Install the App

1. In the left sidebar, go to **Settings > Install App**
2. Click **Install to Workspace**
3. Review and allow the permissions
4. **Copy the `xoxb-...` token** -- this is your `SLACK_BOT_TOKEN`

## Configure TeleCoder

Set the following environment variables before starting the server:

```bash
# Required for Slack integration
export SLACK_BOT_TOKEN=xoxb-your-bot-token
export SLACK_APP_TOKEN=xapp-your-app-level-token

# Optional: default repo when --repo is not specified
export SLACK_DEFAULT_REPO=your-org/your-repo
```

If you're using Docker Compose, add them to a `.env` file in the project root:

```env
GITHUB_TOKEN=ghp_...
ANTHROPIC_API_KEY=sk-ant-...
SLACK_BOT_TOKEN=xoxb-...
SLACK_APP_TOKEN=xapp-...
SLACK_DEFAULT_REPO=your-org/your-repo
```

Then start the server:

```bash
# Direct
telecoder serve

# Or with Docker Compose
docker compose -f docker/compose.yml up -d
```

You should see in the logs:

```
Slack bot enabled (Socket Mode)
Slack: connecting...
Slack: connected
```

## Invite the Bot to a Channel

In Slack, go to the channel you want to use and type:

```
/invite @TeleCoder
```

## Usage

Mention the bot with a task description:

```
@TeleCoder add rate limiting to the users API --repo your-org/your-repo
```

If you configured `SLACK_DEFAULT_REPO`, you can omit `--repo`:

```
@TeleCoder fix the broken login page redirect
```

The bot will:
1. Acknowledge the task in a thread
2. Post status milestones (planning, sandbox started, creating PR)
3. Upload the full terminal output as a `.log` file
4. Post the PR link when done

## Deployment

The TeleCoder server + Slack bot can run anywhere with Docker:

- **Local machine**: `docker compose up` (great for testing)
- **Any VPS** (Hetzner, DigitalOcean, AWS EC2, etc.): clone the repo, set env vars, `docker compose up -d`
- **Home server / Raspberry Pi**: same as above

Since Socket Mode uses an outbound WebSocket connection, **no public URL or port forwarding is needed**. The bot connects to Slack from wherever it runs.

### Minimum Requirements

- Docker installed
- ~2GB RAM per concurrent sandbox (each task gets its own container)
- Network access to GitHub and Slack APIs

## Troubleshooting

| Issue | Fix |
|---|---|
| Bot doesn't respond | Make sure it's invited to the channel (`/invite @TeleCoder`) |
| "Connection error, will retry..." | Check that `SLACK_APP_TOKEN` starts with `xapp-` and Socket Mode is enabled |
| "Invalid auth" | Check that `SLACK_BOT_TOKEN` starts with `xoxb-` and the app is installed |
| Bot responds but sessions fail | Check `GITHUB_TOKEN` and `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` are set |
| File upload fails | Ensure `files:write` and `files:read` scopes are added and app is reinstalled |
