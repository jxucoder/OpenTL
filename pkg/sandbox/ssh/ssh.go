// Package ssh implements sandbox.Runtime by running Docker commands on a remote
// host via SSH. This allows TeleCoder to run sandboxes on any VPS or cloud
// Docker host without requiring the Docker daemon to be local.
//
// Usage:
//
//	runtime, err := ssh.New(ssh.Config{
//	    Host:       "vps.example.com:22",
//	    User:       "deploy",
//	    KeyPath:    "/home/user/.ssh/id_ed25519",
//	})
//	builder.WithSandbox(runtime)
package ssh

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jxucoder/TeleCoder/pkg/sandbox"
)

// Config holds SSH connection settings.
type Config struct {
	// Host is the remote host in "host:port" format (e.g. "vps.example.com:22").
	Host string
	// User is the SSH user.
	User string
	// KeyPath is the path to the SSH private key file.
	KeyPath string
	// DockerBin is the path to docker on the remote host (default "docker").
	DockerBin string
}

// Runtime implements sandbox.Runtime over SSH.
type Runtime struct {
	config Config
}

// New creates a new SSH-based sandbox runtime.
func New(cfg Config) (*Runtime, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("ssh: Host is required")
	}
	if cfg.User == "" {
		return nil, fmt.Errorf("ssh: User is required")
	}
	if cfg.KeyPath == "" {
		return nil, fmt.Errorf("ssh: KeyPath is required")
	}
	if _, err := os.Stat(cfg.KeyPath); err != nil {
		return nil, fmt.Errorf("ssh: key file not found: %w", err)
	}
	if cfg.DockerBin == "" {
		cfg.DockerBin = "docker"
	}
	return &Runtime{config: cfg}, nil
}

// sshCmd builds an exec.Cmd that runs a command on the remote host via SSH.
func (r *Runtime) sshCmd(ctx context.Context, remoteCmd string) *exec.Cmd {
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		"-i", r.config.KeyPath,
		fmt.Sprintf("%s@%s", r.config.User, r.config.Host),
		remoteCmd,
	}
	return exec.CommandContext(ctx, "ssh", args...)
}

// docker runs a docker command on the remote host and returns the combined output.
func (r *Runtime) docker(ctx context.Context, args string) (string, error) {
	cmd := r.sshCmd(ctx, r.config.DockerBin+" "+args)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// Start creates and starts a new sandbox container on the remote host.
func (r *Runtime) Start(ctx context.Context, opts sandbox.StartOptions) (string, error) {
	args := fmt.Sprintf("run -d --name telecoder-%s --label telecoder.session=%s",
		opts.SessionID, opts.SessionID)

	if opts.Network != "" {
		args += " --network " + opts.Network
	}

	var envVars []string
	envVars = append(envVars, opts.Env...)
	envVars = append(envVars,
		"TELECODER_SESSION_ID="+opts.SessionID,
		"TELECODER_REPO="+opts.Repo,
		"TELECODER_BRANCH="+opts.Branch,
	)
	if !opts.Persistent {
		envVars = append(envVars, "TELECODER_PROMPT="+opts.Prompt)
	}
	for _, e := range envVars {
		args += fmt.Sprintf(" -e %q", e)
	}

	if opts.Persistent {
		args += " --entrypoint sleep " + opts.Image + " infinity"
	} else {
		args += " " + opts.Image
	}

	output, err := r.docker(ctx, args)
	if err != nil {
		return "", fmt.Errorf("starting container: %w\noutput: %s", err, output)
	}
	return strings.TrimSpace(output), nil
}

// Stop kills and removes a container on the remote host.
func (r *Runtime) Stop(ctx context.Context, containerID string) error {
	r.docker(ctx, "kill "+containerID)
	output, err := r.docker(ctx, "rm -f "+containerID)
	if err != nil {
		return fmt.Errorf("removing container: %w\noutput: %s", err, output)
	}
	return nil
}

// Wait blocks until the container exits and returns the exit code.
func (r *Runtime) Wait(ctx context.Context, containerID string) (int, error) {
	output, err := r.docker(ctx, "wait "+containerID)
	if err != nil {
		return -1, fmt.Errorf("waiting for container: %w", err)
	}

	var exitCode int
	_, scanErr := fmt.Sscanf(strings.TrimSpace(output), "%d", &exitCode)
	if scanErr != nil {
		return -1, fmt.Errorf("parsing exit code: %w", scanErr)
	}
	return exitCode, nil
}

// StreamLogs attaches to a container's logs on the remote host.
func (r *Runtime) StreamLogs(ctx context.Context, containerID string) (sandbox.LineScanner, error) {
	cmd := r.sshCmd(ctx, r.config.DockerBin+" logs -f "+containerID)
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

	return &lineScanner{scanner: scanner, cmd: cmd}, nil
}

// Exec runs a command inside a running container on the remote host.
func (r *Runtime) Exec(ctx context.Context, containerID string, command []string) (sandbox.LineScanner, error) {
	quotedCmd := quoteArgs(command)
	cmd := r.sshCmd(ctx, fmt.Sprintf("%s exec %s %s", r.config.DockerBin, containerID, quotedCmd))

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

	return &lineScanner{scanner: scanner, cmd: cmd}, nil
}

// ExecCollect runs a command inside a container and returns all output.
func (r *Runtime) ExecCollect(ctx context.Context, containerID string, command []string) (string, error) {
	quotedCmd := quoteArgs(command)
	output, err := r.docker(ctx, fmt.Sprintf("exec %s %s", containerID, quotedCmd))
	if err != nil {
		return "", fmt.Errorf("exec failed: %w\noutput: %s", err, output)
	}
	return output, nil
}

// CommitAndPush stages, commits, and pushes inside the container.
func (r *Runtime) CommitAndPush(ctx context.Context, containerID, message, branch string) error {
	if _, err := r.ExecCollect(ctx, containerID, []string{
		"git", "-C", "/workspace/repo", "add", "-A",
	}); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check for changes.
	checkCmd := r.sshCmd(ctx, fmt.Sprintf("%s exec %s git -C /workspace/repo diff --cached --quiet",
		r.config.DockerBin, containerID))
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

// EnsureNetwork creates the Docker network on the remote host if needed.
func (r *Runtime) EnsureNetwork(ctx context.Context, name string) error {
	checkCmd := r.sshCmd(ctx, r.config.DockerBin+" network inspect "+name)
	if checkCmd.Run() == nil {
		return nil
	}

	output, err := r.docker(ctx, "network create "+name)
	if err != nil {
		return fmt.Errorf("creating network %q: %w\noutput: %s", name, err, output)
	}
	return nil
}

// IsRunning checks if a container is running on the remote host.
func (r *Runtime) IsRunning(ctx context.Context, containerID string) bool {
	output, err := r.docker(ctx, fmt.Sprintf("inspect -f {{.State.Running}} %s", containerID))
	if err != nil {
		return false
	}
	return strings.TrimSpace(output) == "true"
}

// quoteArgs quotes command arguments for safe SSH transmission.
func quoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = fmt.Sprintf("%q", a)
	}
	return strings.Join(quoted, " ")
}

// lineScanner wraps a bufio.Scanner for reading remote container log lines.
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
