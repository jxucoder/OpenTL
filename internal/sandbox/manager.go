// Package sandbox manages Docker containers for OpenTL sessions.
package sandbox

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// StartOptions configures a new sandbox container.
type StartOptions struct {
	SessionID string
	Repo      string   // "owner/repo"
	Prompt    string
	Branch    string   // git branch name
	Image     string   // Docker image name
	Env       []string // additional environment variables
	Network   string   // Docker network name
}

// Manager handles Docker sandbox lifecycle.
type Manager struct {
	// dockerBin is the resolved path to the docker binary.
	dockerBin string
}

// NewManager creates a new sandbox Manager.
// It resolves the Docker binary path at startup, checking common install
// locations on macOS when docker is not in PATH.
func NewManager() *Manager {
	return &Manager{
		dockerBin: findDocker(),
	}
}

// findDocker locates the docker binary, checking PATH first and then
// well-known install locations (Docker Desktop on macOS, Homebrew, etc.).
func findDocker() string {
	// 1. Check PATH (works when user has docker configured normally).
	if p, err := exec.LookPath("docker"); err == nil {
		return p
	}

	// 2. Check common locations that may not be in PATH.
	candidates := []string{
		"/Applications/Docker.app/Contents/Resources/bin/docker", // Docker Desktop (macOS)
		"/usr/local/bin/docker",                                  // Homebrew / manual install
		"/opt/homebrew/bin/docker",                                // Homebrew on Apple Silicon
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	// Fallback: just use "docker" and let exec fail with a clear error.
	return "docker"
}

// docker creates an exec.Cmd using the resolved docker binary path.
func (m *Manager) docker(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, m.dockerBin, args...)
}

// Start creates and starts a new sandbox container. Returns the container ID.
func (m *Manager) Start(ctx context.Context, opts StartOptions) (string, error) {
	args := []string{
		"run", "-d",
		"--name", fmt.Sprintf("opentl-%s", opts.SessionID),
		"--label", "opentl.session=" + opts.SessionID,
	}

	// Network
	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}

	// Environment variables
	envVars := append(opts.Env,
		"OPENTL_SESSION_ID="+opts.SessionID,
		"OPENTL_REPO="+opts.Repo,
		"OPENTL_PROMPT="+opts.Prompt,
		"OPENTL_BRANCH="+opts.Branch,
	)
	for _, e := range envVars {
		args = append(args, "-e", e)
	}

	// Image
	args = append(args, opts.Image)

	cmd := m.docker(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("starting container: %w\noutput: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

// StreamLogs attaches to a container's stdout and returns a line-by-line reader.
func (m *Manager) StreamLogs(ctx context.Context, containerID string) (*LineScanner, error) {
	cmd := m.docker(ctx, "logs", "-f", containerID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("attaching stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("attaching stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting log stream: %w", err)
	}

	// Merge stdout and stderr into a single reader.
	merged := io.MultiReader(stdout, stderr)
	scanner := bufio.NewScanner(merged)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	return &LineScanner{
		scanner: scanner,
		cmd:     cmd,
	}, nil
}

// Stop kills and removes a sandbox container.
func (m *Manager) Stop(ctx context.Context, containerID string) error {
	// Kill the container (ignore error if already stopped).
	_ = m.docker(ctx, "kill", containerID).Run()

	// Remove the container.
	cmd := m.docker(ctx, "rm", "-f", containerID)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("removing container: %w\noutput: %s", err, string(output))
	}
	return nil
}

// Wait blocks until the container exits and returns the exit code.
func (m *Manager) Wait(ctx context.Context, containerID string) (int, error) {
	cmd := m.docker(ctx, "wait", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return -1, fmt.Errorf("waiting for container: %w", err)
	}

	var exitCode int
	_, err = fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &exitCode)
	if err != nil {
		return -1, fmt.Errorf("parsing exit code: %w", err)
	}
	return exitCode, nil
}

// EnsureNetwork creates the Docker network if it doesn't exist.
func (m *Manager) EnsureNetwork(ctx context.Context, name string) error {
	// Check if network exists.
	check := m.docker(ctx, "network", "inspect", name)
	if check.Run() == nil {
		return nil // Already exists.
	}

	cmd := m.docker(ctx, "network", "create", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating network %q: %w\noutput: %s", name, err, string(output))
	}
	return nil
}

// --- Persistent container support for chat sessions ---

// StartPersistent creates a long-lived container that stays alive between
// messages. The container runs "sleep infinity" instead of the entrypoint.
// Use Exec() to run commands inside it.
func (m *Manager) StartPersistent(ctx context.Context, opts StartOptions) (string, error) {
	args := []string{
		"run", "-d",
		"--name", fmt.Sprintf("opentl-%s", opts.SessionID),
		"--label", "opentl.session=" + opts.SessionID,
	}

	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}

	envVars := append(opts.Env,
		"OPENTL_SESSION_ID="+opts.SessionID,
		"OPENTL_REPO="+opts.Repo,
		"OPENTL_BRANCH="+opts.Branch,
	)
	for _, e := range envVars {
		args = append(args, "-e", e)
	}

	// Override entrypoint to keep the container alive.
	args = append(args, "--entrypoint", "sleep")
	args = append(args, opts.Image, "infinity")

	cmd := m.docker(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("starting persistent container: %w\noutput: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

// Exec runs a command inside a running container and returns a streaming
// line scanner. The caller must call Close() on the returned scanner.
func (m *Manager) Exec(ctx context.Context, containerID string, command []string) (*LineScanner, error) {
	args := append([]string{"exec", containerID}, command...)
	cmd := m.docker(ctx, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("attaching stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("attaching stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting exec: %w", err)
	}

	merged := io.MultiReader(stdout, stderr)
	scanner := bufio.NewScanner(merged)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	return &LineScanner{
		scanner: scanner,
		cmd:     cmd,
	}, nil
}

// ExecCollect runs a command inside a container and returns all output as a string.
func (m *Manager) ExecCollect(ctx context.Context, containerID string, command []string) (string, error) {
	args := append([]string{"exec", containerID}, command...)
	cmd := m.docker(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("exec failed: %w\noutput: %s", err, string(output))
	}
	return string(output), nil
}

// CommitAndPush stages all changes, commits them, and pushes the branch.
func (m *Manager) CommitAndPush(ctx context.Context, containerID, message, branch string) error {
	// Stage all changes.
	if _, err := m.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "add", "-A",
	}); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check if there are changes to commit.
	checkCmd := m.docker(ctx, "exec", containerID,
		"git", "-C", "/workspace/repo", "diff", "--cached", "--quiet")
	if checkCmd.Run() == nil {
		return fmt.Errorf("no changes to commit")
	}

	// Truncate commit message.
	if len(message) > 69 {
		message = message[:69] + "..."
	}
	commitMsg := "opentl: " + message

	if _, err := m.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "commit", "-m", commitMsg,
	}); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	// Push (force to handle amended commits from multiple messages).
	if _, err := m.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "push", "--force-with-lease", "origin", branch,
	}); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	return nil
}

// GetDiff returns the git diff from the container's working directory.
func (m *Manager) GetDiff(ctx context.Context, containerID string) (string, error) {
	return m.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "diff",
	})
}

// GetDiffStaged returns the staged diff (changes that have been git added).
func (m *Manager) GetDiffStaged(ctx context.Context, containerID string) (string, error) {
	return m.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "diff", "--cached",
	})
}

// GetDiffAll returns the full diff of all changes compared to the initial branch state.
func (m *Manager) GetDiffAll(ctx context.Context, containerID, branch string) (string, error) {
	// Show all changes from the branch point (committed + staged + unstaged).
	return m.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "diff", "origin/HEAD..HEAD",
	})
}

// IsRunning checks if a container is still running.
func (m *Manager) IsRunning(ctx context.Context, containerID string) bool {
	cmd := m.docker(ctx, "inspect", "-f", "{{.State.Running}}", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// LineScanner wraps a bufio.Scanner for reading container log lines.
type LineScanner struct {
	scanner *bufio.Scanner
	cmd     *exec.Cmd
}

// Scan advances to the next line. Returns false when done.
func (ls *LineScanner) Scan() bool {
	return ls.scanner.Scan()
}

// Text returns the current line.
func (ls *LineScanner) Text() string {
	return ls.scanner.Text()
}

// Err returns any error from scanning.
func (ls *LineScanner) Err() error {
	return ls.scanner.Err()
}

// Close terminates the log stream.
func (ls *LineScanner) Close() error {
	if ls.cmd.Process != nil {
		_ = ls.cmd.Process.Kill()
	}
	return ls.cmd.Wait()
}
