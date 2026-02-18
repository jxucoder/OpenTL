// Package store defines the SessionStore interface for TeleCoder persistence.
package store

import "github.com/jxucoder/TeleCoder/model"

// SessionStore provides persistence for sessions, messages, and events.
type SessionStore interface {
	CreateSession(sess *model.Session) error
	GetSession(id string) (*model.Session, error)
	ListSessions() ([]*model.Session, error)
	UpdateSession(sess *model.Session) error
	GetSessionByPR(repo string, prNumber int) (*model.Session, error)
	AddMessage(msg *model.Message) error
	GetMessages(sessionID string) ([]*model.Message, error)
	AddEvent(event *model.Event) error
	GetEvents(sessionID string, afterID int64) ([]*model.Event, error)
	Close() error
}
