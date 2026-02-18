package sqlite

import (
	"fmt"
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

func TestListSessions(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	for _, id := range []string{"s1", "s2", "s3"} {
		sess := &model.Session{
			ID: id, Repo: "owner/repo", Prompt: "p", Mode: model.ModeTask,
			Status: model.StatusPending, Branch: "telecoder/" + id,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.CreateSession(sess); err != nil {
			t.Fatalf("create session %s: %v", id, err)
		}
		now = now.Add(time.Second)
	}

	sessions, err := store.ListSessions()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
	// Newest first.
	if sessions[0].ID != "s3" {
		t.Fatalf("expected s3 first (newest), got %s", sessions[0].ID)
	}
	if sessions[2].ID != "s1" {
		t.Fatalf("expected s1 last (oldest), got %s", sessions[2].ID)
	}
}

func TestGetSessionByPR(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	sess := &model.Session{
		ID: "pr-test", Repo: "owner/repo", Prompt: "fix",
		Mode: model.ModeTask, Status: model.StatusComplete,
		Branch: "telecoder/pr-test", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.PRUrl = "https://github.com/owner/repo/pull/42"
	sess.PRNumber = 42
	if err := store.UpdateSession(sess); err != nil {
		t.Fatalf("update session: %v", err)
	}

	got, err := store.GetSessionByPR("owner/repo", 42)
	if err != nil {
		t.Fatalf("GetSessionByPR: %v", err)
	}
	if got.ID != "pr-test" {
		t.Fatalf("expected session pr-test, got %s", got.ID)
	}
	if got.PRNumber != 42 {
		t.Fatalf("expected PR number 42, got %d", got.PRNumber)
	}
}

func TestGetSessionByPRNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetSessionByPR("owner/repo", 999)
	if err == nil {
		t.Fatal("expected error for non-existent PR")
	}
}

func TestGetEventsAfterID(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	sess := &model.Session{
		ID: "evt-after", Repo: "owner/repo", Prompt: "p",
		Mode: model.ModeTask, Status: model.StatusRunning,
		Branch: "telecoder/evt-after", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	for i := 0; i < 5; i++ {
		ev := &model.Event{
			SessionID: sess.ID, Type: "output",
			Data: fmt.Sprintf("line %d", i), CreatedAt: now,
		}
		if err := store.AddEvent(ev); err != nil {
			t.Fatalf("add event: %v", err)
		}
	}

	// Get all events.
	all, _ := store.GetEvents(sess.ID, 0)
	if len(all) != 5 {
		t.Fatalf("expected 5 events, got %d", len(all))
	}

	// Get events after the 3rd one.
	after, _ := store.GetEvents(sess.ID, all[2].ID)
	if len(after) != 2 {
		t.Fatalf("expected 2 events after ID %d, got %d", all[2].ID, len(after))
	}
	if after[0].Data != "line 3" {
		t.Fatalf("expected 'line 3', got %q", after[0].Data)
	}
}

func TestSessionResult_Persistence(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	sess := &model.Session{
		ID: "res-test", Repo: "owner/repo", Prompt: "what lang?",
		Mode: model.ModeTask, Status: model.StatusRunning,
		Branch: "telecoder/res-test", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Set a text result.
	sess.Status = model.StatusComplete
	sess.Result = model.Result{
		Type:    model.ResultText,
		Content: "This project is written in Go.",
	}
	if err := store.UpdateSession(sess); err != nil {
		t.Fatalf("update session: %v", err)
	}

	got, err := store.GetSession("res-test")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.Result.Type != model.ResultText {
		t.Fatalf("expected result type 'text', got %q", got.Result.Type)
	}
	if got.Result.Content != "This project is written in Go." {
		t.Fatalf("expected result content, got %q", got.Result.Content)
	}

	// Now test PR result with backfill.
	sess2 := &model.Session{
		ID: "res-pr", Repo: "owner/repo", Prompt: "fix bug",
		Mode: model.ModeTask, Status: model.StatusRunning,
		Branch: "telecoder/res-pr", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateSession(sess2); err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess2.Status = model.StatusComplete
	sess2.PRUrl = "https://github.com/owner/repo/pull/10"
	sess2.PRNumber = 10
	sess2.Result = model.Result{
		Type:     model.ResultPR,
		PRUrl:    "https://github.com/owner/repo/pull/10",
		PRNumber: 10,
	}
	if err := store.UpdateSession(sess2); err != nil {
		t.Fatalf("update session: %v", err)
	}

	got2, _ := store.GetSession("res-pr")
	if got2.Result.Type != model.ResultPR {
		t.Fatalf("expected result type 'pr', got %q", got2.Result.Type)
	}
	if got2.Result.PRUrl != "https://github.com/owner/repo/pull/10" {
		t.Fatalf("expected result PR URL, got %q", got2.Result.PRUrl)
	}
	if got2.Result.PRNumber != 10 {
		t.Fatalf("expected result PR number 10, got %d", got2.Result.PRNumber)
	}
}

func TestUpdateSessionPRFields(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	sess := &model.Session{
		ID: "upd-pr", Repo: "owner/repo", Prompt: "p",
		Mode: model.ModeTask, Status: model.StatusRunning,
		Branch: "telecoder/upd-pr", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	sess.PRUrl = "https://github.com/owner/repo/pull/7"
	sess.PRNumber = 7
	sess.Status = model.StatusComplete
	if err := store.UpdateSession(sess); err != nil {
		t.Fatalf("update session: %v", err)
	}

	got, _ := store.GetSession("upd-pr")
	if got.PRUrl != "https://github.com/owner/repo/pull/7" {
		t.Fatalf("expected PR URL, got %q", got.PRUrl)
	}
	if got.PRNumber != 7 {
		t.Fatalf("expected PR number 7, got %d", got.PRNumber)
	}
	if got.Status != model.StatusComplete {
		t.Fatalf("expected complete status, got %q", got.Status)
	}
}
