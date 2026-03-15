// Package store provides JSON file-backed persistence for sessions, events, and messages.
// This is intentionally simple for v1 — no external database dependency.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jxucoder/telecoder/internal/model"
)

// Store provides file-backed persistence for sessions, events, and messages.
type Store struct {
	dir string
	mu  sync.RWMutex

	sessions map[string]*model.Session
	events   map[string][]*model.Event
	messages map[string][]*model.Message
	eventSeq int64
	msgSeq   int64
}

// Open creates or opens the store at the given directory.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}

	s := &Store{
		dir:      dir,
		sessions: make(map[string]*model.Session),
		events:   make(map[string][]*model.Event),
		messages: make(map[string][]*model.Message),
	}

	// Load existing data.
	if err := s.load(); err != nil {
		return nil, fmt.Errorf("load store: %w", err)
	}
	return s, nil
}

// Close persists the store to disk.
func (s *Store) Close() error {
	return s.save()
}

// CreateSession inserts a new session.
func (s *Store) CreateSession(sess *model.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
	return s.saveLocked()
}

// UpdateSession updates mutable session fields.
func (s *Store) UpdateSession(sess *model.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.UpdatedAt = time.Now().UTC()
	s.sessions[sess.ID] = sess
	return s.saveLocked()
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(id string) (*model.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return sess, nil
}

// ListSessions returns all sessions ordered by creation time (newest first).
func (s *Store) ListSessions() ([]*model.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*model.Session
	for _, sess := range s.sessions {
		list = append(list, sess)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list, nil
}

// InsertEvent records a session event.
func (s *Store) InsertEvent(ev *model.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventSeq++
	ev.ID = s.eventSeq
	s.events[ev.SessionID] = append(s.events[ev.SessionID], ev)
	return nil // Events are not persisted to disk — they are ephemeral logs.
}

// ListEvents returns events for a session in chronological order.
func (s *Store) ListEvents(sessionID string, afterID int64) ([]*model.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*model.Event
	for _, ev := range s.events[sessionID] {
		if ev.ID > afterID {
			result = append(result, ev)
		}
	}
	return result, nil
}

// InsertMessage records a chat message.
func (s *Store) InsertMessage(msg *model.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgSeq++
	msg.ID = s.msgSeq
	s.messages[msg.SessionID] = append(s.messages[msg.SessionID], msg)
	return s.saveLocked()
}

// ListMessages returns messages for a session in chronological order.
func (s *Store) ListMessages(sessionID string) ([]*model.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.messages[sessionID], nil
}

// Persistence: simple JSON file.

type storeData struct {
	Sessions map[string]*model.Session    `json:"sessions"`
	Messages map[string][]*model.Message  `json:"messages"`
}

func (s *Store) load() error {
	path := filepath.Join(s.dir, "store.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var d storeData
	if err := json.Unmarshal(data, &d); err != nil {
		return err
	}
	if d.Sessions != nil {
		s.sessions = d.Sessions
	}
	if d.Messages != nil {
		s.messages = d.Messages
	}
	return nil
}

func (s *Store) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	d := storeData{
		Sessions: s.sessions,
		Messages: s.messages,
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, "store.json")
	return os.WriteFile(path, data, 0o644)
}
