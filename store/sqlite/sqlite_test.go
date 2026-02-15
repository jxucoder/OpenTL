package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jxucoder/TeleCoder/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestSessionCRUD(t *testing.T) {
	store := newTestStore(t)

	now := time.Now().UTC()
	sess := &model.Session{
		ID:        "abc12345",
		Repo:      "owner/repo",
		Prompt:    "add tests",
		Mode:      model.ModeTask,
		Status:    model.StatusPending,
		Branch:    "telecoder/abc12345",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := store.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.ID != sess.ID || got.Repo != sess.Repo || got.Status != model.StatusPending {
		t.Fatalf("unexpected session: %+v", got)
	}

	got.Status = model.StatusRunning
	got.Error = "none"
	if err := store.UpdateSession(got); err != nil {
		t.Fatalf("update session: %v", err)
	}

	got2, err := store.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("get updated session: %v", err)
	}
	if got2.Status != model.StatusRunning {
		t.Fatalf("status not updated: %s", got2.Status)
	}
}

func TestMessagesAndEvents(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	sess := &model.Session{
		ID:        "evt12345",
		Repo:      "owner/repo",
		Prompt:    "prompt",
		Mode:      model.ModeChat,
		Status:    model.StatusIdle,
		Branch:    "telecoder/evt12345",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	msg := &model.Message{
		SessionID: sess.ID,
		Role:      "user",
		Content:   "hello",
		CreatedAt: now,
	}
	if err := store.AddMessage(msg); err != nil {
		t.Fatalf("add message: %v", err)
	}
	msgs, err := store.GetMessages(sess.ID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}

	ev := &model.Event{
		SessionID: sess.ID,
		Type:      "status",
		Data:      "Running",
		CreatedAt: now,
	}
	if err := store.AddEvent(ev); err != nil {
		t.Fatalf("add event: %v", err)
	}
	events, err := store.GetEvents(sess.ID, 0)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	if len(events) != 1 || events[0].Data != "Running" {
		t.Fatalf("unexpected events: %+v", events)
	}
}
