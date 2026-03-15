// Package engine orchestrates coding sessions: workspace setup, ACP agent
// communication, event storage, and verification.
package engine

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/jxucoder/telecoder/internal/acp"
	"github.com/jxucoder/telecoder/internal/config"
	"github.com/jxucoder/telecoder/internal/model"
	"github.com/jxucoder/telecoder/internal/store"
	"github.com/jxucoder/telecoder/internal/workspace"
)

// Engine manages coding sessions.
type Engine struct {
	cfg   *config.Config
	store *store.Store
	ws    *workspace.Manager

	mu       sync.Mutex
	agents   map[string]*acp.Client // sessionID -> active ACP client
	cancels  map[string]context.CancelFunc
	eventSub map[string][]chan *model.Event
}

// New creates a new engine.
func New(cfg *config.Config, st *store.Store) *Engine {
	return &Engine{
		cfg:      cfg,
		store:    st,
		ws:       workspace.NewManager(cfg.WorkspacesDir()),
		agents:   make(map[string]*acp.Client),
		cancels:  make(map[string]context.CancelFunc),
		eventSub: make(map[string][]chan *model.Event),
	}
}

// CreateAndRun creates a task-mode session, runs the prompt, and returns the session.
func (e *Engine) CreateAndRun(ctx context.Context, repo, prompt string) (*model.Session, error) {
	sess := &model.Session{
		ID:        uuid.New().String()[:8],
		Repo:      repo,
		Prompt:    prompt,
		Mode:      model.ModeTask,
		Status:    model.StatusPending,
		Branch:    fmt.Sprintf("telecoder/%s", uuid.New().String()[:8]),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := e.store.CreateSession(sess); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Run in background.
	go e.runTask(sess)

	return sess, nil
}

// CreateChat creates an interactive chat-mode session.
func (e *Engine) CreateChat(ctx context.Context, repo string) (*model.Session, error) {
	sess := &model.Session{
		ID:        uuid.New().String()[:8],
		Repo:      repo,
		Mode:      model.ModeChat,
		Status:    model.StatusPending,
		Branch:    fmt.Sprintf("telecoder/%s", uuid.New().String()[:8]),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := e.store.CreateSession(sess); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	go e.initChat(sess)

	return sess, nil
}

// SendMessage sends a chat message to an active session.
func (e *Engine) SendMessage(ctx context.Context, sessionID, content string) (string, error) {
	e.mu.Lock()
	agent, ok := e.agents[sessionID]
	e.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("session %s has no active agent connection", sessionID)
	}

	sess, err := e.store.GetSession(sessionID)
	if err != nil {
		return "", err
	}

	// Store user message.
	e.store.InsertMessage(&model.Message{
		SessionID: sessionID,
		Role:      "user",
		Content:   content,
		CreatedAt: time.Now().UTC(),
	})

	e.emitEvent(sessionID, "status", "processing message")

	response, err := agent.Prompt(ctx, sess.ACPSessionID, content)
	if err != nil {
		e.emitEvent(sessionID, "error", err.Error())
		return "", err
	}

	// Store assistant message.
	e.store.InsertMessage(&model.Message{
		SessionID: sessionID,
		Role:      "assistant",
		Content:   response,
		CreatedAt: time.Now().UTC(),
	})

	e.emitEvent(sessionID, "output", response)
	return response, nil
}

// StopSession stops a running session.
func (e *Engine) StopSession(sessionID string) error {
	e.mu.Lock()
	cancel, ok := e.cancels[sessionID]
	agent := e.agents[sessionID]
	e.mu.Unlock()

	if ok {
		cancel()
	}
	if agent != nil {
		agent.Close()
	}

	sess, err := e.store.GetSession(sessionID)
	if err != nil {
		return err
	}

	sess.Status = model.StatusStopped
	return e.store.UpdateSession(sess)
}

// GetSession returns a session by ID.
func (e *Engine) GetSession(id string) (*model.Session, error) {
	return e.store.GetSession(id)
}

// ListSessions returns all sessions.
func (e *Engine) ListSessions() ([]*model.Session, error) {
	return e.store.ListSessions()
}

// GetEvents returns events for a session.
func (e *Engine) GetEvents(sessionID string, afterID int64) ([]*model.Event, error) {
	return e.store.ListEvents(sessionID, afterID)
}

// GetMessages returns chat messages for a session.
func (e *Engine) GetMessages(sessionID string) ([]*model.Message, error) {
	return e.store.ListMessages(sessionID)
}

// Subscribe returns a channel that receives new events for a session.
func (e *Engine) Subscribe(sessionID string) chan *model.Event {
	ch := make(chan *model.Event, 64)
	e.mu.Lock()
	e.eventSub[sessionID] = append(e.eventSub[sessionID], ch)
	e.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (e *Engine) Unsubscribe(sessionID string, ch chan *model.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	subs := e.eventSub[sessionID]
	for i, s := range subs {
		if s == ch {
			e.eventSub[sessionID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// runTask runs a task-mode session end-to-end.
func (e *Engine) runTask(sess *model.Session) {
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancels[sess.ID] = cancel
	e.mu.Unlock()
	defer cancel()
	defer e.cleanup(sess.ID)

	// 1. Set up workspace.
	sess.Status = model.StatusRunning
	e.store.UpdateSession(sess)
	e.emitEvent(sess.ID, "status", "setting up workspace")

	workDir, err := e.ws.Setup(sess.ID, sess.Repo, sess.Branch)
	if err != nil {
		e.failSession(sess, "workspace setup: "+err.Error())
		return
	}
	sess.WorkDir = workDir
	e.store.UpdateSession(sess)

	// 2. Connect to agent via ACP.
	e.emitEvent(sess.ID, "status", "connecting to agent")

	agent := acp.NewClient(e.cfg.AgentCommand, workDir, func(update acpsdk.SessionUpdate) {
		e.handleACPUpdate(sess.ID, update)
	})

	if err := agent.Connect(ctx); err != nil {
		e.failSession(sess, "agent connect: "+err.Error())
		return
	}

	e.mu.Lock()
	e.agents[sess.ID] = agent
	e.mu.Unlock()

	// 3. Create ACP session.
	acpSessionID, err := agent.NewSession(ctx)
	if err != nil {
		e.failSession(sess, "acp new session: "+err.Error())
		return
	}
	sess.ACPSessionID = acpSessionID
	e.store.UpdateSession(sess)

	// 4. Send prompt.
	e.emitEvent(sess.ID, "status", "running prompt")

	response, err := agent.Prompt(ctx, acpSessionID, sess.Prompt)
	if err != nil {
		e.failSession(sess, "acp prompt: "+err.Error())
		return
	}

	e.emitEvent(sess.ID, "output", response)

	// 5. Run verification if configured.
	e.runVerification(ctx, sess)

	// 6. Collect results.
	e.emitEvent(sess.ID, "status", "collecting results")

	changed, _ := e.ws.ChangedFiles(sess.ID)
	if len(changed) > 0 {
		sess.ResultType = model.ResultPR
		diff, _ := e.ws.Diff(sess.ID)
		sess.ResultText = fmt.Sprintf("Changed %d files:\n%s\n\n%s",
			len(changed), strings.Join(changed, "\n"), truncate(diff, 4000))
		branch, _ := e.ws.CurrentBranch(sess.ID)
		sess.Branch = branch
	} else {
		sess.ResultType = model.ResultText
		sess.ResultText = truncate(response, 4000)
	}

	sess.Status = model.StatusComplete
	e.store.UpdateSession(sess)
	e.emitEvent(sess.ID, "done", string(sess.ResultType))
}

// initChat initializes a chat-mode session.
func (e *Engine) initChat(sess *model.Session) {
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancels[sess.ID] = cancel
	e.mu.Unlock()

	// Set up workspace.
	sess.Status = model.StatusRunning
	e.store.UpdateSession(sess)

	workDir, err := e.ws.Setup(sess.ID, sess.Repo, sess.Branch)
	if err != nil {
		e.failSession(sess, "workspace setup: "+err.Error())
		cancel()
		return
	}
	sess.WorkDir = workDir

	// Connect to agent via ACP.
	agent := acp.NewClient(e.cfg.AgentCommand, workDir, func(update acpsdk.SessionUpdate) {
		e.handleACPUpdate(sess.ID, update)
	})

	if err := agent.Connect(ctx); err != nil {
		e.failSession(sess, "agent connect: "+err.Error())
		cancel()
		return
	}

	acpSessionID, err := agent.NewSession(ctx)
	if err != nil {
		e.failSession(sess, "acp new session: "+err.Error())
		agent.Close()
		cancel()
		return
	}

	sess.ACPSessionID = acpSessionID
	sess.Status = model.StatusIdle
	e.store.UpdateSession(sess)

	e.mu.Lock()
	e.agents[sess.ID] = agent
	e.mu.Unlock()

	e.emitEvent(sess.ID, "status", "ready for messages")
}

// handleACPUpdate converts ACP session updates into TeleCoder events.
func (e *Engine) handleACPUpdate(sessionID string, update acpsdk.SessionUpdate) {
	switch {
	case update.AgentMessageChunk != nil:
		if update.AgentMessageChunk.Content.Text != nil {
			e.emitEvent(sessionID, "output", update.AgentMessageChunk.Content.Text.Text)
		}
	case update.AgentThoughtChunk != nil:
		if update.AgentThoughtChunk.Content.Text != nil {
			e.emitEvent(sessionID, "thought", update.AgentThoughtChunk.Content.Text.Text)
		}
	case update.ToolCall != nil:
		e.emitEvent(sessionID, "tool_call", update.ToolCall.Title)
	case update.ToolCallUpdate != nil:
		e.emitEvent(sessionID, "tool_update", string(update.ToolCallUpdate.ToolCallId))
	case update.Plan != nil:
		e.emitEvent(sessionID, "status", "planning")
	}
}

// runVerification runs optional test and lint commands.
func (e *Engine) runVerification(ctx context.Context, sess *model.Session) {
	if e.cfg.VerifyCommand != "" {
		e.emitEvent(sess.ID, "status", "running verification: "+e.cfg.VerifyCommand)
		result := runCommand(ctx, sess.WorkDir, e.cfg.VerifyCommand)
		e.emitEvent(sess.ID, "verify", result)
	}
	if e.cfg.LintCommand != "" {
		e.emitEvent(sess.ID, "status", "running lint: "+e.cfg.LintCommand)
		result := runCommand(ctx, sess.WorkDir, e.cfg.LintCommand)
		e.emitEvent(sess.ID, "lint", result)
	}
}

func runCommand(ctx context.Context, dir, command string) string {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("FAIL: %s\n%s", err, string(out))
	}
	return fmt.Sprintf("PASS\n%s", string(out))
}

// emitEvent stores and broadcasts an event.
func (e *Engine) emitEvent(sessionID, typ, data string) {
	ev := &model.Event{
		SessionID: sessionID,
		Type:      typ,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}
	if err := e.store.InsertEvent(ev); err != nil {
		log.Printf("store event: %v", err)
	}

	e.mu.Lock()
	subs := e.eventSub[sessionID]
	e.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// Drop if subscriber is full.
		}
	}
}

func (e *Engine) failSession(sess *model.Session, errMsg string) {
	log.Printf("session %s failed: %s", sess.ID, errMsg)
	sess.Status = model.StatusError
	sess.Error = errMsg
	e.store.UpdateSession(sess)
	e.emitEvent(sess.ID, "error", errMsg)
}

func (e *Engine) cleanup(sessionID string) {
	e.mu.Lock()
	if agent, ok := e.agents[sessionID]; ok {
		agent.Close()
		delete(e.agents, sessionID)
	}
	delete(e.cancels, sessionID)
	e.mu.Unlock()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n... (truncated)"
}
