# User Stories

Real-world scenarios that TeleCoder can power — today or with targeted extensions.

---

## Core: Background Coding

### 1. Background Coding Agent

**Story:** An engineer types a task in Slack and walks away. Ten minutes later a PR appears with the changes, tests passing, ready for review.

This is the core flow. No changes needed.

```
telecoder run "add rate limiting to /api/users" --repo myorg/backend
```

- Task arrives via CLI, Slack, or Telegram.
- Engine decomposes the task, generates a plan, spins up a Docker sandbox.
- The agent implements the change inside the sandbox.
- Verify stage runs tests and linting; failures trigger automatic revisions.
- Review stage checks the diff; if rejected, a revision round runs (up to `MaxRevisions`).
- A PR is opened on GitHub.

| Goal | Lever |
|:-----|:------|
| Faster cold starts | Enable the pre-warming pool (`sandbox/pool.go`) |
| Run on remote machines | Use the SSH sandbox runtime (`sandbox/ssh/`) |
| Custom quality gates | Add a custom `pipeline.Stage` (e.g. security scan) |

### 2. Swap the In-Sandbox Agent

**Story:** The team wants to use a different coding agent — Claude Code, Codex CLI, Goose, Aider, or any CLI-based agent.

The sandbox entrypoint (`docker/entrypoint.sh`) selects the agent. Swapping it is a Dockerfile + entrypoint change — no Go code changes.

**Option A — Entrypoint swap**

```dockerfile
# docker/base.Dockerfile — install the agent
RUN npm install -g @anthropic-ai/claude-code
```

```bash
# docker/entrypoint.sh — invoke it
claude -p "$TELECODER_PROMPT" --allowedTools "Bash,Read,Write,Edit" --output-format stream-json
```

**Option B — Separate image**

Build a dedicated image and point TeleCoder at it:

```bash
export TELECODER_DOCKER_IMAGE=my-claude-sandbox
```

The `sandbox.Runtime` interface doesn't care what runs inside the container.

---

## Triggered Workflows

### 3. Ticket-Driven Automation

**Story:** When a ticket is created in Jira / Linear / GitHub Issues — or moves to "Ready for Dev" — an agent picks it up. When it finishes, it Slacks the engineer with a PR link. If it gets stuck, it asks for help.

**Approach A — Webhook listener (recommended)**

```go
func handleTicketWebhook(w http.ResponseWriter, r *http.Request) {
    ticket := parseTicketEvent(r)
    if ticket.Status != "ready_for_dev" {
        return
    }
    resp, _ := http.Post("http://localhost:7080/api/sessions", "application/json",
        toSessionRequest(ticket.Title, ticket.Description, ticket.Repo))
    store.LinkTicket(ticket.ID, resp.SessionID)
}
```

**Approach B — Custom channel**

Implement `channel.Channel` as a ticket poller that creates sessions and notifies engineers on completion/error via Slack.

### 4. PR Comment → Auto-Fix

**Story:** A reviewer leaves a comment on a PR. The agent picks it up, pushes a fix commit, and replies to the thread.

TeleCoder already has a GitHub webhook handler (`POST /api/webhooks/github`) that listens for PR comment events. When a comment mentions the agent (e.g. `@telecoder fix this`), the engine creates a session scoped to that PR and branch. The agent reads the comment, applies the fix, and pushes to the same branch.

**What's needed:** Mostly built today via `gitprovider/github/webhook.go`. Wire the webhook to your GitHub repo settings.

### 5. Flaky Test Auto-Repair

**Story:** CI detects a flaky test. A ticket or webhook fires automatically. The agent fixes the test and opens a PR.

This is a specialization of story #3. The trigger is your CI system (GitHub Actions, CircleCI, Jenkins) firing a webhook when a test is marked flaky.

```yaml
# Example GitHub Actions step
- name: Trigger TeleCoder on flaky test
  if: steps.tests.outputs.flaky == 'true'
  run: |
    curl -X POST http://telecoder:7080/api/sessions \
      -d '{"repo":"myorg/backend","prompt":"Fix flaky test: ${{ steps.tests.outputs.test_name }}"}'
```

### 6. Dependency Upgrades / Security Patches

**Story:** Dependabot or Snyk flags a vulnerability. The agent upgrades the dependency, fixes breaking changes, and opens a PR with passing tests.

Same trigger pattern — webhook from Dependabot/Snyk/Renovate. The prompt includes the CVE and the target version. The agent runs inside the sandbox with full access to the package manager, so it can `npm install`, run tests, and fix whatever breaks.

```
telecoder run "upgrade lodash to 4.17.21 to fix CVE-2021-23337, fix any breaking changes" --repo myorg/frontend
```

### 7. Codemod / Migration at Scale

**Story:** "Migrate from SDK v2 to v3 across 12 repos." Fan out parallel TeleCoder sessions, one per repo.

```bash
for repo in myorg/svc-a myorg/svc-b myorg/svc-c; do
  telecoder run "migrate from stripe-sdk v2 to v3, update all call sites" --repo "$repo" &
done
```

Each session runs in its own sandbox. TeleCoder handles parallelism natively — every session is an independent container.

---

## Integrations: Data & Services

### 8. Connect to Third-Party Services (Supabase, etc.)

**Story:** The agent needs access to Supabase (or Stripe, Firebase, PlanetScale, ...) to read schemas, run migrations, or test API calls.

The sandbox is a full Linux container. Four options, from simplest to most flexible:

1. **Environment variables** — pass credentials via `SandboxEnv` in config.
2. **Custom sandbox image** — pre-install service CLIs (`supabase`, `stripe`, `firebase`).
3. **MCP servers in the sandbox** — agent calls service operations as tools.
4. **Pipeline stage for context injection** — fetch schemas/specs before the agent starts and inject into `pipeline.Context.RepoCtx`.

### 9. Query Snowflake

**Story:** The agent needs to query Snowflake to understand data models, validate SQL migrations, or generate reports.

Same pattern as story #8. Options:

1. **SnowSQL CLI in the sandbox** — install it in the Dockerfile, pass credentials via env.
2. **Pipeline stage** — fetch `information_schema` before planning and inject as context.
3. **MCP server** — agent gets `query_snowflake`, `list_tables`, `describe_table` as tools.
4. **Read-only proxy** — if you don't want raw credentials in the sandbox, expose a query proxy on the Docker network.

---

## Post-Completion Hooks

### 10. Trigger Model Training (Modal)

**Story:** After the agent finishes a code change, automatically trigger a fine-tuning or evaluation job on Modal (or any compute platform).

**Option A — Event bus subscriber**

```go
bus.Subscribe(func(event model.Event) {
    if event.Type == "session_complete" {
        triggerTraining(event.SessionID, event.Metadata)
    }
})
```

**Option B — Custom pipeline stage** that runs after the review stage approves.

**Option C — Webhook** — use the GitHub PR creation event to trigger an external service.

### 11. Auto-Generate Docs / Changelogs

**Story:** After a PR merges, an agent reads the diff and updates API docs, README, or changelog.

**Approach A — GitHub webhook on merge**

Listen for `pull_request.closed` + `merged: true` events. Create a TeleCoder session with a prompt like:

```
"Update CHANGELOG.md and relevant API docs based on this diff: <diff>"
```

**Approach B — Pipeline stage**

Add a post-review stage that generates documentation from the plan and diff before the PR is opened.

---

## Observability & Ops

### 12. Observability (Datadog / Grafana / OpenTelemetry)

**Story:** The team wants to monitor agent performance, cost, and failure rates in their existing observability stack.

TeleCoder's event bus publishes every session lifecycle event. Observability plugs in at three layers:

1. **Event bus subscriber** — translate events into spans/metrics, forward to your backend.
2. **LLM client wrapper** — wrap `llm.Client` to capture token usage, latency, and cost per pipeline stage.
3. **Pipeline stage** — ship plan, diff, and review results to your analytics backend.

All three work with existing interfaces. No framework changes.

### 13. On-Call Incident Response

**Story:** A PagerDuty / OpsGenie alert fires. The agent reads the alert context (error logs, metrics, stack traces), proposes a hotfix, and opens a PR. The on-call engineer reviews under pressure instead of writing from scratch.

**Approach:** Webhook from your alerting system → TeleCoder session. The prompt includes the alert payload and relevant log snippets. The agent has access to the repo and can look at recent commits, error patterns, and propose a targeted fix.

```go
func handlePagerDutyWebhook(w http.ResponseWriter, r *http.Request) {
    alert := parseAlert(r)
    prompt := fmt.Sprintf("Hotfix: %s\n\nError: %s\nStack trace:\n%s",
        alert.Summary, alert.Error, alert.StackTrace)
    createSession(alert.Repo, prompt)
    slack.PostMessage(alert.OnCallEngineer, "Agent working on hotfix for: " + alert.Summary)
}
```

---

## Summary

| # | Story | Category | Works today? | Extension needed |
|:--|:------|:---------|:-------------|:-----------------|
| 1 | Background coding agent | Core | Yes | None |
| 2 | Swap the in-sandbox agent | Core | Yes | Dockerfile + entrypoint |
| 3 | Ticket-driven automation | Triggered | Yes | Webhook handler or custom channel |
| 4 | PR comment → auto-fix | Triggered | Yes | Wire GitHub webhook |
| 5 | Flaky test auto-repair | Triggered | Yes | CI webhook trigger |
| 6 | Dependency upgrades | Triggered | Yes | Webhook from Dependabot/Snyk |
| 7 | Codemod at scale | Triggered | Yes | Loop over repos |
| 8 | Third-party services | Data | Yes | Env vars, custom image, MCP, or pipeline stage |
| 9 | Query Snowflake | Data | Yes | Env vars, custom image, MCP, or pipeline stage |
| 10 | Trigger model training | Post-completion | Yes | Event subscriber or pipeline stage |
| 11 | Auto-generate docs | Post-completion | Yes | Merge webhook or pipeline stage |
| 12 | Observability | Ops | Yes | Event subscriber or LLM wrapper |
| 13 | On-call incident response | Ops | Yes | Alerting webhook |

**The common pattern:** TeleCoder's interfaces (`sandbox.Runtime`, `pipeline.Stage`, `llm.Client`, `eventbus.Bus`, `channel.Channel`) are the extension points. Most stories require zero framework changes — just a new implementation of an existing interface, a webhook handler, or a subscriber on the event bus.
