// Package docker implements sandbox.Runtime using Docker containers.
package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jxucoder/TeleCoder/sandbox"
)

// Runtime implements sandbox.Runtime using Docker.
type Runtime struct {
	dockerBin string
}

// New creates a new Docker sandbox runtime.
func New() *Runtime {
	return &Runtime{
		dockerBin: findDocker(),
	}
}

// findDocker locates the docker binary, checking PATH first and then
// well-known install locations (Docker Desktop on macOS, Homebrew, etc.).
func findDocker() string {
	if p, err := exec.LookPath("docker"); err == nil {
		return p
	}
	candidates := []string{
		"/Applications/Docker.app/Contents/Resources/bin/docker",
		"/usr/local/bin/docker",
		"/opt/homebrew/bin/docker",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "docker"
}

func (r *Runtime) docker(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, r.dockerBin, args...)
}

// Start creates and starts a new sandbox container. Returns the container ID.
func (r *Runtime) Start(ctx context.Context, opts sandbox.StartOptions) (string, error) {
	args := []string{
		"run", "-d",
		"--name", fmt.Sprintf("telecoder-%s", opts.SessionID),
		"--label", "telecoder.session=" + opts.SessionID,
	}

	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}

	envVars := make([]string, 0, len(opts.Env)+4)
	envVars = append(envVars, opts.Env...)
	envVars = append(envVars, "TELECODER_SESSION_ID="+opts.SessionID, "TELECODER_REPO="+opts.Repo, "TELECODER_BRANCH="+opts.Branch)
	if !opts.Persistent {
		envVars = append(envVars, "TELECODER_PROMPT="+opts.Prompt)
	}
	for _, e := range envVars {
		args = append(args, "-e", e)
	}

	if opts.Persistent {
		args = append(args, "--entrypoint", "sleep", opts.Image, "infinity")
	} else {
		args = append(args, opts.Image)
	}

	cmd := r.docker(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("starting container: %w\noutput: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

// StreamLogs attaches to a container's stdout and returns a line-by-line reader.
func (r *Runtime) StreamLogs(ctx context.Context, containerID string) (sandbox.LineScanner, error) {
	cmd := r.docker(ctx, "logs", "-f", containerID)
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

	merged := io.MultiReader(stdout, stderr)
	scanner := bufio.NewScanner(merged)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	return &lineScanner{
		scanner: scanner,
		cmd:     cmd,
	}, nil
}

// Stop kills and removes a sandbox container.
func (r *Runtime) Stop(ctx context.Context, containerID string) error {
	_ = r.docker(ctx, "kill", containerID).Run()
	cmd := r.docker(ctx, "rm", "-f", containerID)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("removing container: %w\noutput: %s", err, string(output))
	}
	return nil
}

// Wait blocks until the container exits and returns the exit code.
func (r *Runtime) Wait(ctx context.Context, containerID string) (int, error) {
	cmd := r.docker(ctx, "wait", containerID)
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
func (r *Runtime) EnsureNetwork(ctx context.Context, name string) error {
	check := r.docker(ctx, "network", "inspect", name)
	if check.Run() == nil {
		return nil
	}

	cmd := r.docker(ctx, "network", "create", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating network %q: %w\noutput: %s", name, err, string(output))
	}
	return nil
}

// Exec runs a command inside a running container and returns a streaming
// line scanner. The caller must call Close() on the returned scanner.
func (r *Runtime) Exec(ctx context.Context, containerID string, command []string) (sandbox.LineScanner, error) {
	args := append([]string{"exec", containerID}, command...)
	cmd := r.docker(ctx, args...)

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

	return &lineScanner{
		scanner: scanner,
		cmd:     cmd,
	}, nil
}

// ExecCollect runs a command inside a container and returns all output as a string.
func (r *Runtime) ExecCollect(ctx context.Context, containerID string, command []string) (string, error) {
	args := append([]string{"exec", containerID}, command...)
	cmd := r.docker(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("exec failed: %w\noutput: %s", err, string(output))
	}
	return string(output), nil
}

// CommitAndPush stages all changes, commits them, and pushes the branch.
func (r *Runtime) CommitAndPush(ctx context.Context, containerID, message, branch string) error {
	if _, err := r.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "add", "-A",
	}); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	checkCmd := r.docker(ctx, "exec", containerID,
		"git", "-C", "/workspace/repo", "diff", "--cached", "--quiet")
	if checkCmd.Run() == nil {
		return fmt.Errorf("no changes to commit")
	}

	if len(message) > 69 {
		message = message[:69] + "..."
	}
	commitMsg := "telecoder: " + message

	if _, err := r.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "commit", "-m", commitMsg,
	}); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	if _, err := r.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "push", "--force-with-lease", "origin", branch,
	}); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	return nil
}

// IsRunning checks if a container is still running.
func (r *Runtime) IsRunning(ctx context.Context, containerID string) bool {
	cmd := r.docker(ctx, "inspect", "-f", "{{.State.Running}}", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// lineScanner wraps a bufio.Scanner for reading container log lines.
type lineScanner struct {
	scanner *bufio.Scanner
	cmd     *exec.Cmd
}

func (ls *lineScanner) Scan() bool  { return ls.scanner.Scan() }
func (ls *lineScanner) Text() string { return ls.scanner.Text() }
func (ls *lineScanner) Err() error   { return ls.scanner.Err() }

func (ls *lineScanner) Close() error {
	if ls.cmd.Process != nil {
		_ = ls.cmd.Process.Kill()
	}
	return ls.cmd.Wait()
}
