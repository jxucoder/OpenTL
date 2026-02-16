# User Stories

Real-world scenarios that TeleCoder can power, with concrete examples.

---

## Core: Background Coding

### 1. Background Coding Agent

**Story:** An engineer types a task in Slack and walks away. Ten minutes later a PR appears with the changes, tests passing, ready for review.

This is the core flow. No extensions needed.

**Example: CLI**

```bash
telecoder run "add rate limiting to /api/users -- max 100 requests per minute per API key, return 429 with Retry-After header" --repo myorg/backend
```

**Example: Slack**

```
@TeleCoder add input validation to the POST /api/orders endpoint. Validate that quantity is a positive integer and product_id exists. Return 400 with field-level errors. --repo myorg/backend
```

**Example: Telegram**

```
refactor the database connection pool to use pgxpool instead of database/sql. Update all repository files. --repo myorg/backend
```

**Example: Linear**

> **Issue title:** Add pagination to GET /api/products
>
> **Description:** Currently returns all products. Add cursor-based pagination with `limit` and `after` parameters. Default limit 20, max 100. --repo myorg/backend
>
> **Labels:** `telecoder`

**Example: Jira**

> **BACK-789:** Migrate user avatars from local storage to S3
>
> **Description:** Move avatar upload/download to use AWS S3. Use the existing AWS credentials from environment variables. Update the UserService and storage layer. --repo myorg/backend
>
> **Labels:** `telecoder`

**What happens:**

- Task arrives via CLI, Slack, Telegram, Linear, or Jira.
- Engine decomposes the task, generates a plan, spins up a Docker sandbox.
- The agent implements the change inside the sandbox.
- Verify stage runs tests and linting; failures trigger automatic revisions.
- Review stage checks the diff; if rejected, a revision round runs (up to `MaxRevisions`).
- If code was changed, a PR is opened on GitHub. If not, a text answer is posted back.

| Goal | Lever |
|:-----|:------|
| Faster cold starts | Enable the pre-warming pool (`sandbox/pool.go`) |
| Run on remote machines | Use the SSH sandbox runtime (`sandbox/ssh/`) |
| Custom quality gates | Add a custom `pipeline.Stage` (e.g. security scan) |

### 2. Ask a Question (No PR)

**Story:** An engineer asks a question about a codebase. The agent reads the code and returns a text answer. No branch, no PR.

**Example: CLI**

```bash
telecoder run "what testing framework does this project use and how are tests organized?" --repo myorg/frontend
```

Output:

```
Done: This project uses Vitest for unit tests (in __tests__/ directories co-located
with source files) and Playwright for e2e tests (in e2e/). Test config is in
vitest.config.ts and playwright.config.ts. Run unit tests with `npm test` and
e2e tests with `npm run test:e2e`.
```

**Example: Slack**

```
@TeleCoder what environment variables does this service require? List them with descriptions. --repo myorg/auth-service
```

**Example: Telegram**

```
explain the authentication flow in this codebase -- how does a request go from login to getting a JWT? --repo myorg/backend
```

The agent reads the code, determines no changes are needed, and returns a text answer directly.

### 3. Swap the In-Sandbox Agent

**Story:** The team wants to use a specific coding agent for a task -- Claude Code for complex refactors, Codex for simple fixes.

**Example: Per-session override via CLI**

```bash
# Use Claude Code for this task
telecoder run "refactor the payment module to use the strategy pattern" --repo myorg/backend --agent claude-code

# Use Codex for a simple fix
telecoder run "fix the typo in the error message on line 42 of src/utils.go" --repo myorg/backend --agent codex
```

**Example: Per-session override via API**

```bash
curl -X POST http://localhost:7080/api/sessions \
  -H "Content-Type: application/json" \
  -d '{"repo":"myorg/backend","prompt":"add retry logic to the HTTP client","agent":"claude-code"}'
```

**Example: Global default**

```bash
# Set the default agent for all sessions
export TELECODER_CODING_AGENT=claude-code
telecoder serve
```

The sandbox ships with all three agents (OpenCode, Claude Code, Codex CLI). `TELECODER_CODING_AGENT` controls which one runs as primary; the others remain available as CLI tools the agent can invoke.

---

## Triggered Workflows

### 4. Ticket-Driven Automation (Linear / Jira)

**Story:** When a ticket is labeled in Linear or Jira, an agent picks it up automatically. When it finishes, it posts the PR link as a comment on the ticket.

**Example: Linear**

1. An engineer creates a Linear issue:
   > **Title:** Add request logging middleware
   >
   > **Description:** Log method, path, status code, and duration for every HTTP request. Use structured JSON logging. --repo myorg/api-gateway

2. The engineer (or an automation) adds the `telecoder` label.

3. TeleCoder picks it up, creates a session, and comments:
   > Starting TeleCoder session for `myorg/api-gateway`...

4. When done:
   > PR ready: [#87](https://github.com/myorg/api-gateway/pull/87)
   >
   > Session `f3a1b2c4` | Branch `telecoder/f3a1b2c4`

**Example: Jira**

1. A Jira issue exists:
   > **PLAT-456:** Add health check endpoint
   >
   > **Description:** Add GET /healthz that returns 200 when the service is up and all dependencies (DB, Redis, S3) are reachable. Return 503 with details when any dependency is down. --repo myorg/platform-service

2. Label it `telecoder`.

3. TeleCoder comments on the issue with the PR link when done.

**Example: Webhook approach (custom integration)**

For issue trackers without a built-in channel, POST to the API:

```bash
curl -X POST http://localhost:7080/api/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "repo": "myorg/backend",
    "prompt": "Add health check endpoint that verifies DB and Redis connectivity"
  }'
```

### 5. PR Comment Auto-Fix

**Story:** A reviewer leaves a comment on a PR. The agent picks it up, pushes a fix commit, and replies to the thread.

**Example:**

1. A TeleCoder PR is open: `PR #142 - Add rate limiting`
2. A reviewer comments: `@telecoder the rate limit key should include the user ID, not just the IP address`
3. TeleCoder creates a new session scoped to that PR and branch
4. The agent reads the comment, modifies the code, and pushes a fix commit to the same branch
5. The PR updates automatically

**Setup:** Wire the GitHub webhook (`POST /api/webhooks/github`) to your repo. See the GitHub webhook settings under Settings > Webhooks.

### 6. Flaky Test Auto-Repair

**Story:** CI detects a flaky test. The agent fixes it and opens a PR.

**Example: GitHub Actions**

```yaml
name: Fix Flaky Tests
on:
  workflow_dispatch:
    inputs:
      test_name:
        description: 'Flaky test name'
        required: true

jobs:
  fix:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger TeleCoder
        run: |
          curl -X POST http://telecoder.internal:7080/api/sessions \
            -H "Content-Type: application/json" \
            -d '{
              "repo": "${{ github.repository }}",
              "prompt": "Fix flaky test: ${{ inputs.test_name }}. Analyze why it fails intermittently (timing issues, shared state, network dependency) and fix the root cause. Do not just add retries."
            }'
```

**Example: CLI after noticing a flaky test**

```bash
telecoder run "TestUserCreation in user_test.go is flaky -- it fails about 10% of the time in CI. Find the race condition and fix it." --repo myorg/backend
```

### 7. Dependency Upgrades / Security Patches

**Story:** A vulnerability is flagged. The agent upgrades the dependency, fixes breaking changes, and opens a PR with passing tests.

**Example: Specific CVE**

```bash
telecoder run "upgrade lodash from 4.17.15 to 4.17.21 to fix CVE-2021-23337. Update any call sites that use removed or changed APIs. Run tests to verify." --repo myorg/frontend
```

**Example: Major version upgrade**

```bash
telecoder run "upgrade React from v17 to v18. Update createRoot usage, remove deprecated lifecycle methods, update test utilities from @testing-library/react to use the new render API." --repo myorg/dashboard
```

**Example: Automated via Dependabot webhook**

When Dependabot opens a PR that fails CI, trigger TeleCoder to fix the breakage:

```bash
telecoder run "dependabot upgraded stripe-node from v12 to v14 but tests are failing. Fix the breaking API changes -- Stripe renamed PaymentIntents.create to paymentIntents.create (lowercase) and changed the error type." --repo myorg/billing
```

### 8. Codemod / Migration at Scale

**Story:** "Migrate from SDK v2 to v3 across 12 repos." Fan out parallel TeleCoder sessions, one per repo.

**Example:**

```bash
REPOS="myorg/svc-users myorg/svc-orders myorg/svc-payments myorg/svc-notifications myorg/svc-inventory"
PROMPT="migrate from aws-sdk-v2 to aws-sdk-v3. Replace require('aws-sdk') with modular imports (@aws-sdk/client-s3, @aws-sdk/client-dynamodb, etc). Update all API calls to use the new command pattern (new PutObjectCommand instead of s3.putObject). Run tests."

for repo in $REPOS; do
  telecoder run "$PROMPT" --repo "$repo" &
done
wait
echo "All migrations submitted"
```

Each session runs in its own sandbox. TeleCoder handles parallelism natively -- every session is an independent container.

---

## Integrations: Data & Services

### 9. Connect to Third-Party Services

**Story:** The agent needs access to Supabase, Stripe, Firebase, or another service to read schemas, run migrations, or test API calls.

**Example: Pass credentials via environment**

```go
// In your custom builder
app, err := telecoder.NewBuilder().
    WithConfig(telecoder.Config{
        SandboxEnv: []string{
            "SUPABASE_URL=https://xyz.supabase.co",
            "SUPABASE_SERVICE_KEY=eyJ...",
            "STRIPE_SECRET_KEY=sk_test_...",
        },
    }).
    Build()
```

**Example: Custom sandbox image with service CLIs**

```dockerfile
# docker/custom.Dockerfile
FROM telecoder-sandbox

# Install Supabase CLI
RUN npm install -g supabase

# Install Stripe CLI
RUN curl -s https://packages.stripe.dev/api/security/keypair/stripe-cli-gpg/public | gpg --dearmor > /usr/share/keyrings/stripe.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/stripe.gpg] https://packages.stripe.dev/stripe-cli-debian-local stable main" > /etc/apt/sources.list.d/stripe.list \
    && apt-get update && apt-get install -y stripe
```

```bash
export TELECODER_DOCKER_IMAGE=my-custom-sandbox
telecoder serve
```

Then run tasks that use these services:

```bash
telecoder run "add a new 'subscriptions' table in Supabase with columns: id, user_id, plan, status, created_at. Generate the migration file and update the TypeScript types." --repo myorg/saas-app
```

### 10. Query a Data Warehouse

**Story:** The agent needs to query Snowflake/BigQuery to understand data models or validate SQL.

**Example: SnowSQL in the sandbox**

Add SnowSQL to your custom sandbox image, pass credentials via env, then:

```bash
telecoder run "the revenue_daily materialized view is broken -- it's double-counting refunds. Query information_schema to understand the table relationships, fix the SQL in migrations/views/revenue_daily.sql, and verify the output matches expected totals." --repo myorg/data-platform
```

**Example: Pipeline stage for schema injection**

Build a custom pipeline stage that fetches `information_schema` from your warehouse before planning begins. The agent receives the schema as context and can write accurate SQL without needing direct database access.

---

## Post-Completion Hooks

### 11. Trigger Downstream Jobs

**Story:** After the agent finishes a code change, trigger a build, deploy, or training job.

**Example: Event bus subscriber**

```go
ch := app.Engine().Bus().Subscribe(sessionID)
go func() {
    for event := range ch {
        if event.Type == "done" {
            sess, _ := app.Engine().Store().GetSession(sessionID)
            if sess.PRUrl != "" {
                // Trigger a staging deploy
                triggerDeploy(sess.Repo, sess.Branch)
                // Or trigger model training on Modal
                triggerModalTraining(sess.Repo, sess.PRUrl)
            }
        }
    }
}()
```

**Example: GitHub Actions on PR creation**

The PR created by TeleCoder triggers your existing CI/CD pipeline automatically. No extra setup needed -- GitHub Actions, CircleCI, or whatever you use will pick up the new branch.

### 12. Auto-Generate Docs / Changelogs

**Story:** After a PR merges, an agent reads the diff and updates docs or changelog.

**Example: GitHub webhook on merge**

```yaml
# .github/workflows/auto-docs.yml
name: Update Docs on Merge
on:
  pull_request:
    types: [closed]

jobs:
  update-docs:
    if: github.event.pull_request.merged == true && startsWith(github.event.pull_request.head.ref, 'telecoder/')
    runs-on: ubuntu-latest
    steps:
      - name: Trigger doc update
        run: |
          curl -X POST http://telecoder.internal:7080/api/sessions \
            -H "Content-Type: application/json" \
            -d '{
              "repo": "${{ github.repository }}",
              "prompt": "PR #${{ github.event.pull_request.number }} was just merged (branch: ${{ github.event.pull_request.head.ref }}). Update CHANGELOG.md with a summary of the changes and update any affected API documentation in docs/."
            }'
```

---

## Observability & Ops

### 13. Observability

**Story:** The team wants to monitor agent performance, cost, and failure rates.

TeleCoder's event bus publishes every session lifecycle event. Three extension points:

**Example: Metrics via event bus**

```go
ch := app.Engine().Bus().Subscribe("*") // subscribe to all sessions
go func() {
    for event := range ch {
        switch event.Type {
        case "done":
            metrics.IncrCounter("telecoder.sessions.completed", 1)
        case "error":
            metrics.IncrCounter("telecoder.sessions.failed", 1)
        }
        metrics.RecordDuration("telecoder.session.duration", time.Since(event.CreatedAt))
    }
}()
```

**Example: LLM cost tracking**

Wrap `llm.Client` to capture token usage per pipeline stage:

```go
type instrumentedLLM struct {
    inner llm.Client
}

func (i *instrumentedLLM) Chat(ctx context.Context, msgs []llm.Message) (string, error) {
    start := time.Now()
    resp, err := i.inner.Chat(ctx, msgs)
    metrics.RecordHistogram("telecoder.llm.latency", time.Since(start).Seconds())
    metrics.IncrCounter("telecoder.llm.calls", 1)
    return resp, err
}
```

### 14. On-Call Incident Response

**Story:** A PagerDuty alert fires. The agent reads the alert context, proposes a hotfix, and opens a PR. The on-call engineer reviews instead of writing from scratch.

**Example: PagerDuty webhook**

```go
func handlePagerDutyWebhook(w http.ResponseWriter, r *http.Request) {
    alert := parsePagerDutyAlert(r)
    prompt := fmt.Sprintf(`Hotfix needed for production incident.

Alert: %s
Service: %s
Error: %s
Stack trace:
%s

Analyze the error, find the root cause in the codebase, and implement a minimal fix.
Do not refactor unrelated code. Focus only on stopping the error.`,
        alert.Summary, alert.Service, alert.Error, alert.StackTrace)

    sess, _ := engine.CreateAndRunSession(alert.Repo, prompt)

    // Notify the on-call engineer
    slack.PostMessage(alert.OnCallChannel,
        fmt.Sprintf("Agent working on hotfix for: %s (session %s)", alert.Summary, sess.ID))
}
```

**Example: CLI during an incident**

```bash
telecoder run "production is returning 500 errors on POST /api/checkout. The error is 'nil pointer dereference in calculateDiscount'. Find the bug and fix it. This is urgent -- keep the fix minimal." --repo myorg/backend --agent claude-code
```

---

## Summary

| # | Story | Channel/Trigger | Extension needed |
|:--|:------|:----------------|:-----------------|
| 1 | Background coding agent | CLI, Slack, Telegram, Linear, Jira | None |
| 2 | Ask a question (no PR) | CLI, Slack, Telegram | None |
| 3 | Swap the coding agent | CLI, API | None (`--agent` flag) |
| 4 | Ticket-driven automation | Linear, Jira, or webhook | None (built-in channels) |
| 5 | PR comment auto-fix | GitHub webhook | Wire webhook to repo |
| 6 | Flaky test auto-repair | CI webhook or CLI | CI trigger step |
| 7 | Dependency upgrades | CLI or Dependabot webhook | Webhook trigger |
| 8 | Codemod at scale | CLI (loop) | None |
| 9 | Third-party services | Any | Env vars or custom sandbox image |
| 10 | Query data warehouse | Any | Custom sandbox image or pipeline stage |
| 11 | Post-completion hooks | Event bus or GitHub Actions | Event subscriber or workflow |
| 12 | Auto-generate docs | GitHub Actions on merge | Workflow trigger |
| 13 | Observability | Event bus | Event subscriber or LLM wrapper |
| 14 | On-call incident response | Alerting webhook or CLI | Webhook handler |

**The common pattern:** TeleCoder's interfaces (`sandbox.Runtime`, `pipeline.Stage`, `llm.Client`, `eventbus.Bus`, `channel.Channel`) are the extension points. Most stories require zero framework changes -- just a new implementation of an existing interface, a webhook handler, or a subscriber on the event bus.
