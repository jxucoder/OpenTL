// Package server provides the HTTP API for TeleCoder.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/jxucoder/telecoder/internal/engine"
)

// Server is the TeleCoder HTTP API server.
type Server struct {
	engine *engine.Engine
	router chi.Router
}

// New creates a new server.
func New(eng *engine.Engine) *Server {
	s := &Server{engine: eng}
	s.routes()
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) routes() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", s.health)

	r.Route("/api/sessions", func(r chi.Router) {
		r.Post("/", s.createSession)
		r.Get("/", s.listSessions)
		r.Get("/{id}", s.getSession)
		r.Post("/{id}/stop", s.stopSession)
		r.Get("/{id}/events", s.streamEvents)
		r.Post("/{id}/messages", s.sendMessage)
		r.Get("/{id}/messages", s.getMessages)
	})

	s.router = r
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type createSessionRequest struct {
	Repo   string `json:"repo"`
	Prompt string `json:"prompt"`
	Mode   string `json:"mode"` // "task" (default) or "chat"
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Repo == "" {
		http.Error(w, "repo is required", http.StatusBadRequest)
		return
	}

	var sess interface{}
	var err error

	if req.Mode == "chat" {
		sess, err = s.engine.CreateChat(r.Context(), req.Repo)
	} else {
		if req.Prompt == "" {
			http.Error(w, "prompt is required for task mode", http.StatusBadRequest)
			return
		}
		sess, err = s.engine.CreateAndRun(r.Context(), req.Repo, req.Prompt)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sess)
}

func (s *Server) listSessions(w http.ResponseWriter, _ *http.Request) {
	sessions, err := s.engine.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.engine.GetSession(id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sess)
}

func (s *Server) stopSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.engine.StopSession(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send existing events first.
	afterIDStr := r.URL.Query().Get("after")
	afterID, _ := strconv.ParseInt(afterIDStr, 10, 64)

	existing, _ := s.engine.GetEvents(id, afterID)
	for _, ev := range existing {
		writeSSE(w, ev.Type, ev.Data, ev.ID)
	}
	flusher.Flush()

	// Subscribe to new events.
	ch := s.engine.Subscribe(id)
	defer s.engine.Unsubscribe(id, ch)

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, ev.Type, ev.Data, ev.ID)
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-time.After(30 * time.Second):
			// Keep-alive.
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

type sendMessageRequest struct {
	Content string `json:"content"`
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response, err := s.engine.SendMessage(r.Context(), id, req.Content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"response": response})
}

func (s *Server) getMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	msgs, err := s.engine.GetMessages(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func writeSSE(w http.ResponseWriter, event, data string, id int64) {
	if id > 0 {
		fmt.Fprintf(w, "id: %d\n", id)
	}
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
