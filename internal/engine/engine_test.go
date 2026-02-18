package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jxucoder/TeleCoder/pkg/eventbus"
	"github.com/jxucoder/TeleCoder/pkg/gitprovider"
	"github.com/jxucoder/TeleCoder/pkg/model"
	"github.com/jxucoder/TeleCoder/pkg/pipeline"
	"github.com/jxucoder/TeleCoder/pkg/sandbox"
	sqliteStore "github.com/jxucoder/TeleCoder/pkg/store/sqlite"
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

// --- Agent selection tests ---

func TestResolveAgentName_SessionOverride(t *testing.T) {
	eng, _, _ := testEngine(t)

	got := eng.resolveAgentName("claude-code")
	if got != "claude-code" {
		t.Fatalf("expected 'claude-code', got %q", got)
	}
}

func TestResolveAgentName_DefaultAgent(t *testing.T) {
	eng, _, _ := testEngine(t)
	eng.config.CodingAgent = "opencode"

	got := eng.resolveAgentName("")
	if got != "opencode" {
		t.Fatalf("expected 'opencode', got %q", got)
	}
}

func TestResolveAgentName_AutoReturnsEmpty(t *testing.T) {
	eng, _, _ := testEngine(t)
	eng.config.CodingAgent = "auto"

	got := eng.resolveAgentName("")
	if got != "" {
		t.Fatalf("expected empty for auto, got %q", got)
	}

	got = eng.resolveAgentName("auto")
	if got != "" {
		t.Fatalf("expected empty for session auto, got %q", got)
	}
}

func TestResolveAgentName_SessionOverridesDefault(t *testing.T) {
	eng, _, _ := testEngine(t)
	eng.config.CodingAgent = "opencode"

	// Session override takes priority.
	got := eng.resolveAgentName("claude-code")
	if got != "claude-code" {
		t.Fatalf("expected 'claude-code', got %q", got)
	}

	// No session override: falls through to default.
	got = eng.resolveAgentName("")
	if got != "opencode" {
		t.Fatalf("expected 'opencode', got %q", got)
	}
}

func TestCreateAndRunSessionWithAgent(t *testing.T) {
	eng, sb, _ := testEngine(t)

	sess, err := eng.CreateAndRunSessionWithAgent("owner/repo", "fix the bug", "claude-code")
	if err != nil {
		t.Fatalf("CreateAndRunSessionWithAgent: %v", err)
	}
	if sess.Agent != "claude-code" {
		t.Fatalf("expected agent 'claude-code', got %q", sess.Agent)
	}

	got, err := eng.Store().GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Agent != "claude-code" {
		t.Fatalf("expected persisted agent 'claude-code', got %q", got.Agent)
	}

	time.Sleep(200 * time.Millisecond)
	if sb.startCalls < 1 {
		t.Fatalf("expected sandbox Start to be called, got %d calls", sb.startCalls)
	}
}

func TestRunSandboxRoundWithAgent_PassesAgentEnv(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	capSb := &capturingStubSandbox{}
	git := &stubGitProvider{}
	llmClient := &stubLLM{}

	eng := New(
		Config{
			DockerImage:     "test-image",
			MaxRevisions:    1,
			ChatIdleTimeout: 30 * time.Minute,
			ChatMaxMessages: 50,
			SandboxEnv:      []string{"GITHUB_TOKEN=abc"},
			CodingAgent:     "claude-code",
		},
		st, bus, capSb, git,
		pipeline.NewPlanStage(llmClient, ""),
		pipeline.NewReviewStage(llmClient, ""),
		pipeline.NewDecomposeStage(llmClient, ""),
		pipeline.NewVerifyStage(llmClient, ""),
	)

	sess := &model.Session{
		ID:     "test-env-pass",
		Repo:   "owner/repo",
		Branch: "telecoder/test",
		Status: model.StatusPending,
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	ctx := context.Background()
	_, err = eng.runSandboxRoundWithAgent(ctx, sess, "do the thing", "")
	if err != nil {
		t.Fatalf("runSandboxRoundWithAgent: %v", err)
	}

	if capSb.lastOpts == nil {
		t.Fatal("expected sandbox Start to be called")
	}

	foundAgent := false
	for _, e := range capSb.lastOpts.Env {
		if e == "TELECODER_CODING_AGENT=claude-code" {
			foundAgent = true
		}
	}
	if !foundAgent {
		t.Fatalf("expected TELECODER_CODING_AGENT=claude-code in sandbox env, got %v", capSb.lastOpts.Env)
	}
}

// capturingStubSandbox records the StartOptions from the last Start call.
type capturingStubSandbox struct {
	lastOpts *sandbox.StartOptions
}

func (s *capturingStubSandbox) Start(_ context.Context, opts sandbox.StartOptions) (string, error) {
	s.lastOpts = &opts
	return "cap-container", nil
}
func (s *capturingStubSandbox) Stop(_ context.Context, _ string) error                             { return nil }
func (s *capturingStubSandbox) Wait(_ context.Context, _ string) (int, error)                      { return 0, nil }
func (s *capturingStubSandbox) StreamLogs(_ context.Context, _ string) (sandbox.LineScanner, error) { return &stubScanner{}, nil }
func (s *capturingStubSandbox) Exec(_ context.Context, _ string, _ []string) (sandbox.LineScanner, error) { return &stubScanner{}, nil }
func (s *capturingStubSandbox) ExecCollect(_ context.Context, _ string, _ []string) (string, error) { return "", nil }
func (s *capturingStubSandbox) CommitAndPush(_ context.Context, _, _, _ string) error               { return nil }
func (s *capturingStubSandbox) EnsureNetwork(_ context.Context, _ string) error                     { return nil }
func (s *capturingStubSandbox) IsRunning(_ context.Context, _ string) bool                          { return true }

// --- scriptedScanner emits pre-defined lines to simulate sandbox output ---

type scriptedScanner struct {
	lines []string
	idx   int
}

func (s *scriptedScanner) Scan() bool {
	if s.idx < len(s.lines) {
		s.idx++
		return true
	}
	return false
}
func (s *scriptedScanner) Text() string { return s.lines[s.idx-1] }
func (s *scriptedScanner) Err() error   { return nil }
func (s *scriptedScanner) Close() error { return nil }

// scriptedSandbox returns a scripted log stream from StreamLogs.
type scriptedSandbox struct {
	logLines      []string
	createPRCalls *int // shared counter, optional
}

func (s *scriptedSandbox) Start(_ context.Context, _ sandbox.StartOptions) (string, error) {
	return "scripted-container", nil
}
func (s *scriptedSandbox) Stop(_ context.Context, _ string) error        { return nil }
func (s *scriptedSandbox) Wait(_ context.Context, _ string) (int, error) { return 0, nil }
func (s *scriptedSandbox) StreamLogs(_ context.Context, _ string) (sandbox.LineScanner, error) {
	return &scriptedScanner{lines: s.logLines}, nil
}
func (s *scriptedSandbox) Exec(_ context.Context, _ string, _ []string) (sandbox.LineScanner, error) {
	return &stubScanner{}, nil
}
func (s *scriptedSandbox) ExecCollect(_ context.Context, _ string, _ []string) (string, error) {
	return "", nil
}
func (s *scriptedSandbox) CommitAndPush(_ context.Context, _, _, _ string) error { return nil }
func (s *scriptedSandbox) EnsureNetwork(_ context.Context, _ string) error       { return nil }
func (s *scriptedSandbox) IsRunning(_ context.Context, _ string) bool            { return true }

// --- Flexible output tests ---

func TestRunSession_TextResult_NoPR(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	git := &stubGitProvider{}
	llmClient := &stubLLM{}

	sb := &scriptedSandbox{
		logLines: []string{
			"This project is written in Go.",
			`###TELECODER_RESULT### {"type":"text"}`,
		},
	}

	eng := New(
		Config{
			DockerImage:     "test-image",
			MaxRevisions:    1,
			ChatIdleTimeout: 30 * time.Minute,
			ChatMaxMessages: 50,
		},
		st, bus, sb, git,
		pipeline.NewPlanStage(llmClient, ""),
		pipeline.NewReviewStage(llmClient, ""),
		pipeline.NewDecomposeStage(llmClient, ""),
		pipeline.NewVerifyStage(llmClient, ""),
	)

	sess, err := eng.CreateAndRunSession("owner/repo", "what language is this?")
	if err != nil {
		t.Fatalf("CreateAndRunSession: %v", err)
	}

	// Wait for the background goroutine to finish.
	time.Sleep(500 * time.Millisecond)

	got, err := eng.Store().GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	if got.Status != model.StatusComplete {
		t.Fatalf("expected status 'complete', got %q", got.Status)
	}
	if got.Result.Type != model.ResultText {
		t.Fatalf("expected result type 'text', got %q", got.Result.Type)
	}
	if got.Result.Content == "" {
		t.Fatal("expected non-empty result content")
	}
	// No PR should have been created.
	if git.createPRCalls > 0 {
		t.Fatalf("expected no CreatePR calls for text result, got %d", git.createPRCalls)
	}
	if got.PRUrl != "" {
		t.Fatalf("expected empty PR URL for text result, got %q", got.PRUrl)
	}
}

func TestRunSession_PRResult_BackwardCompat(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	git := &stubGitProvider{}
	llmClient := &stubLLM{}

	sb := &scriptedSandbox{
		logLines: []string{
			"Making changes...",
			"###TELECODER_DONE### telecoder/test-pr",
		},
	}

	eng := New(
		Config{
			DockerImage:     "test-image",
			MaxRevisions:    1,
			ChatIdleTimeout: 30 * time.Minute,
			ChatMaxMessages: 50,
		},
		st, bus, sb, git,
		pipeline.NewPlanStage(llmClient, ""),
		pipeline.NewReviewStage(llmClient, ""),
		pipeline.NewDecomposeStage(llmClient, ""),
		pipeline.NewVerifyStage(llmClient, ""),
	)

	sess, err := eng.CreateAndRunSession("owner/repo", "fix the bug")
	if err != nil {
		t.Fatalf("CreateAndRunSession: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	got, err := eng.Store().GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	if got.Status != model.StatusComplete {
		t.Fatalf("expected status 'complete', got %q", got.Status)
	}
	if git.createPRCalls < 1 {
		t.Fatalf("expected CreatePR to be called, got %d calls", git.createPRCalls)
	}
	// Legacy fields should be populated.
	if got.PRUrl == "" {
		t.Fatal("expected non-empty PRUrl")
	}
	if got.PRNumber == 0 {
		t.Fatal("expected non-zero PRNumber")
	}
	// Result should also be populated.
	if got.Result.Type != model.ResultPR {
		t.Fatalf("expected result type 'pr', got %q", got.Result.Type)
	}
	if got.Result.PRUrl != got.PRUrl {
		t.Fatalf("expected Result.PRUrl to match PRUrl, got %q vs %q", got.Result.PRUrl, got.PRUrl)
	}
}

// --- Multi-step helper tests ---

// multiStepSandbox simulates a persistent container that tracks exec calls
// and optionally simulates uncommitted changes.
type multiStepSandbox struct {
	execCalls    [][]string
	execResults  map[string]string // command prefix → output
	execErrors   map[string]error  // command prefix → error
	startCalls   int
	stopCalls    int
	hasChanges   bool // whether git diff --cached --quiet should "fail" (has changes)
}

func newMultiStepSandbox() *multiStepSandbox {
	return &multiStepSandbox{
		execResults: make(map[string]string),
		execErrors:  make(map[string]error),
	}
}

func (s *multiStepSandbox) Start(_ context.Context, _ sandbox.StartOptions) (string, error) {
	s.startCalls++
	return "multi-container", nil
}
func (s *multiStepSandbox) Stop(_ context.Context, _ string) error { s.stopCalls++; return nil }
func (s *multiStepSandbox) Wait(_ context.Context, _ string) (int, error) { return 0, nil }
func (s *multiStepSandbox) StreamLogs(_ context.Context, _ string) (sandbox.LineScanner, error) {
	return &stubScanner{}, nil
}
func (s *multiStepSandbox) Exec(_ context.Context, _ string, cmd []string) (sandbox.LineScanner, error) {
	s.execCalls = append(s.execCalls, cmd)
	return &stubScanner{}, nil
}
func (s *multiStepSandbox) ExecCollect(_ context.Context, _ string, cmd []string) (string, error) {
	s.execCalls = append(s.execCalls, cmd)

	// Join cmd to match against known results.
	joined := strings.Join(cmd, " ")

	// Simulate git diff --cached --quiet behavior.
	if strings.Contains(joined, "git diff --cached --quiet") {
		if s.hasChanges {
			return "", fmt.Errorf("exit status 1")
		}
		return "", nil
	}

	// Simulate git rev-parse HEAD.
	if strings.Contains(joined, "git rev-parse HEAD") {
		return "abc123def456\n", nil
	}

	// Check for custom results.
	for prefix, result := range s.execResults {
		if strings.Contains(joined, prefix) {
			return result, s.execErrors[prefix]
		}
	}

	return "", nil
}
func (s *multiStepSandbox) CommitAndPush(_ context.Context, _, _, _ string) error { return nil }
func (s *multiStepSandbox) EnsureNetwork(_ context.Context, _ string) error       { return nil }
func (s *multiStepSandbox) IsRunning(_ context.Context, _ string) bool            { return true }

func TestBuildSandboxEnv(t *testing.T) {
	eng, _, _ := testEngine(t)
	eng.config.SandboxEnv = []string{"GITHUB_TOKEN=abc", "ANTHROPIC_API_KEY=xyz"}
	eng.config.CodingAgent = "opencode"

	env := eng.buildSandboxEnv("")
	found := false
	for _, e := range env {
		if e == "TELECODER_CODING_AGENT=opencode" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected TELECODER_CODING_AGENT=opencode, got %v", env)
	}

	// Verify original slice not modified.
	if len(eng.config.SandboxEnv) != 2 {
		t.Fatalf("original sandbox env should not be modified, got %v", eng.config.SandboxEnv)
	}
}

func TestBuildSandboxEnv_AutoAgent(t *testing.T) {
	eng, _, _ := testEngine(t)
	eng.config.SandboxEnv = []string{"GITHUB_TOKEN=abc"}
	eng.config.CodingAgent = "auto"

	env := eng.buildSandboxEnv("")
	for _, e := range env {
		if strings.HasPrefix(e, "TELECODER_CODING_AGENT=") {
			t.Fatalf("should not set agent env for 'auto', got %v", env)
		}
	}
}

func TestCheckpointSubTask(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	sb := newMultiStepSandbox()
	sb.hasChanges = true // Simulate uncommitted changes.
	git := &stubGitProvider{}
	llmClient := &stubLLM{}

	eng := New(
		Config{DockerImage: "test-image", MaxRevisions: 1, ChatIdleTimeout: 30 * time.Minute, ChatMaxMessages: 50},
		st, bus, sb, git,
		pipeline.NewPlanStage(llmClient, ""),
		pipeline.NewReviewStage(llmClient, ""),
		pipeline.NewDecomposeStage(llmClient, ""),
		pipeline.NewVerifyStage(llmClient, ""),
	)

	ctx := context.Background()
	hash, err := eng.checkpointSubTask(ctx, "multi-container", "Add auth", 0)
	if err != nil {
		t.Fatalf("checkpointSubTask: %v", err)
	}
	if hash != "abc123def456" {
		t.Fatalf("expected hash 'abc123def456', got %q", hash)
	}

	// Verify git add and git commit were called.
	foundAdd := false
	foundCommit := false
	for _, cmd := range sb.execCalls {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "git add -A") {
			foundAdd = true
		}
		if strings.Contains(joined, "git commit") && strings.Contains(joined, "step 1") {
			foundCommit = true
		}
	}
	if !foundAdd {
		t.Fatal("expected git add -A call")
	}
	if !foundCommit {
		t.Fatal("expected git commit call with step number")
	}
}

func TestCheckpointSubTask_NoChanges(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	sb := newMultiStepSandbox()
	sb.hasChanges = false // No uncommitted changes.
	git := &stubGitProvider{}
	llmClient := &stubLLM{}

	eng := New(
		Config{DockerImage: "test-image", MaxRevisions: 1, ChatIdleTimeout: 30 * time.Minute, ChatMaxMessages: 50},
		st, bus, sb, git,
		pipeline.NewPlanStage(llmClient, ""),
		pipeline.NewReviewStage(llmClient, ""),
		pipeline.NewDecomposeStage(llmClient, ""),
		pipeline.NewVerifyStage(llmClient, ""),
	)

	ctx := context.Background()
	hash, err := eng.checkpointSubTask(ctx, "multi-container", "Add auth", 0)
	if err != nil {
		t.Fatalf("checkpointSubTask: %v", err)
	}
	// Should return HEAD hash even without new commit.
	if hash != "abc123def456" {
		t.Fatalf("expected hash 'abc123def456', got %q", hash)
	}

	// Should NOT have a git commit call since there were no changes.
	for _, cmd := range sb.execCalls {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "git commit") {
			t.Fatal("should not call git commit when no changes")
		}
	}
}

func TestHasUncommittedChanges(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	sb := newMultiStepSandbox()
	git := &stubGitProvider{}
	llmClient := &stubLLM{}

	eng := New(
		Config{DockerImage: "test-image", MaxRevisions: 1, ChatIdleTimeout: 30 * time.Minute, ChatMaxMessages: 50},
		st, bus, sb, git,
		pipeline.NewPlanStage(llmClient, ""),
		pipeline.NewReviewStage(llmClient, ""),
		pipeline.NewDecomposeStage(llmClient, ""),
		pipeline.NewVerifyStage(llmClient, ""),
	)

	ctx := context.Background()

	sb.hasChanges = true
	if !eng.hasUncommittedChanges(ctx, "multi-container") {
		t.Fatal("expected hasUncommittedChanges=true")
	}

	sb.hasChanges = false
	if eng.hasUncommittedChanges(ctx, "multi-container") {
		t.Fatal("expected hasUncommittedChanges=false")
	}
}

func TestRollbackToCheckpoint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	sb := newMultiStepSandbox()
	git := &stubGitProvider{}
	llmClient := &stubLLM{}

	eng := New(
		Config{DockerImage: "test-image", MaxRevisions: 1, ChatIdleTimeout: 30 * time.Minute, ChatMaxMessages: 50},
		st, bus, sb, git,
		pipeline.NewPlanStage(llmClient, ""),
		pipeline.NewReviewStage(llmClient, ""),
		pipeline.NewDecomposeStage(llmClient, ""),
		pipeline.NewVerifyStage(llmClient, ""),
	)

	ctx := context.Background()
	err = eng.rollbackToCheckpoint(ctx, "multi-container", "abc123")
	if err != nil {
		t.Fatalf("rollbackToCheckpoint: %v", err)
	}

	// Verify git reset --hard was called.
	found := false
	for _, cmd := range sb.execCalls {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "git reset --hard abc123") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected git reset --hard call")
	}
}

func TestWriteProgressFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	sb := newMultiStepSandbox()
	git := &stubGitProvider{}
	llmClient := &stubLLM{}

	eng := New(
		Config{DockerImage: "test-image", MaxRevisions: 1, ChatIdleTimeout: 30 * time.Minute, ChatMaxMessages: 50},
		st, bus, sb, git,
		pipeline.NewPlanStage(llmClient, ""),
		pipeline.NewReviewStage(llmClient, ""),
		pipeline.NewDecomposeStage(llmClient, ""),
		pipeline.NewVerifyStage(llmClient, ""),
	)

	statuses := []pipeline.SubTaskStatus{
		{Title: "Step 1", Description: "Do thing", Status: "completed", CommitHash: "abc"},
	}

	ctx := context.Background()
	err = eng.writeProgressFile(ctx, "multi-container", statuses)
	if err != nil {
		t.Fatalf("writeProgressFile: %v", err)
	}

	// Verify a cat command was exec'd that writes to .telecoder-progress.json.
	found := false
	for _, cmd := range sb.execCalls {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, ".telecoder-progress.json") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected exec call writing .telecoder-progress.json")
	}
}

// TestRunSessionMultiStep_PersistentContainer verifies that the multi-step path
// starts a persistent container and runs setup.
func TestRunSessionMultiStep_PersistentContainer(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	sb := newMultiStepSandbox()
	sb.hasChanges = true // Simulate code changes.
	git := &stubGitProvider{}

	// Use an LLM that returns 2 sub-tasks.
	multiLLM := &multiStepLLM{}

	eng := New(
		Config{
			DockerImage:     "test-image",
			MaxRevisions:    1,
			MaxSubTasks:     5,
			ChatIdleTimeout: 30 * time.Minute,
			ChatMaxMessages: 50,
		},
		st, bus, sb, git,
		pipeline.NewPlanStage(multiLLM, ""),
		pipeline.NewReviewStage(multiLLM, ""),
		pipeline.NewDecomposeStage(multiLLM, ""),
		pipeline.NewVerifyStage(multiLLM, ""),
	)

	sess, err := eng.CreateAndRunSession("owner/repo", "complex feature")
	if err != nil {
		t.Fatalf("CreateAndRunSession: %v", err)
	}

	// Wait for the session to complete.
	time.Sleep(1 * time.Second)

	got, _ := eng.Store().GetSession(sess.ID)
	if got.Status != model.StatusComplete {
		t.Fatalf("expected 'complete', got %q (error: %s)", got.Status, got.Error)
	}

	// Verify persistent container was started.
	if sb.startCalls < 1 {
		t.Fatal("expected at least one sandbox Start call")
	}

	// Verify PR was created (since hasChanges=true).
	if git.createPRCalls < 1 {
		t.Fatal("expected PR to be created for multi-step task with changes")
	}

	// Verify progress events were emitted.
	events, _ := eng.Store().GetEvents(sess.ID, 0)
	progressCount := 0
	stepCount := 0
	for _, ev := range events {
		if ev.Type == "progress" {
			progressCount++
		}
		if ev.Type == "step" {
			stepCount++
		}
	}
	if stepCount < 2 {
		t.Fatalf("expected at least 2 step events, got %d", stepCount)
	}
}

// multiStepLLM returns 2 sub-tasks from decompose and simple plans/reviews.
type multiStepLLM struct{}

func (m *multiStepLLM) Complete(_ context.Context, system, _ string) (string, error) {
	lower := strings.ToLower(system)
	if strings.Contains(lower, "decompos") || strings.Contains(lower, "sub-task") {
		return `[{"title":"Add feature","description":"Implement the core feature"},{"title":"Add tests","description":"Add unit tests for the feature"}]`, nil
	}
	if strings.Contains(lower, "plan") {
		return "1. Modify files\n2. Add tests", nil
	}
	if strings.Contains(lower, "review") {
		return "APPROVED: looks good", nil
	}
	if strings.Contains(lower, "verify") || strings.Contains(lower, "test output") {
		return "PASSED: all tests pass", nil
	}
	return "ok", nil
}

func TestDispatchLogLine_ResultMarker(t *testing.T) {
	eng, _, _ := testEngine(t)

	sess, _ := eng.CreateAndRunSession("owner/repo", "task")
	time.Sleep(100 * time.Millisecond)

	eng.dispatchLogLine(sess.ID, `###TELECODER_RESULT### {"type":"text"}`)

	events, _ := eng.Store().GetEvents(sess.ID, 0)
	resultFound := false
	for _, e := range events {
		if e.Type == "result" && strings.Contains(e.Data, "text") {
			resultFound = true
		}
	}
	if !resultFound {
		t.Fatal("expected RESULT dispatch event with type 'result'")
	}
}
