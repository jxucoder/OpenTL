// Package model defines the core domain types shared across all TeleCoder packages.
// It has zero dependencies on other TeleCoder packages.
package model

import "time"

// Status represents the current state of a session.
type Status string

const (
	StatusPending  Status = "pending"
	StatusRunning  Status = "running"
	StatusComplete Status = "complete"
	StatusError    Status = "error"
	// StatusIdle means the chat sandbox is alive and waiting for the next message.
	StatusIdle Status = "idle"
)

// Mode represents the session interaction mode.
type Mode string

const (
	// ModeTask is the default fire-and-forget mode (one prompt → agent-decided result).
	ModeTask Mode = "task"
	// ModeChat is multi-turn interactive mode (persistent sandbox, multiple messages).
	ModeChat Mode = "chat"
)

// ResultType indicates what kind of output the agent produced.
type ResultType string

const (
	ResultPR   ResultType = "pr"
	ResultText ResultType = "text"
	ResultNone ResultType = ""
)

// Result holds the agent's output. The type determines which fields are populated.
type Result struct {
	Type     ResultType `json:"type"`
	Content  string     `json:"content,omitempty"`
	PRUrl    string     `json:"pr_url,omitempty"`
	PRNumber int        `json:"pr_number,omitempty"`
}

// Session represents a single TeleCoder task execution.
type Session struct {
	ID          string    `json:"id"`
	Repo        string    `json:"repo"`
	Prompt      string    `json:"prompt"`
	Mode        Mode      `json:"mode"`
	Status      Status    `json:"status"`
	Branch      string    `json:"branch"`
	Agent       string    `json:"agent,omitempty"`
	ChainID     string    `json:"chain_id,omitempty"`
	ChainDepth  int       `json:"chain_depth,omitempty"`
	PRUrl       string    `json:"pr_url,omitempty"`
	PRNumber    int       `json:"pr_number,omitempty"`
	Result      Result    `json:"result"`
	ContainerID string    `json:"-"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MaxChainDepth is the default maximum depth for agent chains to prevent loops.
const MaxChainDepth = 3

// Message represents a single message in a chat session.
type Message struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Event represents a single event in a session's lifecycle.
type Event struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Type      string    `json:"type"` // "status", "output", "error", "done"
	Data      string    `json:"data"`
	CreatedAt time.Time `json:"created_at"`
}

// Truncate shortens a string to maxLen runes, adding "..." if truncated.
func Truncate(s string, maxLen int) string {
	if maxLen <= 3 {
		r := []rune(s)
		if len(r) <= maxLen {
			return s
		}
		return string(r[:maxLen])
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-3]) + "..."
}
