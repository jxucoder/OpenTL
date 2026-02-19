// Package engine provides the session orchestration logic for TeleCoder.
// It depends only on interfaces (store, sandbox, gitprovider, eventbus).
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jxucoder/TeleCoder/pkg/agent"
	"github.com/jxucoder/TeleCoder/pkg/eventbus"
	"github.com/jxucoder/TeleCoder/pkg/gitprovider"
	"github.com/jxucoder/TeleCoder/pkg/model"
	"github.com/jxucoder/TeleCoder/pkg/sandbox"
	"github.com/jxucoder/TeleCoder/pkg/store"
)

// Config holds engine-specific configuration.
type Config struct {
	DockerImage     string
	DockerNetwork   string
	SandboxEnv      []string
	MaxRevisions    int
	ChatIdleTimeout time.Duration
	ChatMaxMessages int
	WebhookSecret   string

	// CodingAgent is the coding agent to run inside the sandbox.
	// "opencode", "claude-code", "codex", or "auto" (default).
	CodingAgent string

	// MaxSubTasks is the maximum number of sub-tasks for task decomposition (default 5).
	MaxSubTasks int
}

// Engine orchestrates TeleCoder session lifecycle.
type Engine struct {
	config  Config
	store   store.SessionStore
	bus     eventbus.Bus
	sandbox sandbox.Runtime
	git     gitprovider.Provider

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new Engine with all dependencies.
func New(
	cfg Config,
	st store.SessionStore,
	bus eventbus.Bus,
	sb sandbox.Runtime,
	git gitprovider.Provider,
) *Engine {
	return &Engine{
		config:  cfg,
		store:   st,
		bus:     bus,
		sandbox: sb,
		git:     git,
	}
}

// Start starts background goroutines (idle reaper). Call Stop to shut down.
func (e *Engine) Start(ctx context.Context) {
	e.ctx, e.cancel = context.WithCancel(ctx)

	if e.config.DockerNetwork != "" {
		if err := e.sandbox.EnsureNetwork(e.ctx, e.config.DockerNetwork); err != nil {
			log.Printf("Warning: could not create Docker network: %v", err)
		}
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.reapIdleChatSessions(e.ctx)
	}()
}

// Stop cancels all background work and waits for goroutines to finish.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
}

// Store returns the session store.
func (e *Engine) Store() store.SessionStore { return e.store }

// Bus returns the event bus.
func (e *Engine) Bus() eventbus.Bus { return e.bus }

// WebhookSecret returns the configured webhook secret.
func (e *Engine) WebhookSecret() string { return e.config.WebhookSecret }

// CreateAndRunSession creates a task-mode session and starts the sandbox.
func (e *Engine) CreateAndRunSession(repo, prompt string) (*model.Session, error) {
	return e.CreateAndRunSessionWithAgent(repo, prompt, "")
}

// CreateAndRunSessionWithAgent creates a task-mode session with an optional
// per-session agent override and starts the sandbox.
func (e *Engine) CreateAndRunSessionWithAgent(repo, prompt, agent string) (*model.Session, error) {
	id := uuid.New().String()[:8]
	branch := fmt.Sprintf("telecoder/%s", id)
	now := time.Now().UTC()

	sess := &model.Session{
		ID:        id,
		Repo:      repo,
		Prompt:    prompt,
		Agent:     agent,
		Mode:      model.ModeTask,
		Status:    model.StatusPending,
		Branch:    branch,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := e.store.CreateSession(sess); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runSession(sess.ID)
	}()

	return sess, nil
}

// CreateChatSession creates a chat-mode session with a persistent sandbox.
func (e *Engine) CreateChatSession(repo string) (*model.Session, error) {
	id := uuid.New().String()[:8]
	branch := fmt.Sprintf("telecoder/%s", id)
	now := time.Now().UTC()

	sess := &model.Session{
		ID:        id,
		Repo:      repo,
		Prompt:    "",
		Mode:      model.ModeChat,
		Status:    model.StatusPending,
		Branch:    branch,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := e.store.CreateSession(sess); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.initChatSession(sess.ID)
	}()

	return sess, nil
}

func (e *Engine) initChatSession(sessionID string) {
	ctx := e.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	sess, err := e.store.GetSession(sessionID)
	if err != nil {
		log.Printf("chat session %s not found during init: %v", sessionID, err)
		return
	}

	e.emitEvent(sess.ID, "status", "Starting sandbox...")

	containerID, err := e.sandbox.Start(ctx, sandbox.StartOptions{
		SessionID:  sess.ID,
		Repo:       sess.Repo,
		Branch:     sess.Branch,
		Persistent: true,
		Image:      e.config.DockerImage,
		Env:        e.config.SandboxEnv,
		Network:    e.config.DockerNetwork,
	})
	if err != nil {
		e.failSession(sess, fmt.Sprintf("failed to start sandbox: %v", err))
		return
	}

	sess.ContainerID = containerID
	e.store.UpdateSession(sess)

	e.emitEvent(sess.ID, "status", "Setting up repository...")
	setupStream, err := e.sandbox.Exec(ctx, containerID, []string{"/setup.sh"})
	if err != nil {
		e.failSession(sess, fmt.Sprintf("failed to run setup: %v", err))
		return
	}

	for setupStream.Scan() {
		e.dispatchLogLine(sess.ID, setupStream.Text())
	}
	setupStream.Close()

	sess.Status = model.StatusIdle
	e.store.UpdateSession(sess)
	e.emitEvent(sess.ID, "status", "Ready — send a message to start coding")
}

func (e *Engine) reapIdleChatSessions(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sessions, err := e.store.ListSessions()
			if err != nil {
				log.Printf("reaper: list sessions failed: %v", err)
				continue
			}
			for _, sess := range sessions {
				if sess.Mode != model.ModeChat || sess.Status != model.StatusIdle {
					continue
				}
				if time.Since(sess.UpdatedAt) > e.config.ChatIdleTimeout {
					log.Printf("Reaping idle chat session %s (idle for %v)", sess.ID, time.Since(sess.UpdatedAt))
					if sess.ContainerID != "" {
						e.sandbox.Stop(ctx, sess.ContainerID)
					}
					sess.Status = model.StatusError
					sess.Error = "session timed out due to inactivity"
					e.store.UpdateSession(sess)
					e.emitEvent(sess.ID, "status", "Session stopped (idle timeout)")
				}
			}
		}
	}
}

// SendChatMessage sends a user message to a chat session and runs the agent.
func (e *Engine) SendChatMessage(sessionID, content string) (*model.Message, error) {
	sess, err := e.store.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	if sess.Mode != model.ModeChat {
		return nil, fmt.Errorf("session %s is not a chat session", sessionID)
	}
	if sess.Status != model.StatusIdle {
		return nil, fmt.Errorf("session is %s, not idle (wait for current operation to finish)", sess.Status)
	}
	if sess.ContainerID == "" {
		return nil, fmt.Errorf("session has no container")
	}

	msgs, _ := e.store.GetMessages(sessionID)
	userMsgCount := 0
	for _, m := range msgs {
		if m.Role == "user" {
			userMsgCount++
		}
	}
	if userMsgCount >= e.config.ChatMaxMessages {
		return nil, fmt.Errorf("message limit reached (%d messages)", e.config.ChatMaxMessages)
	}

	msg := &model.Message{
		SessionID: sessionID,
		Role:      "user",
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
	if err := e.store.AddMessage(msg); err != nil {
		return nil, fmt.Errorf("storing message: %w", err)
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runChatMessage(sessionID, msg.Content)
	}()

	return msg, nil
}

func (e *Engine) runChatMessage(sessionID, content string) {
	ctx := e.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	sess, err := e.store.GetSession(sessionID)
	if err != nil {
		log.Printf("chat session %s not found while running message: %v", sessionID, err)
		return
	}

	sess.Status = model.StatusRunning
	e.store.UpdateSession(sess)
	e.emitEvent(sess.ID, "status", "Running agent...")

	agentCmd := e.chatAgentCommand(sess.Agent, content)
	agentStream, err := e.sandbox.Exec(ctx, sess.ContainerID, []string{
		"bash", "-c", agentCmd,
	})
	if err != nil {
		log.Printf("Chat message exec failed: %v", err)
		e.emitEvent(sess.ID, "error", fmt.Sprintf("Agent failed to start: %v", err))
		sess.Status = model.StatusIdle
		e.store.UpdateSession(sess)
		return
	}

	var outputLines []string
	for agentStream.Scan() {
		line := agentStream.Text()
		outputLines = append(outputLines, line)
		e.emitEvent(sess.ID, "output", line)
	}
	agentStream.Close()

	assistantContent := strings.Join(outputLines, "\n")
	if assistantContent == "" {
		assistantContent = "(no output)"
	}
	assistantMsg := &model.Message{
		SessionID: sess.ID,
		Role:      "assistant",
		Content:   assistantContent,
		CreatedAt: time.Now().UTC(),
	}
	e.store.AddMessage(assistantMsg)

	sess.Status = model.StatusIdle
	e.store.UpdateSession(sess)
	e.emitEvent(sess.ID, "status", "Ready")
}

// CreatePRFromChat commits all changes in a chat session and creates a PR.
func (e *Engine) CreatePRFromChat(sessionID string) (string, int, error) {
	sess, err := e.store.GetSession(sessionID)
	if err != nil {
		return "", 0, fmt.Errorf("session not found: %w", err)
	}
	if sess.Mode != model.ModeChat {
		return "", 0, fmt.Errorf("session %s is not a chat session", sessionID)
	}
	if sess.Status != model.StatusIdle {
		return "", 0, fmt.Errorf("session is %s, wait for it to be idle", sess.Status)
	}
	if sess.ContainerID == "" {
		return "", 0, fmt.Errorf("session has no container")
	}

	ctx := e.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	e.emitEvent(sess.ID, "status", "Committing and pushing changes...")

	msgs, _ := e.store.GetMessages(sessionID)
	commitDesc := "chat session changes"
	for _, m := range msgs {
		if m.Role == "user" {
			commitDesc = m.Content
			break
		}
	}

	if err := e.sandbox.CommitAndPush(ctx, sess.ContainerID, commitDesc, sess.Branch); err != nil {
		return "", 0, fmt.Errorf("commit/push failed: %w", err)
	}

	e.emitEvent(sess.ID, "status", "Creating pull request...")

	defaultBranch, err := e.git.GetDefaultBranch(ctx, sess.Repo)
	if err != nil {
		defaultBranch = "main"
	}

	prTitle := fmt.Sprintf("telecoder: %s", model.Truncate(commitDesc, 72))
	prBody := fmt.Sprintf("## TeleCoder Chat Session `%s`\n\n", sess.ID)
	for _, m := range msgs {
		if m.Role == "user" {
			prBody += fmt.Sprintf("> **You:** %s\n\n", m.Content)
		}
	}
	prBody += "---\n*Created by [TeleCoder](https://github.com/jxucoder/TeleCoder)*"

	prURL, prNumber, err := e.git.CreatePR(ctx, gitprovider.PROptions{
		Repo:   sess.Repo,
		Branch: sess.Branch,
		Base:   defaultBranch,
		Title:  prTitle,
		Body:   prBody,
	})
	if err != nil {
		return "", 0, fmt.Errorf("failed to create PR: %w", err)
	}

	sess.PRUrl = prURL
	sess.PRNumber = prNumber
	sess.Status = model.StatusComplete
	e.store.UpdateSession(sess)

	e.emitEvent(sess.ID, "done", prURL)

	return prURL, prNumber, nil
}

// --- Agent helpers ---

// resolveAgentName returns the agent name for the sandbox env var.
// Per-session override takes priority, then the global Agent config.
// Returns "" for "auto" (let entrypoint decide based on API keys).
func (e *Engine) resolveAgentName(sessionAgent string) string {
	if sessionAgent != "" && sessionAgent != "auto" {
		return sessionAgent
	}
	if e.config.CodingAgent != "" && e.config.CodingAgent != "auto" {
		return e.config.CodingAgent
	}
	return ""
}

// chatAgentCommand returns the shell command to run for a chat message,
// using the CodingAgent interface for command generation.
func (e *Engine) chatAgentCommand(sessionAgent, content string) string {
	name := e.resolveAgentName(sessionAgent)
	return agent.Resolve(name).Command(content)
}

// --- Sandbox env helper ---

// buildSandboxEnv creates a copy of the base sandbox env with agent selection applied.
func (e *Engine) buildSandboxEnv(sessionAgent string) []string {
	sandboxEnv := make([]string, len(e.config.SandboxEnv))
	copy(sandboxEnv, e.config.SandboxEnv)

	agentName := e.resolveAgentName(sessionAgent)
	if agentName != "" {
		sandboxEnv = append(sandboxEnv, "TELECODER_CODING_AGENT="+agentName)
	}
	return sandboxEnv
}

// --- Multi-step persistent container helpers ---

// writeProgressFile writes .telecoder-progress.json into the sandbox container.
func (e *Engine) writeProgressFile(ctx context.Context, containerID string, statuses []model.SubTaskStatus) error {
	data, err := model.FormatProgressJSON(statuses)
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf("cat > /workspace/repo/.telecoder-progress.json << 'PROGRESS_EOF'\n%s\nPROGRESS_EOF", data)
	_, err = e.sandbox.ExecCollect(ctx, containerID, []string{"bash", "-c", cmd})
	return err
}

// checkpointSubTask commits all current changes in the sandbox container with a
// descriptive message and returns the commit hash.
func (e *Engine) checkpointSubTask(ctx context.Context, containerID, title string, index int) (string, error) {
	_, err := e.sandbox.ExecCollect(ctx, containerID, []string{
		"bash", "-c", "cd /workspace/repo && git add -A",
	})
	if err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}

	_, err = e.sandbox.ExecCollect(ctx, containerID, []string{
		"bash", "-c", "cd /workspace/repo && git diff --cached --quiet",
	})
	if err == nil {
		hash, _ := e.sandbox.ExecCollect(ctx, containerID, []string{
			"bash", "-c", "cd /workspace/repo && git rev-parse HEAD",
		})
		return strings.TrimSpace(hash), nil
	}

	commitMsg := fmt.Sprintf("telecoder: step %d — %s", index+1, title)
	_, err = e.sandbox.ExecCollect(ctx, containerID, []string{
		"bash", "-c", fmt.Sprintf("cd /workspace/repo && git commit -m %q", commitMsg),
	})
	if err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}

	hash, err := e.sandbox.ExecCollect(ctx, containerID, []string{
		"bash", "-c", "cd /workspace/repo && git rev-parse HEAD",
	})
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(hash), nil
}

// preValidate runs verify commands to check codebase health before starting
// the next sub-task. Returns nil if no issues.
func (e *Engine) preValidate(ctx context.Context, sess *model.Session, containerID, taskPrompt string) *sandbox.VerifyResult {
	return e.runVerify(ctx, sess, containerID, taskPrompt)
}

// rollbackToCheckpoint resets the sandbox repo to the given commit hash.
func (e *Engine) rollbackToCheckpoint(ctx context.Context, containerID, commitHash string) error {
	_, err := e.sandbox.ExecCollect(ctx, containerID, []string{
		"bash", "-c", fmt.Sprintf("cd /workspace/repo && git reset --hard %s", commitHash),
	})
	return err
}

// runAgentInContainer runs the coding agent inside an already-running persistent
// container with the given prompt. Returns collected output lines.
func (e *Engine) runAgentInContainer(ctx context.Context, sess *model.Session, containerID, prompt string) ([]string, error) {
	agentCmd := e.chatAgentCommand(sess.Agent, prompt)
	agentStream, err := e.sandbox.Exec(ctx, containerID, []string{
		"bash", "-c", agentCmd,
	})
	if err != nil {
		return nil, fmt.Errorf("agent exec failed: %w", err)
	}

	var outputLines []string
	for agentStream.Scan() {
		line := agentStream.Text()
		outputLines = append(outputLines, line)
		e.emitEvent(sess.ID, "output", line)
	}
	agentStream.Close()

	return outputLines, nil
}

// pushBranch pushes the current branch from the persistent container.
func (e *Engine) pushBranch(ctx context.Context, containerID, branch string) error {
	_, err := e.sandbox.ExecCollect(ctx, containerID, []string{
		"bash", "-c", fmt.Sprintf("cd /workspace/repo && git push -u origin %s", branch),
	})
	return err
}

// hasUncommittedChanges stages all changes and returns true if there are diffs.
func (e *Engine) hasUncommittedChanges(ctx context.Context, containerID string) bool {
	_, err := e.sandbox.ExecCollect(ctx, containerID, []string{
		"bash", "-c", "cd /workspace/repo && git add -A && git diff --cached --quiet",
	})
	return err != nil
}

// --- Task session execution ---

type sandboxRoundResult struct {
	containerID string
	exitCode    int
	lastLine    string
	resultType  model.ResultType
	outputLines []string
}

func (e *Engine) runSession(sessionID string) {
	ctx := e.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	sess, err := e.store.GetSession(sessionID)
	if err != nil {
		log.Printf("session %s not found while starting run: %v", sessionID, err)
		return
	}

	subTasks := []model.SubTask{{Title: "Complete task", Description: sess.Prompt}}
	e.runSessionSingleTask(ctx, sess, subTasks)
}

// runSessionSingleTask handles the fire-and-forget flow for sub-tasks.
func (e *Engine) runSessionSingleTask(ctx context.Context, sess *model.Session, subTasks []model.SubTask) {
	var lastResult *sandboxRoundResult
	for i, task := range subTasks {
		if len(subTasks) > 1 {
			e.emitEvent(sess.ID, "step", fmt.Sprintf("Step %d/%d: %s", i+1, len(subTasks), task.Title))
		}

		result, err := e.runSubTask(ctx, sess, task.Description, sess.Agent)
		if err != nil {
			e.failSession(sess, fmt.Sprintf("step %d/%d failed: %v", i+1, len(subTasks), err))
			if lastResult != nil {
				e.sandbox.Stop(ctx, lastResult.containerID)
			}
			return
		}

		if lastResult != nil && lastResult.containerID != result.containerID {
			e.sandbox.Stop(ctx, lastResult.containerID)
		}
		lastResult = result
	}

	e.finalizeSession(ctx, sess, lastResult)
}

// runSessionMultiStep handles multiple sub-tasks using a persistent container
// with progress tracking, git checkpoints, pre-validation, and self-correction.
func (e *Engine) runSessionMultiStep(ctx context.Context, sess *model.Session, subTasks []model.SubTask) {
	statuses := make([]model.SubTaskStatus, len(subTasks))
	for i, t := range subTasks {
		statuses[i] = model.SubTaskStatus{
			Title:       t.Title,
			Description: t.Description,
			Status:      "pending",
		}
	}

	e.emitEvent(sess.ID, "status", "Starting persistent sandbox for multi-step task...")

	containerID, err := e.sandbox.Start(ctx, sandbox.StartOptions{
		SessionID:  sess.ID,
		Repo:       sess.Repo,
		Branch:     sess.Branch,
		Persistent: true,
		Image:      e.config.DockerImage,
		Env:        e.buildSandboxEnv(sess.Agent),
		Network:    e.config.DockerNetwork,
	})
	if err != nil {
		e.failSession(sess, fmt.Sprintf("failed to start persistent sandbox: %v", err))
		return
	}

	sess.ContainerID = containerID
	sess.Status = model.StatusRunning
	e.store.UpdateSession(sess)

	e.emitEvent(sess.ID, "status", "Setting up repository...")
	setupStream, err := e.sandbox.Exec(ctx, containerID, []string{"/setup.sh"})
	if err != nil {
		e.failSession(sess, fmt.Sprintf("failed to run setup: %v", err))
		e.sandbox.Stop(ctx, containerID)
		return
	}
	for setupStream.Scan() {
		e.dispatchLogLine(sess.ID, setupStream.Text())
	}
	setupStream.Close()

	e.sandbox.ExecCollect(ctx, containerID, []string{
		"bash", "-c", `cd /workspace/repo && grep -qxF '.telecoder-progress.json' .gitignore 2>/dev/null || echo '.telecoder-progress.json' >> .gitignore`,
	})

	var lastCheckpointHash string
	anyCodeChanged := false

	for i, task := range subTasks {
		e.emitEvent(sess.ID, "step", fmt.Sprintf("Step %d/%d: %s", i+1, len(subTasks), task.Title))
		e.emitEvent(sess.ID, "progress", fmt.Sprintf(`{"step":%d,"total":%d,"title":%q,"status":"running"}`, i+1, len(subTasks), task.Title))

		statuses[i].Status = "running"

		if err := e.writeProgressFile(ctx, containerID, statuses); err != nil {
			log.Printf("Failed to write progress file: %v", err)
		}

		if i > 0 {
			e.emitEvent(sess.ID, "status", "Pre-validating previous work...")
			preResult := e.preValidate(ctx, sess, containerID, sess.Prompt)
			if preResult != nil && !preResult.Passed {
				e.emitEvent(sess.ID, "output", "## Pre-validation Failed\n"+preResult.Feedback)

				e.emitEvent(sess.ID, "status", "Attempting self-correction...")
				fixPrompt := fmt.Sprintf("Tests/lint failed after the previous step. Fix the following issues WITHOUT starting on the next task:\n\n%s", preResult.Feedback)
				_, fixErr := e.runAgentInContainer(ctx, sess, containerID, fixPrompt)

				if fixErr == nil {
					recheck := e.preValidate(ctx, sess, containerID, sess.Prompt)
					if recheck != nil && !recheck.Passed {
						e.emitEvent(sess.ID, "status", "Self-correction failed, rolling back to last checkpoint")
						if lastCheckpointHash != "" {
							if rbErr := e.rollbackToCheckpoint(ctx, containerID, lastCheckpointHash); rbErr != nil {
								log.Printf("Rollback failed: %v", rbErr)
							}
						}
						if i > 0 {
							statuses[i-1].Status = "failed"
						}
					} else {
						e.emitEvent(sess.ID, "status", "Self-correction succeeded")
						hash, cpErr := e.checkpointSubTask(ctx, containerID, "self-correction", i-1)
						if cpErr == nil && hash != "" {
							lastCheckpointHash = hash
						}
					}
				} else {
					e.emitEvent(sess.ID, "status", "Self-correction agent failed, rolling back")
					if lastCheckpointHash != "" {
						e.rollbackToCheckpoint(ctx, containerID, lastCheckpointHash)
					}
					if i > 0 {
						statuses[i-1].Status = "failed"
					}
				}
			}
		}

		progressCtx := model.ProgressContext(statuses, i)

		prompt := task.Description
		if progressCtx != "" {
			prompt = progressCtx + "\n\n" + prompt
		}

		e.emitEvent(sess.ID, "status", fmt.Sprintf("Running agent for step %d/%d...", i+1, len(subTasks)))
		_, runErr := e.runAgentInContainer(ctx, sess, containerID, prompt)
		if runErr != nil {
			statuses[i].Status = "failed"
			e.emitEvent(sess.ID, "status", fmt.Sprintf("Step %d/%d failed: %v", i+1, len(subTasks), runErr))
			e.emitEvent(sess.ID, "progress", fmt.Sprintf(`{"step":%d,"total":%d,"title":%q,"status":"failed"}`, i+1, len(subTasks), task.Title))
			continue
		}

		e.emitEvent(sess.ID, "status", "Verifying changes...")
		verifyResult := e.runVerify(ctx, sess, containerID, task.Description)
		if verifyResult != nil && !verifyResult.Passed {
			e.emitEvent(sess.ID, "output", "## Verify Failed\n"+verifyResult.Feedback)
			revisePrompt := fmt.Sprintf("Tests/lint failed. Fix the following issues:\n\n%s\n\nKeep changes minimal and focused.", verifyResult.Feedback)
			_, _ = e.runAgentInContainer(ctx, sess, containerID, revisePrompt)
		}

		if e.hasUncommittedChanges(ctx, containerID) {
			anyCodeChanged = true
		}
		hash, cpErr := e.checkpointSubTask(ctx, containerID, task.Title, i)
		if cpErr != nil {
			log.Printf("Checkpoint failed for step %d: %v", i+1, cpErr)
		} else {
			lastCheckpointHash = hash
			statuses[i].CommitHash = hash
		}

		statuses[i].Status = "completed"

		e.writeProgressFile(ctx, containerID, statuses)
		e.emitEvent(sess.ID, "progress", fmt.Sprintf(`{"step":%d,"total":%d,"title":%q,"status":"completed"}`, i+1, len(subTasks), task.Title))
	}

	if anyCodeChanged {
		e.emitEvent(sess.ID, "status", "Pushing branch...")
		if err := e.pushBranch(ctx, containerID, sess.Branch); err != nil {
			e.failSession(sess, fmt.Sprintf("failed to push branch: %v", err))
			e.sandbox.Stop(ctx, containerID)
			return
		}

		e.emitEvent(sess.ID, "status", "Creating pull request...")

		defaultBranch, err := e.git.GetDefaultBranch(ctx, sess.Repo)
		if err != nil {
			defaultBranch = "main"
		}

		prTitle := fmt.Sprintf("telecoder: %s", model.Truncate(sess.Prompt, 72))
		prBody := fmt.Sprintf("## TeleCoder Session `%s`\n\n**Prompt:**\n> %s\n\n### Steps\n", sess.ID, sess.Prompt)
		for j, s := range statuses {
			icon := "✅"
			if s.Status == "failed" {
				icon = "❌"
			} else if s.Status == "pending" {
				icon = "⏳"
			}
			prBody += fmt.Sprintf("%d. %s **%s** — %s\n", j+1, icon, s.Title, s.Description)
		}
		prBody += "\n---\n*Created by [TeleCoder](https://github.com/jxucoder/TeleCoder)*"

		prURL, prNumber, err := e.git.CreatePR(ctx, gitprovider.PROptions{
			Repo:   sess.Repo,
			Branch: sess.Branch,
			Base:   defaultBranch,
			Title:  prTitle,
			Body:   prBody,
		})
		if err != nil {
			e.failSession(sess, fmt.Sprintf("failed to create PR: %v", err))
			e.sandbox.Stop(ctx, containerID)
			return
		}

		sess.Status = model.StatusComplete
		sess.PRUrl = prURL
		sess.PRNumber = prNumber
		sess.Result = model.Result{
			Type:     model.ResultPR,
			PRUrl:    prURL,
			PRNumber: prNumber,
		}
		e.store.UpdateSession(sess)
		e.emitEvent(sess.ID, "done", prURL)
	} else {
		sess.Status = model.StatusComplete
		sess.Result = model.Result{
			Type:    model.ResultText,
			Content: "All steps completed without code changes.",
		}
		e.store.UpdateSession(sess)
		e.emitEvent(sess.ID, "done", "All steps completed without code changes.")
	}

	e.sandbox.Stop(ctx, containerID)
}

// finalizeSession handles the final output decision after sub-task(s) complete
// in the single-task fire-and-forget flow.
func (e *Engine) finalizeSession(ctx context.Context, sess *model.Session, lastResult *sandboxRoundResult) {
	if lastResult != nil && lastResult.resultType == model.ResultText {
		content := strings.Join(lastResult.outputLines, "\n")
		sess.Status = model.StatusComplete
		sess.Result = model.Result{
			Type:    model.ResultText,
			Content: content,
		}
		e.store.UpdateSession(sess)
		e.emitEvent(sess.ID, "done", content)
	} else {
		e.emitEvent(sess.ID, "status", "Creating pull request...")

		defaultBranch, err := e.git.GetDefaultBranch(ctx, sess.Repo)
		if err != nil {
			defaultBranch = "main"
		}

		prTitle := fmt.Sprintf("telecoder: %s", model.Truncate(sess.Prompt, 72))
		prBody := fmt.Sprintf("## TeleCoder Session `%s`\n\n**Prompt:**\n> %s\n\n---\n*Created by [TeleCoder](https://github.com/jxucoder/TeleCoder)*",
			sess.ID, sess.Prompt)

		prURL, prNumber, err := e.git.CreatePR(ctx, gitprovider.PROptions{
			Repo:   sess.Repo,
			Branch: sess.Branch,
			Base:   defaultBranch,
			Title:  prTitle,
			Body:   prBody,
		})
		if err != nil {
			e.failSession(sess, fmt.Sprintf("failed to create PR: %v", err))
			return
		}

		sess.Status = model.StatusComplete
		sess.PRUrl = prURL
		sess.PRNumber = prNumber
		sess.Result = model.Result{
			Type:     model.ResultPR,
			PRUrl:    prURL,
			PRNumber: prNumber,
		}
		e.store.UpdateSession(sess)

		e.emitEvent(sess.ID, "done", prURL)
	}

	if lastResult != nil {
		e.sandbox.Stop(ctx, lastResult.containerID)
	}
}

func (e *Engine) runSubTask(ctx context.Context, sess *model.Session, taskPrompt, sessionAgent string) (*sandboxRoundResult, error) {
	maxRounds := e.config.MaxRevisions
	var lastResult *sandboxRoundResult

	for round := 0; round <= maxRounds; round++ {
		if round > 0 {
			e.emitEvent(sess.ID, "status", fmt.Sprintf("Starting revision round %d/%d...", round, maxRounds))
		}

		result, err := e.runSandboxRoundWithAgent(ctx, sess, taskPrompt, sessionAgent)
		if err != nil {
			return lastResult, err
		}

		if lastResult != nil && lastResult.containerID != result.containerID {
			e.sandbox.Stop(ctx, lastResult.containerID)
		}
		lastResult = result

		if result.exitCode != 0 {
			errMsg := fmt.Sprintf("sandbox exited with code %d", result.exitCode)
			if result.lastLine != "" {
				errMsg += ": " + result.lastLine
			}
			return lastResult, fmt.Errorf("%s", errMsg)
		}

		if result.resultType == model.ResultText {
			return lastResult, nil
		}

		verifyResult := e.runVerify(ctx, sess, result.containerID, taskPrompt)
		if verifyResult != nil && !verifyResult.Passed {
			e.emitEvent(sess.ID, "output", "## Verify Failed\n"+verifyResult.Feedback)
			if round >= maxRounds {
				e.emitEvent(sess.ID, "status", fmt.Sprintf("Tests/lint failed but max revision rounds (%d) reached, proceeding", maxRounds))
			} else {
				taskPrompt = fmt.Sprintf("%s\n\n## Revision\nTests/lint failed. Fix the following issues:\n\n%s", taskPrompt, verifyResult.Feedback)
				continue
			}
		}

		break
	}

	return lastResult, nil
}

func (e *Engine) runSandboxRound(ctx context.Context, sess *model.Session, prompt string) (*sandboxRoundResult, error) {
	return e.runSandboxRoundWithAgent(ctx, sess, prompt, "")
}

func (e *Engine) runSandboxRoundWithAgent(ctx context.Context, sess *model.Session, prompt, sessionAgent string) (*sandboxRoundResult, error) {
	e.emitEvent(sess.ID, "status", "Starting sandbox...")

	sandboxEnv := e.buildSandboxEnv(sessionAgent)

	containerID, err := e.sandbox.Start(ctx, sandbox.StartOptions{
		SessionID: sess.ID,
		Repo:      sess.Repo,
		Prompt:    prompt,
		Branch:    sess.Branch,
		Image:     e.config.DockerImage,
		Env:       sandboxEnv,
		Network:   e.config.DockerNetwork,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start sandbox: %w", err)
	}

	sess.ContainerID = containerID
	sess.Status = model.StatusRunning
	e.store.UpdateSession(sess)
	e.emitEvent(sess.ID, "status", "Sandbox started, running agent...")

	logStream, err := e.sandbox.StreamLogs(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to stream logs: %w", err)
	}
	defer logStream.Close()

	a := agent.Resolve(e.resolveAgentName(sessionAgent))
	var lastLine string
	var resultType model.ResultType
	var outputLines []string
	for logStream.Scan() {
		line := logStream.Text()
		lastLine = line
		ev := a.ParseEvent(line)
		if ev != nil {
			ev.SessionID = sess.ID
			e.emitParsedEvent(sess.ID, ev)
			switch ev.Type {
			case "done":
				sess.Branch = ev.Data
				resultType = model.ResultPR
			case "result":
				var parsed struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal([]byte(ev.Data), &parsed); err == nil {
					resultType = model.ResultType(parsed.Type)
				}
			}
		} else {
			e.emitEvent(sess.ID, "output", line)
			outputLines = append(outputLines, line)
		}
	}

	exitCode, err := e.sandbox.Wait(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("error waiting for sandbox: %w", err)
	}

	return &sandboxRoundResult{
		containerID: containerID,
		exitCode:    exitCode,
		lastLine:    lastLine,
		resultType:  resultType,
		outputLines: outputLines,
	}, nil
}

func (e *Engine) runVerify(ctx context.Context, sess *model.Session, containerID, taskPrompt string) *sandbox.VerifyResult {
	e.emitEvent(sess.ID, "status", "Running tests and linting...")

	probeFiles := []string{
		"go.mod", "package.json", "Cargo.toml", "requirements.txt",
		"pyproject.toml", "setup.py", "Makefile",
		".eslintrc.js", ".eslintrc.json", "eslint.config.js", "eslint.config.mjs",
	}
	existing := make(map[string]bool)
	for _, f := range probeFiles {
		_, err := e.sandbox.ExecCollect(ctx, containerID, []string{
			"test", "-f", "/workspace/repo/" + f,
		})
		if err == nil {
			existing[f] = true
		}
	}

	cmds := sandbox.DetectVerifyCommands(existing)
	if len(cmds) == 0 {
		e.emitEvent(sess.ID, "status", "No test/lint commands detected, skipping verify")
		return nil
	}

	var allOutput strings.Builder
	anyFailed := false
	for _, cmd := range cmds {
		output, err := e.sandbox.ExecCollect(ctx, containerID, []string{
			"bash", "-c", "cd /workspace/repo && " + cmd,
		})
		if output != "" {
			allOutput.WriteString(output)
			allOutput.WriteString("\n")
		}
		if err != nil {
			anyFailed = true
		}
	}

	passed := !anyFailed
	if passed {
		e.emitEvent(sess.ID, "status", "Tests and linting passed")
	} else {
		e.emitEvent(sess.ID, "status", "Tests or linting failed")
	}

	return &sandbox.VerifyResult{
		Passed:   passed,
		Output:   allOutput.String(),
		Feedback: allOutput.String(),
	}
}

// CreatePRCommentSession creates a new task session that addresses a PR comment.
func (e *Engine) CreatePRCommentSession(original *model.Session, event *gitprovider.WebhookEvent) (*model.Session, error) {
	id := uuid.New().String()[:8]
	now := time.Now().UTC()

	prompt := fmt.Sprintf(`A reviewer left the following comment on Pull Request #%d in repository %s.

## Reviewer Comment (by @%s)
%s

## Instructions
- Address the reviewer's feedback by making the necessary code changes
- The changes should be committed to the existing PR branch
- Keep changes minimal and focused on the feedback
- Run tests after making changes if a test suite exists
- Do not make unrelated changes`,
		event.PRNumber, event.Repo, event.CommentUser, event.CommentBody)

	sess := &model.Session{
		ID:        id,
		Repo:      event.Repo,
		Prompt:    prompt,
		Mode:      model.ModeTask,
		Status:    model.StatusPending,
		Branch:    original.Branch,
		PRUrl:     original.PRUrl,
		PRNumber:  original.PRNumber,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := e.store.CreateSession(sess); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runPRCommentSession(sess.ID, event)
	}()

	return sess, nil
}

func (e *Engine) runPRCommentSession(sessionID string, event *gitprovider.WebhookEvent) {
	ctx := e.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	sess, err := e.store.GetSession(sessionID)
	if err != nil {
		log.Printf("PR comment session %s not found: %v", sessionID, err)
		return
	}

	ackMsg := fmt.Sprintf("🤖 TeleCoder is addressing this comment (session `%s`)...", sess.ID)
	if err := e.git.ReplyToPRComment(ctx, sess.Repo, sess.PRNumber, ackMsg); err != nil {
		log.Printf("Failed to post ack comment: %v", err)
	}

	result, err := e.runSandboxRound(ctx, sess, sess.Prompt)
	if err != nil {
		e.failSession(sess, fmt.Sprintf("PR comment session failed: %v", err))
		replyMsg := fmt.Sprintf("❌ TeleCoder failed to address this comment (session `%s`): %v", sess.ID, err)
		e.git.ReplyToPRComment(ctx, sess.Repo, sess.PRNumber, replyMsg)
		return
	}

	defer func() {
		if result.containerID != "" {
			e.sandbox.Stop(ctx, result.containerID)
		}
	}()

	if result.exitCode != 0 {
		errMsg := fmt.Sprintf("sandbox exited with code %d", result.exitCode)
		if result.lastLine != "" {
			errMsg += ": " + result.lastLine
		}
		e.failSession(sess, errMsg)
		replyMsg := fmt.Sprintf("❌ TeleCoder encountered an error while addressing this comment (session `%s`): %s", sess.ID, errMsg)
		e.git.ReplyToPRComment(ctx, sess.Repo, sess.PRNumber, replyMsg)
		return
	}

	sess.Status = model.StatusComplete
	e.store.UpdateSession(sess)
	e.emitEvent(sess.ID, "done", sess.PRUrl)

	replyMsg := fmt.Sprintf("✅ TeleCoder has pushed changes to address this comment (session `%s`). Please review the updated code.", sess.ID)
	if err := e.git.ReplyToPRComment(ctx, sess.Repo, sess.PRNumber, replyMsg); err != nil {
		log.Printf("Failed to post completion comment: %v", err)
	}
}

// --- Helpers ---

func (e *Engine) failSession(sess *model.Session, errMsg string) {
	log.Printf("Session %s failed: %s", sess.ID, errMsg)
	sess.Status = model.StatusError
	sess.Error = errMsg
	e.store.UpdateSession(sess)
	e.emitEvent(sess.ID, "error", errMsg)
}

func (e *Engine) emitEvent(sessionID, eventType, data string) {
	event := &model.Event{
		SessionID: sessionID,
		Type:      eventType,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}
	if err := e.store.AddEvent(event); err != nil {
		log.Printf("Error storing event: %v", err)
	}
	e.bus.Publish(sessionID, event)
}

func (e *Engine) dispatchLogLine(sessionID, line string) {
	a := agent.Default()
	ev := a.ParseEvent(line)
	if ev != nil {
		ev.SessionID = sessionID
		e.emitParsedEvent(sessionID, ev)
	} else {
		e.emitEvent(sessionID, "output", line)
	}
}

// emitParsedEvent stores and publishes an event that was already parsed by a CodingAgent.
func (e *Engine) emitParsedEvent(sessionID string, ev *model.Event) {
	ev.SessionID = sessionID
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}
	if ev.Type == "done" {
		e.emitEvent(sessionID, "status", fmt.Sprintf("Branch pushed: %s", ev.Data))
		return
	}
	if err := e.store.AddEvent(ev); err != nil {
		log.Printf("Error storing event: %v", err)
	}
	e.bus.Publish(sessionID, ev)
}
