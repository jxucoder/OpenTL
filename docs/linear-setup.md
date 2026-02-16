# Linear Setup

Label a Linear issue with `telecoder` and get a PR back.

## How It Works

1. An issue in Linear gets the `telecoder` label (or whatever label you configure)
2. Linear fires a webhook to your TeleCoder server
3. TeleCoder creates a session using the issue title + description as the prompt
4. The agent works in a Docker sandbox, makes changes, and pushes a branch
5. TeleCoder posts the PR link (or text answer) as a comment on the Linear issue

## Setup

### 1. Get a Linear API Key

1. Go to **Linear > Settings > API** (or visit [linear.app/settings/api](https://linear.app/settings/api))
2. Create a new **Personal API key** or **OAuth application**
3. Copy the key

### 2. Create a Linear Webhook

1. Go to **Linear > Settings > API > Webhooks**
2. Click **New webhook**
3. Set the URL to: `http://YOUR_SERVER:7090/api/webhooks/linear`
4. Select **Issues** under event types
5. Optionally set a signing secret for HMAC verification
6. Save

### 3. Create the Trigger Label

In Linear, create a label called `telecoder` (or your chosen trigger label). You can scope it to a team or make it workspace-wide.

### 4. Configure TeleCoder

```bash
# Required
export LINEAR_API_KEY=lin_api_xxxxxxxxxxxxx

# Optional
export LINEAR_WEBHOOK_SECRET=your-webhook-signing-secret
export LINEAR_TRIGGER_LABEL=telecoder          # default: telecoder
export LINEAR_DEFAULT_REPO=your-org/your-repo  # used when --repo isn't in the description
export LINEAR_WEBHOOK_ADDR=:7090               # default: :7090
```

Or add to `~/.telecoder/config.env`:

```env
LINEAR_API_KEY=lin_api_xxxxxxxxxxxxx
LINEAR_WEBHOOK_SECRET=your-webhook-signing-secret
LINEAR_DEFAULT_REPO=your-org/your-repo
```

### 5. Start

```bash
telecoder serve
```

You should see:

```
Linear channel enabled (webhook)
Linear webhook listening on :7090
```

## Usage

### Basic: Use a Default Repo

If `LINEAR_DEFAULT_REPO` is set, just create an issue and add the label:

> **Issue title:** Add rate limiting to the /api/users endpoint
>
> **Labels:** `telecoder`

The agent uses the title (and description, if any) as the prompt.

### Specify a Repo in the Description

Add `--repo owner/repo` anywhere in the issue description:

> **Issue title:** Fix the broken login redirect
>
> **Description:** The login page redirects to /dashboard even when the session is expired. It should redirect to /login instead. --repo myorg/frontend

### What Happens

1. A comment appears on the issue: "Starting TeleCoder session for `myorg/frontend`..."
2. The agent works on the task in a Docker sandbox
3. When done, another comment appears with the PR link:
   > PR ready: [#142](https://github.com/myorg/frontend/pull/142)
   >
   > Session `a1b2c3d4` | Branch `telecoder/a1b2c3d4`

If no code changes are needed (e.g., a question), the text answer is posted as a comment instead.

## Troubleshooting

| Issue | Fix |
|:------|:----|
| No response on labeled issues | Check that the webhook URL is correct and port 7090 is accessible |
| "Could not determine repository" comment | Set `LINEAR_DEFAULT_REPO` or add `--repo owner/repo` to the issue description |
| "Invalid signature" in server logs | Check that `LINEAR_WEBHOOK_SECRET` matches the secret in Linear webhook settings |
| Comments not appearing on issues | Verify `LINEAR_API_KEY` has permission to comment on issues |
