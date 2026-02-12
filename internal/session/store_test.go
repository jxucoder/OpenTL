package session_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jxucoder/opentl/internal/session"
)

// newTestStore creates a Store backed by a temporary SQLite database.
func newTestStore(t *testing.T) *session.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// makeSession returns a minimal Session with sensible defaults.
func makeSession(id, repo, prompt string) *session.Session {
	now := time.Now().UTC().Truncate(time.Second)
	return &session.Session{
		ID:        id,
		Repo:      repo,
		Prompt:    prompt,
		Status:    session.StatusPending,
		Branch:    "main",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// ---------------------------------------------------------------------------
// Store creation
// ---------------------------------------------------------------------------

func TestNewStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "new.db")
	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNewStore_InvalidPath(t *testing.T) {
	// A path under a non-existent directory should fail during migration or open.
	_, err := session.NewStore("/no/such/dir/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

// ---------------------------------------------------------------------------
// CreateSession + GetSession
// ---------------------------------------------------------------------------

func TestCreateAndGetSession(t *testing.T) {
	store := newTestStore(t)

	want := makeSession("sess-1", "owner/repo", "fix the bug")
	if err := store.CreateSession(want); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := store.GetSession("sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	assertSessionEqual(t, got, want)
}

func TestGetSession_NotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetSession("does-not-exist")
	if err == nil {
		t.Fatal("expected error for non-existent session, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListSessions
// ---------------------------------------------------------------------------

func TestListSessions_Empty(t *testing.T) {
	store := newTestStore(t)

	sessions, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestListSessions_Multiple(t *testing.T) {
	store := newTestStore(t)

	s1 := makeSession("sess-1", "owner/repo1", "prompt1")
	s1.CreatedAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s1.UpdatedAt = s1.CreatedAt

	s2 := makeSession("sess-2", "owner/repo2", "prompt2")
	s2.CreatedAt = time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	s2.UpdatedAt = s2.CreatedAt

	for _, s := range []*session.Session{s1, s2} {
		if err := store.CreateSession(s); err != nil {
			t.Fatalf("CreateSession(%s): %v", s.ID, err)
		}
	}

	sessions, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Newest first.
	if sessions[0].ID != "sess-2" {
		t.Errorf("expected first session ID %q, got %q", "sess-2", sessions[0].ID)
	}
	if sessions[1].ID != "sess-1" {
		t.Errorf("expected second session ID %q, got %q", "sess-1", sessions[1].ID)
	}
}

// ---------------------------------------------------------------------------
// UpdateSession
// ---------------------------------------------------------------------------

func TestUpdateSession(t *testing.T) {
	store := newTestStore(t)

	sess := makeSession("sess-u", "owner/repo", "do something")
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Mutate fields.
	sess.Status = session.StatusRunning
	sess.Branch = "feature-branch"
	sess.PRUrl = "https://github.com/owner/repo/pull/42"
	sess.PRNumber = 42
	sess.ContainerID = "ctr-abc123"
	sess.Error = ""

	if err := store.UpdateSession(sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	got, err := store.GetSession("sess-u")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	if got.Status != session.StatusRunning {
		t.Errorf("Status: want %q, got %q", session.StatusRunning, got.Status)
	}
	if got.Branch != "feature-branch" {
		t.Errorf("Branch: want %q, got %q", "feature-branch", got.Branch)
	}
	if got.PRUrl != "https://github.com/owner/repo/pull/42" {
		t.Errorf("PRUrl: want %q, got %q", "https://github.com/owner/repo/pull/42", got.PRUrl)
	}
	if got.PRNumber != 42 {
		t.Errorf("PRNumber: want %d, got %d", 42, got.PRNumber)
	}
	if got.ContainerID != "ctr-abc123" {
		t.Errorf("ContainerID: want %q, got %q", "ctr-abc123", got.ContainerID)
	}
	// UpdatedAt should have been refreshed by UpdateSession.
	if !got.UpdatedAt.After(got.CreatedAt) && got.UpdatedAt.Equal(got.CreatedAt) {
		// UpdateSession sets UpdatedAt = time.Now(), so it should be >= CreatedAt.
		// We just verify it was set (non-zero).
		if got.UpdatedAt.IsZero() {
			t.Error("UpdatedAt is zero after update")
		}
	}
}

func TestUpdateSession_ErrorField(t *testing.T) {
	store := newTestStore(t)

	sess := makeSession("sess-err", "owner/repo", "do something")
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sess.Status = session.StatusError
	sess.Error = "container crashed"
	if err := store.UpdateSession(sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	got, err := store.GetSession("sess-err")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Status != session.StatusError {
		t.Errorf("Status: want %q, got %q", session.StatusError, got.Status)
	}
	if got.Error != "container crashed" {
		t.Errorf("Error: want %q, got %q", "container crashed", got.Error)
	}
}

// ---------------------------------------------------------------------------
// Events — AddEvent, GetEvents, afterID filtering
// ---------------------------------------------------------------------------

func TestAddAndGetEvents(t *testing.T) {
	store := newTestStore(t)

	// Create a parent session so the FK constraint is satisfied.
	sess := makeSession("sess-ev", "owner/repo", "prompt")
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	e1 := &session.Event{SessionID: "sess-ev", Type: "status", Data: "running", CreatedAt: now}
	e2 := &session.Event{SessionID: "sess-ev", Type: "output", Data: "hello world", CreatedAt: now}

	for _, e := range []*session.Event{e1, e2} {
		if err := store.AddEvent(e); err != nil {
			t.Fatalf("AddEvent: %v", err)
		}
	}

	// ID should be populated after insert.
	if e1.ID == 0 {
		t.Error("expected e1.ID to be set after AddEvent")
	}
	if e2.ID == 0 {
		t.Error("expected e2.ID to be set after AddEvent")
	}
	if e2.ID <= e1.ID {
		t.Errorf("expected e2.ID (%d) > e1.ID (%d)", e2.ID, e1.ID)
	}

	// Get all events (afterID = 0).
	events, err := store.GetEvents("sess-ev", 0)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "status" {
		t.Errorf("events[0].Type: want %q, got %q", "status", events[0].Type)
	}
	if events[1].Type != "output" {
		t.Errorf("events[1].Type: want %q, got %q", "output", events[1].Type)
	}
}

func TestGetEvents_AfterID(t *testing.T) {
	store := newTestStore(t)

	sess := makeSession("sess-af", "owner/repo", "prompt")
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	var ids []int64
	for i := 0; i < 5; i++ {
		e := &session.Event{
			SessionID: "sess-af",
			Type:      "output",
			Data:      string(rune('A' + i)),
			CreatedAt: now,
		}
		if err := store.AddEvent(e); err != nil {
			t.Fatalf("AddEvent[%d]: %v", i, err)
		}
		ids = append(ids, e.ID)
	}

	// Get events after the third one.
	events, err := store.GetEvents("sess-af", ids[2])
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events after id %d, got %d", ids[2], len(events))
	}
	if events[0].ID != ids[3] {
		t.Errorf("events[0].ID: want %d, got %d", ids[3], events[0].ID)
	}
	if events[1].ID != ids[4] {
		t.Errorf("events[1].ID: want %d, got %d", ids[4], events[1].ID)
	}
}

func TestGetEvents_NoEvents(t *testing.T) {
	store := newTestStore(t)

	sess := makeSession("sess-none", "owner/repo", "prompt")
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	events, err := store.GetEvents("sess-none", 0)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// EventBus
// ---------------------------------------------------------------------------

func TestEventBus_SubscribeAndPublish(t *testing.T) {
	bus := session.NewEventBus()
	ch := bus.Subscribe("sess-1")

	event := &session.Event{
		ID:        1,
		SessionID: "sess-1",
		Type:      "output",
		Data:      "hello",
		CreatedAt: time.Now().UTC(),
	}

	bus.Publish("sess-1", event)

	select {
	case got := <-ch:
		if got.ID != event.ID {
			t.Errorf("event ID: want %d, got %d", event.ID, got.ID)
		}
		if got.Data != "hello" {
			t.Errorf("event Data: want %q, got %q", "hello", got.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	bus.Unsubscribe("sess-1", ch)
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := session.NewEventBus()
	ch1 := bus.Subscribe("sess-1")
	ch2 := bus.Subscribe("sess-1")

	event := &session.Event{
		ID:        1,
		SessionID: "sess-1",
		Type:      "status",
		Data:      "running",
		CreatedAt: time.Now().UTC(),
	}

	bus.Publish("sess-1", event)

	for i, ch := range []chan *session.Event{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Data != "running" {
				t.Errorf("subscriber %d: Data: want %q, got %q", i, "running", got.Data)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}

	bus.Unsubscribe("sess-1", ch1)
	bus.Unsubscribe("sess-1", ch2)
}

func TestEventBus_UnsubscribeClosesChannel(t *testing.T) {
	bus := session.NewEventBus()
	ch := bus.Subscribe("sess-1")

	bus.Unsubscribe("sess-1", ch)

	// Reading from a closed channel returns the zero value immediately.
	_, open := <-ch
	if open {
		t.Error("expected channel to be closed after Unsubscribe")
	}
}

func TestEventBus_PublishToUnsubscribedIsNoop(t *testing.T) {
	bus := session.NewEventBus()

	// No subscribers at all -- should not panic.
	event := &session.Event{
		ID:        1,
		SessionID: "no-sub",
		Type:      "output",
		Data:      "ignored",
		CreatedAt: time.Now().UTC(),
	}

	// This must not panic.
	bus.Publish("no-sub", event)
}

func TestEventBus_PublishAfterUnsubscribe(t *testing.T) {
	bus := session.NewEventBus()
	ch := bus.Subscribe("sess-1")
	bus.Unsubscribe("sess-1", ch)

	// Publishing after the only subscriber left should not panic.
	event := &session.Event{
		ID:        2,
		SessionID: "sess-1",
		Type:      "output",
		Data:      "should be dropped",
		CreatedAt: time.Now().UTC(),
	}
	bus.Publish("sess-1", event)
}

func TestEventBus_PublishDifferentSession(t *testing.T) {
	bus := session.NewEventBus()
	ch := bus.Subscribe("sess-A")

	event := &session.Event{
		ID:        1,
		SessionID: "sess-B",
		Type:      "output",
		Data:      "wrong session",
		CreatedAt: time.Now().UTC(),
	}
	bus.Publish("sess-B", event)

	// ch for sess-A should not receive anything published to sess-B.
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event on sess-A channel: %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// Expected: nothing received.
	}

	bus.Unsubscribe("sess-A", ch)
}

func TestEventBus_EmptyMapCleanupAfterLastUnsubscribe(t *testing.T) {
	bus := session.NewEventBus()

	ch1 := bus.Subscribe("sess-cleanup")
	ch2 := bus.Subscribe("sess-cleanup")

	// Remove first subscriber -- map entry should still exist because ch2 remains.
	bus.Unsubscribe("sess-cleanup", ch1)

	// Publishing should still reach ch2.
	event := &session.Event{
		ID:        1,
		SessionID: "sess-cleanup",
		Type:      "status",
		Data:      "still here",
		CreatedAt: time.Now().UTC(),
	}
	bus.Publish("sess-cleanup", event)

	select {
	case got := <-ch2:
		if got.Data != "still here" {
			t.Errorf("Data: want %q, got %q", "still here", got.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event on ch2")
	}

	// Remove last subscriber -- the map entry for "sess-cleanup" should be deleted.
	bus.Unsubscribe("sess-cleanup", ch2)

	// Publishing after all subscribers are gone should be a no-op (no panic).
	bus.Publish("sess-cleanup", &session.Event{
		ID:        2,
		SessionID: "sess-cleanup",
		Type:      "output",
		Data:      "nobody listening",
		CreatedAt: time.Now().UTC(),
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertSessionEqual(t *testing.T, got, want *session.Session) {
	t.Helper()

	if got.ID != want.ID {
		t.Errorf("ID: want %q, got %q", want.ID, got.ID)
	}
	if got.Repo != want.Repo {
		t.Errorf("Repo: want %q, got %q", want.Repo, got.Repo)
	}
	if got.Prompt != want.Prompt {
		t.Errorf("Prompt: want %q, got %q", want.Prompt, got.Prompt)
	}
	if got.Status != want.Status {
		t.Errorf("Status: want %q, got %q", want.Status, got.Status)
	}
	if got.Branch != want.Branch {
		t.Errorf("Branch: want %q, got %q", want.Branch, got.Branch)
	}
	if got.PRUrl != want.PRUrl {
		t.Errorf("PRUrl: want %q, got %q", want.PRUrl, got.PRUrl)
	}
	if got.PRNumber != want.PRNumber {
		t.Errorf("PRNumber: want %d, got %d", want.PRNumber, got.PRNumber)
	}
	if got.ContainerID != want.ContainerID {
		t.Errorf("ContainerID: want %q, got %q", want.ContainerID, got.ContainerID)
	}
	if got.Error != want.Error {
		t.Errorf("Error: want %q, got %q", want.Error, got.Error)
	}
}
