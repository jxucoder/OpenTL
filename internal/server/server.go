// Package server provides the OpenTL HTTP API server.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/jxucoder/opentl/internal/config"
	"github.com/jxucoder/opentl/internal/decomposer"
	"github.com/jxucoder/opentl/internal/github"
	"github.com/jxucoder/opentl/internal/indexer"
	"github.com/jxucoder/opentl/internal/orchestrator"
	"github.com/jxucoder/opentl/internal/sandbox"
	"github.com/jxucoder/opentl/internal/session"
	opentlslack "github.com/jxucoder/opentl/internal/slack"
	opentltelegram "github.com/jxucoder/opentl/internal/telegram"
)

// Server is the OpenTL HTTP API server.
type Server struct {
	config       *config.Config
	store        *session.Store
	bus          *session.EventBus
	sandbox      *sandbox.Manager
	github       *github.Client
	orchestrator *orchestrator.Orchestrator // nil if no LLM key for planner
	router       chi.Router
	slackBot     *opentlslack.Bot    // nil if Slack is not configured
	telegramBot  *opentltelegram.Bot // nil if Telegram is not configured
}

// New creates a new Server with all dependencies.
func New(cfg *config.Config) (*Server, error) {
	store, err := session.NewStore(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("initializing store: %w", err)
	}

	// Initialize the orchestrator if an LLM key is available.
	// If not, sessions will run without plan/review (direct prompt passthrough).
	var orch *orchestrator.Orchestrator
	if llm, err := orchestrator.NewLLMClientFromEnv(); err == nil {
		orch = orchestrator.New(llm)
		log.Println("Orchestrator enabled (plan/code/review pipeline)")
	} else {
		log.Println("Orchestrator disabled (no LLM key for planner, using direct mode)")
	}

	s := &Server{
		config:       cfg,
		store:        store,
		bus:          session.NewEventBus(),
		sandbox:      sandbox.NewManager(),
		github:       github.NewClient(cfg.GitHubToken),
		orchestrator: orch,
	}

	s.router = s.buildRouter()

	// Initialize Slack bot if configured.
	if cfg.SlackEnabled() {
		s.slackBot = opentlslack.NewBot(
			cfg.SlackBotToken,
			cfg.SlackAppToken,
			cfg.SlackDefaultRepo,
			s.store,
			s.bus,
			s, // Server implements slack.SessionCreator
		)
		log.Println("Slack bot enabled (Socket Mode)")
	}

	// Initialize Telegram bot if configured.
	if cfg.TelegramEnabled() {
		tgBot, err := opentltelegram.NewBot(
			cfg.TelegramBotToken,
			cfg.TelegramDefaultRepo,
			s.store,
			s.bus,
			s, // Server implements telegram.ChatSessionCreator
		)
		if err != nil {
			log.Printf("Warning: failed to initialize Telegram bot: %v", err)
		} else {
			s.telegramBot = tgBot
			log.Println("Telegram bot enabled (long polling)")
		}
	}

	return s, nil
}

// Start starts the HTTP server and (optionally) the Slack bot.
func (s *Server) Start(ctx context.Context) error {
	// Ensure Docker network exists.
	if err := s.sandbox.EnsureNetwork(ctx, s.config.DockerNetwork); err != nil {
		log.Printf("Warning: could not create Docker network: %v", err)
	}

	// Start idle chat session reaper.
	go s.reapIdleChatSessions(ctx)

	// Start chat bots in background if configured.
	if s.slackBot != nil {
		go func() {
			if err := s.slackBot.Run(ctx); err != nil {
				log.Printf("Slack bot error: %v", err)
			}
		}()
	}
	if s.telegramBot != nil {
		go func() {
			if err := s.telegramBot.Run(ctx); err != nil {
				log.Printf("Telegram bot error: %v", err)
			}
		}()
	}

	srv := &http.Server{
		Addr:    s.config.ServerAddr,
		Handler: s.router,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("OpenTL server listening on %s", s.config.ServerAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return s.store.Close()
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(5 * time.Minute))

	r.Route("/api", func(r chi.Router) {
		r.Post("/sessions", s.handleCreateSession)
		r.Get("/sessions", s.handleListSessions)
		r.Get("/sessions/{id}", s.handleGetSession)
		r.Get("/sessions/{id}/events", s.handleSessionEvents)
		r.Get("/sessions/{id}/messages", s.handleGetMessages)
		r.Post("/sessions/{id}/messages", s.handleSendMessage)
		r.Post("/sessions/{id}/pr", s.handleCreatePR)
		r.Post("/sessions/{id}/stop", s.handleStopSession)
	})

	// Health check.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return r
}

// --- Request/Response types ---

type createSessionRequest struct {
	Repo   string `json:"repo"`
	Prompt string `json:"prompt"`
	Mode   string `json:"mode,omitempty"` // "task" (default) or "chat"
}

type createSessionResponse struct {
	ID     string `json:"id"`
	Branch string `json:"branch"`
	Mode   string `json:"mode"`
}

type sendMessageRequest struct {
	Content string `json:"content"`
}

type sendMessageResponse struct {
	MessageID int64  `json:"message_id"`
	SessionID string `json:"session_id"`
}

type createPRResponse struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// --- Handlers ---

// CreateAndRunSession creates a new task-mode session and starts the sandbox in the background.
// This is the shared entry point used by the HTTP API and the Slack bot.
func (s *Server) CreateAndRunSession(repo, prompt string) (*session.Session, error) {
	id := uuid.New().String()[:8]
	branch := fmt.Sprintf("opentl/%s", id)
	now := time.Now().UTC()

	sess := &session.Session{
		ID:        id,
		Repo:      repo,
		Prompt:    prompt,
		Mode:      session.ModeTask,
		Status:    session.StatusPending,
		Branch:    branch,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.CreateSession(sess); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	// Start sandbox in background.
	go s.runSession(sess)

	return sess, nil
}

// CreateChatSession creates a new chat-mode session with a persistent sandbox.
// The sandbox stays alive between messages. Returns the session once the
// sandbox is ready (repo cloned, deps installed).
func (s *Server) CreateChatSession(repo string) (*session.Session, error) {
	id := uuid.New().String()[:8]
	branch := fmt.Sprintf("opentl/%s", id)
	now := time.Now().UTC()

	sess := &session.Session{
		ID:        id,
		Repo:      repo,
		Prompt:    "", // No initial prompt for chat mode.
		Mode:      session.ModeChat,
		Status:    session.StatusPending,
		Branch:    branch,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.CreateSession(sess); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	// Start persistent sandbox in background and set up the repo.
	go s.initChatSession(sess)

	return sess, nil
}

// initChatSession starts a persistent container and runs setup.sh.
func (s *Server) initChatSession(sess *session.Session) {
	ctx := context.Background()

	s.emitEvent(sess.ID, "status", "Starting sandbox...")

	containerID, err := s.sandbox.StartPersistent(ctx, sandbox.StartOptions{
		SessionID: sess.ID,
		Repo:      sess.Repo,
		Branch:    sess.Branch,
		Image:     s.config.DockerImage,
		Env:       s.config.SandboxEnv(),
		Network:   s.config.DockerNetwork,
	})
	if err != nil {
		s.failSession(sess, fmt.Sprintf("failed to start sandbox: %v", err))
		return
	}

	sess.ContainerID = containerID
	s.store.UpdateSession(sess)

	// Run setup script (clone repo, install deps).
	s.emitEvent(sess.ID, "status", "Setting up repository...")
	setupStream, err := s.sandbox.Exec(ctx, containerID, []string{"/setup.sh"})
	if err != nil {
		s.failSession(sess, fmt.Sprintf("failed to run setup: %v", err))
		return
	}

	for setupStream.Scan() {
		line := setupStream.Text()
		switch {
		case strings.HasPrefix(line, "###OPENTL_STATUS### "):
			msg := strings.TrimPrefix(line, "###OPENTL_STATUS### ")
			s.emitEvent(sess.ID, "status", msg)
		case strings.HasPrefix(line, "###OPENTL_ERROR### "):
			msg := strings.TrimPrefix(line, "###OPENTL_ERROR### ")
			s.emitEvent(sess.ID, "error", msg)
		default:
			s.emitEvent(sess.ID, "output", line)
		}
	}
	setupStream.Close()

	// Mark session as idle (ready for messages).
	sess.Status = session.StatusIdle
	s.store.UpdateSession(sess)
	s.emitEvent(sess.ID, "status", "Ready — send a message to start coding")
}

// reapIdleChatSessions periodically stops chat sandboxes that have been idle
// longer than ChatIdleTimeout.
func (s *Server) reapIdleChatSessions(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sessions, err := s.store.ListSessions()
			if err != nil {
				continue
			}
			for _, sess := range sessions {
				if sess.Mode != session.ModeChat || sess.Status != session.StatusIdle {
					continue
				}
				if time.Since(sess.UpdatedAt) > s.config.ChatIdleTimeout {
					log.Printf("Reaping idle chat session %s (idle for %v)", sess.ID, time.Since(sess.UpdatedAt))
					if sess.ContainerID != "" {
						s.sandbox.Stop(ctx, sess.ContainerID)
					}
					sess.Status = session.StatusError
					sess.Error = "session timed out due to inactivity"
					s.store.UpdateSession(sess)
					s.emitEvent(sess.ID, "status", "Session stopped (idle timeout)")
				}
			}
		}
	}
}

// SendChatMessage sends a user message to a chat session and runs the agent.
// Returns the stored message or an error.
func (s *Server) SendChatMessage(sessionID, content string) (*session.Message, error) {
	sess, err := s.store.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	if sess.Mode != session.ModeChat {
		return nil, fmt.Errorf("session %s is not a chat session", sessionID)
	}
	if sess.Status != session.StatusIdle {
		return nil, fmt.Errorf("session is %s, not idle (wait for current operation to finish)", sess.Status)
	}
	if sess.ContainerID == "" {
		return nil, fmt.Errorf("session has no container")
	}

	// Check message limit.
	msgs, _ := s.store.GetMessages(sessionID)
	userMsgCount := 0
	for _, m := range msgs {
		if m.Role == "user" {
			userMsgCount++
		}
	}
	if userMsgCount >= s.config.ChatMaxMessages {
		return nil, fmt.Errorf("message limit reached (%d messages)", s.config.ChatMaxMessages)
	}

	// Store the user message.
	msg := &session.Message{
		SessionID: sessionID,
		Role:      "user",
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.AddMessage(msg); err != nil {
		return nil, fmt.Errorf("storing message: %w", err)
	}

	// Run the agent in the background.
	go s.runChatMessage(sess, msg)

	return msg, nil
}

// runChatMessage executes the coding agent for a single chat message.
func (s *Server) runChatMessage(sess *session.Session, msg *session.Message) {
	ctx := context.Background()

	// Mark session as running.
	sess.Status = session.StatusRunning
	s.store.UpdateSession(sess)
	s.emitEvent(sess.ID, "status", "Running agent...")

	// Run the coding agent with the user's message.
	agentStream, err := s.sandbox.Exec(ctx, sess.ContainerID, []string{
		"bash", "-c",
		fmt.Sprintf("cd /workspace/repo && opencode -p %q 2>&1", msg.Content),
	})
	if err != nil {
		log.Printf("Chat message exec failed: %v", err)
		s.emitEvent(sess.ID, "error", fmt.Sprintf("Agent failed to start: %v", err))
		sess.Status = session.StatusIdle
		s.store.UpdateSession(sess)
		return
	}

	var outputLines []string
	for agentStream.Scan() {
		line := agentStream.Text()
		outputLines = append(outputLines, line)
		s.emitEvent(sess.ID, "output", line)
	}
	agentStream.Close()

	// Store the assistant response.
	assistantContent := strings.Join(outputLines, "\n")
	if assistantContent == "" {
		assistantContent = "(no output)"
	}
	assistantMsg := &session.Message{
		SessionID: sess.ID,
		Role:      "assistant",
		Content:   assistantContent,
		CreatedAt: time.Now().UTC(),
	}
	s.store.AddMessage(assistantMsg)

	// Mark session as idle (ready for next message).
	sess.Status = session.StatusIdle
	s.store.UpdateSession(sess)
	s.emitEvent(sess.ID, "status", "Ready")
}

// CreatePRFromChat commits all changes in a chat session and creates a PR.
func (s *Server) CreatePRFromChat(sessionID string) (string, int, error) {
	sess, err := s.store.GetSession(sessionID)
	if err != nil {
		return "", 0, fmt.Errorf("session not found: %w", err)
	}
	if sess.Mode != session.ModeChat {
		return "", 0, fmt.Errorf("session %s is not a chat session", sessionID)
	}
	if sess.Status != session.StatusIdle {
		return "", 0, fmt.Errorf("session is %s, wait for it to be idle", sess.Status)
	}
	if sess.ContainerID == "" {
		return "", 0, fmt.Errorf("session has no container")
	}

	ctx := context.Background()

	s.emitEvent(sess.ID, "status", "Committing and pushing changes...")

	// Build a commit message from the conversation.
	msgs, _ := s.store.GetMessages(sessionID)
	commitDesc := "chat session changes"
	for _, m := range msgs {
		if m.Role == "user" {
			commitDesc = m.Content
			break
		}
	}

	if err := s.sandbox.CommitAndPush(ctx, sess.ContainerID, commitDesc, sess.Branch); err != nil {
		return "", 0, fmt.Errorf("commit/push failed: %w", err)
	}

	s.emitEvent(sess.ID, "status", "Creating pull request...")

	defaultBranch, err := s.github.GetDefaultBranch(ctx, sess.Repo)
	if err != nil {
		defaultBranch = "main"
	}

	prTitle := fmt.Sprintf("opentl: %s", truncate(commitDesc, 72))
	prBody := fmt.Sprintf("## OpenTL Chat Session `%s`\n\n", sess.ID)
	for _, m := range msgs {
		if m.Role == "user" {
			prBody += fmt.Sprintf("> **You:** %s\n\n", m.Content)
		}
	}
	prBody += "---\n*Created by [OpenTL](https://github.com/jxucoder/opentl)*"

	prURL, prNumber, err := s.github.CreatePR(ctx, github.PROptions{
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
	sess.Status = session.StatusComplete
	s.store.UpdateSession(sess)

	s.emitEvent(sess.ID, "done", prURL)

	return prURL, prNumber, nil
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "repo is required")
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = "task"
	}

	switch mode {
	case "chat":
		sess, err := s.CreateChatSession(req.Repo)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create chat session")
			log.Printf("Error creating chat session: %v", err)
			return
		}
		writeJSON(w, http.StatusCreated, createSessionResponse{
			ID:     sess.ID,
			Branch: sess.Branch,
			Mode:   "chat",
		})

	case "task":
		if req.Prompt == "" {
			writeError(w, http.StatusBadRequest, "prompt is required for task mode")
			return
		}
		sess, err := s.CreateAndRunSession(req.Repo, req.Prompt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create session")
			log.Printf("Error creating session: %v", err)
			return
		}
		writeJSON(w, http.StatusCreated, createSessionResponse{
			ID:     sess.ID,
			Branch: sess.Branch,
			Mode:   "task",
		})

	default:
		writeError(w, http.StatusBadRequest, "mode must be 'task' or 'chat'")
	}
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		log.Printf("Error listing sessions: %v", err)
		return
	}
	if sessions == nil {
		sessions = []*session.Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.store.GetSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Verify session exists.
	if _, err := s.store.GetSession(id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Send historical events first.
	events, _ := s.store.GetEvents(id, 0)
	for _, e := range events {
		writeSSE(w, e)
	}
	flusher.Flush()

	// Subscribe to real-time events.
	ch := s.bus.Subscribe(id)
	defer s.bus.Unsubscribe(id, ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, event)
			flusher.Flush()
		}
	}
}

func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.store.GetSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if sess.ContainerID != "" {
		if err := s.sandbox.Stop(r.Context(), sess.ContainerID); err != nil {
			log.Printf("Error stopping container: %v", err)
		}
	}

	sess.Status = session.StatusError
	sess.Error = "stopped by user"
	s.store.UpdateSession(sess)

	writeJSON(w, http.StatusOK, sess)
}

// --- Chat session handlers ---

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	msg, err := s.SendChatMessage(id, req.Content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, sendMessageResponse{
		MessageID: msg.ID,
		SessionID: id,
	})
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	msgs, err := s.store.GetMessages(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get messages")
		return
	}
	if msgs == nil {
		msgs = []*session.Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prURL, prNumber, err := s.CreatePRFromChat(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, createPRResponse{
		URL:    prURL,
		Number: prNumber,
	})
}

// --- Session execution ---

// sandboxRoundResult holds the outcome of a single sandbox execution round.
type sandboxRoundResult struct {
	containerID string
	exitCode    int
	lastLine    string
}

func (s *Server) runSession(sess *session.Session) {
	ctx := context.Background()

	// Phase 2: Index the repo for codebase context.
	var repoContext string
	s.emitEvent(sess.ID, "status", "Indexing repository...")
	rc, err := indexer.Index(ctx, s.github.GoGH(), sess.Repo)
	if err != nil {
		log.Printf("Repo indexing failed (proceeding without context): %v", err)
		s.emitEvent(sess.ID, "status", "Repo indexing failed, proceeding without context")
	} else {
		repoContext = rc.String()
		s.emitEvent(sess.ID, "status", "Repository indexed")
	}

	// Phase 2: Decompose the task into sub-tasks (if orchestrator is available).
	var subTasks []decomposer.SubTask
	if s.orchestrator != nil {
		s.emitEvent(sess.ID, "status", "Analyzing task complexity...")
		var err error
		subTasks, err = decomposer.Decompose(ctx, s.orchestrator.LLM(), sess.Prompt, repoContext)
		if err != nil {
			log.Printf("Task decomposition failed (treating as single task): %v", err)
			subTasks = []decomposer.SubTask{{Title: "Complete task", Description: sess.Prompt}}
		}
		if len(subTasks) > 1 {
			s.emitEvent(sess.ID, "status", fmt.Sprintf("Task decomposed into %d steps", len(subTasks)))
		}
	} else {
		subTasks = []decomposer.SubTask{{Title: "Complete task", Description: sess.Prompt}}
	}

	// Execute each sub-task through the plan -> sandbox -> review pipeline.
	var lastContainerID string
	for i, task := range subTasks {
		if len(subTasks) > 1 {
			s.emitEvent(sess.ID, "step", fmt.Sprintf("Step %d/%d: %s", i+1, len(subTasks), task.Title))
		}

		containerID, err := s.runSubTask(ctx, sess, task.Description, repoContext)
		if err != nil {
			s.failSession(sess, fmt.Sprintf("step %d/%d failed: %v", i+1, len(subTasks), err))
			if lastContainerID != "" {
				s.sandbox.Stop(ctx, lastContainerID)
			}
			return
		}

		// Clean up previous step's container.
		if lastContainerID != "" && lastContainerID != containerID {
			s.sandbox.Stop(ctx, lastContainerID)
		}
		lastContainerID = containerID
	}

	// Create PR.
	s.emitEvent(sess.ID, "status", "Creating pull request...")

	defaultBranch, err := s.github.GetDefaultBranch(ctx, sess.Repo)
	if err != nil {
		defaultBranch = "main"
	}

	prTitle := fmt.Sprintf("opentl: %s", truncate(sess.Prompt, 72))
	prBody := fmt.Sprintf("## OpenTL Session `%s`\n\n**Prompt:**\n> %s\n\n---\n*Created by [OpenTL](https://github.com/jxucoder/opentl)*",
		sess.ID, sess.Prompt)

	prURL, prNumber, err := s.github.CreatePR(ctx, github.PROptions{
		Repo:   sess.Repo,
		Branch: sess.Branch,
		Base:   defaultBranch,
		Title:  prTitle,
		Body:   prBody,
	})
	if err != nil {
		s.failSession(sess, fmt.Sprintf("failed to create PR: %v", err))
		return
	}

	sess.Status = session.StatusComplete
	sess.PRUrl = prURL
	sess.PRNumber = prNumber
	s.store.UpdateSession(sess)

	s.emitEvent(sess.ID, "done", prURL)

	// Clean up last container.
	if lastContainerID != "" {
		s.sandbox.Stop(ctx, lastContainerID)
	}
}

// runSubTask executes a single sub-task through the plan -> sandbox -> review
// pipeline with up to MaxRevisions review-revision rounds. Returns the last
// container ID used, or an error.
func (s *Server) runSubTask(ctx context.Context, sess *session.Session, taskPrompt, repoContext string) (string, error) {
	// Plan step.
	prompt := taskPrompt
	var plan string
	if s.orchestrator != nil {
		s.emitEvent(sess.ID, "status", "Planning task...")
		var err error
		plan, err = s.orchestrator.Plan(ctx, sess.Repo, taskPrompt, repoContext)
		if err != nil {
			log.Printf("Planning failed (falling back to direct prompt): %v", err)
			s.emitEvent(sess.ID, "status", "Planning failed, using direct prompt")
		} else {
			s.emitEvent(sess.ID, "output", "## Plan\n"+plan)
			prompt = s.orchestrator.EnrichPrompt(taskPrompt, plan)
		}
	}

	// Sandbox execution with review-revision loop.
	maxRounds := s.config.MaxRevisions
	var lastContainerID string

	for round := 0; round <= maxRounds; round++ {
		if round > 0 {
			s.emitEvent(sess.ID, "status", fmt.Sprintf("Starting revision round %d/%d...", round, maxRounds))
		}

		result, err := s.runSandboxRound(ctx, sess, prompt)
		if err != nil {
			return lastContainerID, err
		}

		// Clean up previous round's container.
		if lastContainerID != "" && lastContainerID != result.containerID {
			s.sandbox.Stop(ctx, lastContainerID)
		}
		lastContainerID = result.containerID

		if result.exitCode != 0 {
			errMsg := fmt.Sprintf("sandbox exited with code %d", result.exitCode)
			if result.lastLine != "" {
				errMsg += ": " + result.lastLine
			}
			return lastContainerID, fmt.Errorf("%s", errMsg)
		}

		// Review step (if orchestrator is available and we have a plan).
		if s.orchestrator == nil || plan == "" {
			break
		}

		s.emitEvent(sess.ID, "status", "Reviewing changes...")
		diff := s.getDiffFromContainer(ctx, result.containerID)
		if diff == "" {
			s.emitEvent(sess.ID, "status", "No diff found, skipping review")
			break
		}

		review, err := s.orchestrator.Review(ctx, taskPrompt, plan, diff)
		if err != nil {
			log.Printf("Review failed (proceeding): %v", err)
			s.emitEvent(sess.ID, "status", "Review failed, proceeding")
			break
		}

		if review.Approved {
			s.emitEvent(sess.ID, "output", "## Review\n"+review.Feedback)
			break
		}

		// Review requested revision.
		s.emitEvent(sess.ID, "output", "## Review Feedback\n"+review.Feedback)

		if round >= maxRounds {
			s.emitEvent(sess.ID, "status", fmt.Sprintf("Max revision rounds (%d) reached, proceeding", maxRounds))
			break
		}

		prompt = s.orchestrator.RevisePrompt(taskPrompt, plan, review.Feedback)
	}

	return lastContainerID, nil
}

// runSandboxRound executes a single sandbox container run (start, stream logs,
// wait for exit). It returns the container ID, exit code, and last log line.
func (s *Server) runSandboxRound(ctx context.Context, sess *session.Session, prompt string) (*sandboxRoundResult, error) {
	s.emitEvent(sess.ID, "status", "Starting sandbox...")

	containerID, err := s.sandbox.Start(ctx, sandbox.StartOptions{
		SessionID: sess.ID,
		Repo:      sess.Repo,
		Prompt:    prompt,
		Branch:    sess.Branch,
		Image:     s.config.DockerImage,
		Env:       s.config.SandboxEnv(),
		Network:   s.config.DockerNetwork,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start sandbox: %w", err)
	}

	sess.ContainerID = containerID
	sess.Status = session.StatusRunning
	s.store.UpdateSession(sess)
	s.emitEvent(sess.ID, "status", "Sandbox started, running agent...")

	// Stream container logs.
	logStream, err := s.sandbox.StreamLogs(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to stream logs: %w", err)
	}
	defer logStream.Close()

	var lastLine string
	for logStream.Scan() {
		line := logStream.Text()
		lastLine = line

		switch {
		case strings.HasPrefix(line, "###OPENTL_STATUS### "):
			msg := strings.TrimPrefix(line, "###OPENTL_STATUS### ")
			s.emitEvent(sess.ID, "status", msg)
		case strings.HasPrefix(line, "###OPENTL_ERROR### "):
			msg := strings.TrimPrefix(line, "###OPENTL_ERROR### ")
			s.emitEvent(sess.ID, "error", msg)
		case strings.HasPrefix(line, "###OPENTL_DONE### "):
			branch := strings.TrimPrefix(line, "###OPENTL_DONE### ")
			sess.Branch = branch
			s.emitEvent(sess.ID, "status", fmt.Sprintf("Branch pushed: %s", branch))
		default:
			s.emitEvent(sess.ID, "output", line)
		}
	}

	// Wait for container to exit.
	exitCode, err := s.sandbox.Wait(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("error waiting for sandbox: %w", err)
	}

	return &sandboxRoundResult{
		containerID: containerID,
		exitCode:    exitCode,
		lastLine:    lastLine,
	}, nil
}

// getDiffFromContainer runs `git diff` inside the container to get the changes.
func (s *Server) getDiffFromContainer(ctx context.Context, containerID string) string {
	execArgs := []string{"exec", containerID, "git", "-C", "/workspace/repo", "diff", "HEAD~1"}
	cmd := exec.CommandContext(ctx, "docker", execArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(output)
}

func (s *Server) failSession(sess *session.Session, errMsg string) {
	log.Printf("Session %s failed: %s", sess.ID, errMsg)
	sess.Status = session.StatusError
	sess.Error = errMsg
	s.store.UpdateSession(sess)
	s.emitEvent(sess.ID, "error", errMsg)
}

func (s *Server) emitEvent(sessionID, eventType, data string) {
	event := &session.Event{
		SessionID: sessionID,
		Type:      eventType,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.AddEvent(event); err != nil {
		log.Printf("Error storing event: %v", err)
	}
	s.bus.Publish(sessionID, event)
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func writeSSE(w http.ResponseWriter, event *session.Event) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, string(data))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
