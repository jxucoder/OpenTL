# OpenTL API Reference

Base URL: `http://localhost:7080` (configurable via `OPENTL_ADDR`)

## Health Check

### `GET /health`

Returns `ok` if the server is running.

**Response:** `200 OK` — `ok`

---

## Sessions

### Create Session

```
POST /api/sessions
```

Creates a new coding session. The server launches a Docker sandbox, runs the AI coding agent, and creates a GitHub PR when done.

**Request Body:**

| Field    | Type   | Required | Description                          |
|----------|--------|----------|--------------------------------------|
| `repo`   | string | yes      | GitHub repository in `owner/repo` format |
| `prompt` | string | yes      | Task description for the coding agent |

**Example:**

```json
{
  "repo": "myorg/myapp",
  "prompt": "add rate limiting to /api/users"
}
```

**Response:** `201 Created`

```json
{
  "id": "a1b2c3d4e5f6",
  "branch": "opentl/a1b2c3d4e5f6"
}
```

**Errors:**

| Status | Condition                           |
|--------|-------------------------------------|
| 400    | Missing or invalid `repo`/`prompt`  |
| 400    | Invalid repo format                 |
| 500    | Internal server error               |

---

### List Sessions

```
GET /api/sessions
```

Returns all sessions ordered by creation time (newest first).

**Response:** `200 OK`

```json
[
  {
    "id": "a1b2c3d4e5f6",
    "repo": "myorg/myapp",
    "prompt": "add rate limiting to /api/users",
    "status": "complete",
    "branch": "opentl/a1b2c3d4e5f6",
    "pr_url": "https://github.com/myorg/myapp/pull/42",
    "pr_number": 42,
    "created_at": "2025-01-15T10:30:00Z",
    "updated_at": "2025-01-15T10:35:00Z"
  }
]
```

---

### Get Session

```
GET /api/sessions/{id}
```

Returns details for a specific session.

**Response:** `200 OK`

```json
{
  "id": "a1b2c3d4e5f6",
  "repo": "myorg/myapp",
  "prompt": "add rate limiting to /api/users",
  "status": "running",
  "branch": "opentl/a1b2c3d4e5f6",
  "error": "",
  "created_at": "2025-01-15T10:30:00Z",
  "updated_at": "2025-01-15T10:32:00Z"
}
```

**Session Status Values:**

| Status     | Description                        |
|------------|------------------------------------|
| `pending`  | Session created, not yet started   |
| `running`  | Sandbox is executing               |
| `complete` | PR created successfully            |
| `error`    | Session failed (see `error` field) |

**Errors:** `404` if session not found.

---

### Stream Session Events (SSE)

```
GET /api/sessions/{id}/events
```

Opens a Server-Sent Events stream for real-time session updates. Historical events are replayed first, then new events are pushed as they occur.

**Headers:**

```
Accept: text/event-stream
```

**Event Format:**

```
id: 1
event: status
data: {"id":1,"session_id":"a1b2c3d4","type":"status","data":"Starting sandbox...","created_at":"..."}

id: 2
event: output
data: {"id":2,"session_id":"a1b2c3d4","type":"output","data":"...agent output...","created_at":"..."}
```

**Event Types:**

| Type     | Description                                   |
|----------|-----------------------------------------------|
| `status` | Progress updates (planning, cloning, etc.)    |
| `output` | Agent stdout/stderr output                    |
| `error`  | Error messages                                |
| `done`   | Session complete — data contains the PR URL   |

**Errors:** `404` if session not found.

---

### Stop Session

```
POST /api/sessions/{id}/stop
```

Stops a running session. Kills the Docker container and marks the session as errored.

**Response:** `200 OK` — Returns the updated session object.

**Errors:** `404` if session not found.

---

## Session Lifecycle

```
POST /api/sessions
     │
     ▼
  pending ──→ Plan (LLM generates plan, if orchestrator enabled)
     │
     ▼
  running ──→ Sandbox executes agent with enriched prompt
     │
     ├──→ Review (LLM reviews diff, up to 2 rounds)
     │
     ▼
  complete ──→ PR created on GitHub
     │
    (or)
     ▼
   error ──→ Something failed (see session.error)
```

## Resource Limits

- **Request body:** 1 MB maximum
- **Session timeout:** 30 minutes maximum
- **Container memory:** 2 GB default
- **Container CPUs:** 2 default
- **Container PIDs:** 512 max

## Environment Variables

### Required

| Variable           | Description                              |
|--------------------|------------------------------------------|
| `GITHUB_TOKEN`     | GitHub PAT with `repo` scope             |
| `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` | At least one LLM provider key |

### Optional

| Variable                | Default          | Description                      |
|-------------------------|------------------|----------------------------------|
| `OPENTL_ADDR`           | `:7080`          | Server listen address            |
| `OPENTL_DATA_DIR`       | `~/.opentl`      | Data directory (SQLite DB)       |
| `OPENTL_DOCKER_IMAGE`   | `opentl-sandbox` | Sandbox Docker image name        |
| `OPENTL_DOCKER_NETWORK` | `opentl-net`     | Docker network name              |
| `OPENTL_PLANNER_MODEL`  | (provider default) | LLM model for plan/review      |
| `SLACK_BOT_TOKEN`       |                  | Slack Bot User OAuth Token       |
| `SLACK_APP_TOKEN`       |                  | Slack App-Level Token            |
| `SLACK_DEFAULT_REPO`    |                  | Default repo for Slack commands  |
| `TELEGRAM_BOT_TOKEN`    |                  | Telegram bot token from BotFather|
| `TELEGRAM_DEFAULT_REPO` |                  | Default repo for Telegram commands|
