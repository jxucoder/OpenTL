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
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/jxucoder/opentl/internal/config"
	"github.com/jxucoder/opentl/internal/github"
	"github.com/jxucoder/opentl/internal/orchestrator"
	"github.com/jxucoder/opentl/internal/sandbox"
	"github.com/jxucoder/opentl/internal/session"
	opentlslack "github.com/jxucoder/opentl/internal/slack"
	opentltelegram "github.com/jxucoder/opentl/internal/telegram"
)

const (
	// maxRequestBodySize limits request bodies to 1 MB to prevent memory exhaustion.
	maxRequestBodySize = 1 << 20 // 1 MB

	// sessionTimeout is the maximum duration a session sandbox can run.
	sessionTimeout = 30 * time.Minute

	// maxReviewRounds is the maximum number of review-revise cycles.
	maxReviewRounds = 2
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
			s, // Server implements telegram.SessionCreator
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
}

type createSessionResponse struct {
	ID     string `json:"id"`
	Branch string `json:"branch"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// --- Handlers ---

// CreateAndRunSession creates a new session and starts the sandbox in the background.
// This is the shared entry point used by both the HTTP API and the Slack bot.
func (s *Server) CreateAndRunSession(repo, prompt string) (*session.Session, error) {
	// Use 12 hex chars (48 bits) instead of 8 to reduce collision probability.
	id := uuid.New().String()[:12]
	branch := fmt.Sprintf("opentl/%s", id)
	now := time.Now().UTC()

	sess := &session.Session{
		ID:        id,
		Repo:      repo,
		Prompt:    prompt,
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

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent memory exhaustion.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Repo == "" || req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "repo and prompt are required")
		return
	}

	// Validate repo format (must be "owner/repo").
	if !isValidRepoFormat(req.Repo) {
		writeError(w, http.StatusBadRequest, "repo must be in \"owner/repo\" format")
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
	})
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

	// Subscribe BEFORE reading historical events to prevent the race
	// where events published between GetEvents and Subscribe are lost.
	ch := s.bus.Subscribe(id)
	defer s.bus.Unsubscribe(id, ch)

	// Send historical events.
	events, _ := s.store.GetEvents(id, 0)
	var lastEventID int64
	for _, e := range events {
		writeSSE(w, e)
		lastEventID = e.ID
	}
	flusher.Flush()

	// Drain any events that arrived between GetEvents and now,
	// deduplicating against already-sent historical events.
	drainDone := false
	for !drainDone {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.ID > lastEventID {
				writeSSE(w, event)
				flusher.Flush()
			}
		default:
			drainDone = true
		}
	}

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

	// Emit an error event so SSE clients get a terminal signal instead of hanging.
	s.emitEvent(sess.ID, "error", "stopped by user")

	writeJSON(w, http.StatusOK, sess)
}

// --- Session execution ---

func (s *Server) runSession(sess *session.Session) {
	// Enforce a maximum session duration so stuck containers don't run forever.
	ctx, cancel := context.WithTimeout(context.Background(), sessionTimeout)
	defer cancel()

	// Phase 1: Plan step (if orchestrator is available).
	prompt := sess.Prompt
	var plan string
	if s.orchestrator != nil {
		s.emitEvent(sess.ID, "status", "Planning task...")
		var err error
		plan, err = s.orchestrator.Plan(ctx, sess.Repo, sess.Prompt)
		if err != nil {
			log.Printf("Planning failed (falling back to direct prompt): %v", err)
			s.emitEvent(sess.ID, "status", "Planning failed, using direct prompt")
		} else {
			s.emitEvent(sess.ID, "output", "## Plan\n"+plan)
			prompt = s.orchestrator.EnrichPrompt(sess.Prompt, plan)
		}
	}

	s.emitEvent(sess.ID, "status", "Starting sandbox...")

	// Start the Docker container.
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
		s.failSession(sess, fmt.Sprintf("failed to start sandbox: %v", err))
		return
	}

	sess.ContainerID = containerID
	sess.Status = session.StatusRunning
	s.store.UpdateSession(sess)
	s.emitEvent(sess.ID, "status", "Sandbox started, running agent...")

	// Stream container logs.
	logStream, err := s.sandbox.StreamLogs(ctx, containerID)
	if err != nil {
		s.failSession(sess, fmt.Sprintf("failed to stream logs: %v", err))
		return
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
		s.failSession(sess, fmt.Sprintf("error waiting for sandbox: %v", err))
		return
	}

	if exitCode != 0 {
		errMsg := fmt.Sprintf("sandbox exited with code %d", exitCode)
		if lastLine != "" {
			errMsg += ": " + lastLine
		}
		s.failSession(sess, errMsg)
		return
	}

	// Phase 2: Multi-round review (if orchestrator is available and we have a plan).
	if s.orchestrator != nil && plan != "" {
		diff := s.getDiffFromContainer(ctx, containerID)
		if diff != "" {
			s.runReviewCycle(ctx, sess, plan, diff)
		}
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

	// Clean up container.
	s.sandbox.Stop(ctx, containerID)
}

// runReviewCycle performs up to maxReviewRounds of review-then-revise.
// If the review approves on any round, it returns immediately.
func (s *Server) runReviewCycle(ctx context.Context, sess *session.Session, plan, diff string) {
	for round := 1; round <= maxReviewRounds; round++ {
		s.emitEvent(sess.ID, "status", fmt.Sprintf("Reviewing changes (round %d/%d)...", round, maxReviewRounds))

		review, err := s.orchestrator.Review(ctx, sess.Prompt, plan, diff)
		if err != nil {
			log.Printf("Review failed (proceeding with PR): %v", err)
			s.emitEvent(sess.ID, "status", "Review failed, proceeding with PR")
			return
		}

		if review.Approved {
			s.emitEvent(sess.ID, "output", fmt.Sprintf("## Review (Round %d)\n%s", round, review.Feedback))
			return
		}

		s.emitEvent(sess.ID, "output", fmt.Sprintf("## Review Feedback (Round %d)\n%s", round, review.Feedback))

		if round == maxReviewRounds {
			s.emitEvent(sess.ID, "status", fmt.Sprintf("Max review rounds (%d) reached, proceeding with PR", maxReviewRounds))
			return
		}

		// TODO: Re-run the sandbox with review feedback as a revision prompt.
		// This requires restarting the container with an enriched prompt that
		// includes the review feedback. For now, log and proceed.
		s.emitEvent(sess.ID, "status", "Revision round not yet implemented, proceeding with PR")
		return
	}
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

// truncate shortens s to at most maxLen runes, appending "..." if truncated.
// Uses rune count instead of byte count to avoid breaking multi-byte UTF-8 characters.
func truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen-3]) + "..."
}

// isValidRepoFormat checks that s matches the "owner/repo" pattern.
func isValidRepoFormat(s string) bool {
	parts := strings.SplitN(s, "/", 2)
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(parts[1], "/")
}
