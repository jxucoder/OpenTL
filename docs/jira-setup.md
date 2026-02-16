# Jira Setup

Label a Jira issue with `telecoder` and get a PR back.

## How It Works

1. An issue in Jira gets the `telecoder` label (or whatever label you configure)
2. Jira fires a webhook to your TeleCoder server
3. TeleCoder creates a session using the issue summary + description as the prompt
4. The agent works in a Docker sandbox, makes changes, and pushes a branch
5. TeleCoder posts the PR link (or text answer) as a comment on the Jira issue

## Setup

### 1. Get a Jira API Token

1. Go to [id.atlassian.com/manage-profile/security/api-tokens](https://id.atlassian.com/manage-profile/security/api-tokens)
2. Click **Create API token**
3. Give it a label (e.g., "TeleCoder")
4. Copy the token

You'll also need the email address associated with your Atlassian account.

### 2. Create a Jira Webhook

1. Go to **Jira > Settings > System > Webhooks** (or for Jira Cloud: **Settings > Webhooks**)
2. Click **Create a webhook**
3. Set the URL to: `http://YOUR_SERVER:7091/api/webhooks/jira`
4. Under events, select **Issue > updated** (fires when labels are added)
5. Optionally restrict to a specific project using JQL: `project = MYPROJ`
6. Save

### 3. Create the Trigger Label

In your Jira project, make sure a label `telecoder` exists. Jira labels are freeform text -- just type it when labeling an issue.

### 4. Configure TeleCoder

```bash
# Required (all three needed to enable Jira channel)
export JIRA_BASE_URL=https://yourcompany.atlassian.net
export JIRA_USER_EMAIL=you@yourcompany.com
export JIRA_API_TOKEN=your-api-token

# Optional
export JIRA_WEBHOOK_SECRET=your-webhook-secret
export JIRA_TRIGGER_LABEL=telecoder            # default: telecoder
export JIRA_DEFAULT_REPO=your-org/your-repo    # used when --repo isn't in the description
export JIRA_WEBHOOK_ADDR=:7091                 # default: :7091
```

Or add to `~/.telecoder/config.env`:

```env
JIRA_BASE_URL=https://yourcompany.atlassian.net
JIRA_USER_EMAIL=you@yourcompany.com
JIRA_API_TOKEN=your-api-token
JIRA_DEFAULT_REPO=your-org/your-repo
```

### 5. Start

```bash
telecoder serve
```

You should see:

```
Jira channel enabled (webhook)
Jira webhook listening on :7091
```

## Usage

### Basic: Use a Default Repo

If `JIRA_DEFAULT_REPO` is set, just add the `telecoder` label to any issue:

> **PROJ-123:** Add rate limiting to the /api/users endpoint
>
> **Labels:** `telecoder`

The agent uses the summary (and description, if any) as the prompt.

### Specify a Repo in the Description

Add `--repo owner/repo` anywhere in the issue description:

> **PROJ-456:** Fix the broken login redirect
>
> **Description:** The login page redirects to /dashboard even when the session is expired. --repo myorg/frontend

### What Happens

1. A comment appears on the Jira issue: "Starting TeleCoder session for myorg/frontend..."
2. The agent works on the task in a Docker sandbox
3. When done, another comment appears with the result:
   > PR ready: [#142|https://github.com/myorg/frontend/pull/142]
   >
   > Session a1b2c3d4 | Branch telecoder/a1b2c3d4

If no code changes are needed, the text answer is posted as a comment instead.

## Troubleshooting

| Issue | Fix |
|:------|:----|
| No response on labeled issues | Check that the webhook URL is correct and port 7091 is accessible |
| "Could not determine repository" comment | Set `JIRA_DEFAULT_REPO` or add `--repo owner/repo` to the issue description |
| "Invalid signature" in server logs | Check that `JIRA_WEBHOOK_SECRET` matches the secret in Jira webhook settings |
| Comments not appearing on issues | Verify `JIRA_USER_EMAIL` and `JIRA_API_TOKEN` are correct and the user has comment permission |
| 401/403 from Jira API | The API token may have expired. Generate a new one at [id.atlassian.com](https://id.atlassian.com/manage-profile/security/api-tokens) |
