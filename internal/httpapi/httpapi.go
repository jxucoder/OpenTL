// Package httpapi provides the HTTP API handler for TeleCoder.
// It delegates all business logic to the engine.
package httpapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/jxucoder/TeleCoder/engine"
	ghWebhook "github.com/jxucoder/TeleCoder/gitprovider/github"
	"github.com/jxucoder/TeleCoder/model"
)

// Handler provides the HTTP API for TeleCoder.
type Handler struct {
	engine *engine.Engine
	router chi.Router
}

// New creates a new HTTP API handler.
func New(eng *engine.Engine) *Handler {
	h := &Handler{engine: eng}
	h.router = h.buildRouter()
	return h
}

// Router returns the HTTP router.
func (h *Handler) Router() chi.Router {
	return h.router
}

func (h *Handler) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(30 * time.Second))
			r.Post("/sessions", h.handleCreateSession)
			r.Get("/sessions", h.handleListSessions)
			r.Get("/sessions/{id}", h.handleGetSession)
			r.Get("/sessions/{id}/messages", h.handleGetMessages)
			r.Post("/sessions/{id}/messages", h.handleSendMessage)
			r.Post("/sessions/{id}/pr", h.handleCreatePR)
			r.Post("/sessions/{id}/stop", h.handleStopSession)
		})
		r.Get("/sessions/{id}/events", h.handleSessionEvents)
	})

	r.Post("/api/webhooks/github", h.handleGitHubWebhook)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return r
}

// --- Request/Response types ---

type createSessionRequest struct {
	Repo   string `json:"repo"`
	Prompt string `json:"prompt"`
	Mode   string `json:"mode,omitempty"`
	Agent  string `json:"agent,omitempty"`
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

func (h *Handler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Repo = strings.TrimSpace(req.Repo)
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "repo is required")
		return
	}
	if !isValidRepo(req.Repo) {
		writeError(w, http.StatusBadRequest, "repo must be in owner/repo format")
		return
	}
	if len([]rune(req.Prompt)) > 10000 {
		writeError(w, http.StatusBadRequest, "prompt exceeds 10000 characters")
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = string(model.ModeTask)
	}

	switch mode {
	case string(model.ModeChat):
		sess, err := h.engine.CreateChatSession(req.Repo)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create chat session")
			log.Printf("Error creating chat session: %v", err)
			return
		}
		writeJSON(w, http.StatusCreated, createSessionResponse{
			ID: sess.ID, Branch: sess.Branch, Mode: "chat",
		})

	case string(model.ModeTask):
		if req.Prompt == "" {
			writeError(w, http.StatusBadRequest, "prompt is required for task mode")
			return
		}
		sess, err := h.engine.CreateAndRunSessionWithAgent(req.Repo, req.Prompt, req.Agent)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create session")
			log.Printf("Error creating session: %v", err)
			return
		}
		writeJSON(w, http.StatusCreated, createSessionResponse{
			ID: sess.ID, Branch: sess.Branch, Mode: "task",
		})

	default:
		writeError(w, http.StatusBadRequest, "mode must be 'task' or 'chat'")
	}
}

func (h *Handler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.engine.Store().ListSessions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		log.Printf("Error listing sessions: %v", err)
		return
	}
	if sessions == nil {
		sessions = []*model.Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := h.engine.Store().GetSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (h *Handler) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if _, err := h.engine.Store().GetSession(id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	events, err := h.engine.Store().GetEvents(id, 0)
	if err != nil {
		log.Printf("failed to load events for session %s: %v", id, err)
		events = nil
	}
	for _, e := range events {
		writeSSE(w, e)
	}
	flusher.Flush()

	ch := h.engine.Bus().Subscribe(id)
	defer h.engine.Bus().Unsubscribe(id, ch)

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

func (h *Handler) handleStopSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := h.engine.Store().GetSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// NOTE: We don't have direct sandbox access here, but the engine owns it.
	// For now, mark the session as errored. A full stop would require engine method.
	sess.Status = model.StatusError
	sess.Error = "stopped by user"
	h.engine.Store().UpdateSession(sess)

	writeJSON(w, http.StatusOK, sess)
}

func (h *Handler) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if len([]rune(req.Content)) > 10000 {
		writeError(w, http.StatusBadRequest, "content exceeds 10000 characters")
		return
	}

	msg, err := h.engine.SendChatMessage(id, req.Content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, sendMessageResponse{
		MessageID: msg.ID, SessionID: id,
	})
}

func (h *Handler) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	msgs, err := h.engine.Store().GetMessages(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get messages")
		return
	}
	if msgs == nil {
		msgs = []*model.Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (h *Handler) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prURL, prNumber, err := h.engine.CreatePRFromChat(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, createPRResponse{
		URL: prURL, Number: prNumber,
	})
}

func (h *Handler) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	event, err := ghWebhook.ParseWebhook(r, h.engine.WebhookSecret())
	if err != nil {
		log.Printf("Webhook parse error: %v", err)
		writeError(w, http.StatusBadRequest, "invalid webhook")
		return
	}

	if event == nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
		return
	}

	if event.CommentUser == "telecoder[bot]" || event.CommentUser == "TeleCoder" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
		return
	}

	originalSess, err := h.engine.Store().GetSessionByPR(event.Repo, event.PRNumber)
	if err != nil {
		log.Printf("Webhook: no session found for PR #%d on %s: %v", event.PRNumber, event.Repo, err)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
		return
	}

	log.Printf("Webhook: received PR comment on %s#%d from @%s, original session %s",
		event.Repo, event.PRNumber, event.CommentUser, originalSess.ID)

	sess, err := h.engine.CreatePRCommentSession(originalSess, event)
	if err != nil {
		log.Printf("Webhook: failed to create PR comment session: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to process comment")
		return
	}

	writeJSON(w, http.StatusAccepted, createSessionResponse{
		ID: sess.ID, Branch: sess.Branch, Mode: "task",
	})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func writeSSE(w http.ResponseWriter, event *model.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("writeSSE marshal error: %v", err)
		return
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, string(data)); err != nil {
		log.Printf("writeSSE write error: %v", err)
	}
}

func isValidRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
