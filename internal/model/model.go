package model

import "time"

// Session status values.
type Status string

const (
	StatusPending  Status = "pending"
	StatusRunning  Status = "running"
	StatusIdle     Status = "idle"
	StatusComplete Status = "complete"
	StatusError    Status = "error"
	StatusStopped  Status = "stopped"
)

// Session mode values.
type Mode string

const (
	ModeTask Mode = "task"
	ModeChat Mode = "chat"
)

// Result type values.
type ResultType string

const (
	ResultNone ResultType = ""
	ResultPR   ResultType = "pr"
	ResultText ResultType = "text"
)

// Session represents a coding session.
type Session struct {
	ID           string     `json:"id"`
	Repo         string     `json:"repo"`
	Prompt       string     `json:"prompt"`
	Mode         Mode       `json:"mode"`
	Status       Status     `json:"status"`
	Branch       string     `json:"branch,omitempty"`
	ACPSessionID string     `json:"acp_session_id,omitempty"`
	ResultType   ResultType `json:"result_type,omitempty"`
	ResultText   string     `json:"result_text,omitempty"`
	Error        string     `json:"error,omitempty"`
	WorkDir      string     `json:"work_dir,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Event represents a session event (output, status, error, etc).
type Event struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Type      string    `json:"type"` // output, status, error, tool_call, done
	Data      string    `json:"data"`
	CreatedAt time.Time `json:"created_at"`
}

// Message represents a chat message in a session.
type Message struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"` // user, assistant
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}
