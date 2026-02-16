package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jxucoder/TeleCoder/eventbus"
	"github.com/jxucoder/TeleCoder/gitprovider"
	"github.com/jxucoder/TeleCoder/model"
	"github.com/jxucoder/TeleCoder/pipeline"
	"github.com/jxucoder/TeleCoder/sandbox"
	sqliteStore "github.com/jxucoder/TeleCoder/store/sqlite"
)

// --- stubs ---

type stubLLM struct{}

func (s *stubLLM) Complete(_ context.Context, _, _ string) (string, error) {
	return `[{"title":"Complete task","description":"do the thing"}]`, nil
}

type stubSandbox struct {
	startCalls int
}

func (s *stubSandbox) Start(_ context.Context, _ sandbox.StartOptions) (string, error) {
	s.startCalls++
	return "stub-container", nil
}
func (s *stubSandbox) Stop(_ context.Context, _ string) error                             { return nil }
func (s *stubSandbox) Wait(_ context.Context, _ string) (int, error)                      { return 0, nil }
func (s *stubSandbox) StreamLogs(_ context.Context, _ string) (sandbox.LineScanner, error) { return &stubScanner{}, nil }
func (s *stubSandbox) Exec(_ context.Context, _ string, _ []string) (sandbox.LineScanner, error) { return &stubScanner{}, nil }
func (s *stubSandbox) ExecCollect(_ context.Context, _ string, _ []string) (string, error) { return "", nil }
func (s *stubSandbox) CommitAndPush(_ context.Context, _, _, _ string) error               { return nil }
func (s *stubSandbox) EnsureNetwork(_ context.Context, _ string) error                     { return nil }
func (s *stubSandbox) IsRunning(_ context.Context, _ string) bool                          { return true }

type stubScanner struct{}

func (s *stubScanner) Scan() bool   { return false }
func (s *stubScanner) Text() string { return "" }
func (s *stubScanner) Err() error   { return nil }
func (s *stubScanner) Close() error { return nil }

type stubGitProvider struct {
	createPRCalls int
}

func (s *stubGitProvider) CreatePR(_ context.Context, _ gitprovider.PROptions) (string, int, error) {
	s.createPRCalls++
	return "https://github.com/test/repo/pull/1", 1, nil
}
func (s *stubGitProvider) GetDefaultBranch(_ context.Context, _ string) (string, error) {
	return "main", nil
}
func (s *stubGitProvider) IndexRepo(_ context.Context, _ string) (*gitprovider.RepoContext, error) {
	return &gitprovider.RepoContext{Tree: "README.md"}, nil
}
func (s *stubGitProvider) ReplyToPRComment(_ context.Context, _ string, _ int, _ string) error {
	return nil
}

// --- helpers ---

func testEngine(t *testing.T) (*Engine, *stubSandbox, *stubGitProvider) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	sb := &stubSandbox{}
	git := &stubGitProvider{}
	llmClient := &stubLLM{}
	plan := pipeline.NewPlanStage(llmClient, "")
	review := pipeline.NewReviewStage(llmClient, "")
	decompose := pipeline.NewDecomposeStage(llmClient, "")
	verify := pipeline.NewVerifyStage(llmClient, "")

	eng := New(
		Config{
			DockerImage:     "test-image",
			MaxRevisions:    1,
			ChatIdleTimeout: 30 * time.Minute,
			ChatMaxMessages: 50,
		},
		st, bus, sb, git, plan, review, decompose, verify,
	)
	return eng, sb, git
}

// --- tests ---

func TestCreateAndRunSession(t *testing.T) {
	eng, sb, _ := testEngine(t)

	sess, err := eng.CreateAndRunSession("owner/repo", "fix the bug")
	if err != nil {
		t.Fatalf("CreateAndRunSession: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if sess.Repo != "owner/repo" {
		t.Fatalf("expected repo 'owner/repo', got %q", sess.Repo)
	}
	if sess.Mode != model.ModeTask {
		t.Fatalf("expected mode 'task', got %q", sess.Mode)
	}
	if sess.Status != model.StatusPending {
		t.Fatalf("expected status 'pending', got %q", sess.Status)
	}
	if !strings.HasPrefix(sess.Branch, "telecoder/") {
		t.Fatalf("expected branch prefix 'telecoder/', got %q", sess.Branch)
	}

	// Wait for the background goroutine to start the sandbox.
	time.Sleep(200 * time.Millisecond)

	if sb.startCalls < 1 {
		t.Fatalf("expected sandbox Start to be called, got %d calls", sb.startCalls)
	}
}

func TestCreateChatSession(t *testing.T) {
	eng, sb, _ := testEngine(t)

	sess, err := eng.CreateChatSession("owner/repo")
	if err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	if sess.Mode != model.ModeChat {
		t.Fatalf("expected mode 'chat', got %q", sess.Mode)
	}
	if sess.Status != model.StatusPending {
		t.Fatalf("expected status 'pending', got %q", sess.Status)
	}

	// Wait for the background init to run.
	time.Sleep(200 * time.Millisecond)

	if sb.startCalls < 1 {
		t.Fatalf("expected sandbox Start to be called for chat init")
	}
}

func TestSessionStoredInDB(t *testing.T) {
	eng, _, _ := testEngine(t)

	sess, err := eng.CreateAndRunSession("owner/repo", "add tests")
	if err != nil {
		t.Fatalf("CreateAndRunSession: %v", err)
	}

	got, err := eng.Store().GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Prompt != "add tests" {
		t.Fatalf("expected prompt 'add tests', got %q", got.Prompt)
	}
}

func TestEmitEventStoredAndPublished(t *testing.T) {
	eng, _, _ := testEngine(t)

	sess, _ := eng.CreateAndRunSession("owner/repo", "task")
	ch := eng.Bus().Subscribe(sess.ID)
	defer eng.Bus().Unsubscribe(sess.ID, ch)

	// The background goroutine emits events. Wait a bit and check the store.
	time.Sleep(300 * time.Millisecond)

	events, err := eng.Store().GetEvents(sess.ID, 0)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event to be stored")
	}
}

func TestDispatchLogLine(t *testing.T) {
	eng, _, _ := testEngine(t)

	sess, _ := eng.CreateAndRunSession("owner/repo", "task")
	// Wait for session to be created.
	time.Sleep(100 * time.Millisecond)

	// Directly test dispatchLogLine by calling it.
	eng.dispatchLogLine(sess.ID, "###TELECODER_STATUS### Cloning repo")
	eng.dispatchLogLine(sess.ID, "###TELECODER_ERROR### something failed")
	eng.dispatchLogLine(sess.ID, "###TELECODER_DONE### telecoder/abc123")
	eng.dispatchLogLine(sess.ID, "regular log line")

	events, _ := eng.Store().GetEvents(sess.ID, 0)
	statusFound := false
	errorFound := false
	outputFound := false
	for _, e := range events {
		if e.Type == "status" && e.Data == "Cloning repo" {
			statusFound = true
		}
		if e.Type == "error" && e.Data == "something failed" {
			errorFound = true
		}
		if e.Type == "output" && e.Data == "regular log line" {
			outputFound = true
		}
	}
	if !statusFound {
		t.Fatal("expected STATUS dispatch event")
	}
	if !errorFound {
		t.Fatal("expected ERROR dispatch event")
	}
	if !outputFound {
		t.Fatal("expected OUTPUT dispatch event")
	}
}

func TestFailSession(t *testing.T) {
	eng, _, _ := testEngine(t)

	sess, _ := eng.CreateAndRunSession("owner/repo", "task")
	time.Sleep(200 * time.Millisecond)

	got, _ := eng.Store().GetSession(sess.ID)
	eng.failSession(got, "test error")

	updated, _ := eng.Store().GetSession(sess.ID)
	if updated.Status != model.StatusError {
		t.Fatalf("expected status 'error', got %q", updated.Status)
	}
	if updated.Error != "test error" {
		t.Fatalf("expected error 'test error', got %q", updated.Error)
	}
}

func TestEngineStartAndStop(t *testing.T) {
	eng, _, _ := testEngine(t)

	ctx, cancel := context.WithCancel(context.Background())
	eng.Start(ctx)

	// Engine should be running. Stop it.
	cancel()
	eng.Stop()

	// Should not panic or hang.
}
