// Package sandbox defines the Runtime interface for TeleCoder sandbox execution.
package sandbox

import "context"

// StartOptions configures a new sandbox container.
type StartOptions struct {
	SessionID  string
	Repo       string // "owner/repo"
	Prompt     string
	Persistent bool
	Branch     string   // git branch name
	Image      string   // Docker image name
	Env        []string // additional environment variables
	Network    string   // Docker network name
}

// LineScanner provides line-by-line reading of container output.
type LineScanner interface {
	Scan() bool
	Text() string
	Err() error
	Close() error
}

// Runtime manages sandbox container lifecycle.
type Runtime interface {
	Start(ctx context.Context, opts StartOptions) (containerID string, err error)
	Stop(ctx context.Context, containerID string) error
	Wait(ctx context.Context, containerID string) (exitCode int, err error)
	StreamLogs(ctx context.Context, containerID string) (LineScanner, error)
	Exec(ctx context.Context, containerID string, cmd []string) (LineScanner, error)
	ExecCollect(ctx context.Context, containerID string, cmd []string) (string, error)
	CommitAndPush(ctx context.Context, containerID string, message, branch string) error
	EnsureNetwork(ctx context.Context, name string) error
	IsRunning(ctx context.Context, containerID string) bool
}
