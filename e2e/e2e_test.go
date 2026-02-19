// End-to-end tests for the TeleCoder server stack.
//
// This test exercises the full server stack:
//   - Real HTTP router (chi)
//   - Real SQLite store (WAL mode, temp dir)
//   - Real event bus (in-memory pub/sub)
//   - Simulated sandbox (produces realistic marker-protocol output)
//   - Fake git provider (records PR creation)
//   - Fake LLM (deterministic responses)
//
// The only thing simulated is the sandbox container and the git/LLM backends.
// Everything else — HTTP routing, engine orchestration, store persistence,
// event streaming — is real production code.
//
// Does NOT require Docker, API keys, or network access.
package e2e_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	telecoder "github.com/jxucoder/TeleCoder"
	"github.com/jxucoder/TeleCoder/pkg/eventbus"
	"github.com/jxucoder/TeleCoder/pkg/gitprovider"
	"github.com/jxucoder/TeleCoder/internal/engine"
	"github.com/jxucoder/TeleCoder/internal/httpapi"
	"github.com/jxucoder/TeleCoder/pkg/model"
	"github.com/jxucoder/TeleCoder/pkg/sandbox"
	sqliteStore "github.com/jxucoder/TeleCoder/pkg/store/sqlite"
)

// ---------------------------------------------------------------------------
// Simulated sandbox: produces realistic marker-protocol output
// ---------------------------------------------------------------------------

type simSandbox struct {
	mu        sync.Mutex
	starts    []sandbox.StartOptions
	stopCalls int
}

func (s *simSandbox) Start(_ context.Context, opts sandbox.StartOptions) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts = append(s.starts, opts)
	return fmt.Sprintf("sim-%s-%d", opts.SessionID, len(s.starts)), nil
}

func (s *simSandbox) Stop(_ context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopCalls++
	return nil
}

func (s *simSandbox) Wait(_ context.Context, _ string) (int, error) { return 0, nil }

func (s *simSandbox) StreamLogs(_ context.Context, _ string) (sandbox.LineScanner, error) {
	s.mu.Lock()
	opts := s.starts[len(s.starts)-1]
	s.mu.Unlock()

	branch := "telecoder/e2e-test"
	lines := []string{
		"###TELECODER_STATUS### Cloning " + opts.Repo + "...",
		"###TELECODER_STATUS### Repository cloned successfully",
		"###TELECODER_STATUS### Dependencies installed",
		"###TELECODER_STATUS### Running agent...",
		"Agent working on: " + truncate(opts.Prompt, 80),
		"Modified src/main.go",
		"###TELECODER_STATUS### Agent finished",
		"###TELECODER_STATUS### Changes committed",
		"###TELECODER_STATUS### Branch pushed successfully",
		"###TELECODER_DONE### " + branch,
	}
	return &lineSliceScanner{lines: lines}, nil
}

func (s *simSandbox) Exec(_ context.Context, _ string, _ []string) (sandbox.LineScanner, error) {
	return &lineSliceScanner{lines: []string{"exec done"}}, nil
}

func (s *simSandbox) ExecCollect(_ context.Context, _ string, cmd []string) (string, error) {
	if len(cmd) > 0 && cmd[0] == "test" {
		return "", fmt.Errorf("file not found")
	}
	if len(cmd) >= 3 && cmd[0] == "git" {
		return "diff --git a/src/main.go b/src/main.go\n+// changes\n", nil
	}
	return "", nil
}

func (s *simSandbox) CommitAndPush(_ context.Context, _, _, _ string) error { return nil }
func (s *simSandbox) EnsureNetwork(_ context.Context, _ string) error      { return nil }
func (s *simSandbox) IsRunning(_ context.Context, _ string) bool           { return true }

func (s *simSandbox) getStarts() []sandbox.StartOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]sandbox.StartOptions, len(s.starts))
	copy(cp, s.starts)
	return cp
}

type lineSliceScanner struct {
	lines []string
	idx   int
}

func (s *lineSliceScanner) Scan() bool {
	if s.idx < len(s.lines) {
		s.idx++
		return true
	}
	return false
}
func (s *lineSliceScanner) Text() string { return s.lines[s.idx-1] }
func (s *lineSliceScanner) Err() error   { return nil }
func (s *lineSliceScanner) Close() error { return nil }

// ---------------------------------------------------------------------------
// Fake git provider
// ---------------------------------------------------------------------------

type fakeGitProvider struct {
	mu        sync.Mutex
	prCreated bool
	prRepo    string
	prBranch  string
}

func (g *fakeGitProvider) CreatePR(_ context.Context, opts gitprovider.PROptions) (string, int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prCreated = true
	g.prRepo = opts.Repo
	g.prBranch = opts.Branch
	return fmt.Sprintf("https://github.com/%s/pull/42", opts.Repo), 42, nil
}

func (g *fakeGitProvider) GetDefaultBranch(_ context.Context, _ string) (string, error) {
	return "main", nil
}



func (g *fakeGitProvider) ReplyToPRComment(_ context.Context, _ string, _ int, _ string) error {
	return nil
}

// ---------------------------------------------------------------------------
// Fake LLM
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// e2eRouter builds a real engine + HTTP handler and returns the chi router.
// Uses httptest.NewRecorder for each request (no TCP socket needed).
type e2eHarness struct {
	handler *httpapi.Handler
	sb      *simSandbox
	git     *fakeGitProvider
	eng     *engine.Engine
}

func setupE2E(t *testing.T, cfg engine.Config) *e2eHarness {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "e2e.db")
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.NewInMemoryBus()
	sb := &simSandbox{}
	git := &fakeGitProvider{}

	if cfg.DockerImage == "" {
		cfg.DockerImage = "telecoder-sandbox"
	}
	if cfg.MaxRevisions == 0 {
		cfg.MaxRevisions = 1
	}
	if cfg.ChatIdleTimeout == 0 {
		cfg.ChatIdleTimeout = 30 * time.Minute
	}
	if cfg.ChatMaxMessages == 0 {
		cfg.ChatMaxMessages = 50
	}

	eng := engine.New(
		cfg, st, bus, sb, git,
	)

	ctx, cancel := context.WithCancel(context.Background())
	eng.Start(ctx)
	t.Cleanup(func() {
		cancel()
		eng.Stop()
	})

	handler := httpapi.New(eng)
	return &e2eHarness{handler: handler, sb: sb, git: git, eng: eng}
}

// do executes an HTTP request against the handler and returns the response recorder.
func (h *e2eHarness) do(method, path string, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	h.handler.Router().ServeHTTP(w, req)
	return w
}

// waitForSession polls GET /api/sessions/:id until the session reaches a terminal state.
func (h *e2eHarness) waitForSession(t *testing.T, id string, timeout time.Duration) model.Session {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		w := h.do("GET", "/api/sessions/"+id, "")
		var sess model.Session
		json.NewDecoder(w.Body).Decode(&sess)
		if sess.Status == model.StatusComplete || sess.Status == model.StatusError {
			return sess
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("session %s did not complete within %v", id, timeout)
	return model.Session{}
}

// ---------------------------------------------------------------------------
// E2E Tests
// ---------------------------------------------------------------------------

// TestE2E_TaskSessionFullLifecycle tests the happy path:
// POST session → engine runs decompose → plan → sandbox → review → PR created.
// Then verifies GET session, GET events (SSE), and GET sessions list.
func TestE2E_TaskSessionFullLifecycle(t *testing.T) {
	h := setupE2E(t, engine.Config{})

	// 1. Create session via API.
	w := h.do("POST", "/api/sessions", `{"repo":"myorg/myapp","prompt":"add rate limiting to /api/users"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created struct {
		ID     string `json:"id"`
		Branch string `json:"branch"`
		Mode   string `json:"mode"`
	}
	json.NewDecoder(w.Body).Decode(&created)
	if created.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if created.Mode != "task" {
		t.Fatalf("expected mode 'task', got %q", created.Mode)
	}
	t.Logf("Created session %s (branch: %s)", created.ID, created.Branch)

	// 2. Wait for completion.
	sess := h.waitForSession(t, created.ID, 10*time.Second)
	if sess.Status != model.StatusComplete {
		t.Fatalf("expected 'complete', got %q (error: %s)", sess.Status, sess.Error)
	}
	t.Logf("Session completed: PR=%s (#%d)", sess.PRUrl, sess.PRNumber)

	// 3. Verify PR was created.
	if sess.PRUrl == "" || sess.PRNumber == 0 {
		t.Fatal("expected PR URL and number to be set")
	}
	h.git.mu.Lock()
	if !h.git.prCreated {
		t.Fatal("expected git CreatePR to be called")
	}
	if h.git.prRepo != "myorg/myapp" {
		t.Fatalf("expected PR repo 'myorg/myapp', got %q", h.git.prRepo)
	}
	h.git.mu.Unlock()

	// 4. Verify sandbox was started.
	starts := h.sb.getStarts()
	if len(starts) < 1 {
		t.Fatal("expected at least 1 sandbox Start call")
	}
	t.Logf("Sandbox started %d time(s)", len(starts))

	// 5. Verify events stored in the database.
	events, err := h.eng.Store().GetEvents(created.ID, 0)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	eventTypes := map[string]int{}
	for _, ev := range events {
		eventTypes[ev.Type]++
	}
	if eventTypes["status"] == 0 {
		t.Fatal("expected 'status' events")
	}
	if eventTypes["done"] == 0 {
		t.Fatal("expected 'done' event")
	}
	t.Logf("Events stored: %v (total %d)", eventTypes, len(events))

	// 6. Verify SSE endpoint streams historical events.
	// The SSE handler is long-lived, so we run it in a goroutine with a
	// context that we cancel after reading the buffered historical events.
	sseCtx, sseCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer sseCancel()
	sseReq := httptest.NewRequest("GET", "/api/sessions/"+created.ID+"/events", nil)
	sseReq = sseReq.WithContext(sseCtx)
	sseW := httptest.NewRecorder()

	sseDone := make(chan struct{})
	go func() {
		defer close(sseDone)
		h.handler.Router().ServeHTTP(sseW, sseReq)
	}()

	// Wait for context cancellation (which stops the SSE handler).
	<-sseDone

	sseEventCount := 0
	sseScanner := bufio.NewScanner(sseW.Body)
	for sseScanner.Scan() {
		line := sseScanner.Text()
		if strings.HasPrefix(line, "data: ") {
			sseEventCount++
		}
	}
	if sseEventCount == 0 {
		t.Fatal("expected SSE endpoint to stream historical events")
	}
	t.Logf("SSE streamed %d historical events", sseEventCount)

	// 6. Verify session in list endpoint.
	w = h.do("GET", "/api/sessions", "")
	var sessions []model.Session
	json.NewDecoder(w.Body).Decode(&sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != created.ID {
		t.Fatalf("expected session %s, got %s", created.ID, sessions[0].ID)
	}
}

// TestE2E_AgentOverrideReachesSandbox verifies that a per-session agent
// override flows from POST /api/sessions all the way to the sandbox env.
func TestE2E_AgentOverrideReachesSandbox(t *testing.T) {
	h := setupE2E(t, engine.Config{})

	w := h.do("POST", "/api/sessions", `{"repo":"myorg/myapp","prompt":"fix bug","agent":"claude-code"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created struct{ ID string }
	json.NewDecoder(w.Body).Decode(&created)

	sess := h.waitForSession(t, created.ID, 10*time.Second)
	if sess.Status != model.StatusComplete {
		t.Fatalf("session failed: %s", sess.Error)
	}

	// Verify agent field persisted.
	w = h.do("GET", "/api/sessions/"+created.ID, "")
	var gotSess model.Session
	json.NewDecoder(w.Body).Decode(&gotSess)
	if gotSess.Agent != "claude-code" {
		t.Fatalf("expected agent 'claude-code' in session, got %q", gotSess.Agent)
	}

	// Verify sandbox received the env var.
	starts := h.sb.getStarts()
	found := false
	for _, s := range starts {
		for _, env := range s.Env {
			if env == "TELECODER_CODING_AGENT=claude-code" {
				found = true
			}
		}
	}
	if !found {
		envs := []string{}
		for _, s := range starts {
			envs = append(envs, fmt.Sprintf("%v", s.Env))
		}
		t.Fatalf("TELECODER_CODING_AGENT=claude-code not found in sandbox envs: %v", envs)
	}
	t.Log("Agent override claude-code reached sandbox env")
}

// TestE2E_SessionNotFound verifies 404 for non-existent sessions.
func TestE2E_SessionNotFound(t *testing.T) {
	h := setupE2E(t, engine.Config{})

	w := h.do("GET", "/api/sessions/nonexistent", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestE2E_HealthCheck verifies the /health endpoint.
func TestE2E_HealthCheck(t *testing.T) {
	h := setupE2E(t, engine.Config{})

	w := h.do("GET", "/health", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Fatalf("expected 'ok', got %q", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Multi-step sandbox: supports persistent container mode for multi-step tasks
// ---------------------------------------------------------------------------

type multiStepSimSandbox struct {
	mu        sync.Mutex
	starts    []sandbox.StartOptions
	stopCalls int
}

func (s *multiStepSimSandbox) Start(_ context.Context, opts sandbox.StartOptions) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts = append(s.starts, opts)
	return fmt.Sprintf("msim-%s-%d", opts.SessionID, len(s.starts)), nil
}

func (s *multiStepSimSandbox) Stop(_ context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopCalls++
	return nil
}

func (s *multiStepSimSandbox) Wait(_ context.Context, _ string) (int, error) { return 0, nil }

func (s *multiStepSimSandbox) StreamLogs(_ context.Context, _ string) (sandbox.LineScanner, error) {
	return &lineSliceScanner{lines: []string{"single-task log"}}, nil
}

func (s *multiStepSimSandbox) Exec(_ context.Context, _ string, cmd []string) (sandbox.LineScanner, error) {
	// For setup.sh, return status lines.
	if len(cmd) > 0 && cmd[0] == "/setup.sh" {
		return &lineSliceScanner{lines: []string{
			"###TELECODER_STATUS### Cloning repo...",
			"###TELECODER_STATUS### Dependencies installed",
			"###TELECODER_STATUS### Ready",
		}}, nil
	}
	// For agent commands, return agent output.
	return &lineSliceScanner{lines: []string{"Agent completed step."}}, nil
}

func (s *multiStepSimSandbox) ExecCollect(_ context.Context, _ string, cmd []string) (string, error) {
	joined := strings.Join(cmd, " ")

	// Simulate git diff --cached --quiet: return error to indicate changes exist.
	if strings.Contains(joined, "git diff --cached --quiet") {
		return "", fmt.Errorf("exit status 1")
	}

	// Simulate git rev-parse HEAD.
	if strings.Contains(joined, "git rev-parse HEAD") {
		return "e2eabc123\n", nil
	}

	// Simulate file-not-found for test detection probes.
	if len(cmd) > 0 && cmd[0] == "test" {
		return "", fmt.Errorf("file not found")
	}

	// Simulate git push.
	if strings.Contains(joined, "git push") {
		return "pushed\n", nil
	}

	return "", nil
}

func (s *multiStepSimSandbox) CommitAndPush(_ context.Context, _, _, _ string) error { return nil }
func (s *multiStepSimSandbox) EnsureNetwork(_ context.Context, _ string) error      { return nil }
func (s *multiStepSimSandbox) IsRunning(_ context.Context, _ string) bool           { return true }

func (s *multiStepSimSandbox) getStarts() []sandbox.StartOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]sandbox.StartOptions, len(s.starts))
	copy(cp, s.starts)
	return cp
}



// Compile-time check that top-level types are referenced.
var _ = telecoder.Config{}
