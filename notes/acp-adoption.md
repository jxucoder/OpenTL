# Adopting ACP (Agent Client Protocol)

## What Is ACP

ACP (Agent Client Protocol) is a standardized protocol for communicating with
AI coding agents. It uses JSON-RPC 2.0 over stdio — the same transport pattern
as LSP (Language Server Protocol).

Key facts:

- Created by Coder and Block (Square)
- Open standard with community governance
- Go SDK available: `github.com/coder/acp-go-sdk`
- Supported by Claude Code, Goose, Codex, and others
- Spec: https://agentclientprotocol.com

## Why Adopt ACP

TeleCoder's previous `CodingAgent` interface required:

- a custom `Command()` method per agent (shell command strings)
- a custom marker protocol (`###TELECODER_STATUS###`) for event parsing
- each agent to be wrapped with ~50 lines of glue code

ACP replaces all of this with a single standard protocol. Benefits:

1. **Any ACP-compatible agent works out of the box** — no custom adapter needed
2. **Richer communication** — structured messages, tool calls, session updates,
   not just stdout line parsing
3. **Bidirectional** — TeleCoder can send follow-up messages, cancel, and
   inspect state mid-session
4. **Ecosystem alignment** — as more agents adopt ACP, TeleCoder gets them
   for free

## How It Fits V1

The plan says v1 should:

- support one runtime well (Claude Code)
- keep things simple
- use one process per session

ACP fits this perfectly:

- Claude Code supports ACP
- ACP runs over stdio (one process per session, no containers needed)
- The Go SDK handles the protocol, TeleCoder just calls it
- No custom marker protocol needed

## Architecture Change

Before (custom interface):

```
TeleCoder Engine
  → shell exec: `claude --print "prompt" 2>&1`
  → parse stdout lines for ###TELECODER_*### markers
  → map to internal events
```

After (ACP):

```
TeleCoder Engine
  → spawn agent process (e.g. `claude --acp`)
  → ACP client over stdin/stdout (JSON-RPC 2.0)
  → Initialize → NewSession → Prompt
  → receive structured SessionUpdates
  → map to internal events
```

## Key ACP Concepts

### Connection Lifecycle

1. **Initialize** — handshake, exchange capabilities
2. **NewSession** — create a session with optional context
3. **Prompt** — send a user message, get back a response
4. **SessionUpdate** — streaming updates during a prompt turn

### Message Types

- **Request** — has `id`, `method`, `params`
- **Response** — has `id`, `result` or `error`
- **Notification** — no `id`, one-way (e.g. session updates, cancellation)

### Content Blocks

Messages contain `ContentBlock` items:

- Text (plain text or markdown)
- Image
- Audio
- Resource links
- Tool calls and tool results

### Agent Discovery

The Initialize handshake tells TeleCoder what the agent supports:

- capabilities
- supported content types
- tool definitions

## Integration Points

### Runtime Adapter (Workstream 2)

Replace the `CodingAgent` interface with an ACP client:

```go
type AgentConnection struct {
    conn *acpsdk.ClientSideConnection
    cmd  *exec.Cmd
}

func (a *AgentConnection) Initialize(ctx context.Context) error
func (a *AgentConnection) NewSession(ctx context.Context, repo, branch string) (string, error)
func (a *AgentConnection) Prompt(ctx context.Context, sessionID, prompt string) (*PromptResponse, error)
func (a *AgentConnection) Cancel(ctx context.Context, sessionID string) error
func (a *AgentConnection) Close() error
```

### Session Engine (Workstream 3)

The session engine changes:

- `create session` → also calls `acp.NewSession`
- `start run` → calls `acp.Prompt` and streams `SessionUpdate` events
- `stop run` → calls `acp.Cancel`
- `resume session` → reuses ACP session ID (or creates new one with context)

### Event Mapping

ACP `SessionUpdate` events map to TeleCoder events:

| ACP Event | TeleCoder Event |
|-----------|-----------------|
| SessionUpdate (text) | output |
| SessionUpdate (tool_call) | status |
| SessionUpdate (error) | error |
| PromptResponse (final) | done |

No more custom marker parsing.

### Chat Mode

ACP natively supports multi-turn conversation. Chat mode becomes:

1. Keep the ACP connection alive
2. Send new `Prompt` requests for each user message
3. Session state is maintained by the agent

This is simpler than the previous approach of re-running shell commands.

## Go SDK Usage

```go
import acpsdk "github.com/coder/acp-go-sdk"

// Start agent process
cmd := exec.Command("claude", "--acp")
cmd.Dir = workspaceDir

stdin, _ := cmd.StdinPipe()
stdout, _ := cmd.StdoutPipe()
cmd.Start()

// Create ACP client connection
client := &telecoderClient{} // implements acpsdk.Client interface
conn := acpsdk.NewClientSideConnection(client, stdout, stdin)

// Initialize
resp, _ := conn.Initialize(ctx, acpsdk.InitializeRequest{
    ClientInfo: acpsdk.ClientInfo{
        Name:    "telecoder",
        Version: "1.0.0",
    },
})

// Create session
session, _ := conn.NewSession(ctx, acpsdk.NewSessionRequest{})

// Send prompt
result, _ := conn.Prompt(ctx, acpsdk.PromptRequest{
    SessionID: session.ID,
    Content: []acpsdk.ContentBlock{
        acpsdk.TextContent("fix the failing test in auth.go"),
    },
})
```

## What This Means For plan.md

### Changes

- **Workstream 2 (Runtime Adapter)**: build an ACP client, not a shell command
  wrapper. Use `github.com/coder/acp-go-sdk`.
- **Workstream 3 (Session Engine)**: session lifecycle maps to ACP session
  lifecycle. Simpler than before.
- **Open Decision "What exact runtime is v1 using?"**: Claude Code via ACP.
  The `--acp` flag (or equivalent) is the launch mode.

### No Changes

- **Workstream 1 (Bootstrap)**: unchanged
- **Workstream 4 (Git Workspace)**: unchanged — TeleCoder still manages git
- **Workstream 5 (Verification)**: unchanged — TeleCoder still runs test/lint
- **Workstream 6 (CLI)**: unchanged
- **Workstream 7 (Web UI)**: unchanged
- **Workstream 8 (Install)**: unchanged, but install must include ACP-compatible
  agent binary

### Simplifications

- No custom `###TELECODER_*###` marker protocol
- No per-agent adapter code
- No `ParseEvent()` function
- Chat mode is native, not a shell command hack
- Adding new agents later = zero code if they support ACP

## Risks

### ACP Maturity

ACP is relatively new. The spec may change.

Mitigation: pin the Go SDK version. The protocol is simple enough that changes
should be manageable.

### Agent ACP Support

Not all agents support ACP yet.

Mitigation: v1 only needs Claude Code, which does support ACP. Other agents
can be added later, and a fallback shell adapter can exist for non-ACP agents.

### Stdio Reliability

Long-running stdio connections may have buffering or EOF issues.

Mitigation: the Go SDK handles framing. Add reconnection logic if needed.
This is the same transport LSP has used reliably for years.
