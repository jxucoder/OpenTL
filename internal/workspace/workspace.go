package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager handles git workspace operations.
type Manager struct {
	baseDir string
}

// NewManager creates a workspace manager rooted at baseDir.
func NewManager(baseDir string) *Manager {
	return &Manager{baseDir: baseDir}
}

// Setup clones or opens a repo and creates a working branch.
// Returns the workspace directory path.
func (m *Manager) Setup(sessionID, repo, branch string) (string, error) {
	dir := filepath.Join(m.baseDir, sessionID)

	if _, err := os.Stat(dir); err == nil {
		// Workspace already exists (resume case).
		return dir, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create workspace: %w", err)
	}

	// Clone the repo.
	if err := run(dir, "git", "clone", repo, "."); err != nil {
		return "", fmt.Errorf("clone repo: %w", err)
	}

	// Create and checkout working branch.
	if branch != "" {
		if err := run(dir, "git", "checkout", "-b", branch); err != nil {
			return "", fmt.Errorf("create branch: %w", err)
		}
	}

	return dir, nil
}

// Diff returns the git diff for the workspace.
func (m *Manager) Diff(sessionID string) (string, error) {
	dir := filepath.Join(m.baseDir, sessionID)
	out, err := output(dir, "git", "diff", "HEAD")
	if err != nil {
		return "", err
	}
	return out, nil
}

// ChangedFiles returns the list of changed files.
func (m *Manager) ChangedFiles(sessionID string) ([]string, error) {
	dir := filepath.Join(m.baseDir, sessionID)
	out, err := output(dir, "git", "diff", "--name-only", "HEAD")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// CurrentBranch returns the current branch name.
func (m *Manager) CurrentBranch(sessionID string) (string, error) {
	dir := filepath.Join(m.baseDir, sessionID)
	out, err := output(dir, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Push pushes the current branch to origin.
func (m *Manager) Push(sessionID string) error {
	dir := filepath.Join(m.baseDir, sessionID)
	branch, err := m.CurrentBranch(sessionID)
	if err != nil {
		return err
	}
	return run(dir, "git", "push", "-u", "origin", branch)
}

// Remove deletes a workspace directory.
func (m *Manager) Remove(sessionID string) error {
	dir := filepath.Join(m.baseDir, sessionID)
	return os.RemoveAll(dir)
}

func run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func output(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
