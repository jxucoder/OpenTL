// Package sqlite implements store.SessionStore using SQLite.
package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jxucoder/TeleCoder/model"
)

// Store manages session and event persistence in SQLite.
type Store struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at the given path.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for better concurrent read/write performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id           TEXT PRIMARY KEY,
			repo         TEXT NOT NULL,
			prompt       TEXT NOT NULL,
			mode         TEXT NOT NULL DEFAULT 'task',
			status       TEXT NOT NULL DEFAULT 'pending',
			branch       TEXT NOT NULL DEFAULT '',
			pr_url       TEXT NOT NULL DEFAULT '',
			pr_number    INTEGER NOT NULL DEFAULT 0,
			container_id TEXT NOT NULL DEFAULT '',
			error        TEXT NOT NULL DEFAULT '',
			created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS session_events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			type       TEXT NOT NULL,
			data       TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);

		CREATE INDEX IF NOT EXISTS idx_events_session_id
			ON session_events(session_id);

		CREATE TABLE IF NOT EXISTS messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);

		CREATE INDEX IF NOT EXISTS idx_messages_session_id
			ON messages(session_id);
	`)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// CreateSession inserts a new session.
func (s *Store) CreateSession(sess *model.Session) error {
	if sess.Mode == "" {
		sess.Mode = model.ModeTask
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, repo, prompt, mode, status, branch, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Repo, sess.Prompt, sess.Mode, sess.Status, sess.Branch,
		sess.CreatedAt, sess.UpdatedAt,
	)
	return err
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(id string) (*model.Session, error) {
	row := s.db.QueryRow(
		`SELECT id, repo, prompt, mode, status, branch, pr_url, pr_number,
		        container_id, error, created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	)
	return scanSession(row)
}

// ListSessions returns all sessions ordered by creation time (newest first).
func (s *Store) ListSessions() ([]*model.Session, error) {
	rows, err := s.db.Query(
		`SELECT id, repo, prompt, mode, status, branch, pr_url, pr_number,
		        container_id, error, created_at, updated_at
		 FROM sessions ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*model.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// UpdateSession updates mutable fields of a session.
func (s *Store) UpdateSession(sess *model.Session) error {
	sess.UpdatedAt = time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE sessions SET
			status = ?, branch = ?, pr_url = ?, pr_number = ?,
			container_id = ?, error = ?, updated_at = ?
		 WHERE id = ?`,
		sess.Status, sess.Branch, sess.PRUrl, sess.PRNumber,
		sess.ContainerID, sess.Error, sess.UpdatedAt, sess.ID,
	)
	return err
}

// AddEvent inserts a new event and returns its ID.
func (s *Store) AddEvent(event *model.Event) error {
	result, err := s.db.Exec(
		`INSERT INTO session_events (session_id, type, data, created_at)
		 VALUES (?, ?, ?, ?)`,
		event.SessionID, event.Type, event.Data, event.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	event.ID = id
	return nil
}

// GetEvents returns events for a session, optionally after a given event ID.
func (s *Store) GetEvents(sessionID string, afterID int64) ([]*model.Event, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, type, data, created_at
		 FROM session_events
		 WHERE session_id = ? AND id > ?
		 ORDER BY id ASC`,
		sessionID, afterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*model.Event
	for rows.Next() {
		e := &model.Event{}
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Type, &e.Data, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetSessionByPR retrieves a session by its PR number and repository.
func (s *Store) GetSessionByPR(repo string, prNumber int) (*model.Session, error) {
	row := s.db.QueryRow(
		`SELECT id, repo, prompt, mode, status, branch, pr_url, pr_number,
		        container_id, error, created_at, updated_at
		 FROM sessions
		 WHERE repo = ? AND pr_number = ?
		 ORDER BY created_at DESC
		 LIMIT 1`, repo, prNumber,
	)
	return scanSession(row)
}

// --- Message persistence for chat sessions ---

// AddMessage inserts a new message into a chat session.
func (s *Store) AddMessage(msg *model.Message) error {
	result, err := s.db.Exec(
		`INSERT INTO messages (session_id, role, content, created_at)
		 VALUES (?, ?, ?, ?)`,
		msg.SessionID, msg.Role, msg.Content, msg.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	msg.ID = id
	return nil
}

// GetMessages returns all messages for a session ordered by creation time.
func (s *Store) GetMessages(sessionID string) ([]*model.Message, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, role, content, created_at
		 FROM messages
		 WHERE session_id = ?
		 ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*model.Message
	for rows.Next() {
		m := &model.Message{}
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// --- Scan helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanSession(row scannable) (*model.Session, error) {
	sess := &model.Session{}
	err := row.Scan(
		&sess.ID, &sess.Repo, &sess.Prompt, &sess.Mode, &sess.Status,
		&sess.Branch, &sess.PRUrl, &sess.PRNumber,
		&sess.ContainerID, &sess.Error, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return sess, nil
}
