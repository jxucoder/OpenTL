package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jxucoder/TeleCoder/internal/engine"
	"github.com/jxucoder/TeleCoder/pkg/eventbus"
	"github.com/jxucoder/TeleCoder/pkg/model"
	sqliteStore "github.com/jxucoder/TeleCoder/pkg/store/sqlite"
)

func testEngine(t *testing.T) *engine.Engine {
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

	eng := engine.New(
		engine.Config{
			DockerImage:     "test-image",
			MaxRevisions:    1,
			ChatIdleTimeout: 30 * time.Minute,
			ChatMaxMessages: 50,
		},
		st, bus, sb, git,
	)
	return eng
}

func TestHealthEndpoint(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Fatalf("expected 'ok', got %q", w.Body.String())
	}
}

func TestCreateSessionMissingRepo(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"prompt":"fix bug"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateSessionInvalidRepo(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"repo":"noslash","prompt":"fix bug"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp errorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp.Error, "owner/repo") {
		t.Fatalf("expected owner/repo format error, got %q", resp.Error)
	}
}

func TestCreateSessionMissingPrompt(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"repo":"owner/repo"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateSessionInvalidMode(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"repo":"owner/repo","prompt":"fix bug","mode":"invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateTaskSession(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"repo":"owner/repo","prompt":"fix the bug"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp createSessionResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if resp.Mode != "task" {
		t.Fatalf("expected mode 'task', got %q", resp.Mode)
	}
	if !strings.HasPrefix(resp.Branch, "telecoder/") {
		t.Fatalf("expected branch starting with 'telecoder/', got %q", resp.Branch)
	}
}

func TestListSessionsEmpty(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sessions []*model.Session
	json.NewDecoder(w.Body).Decode(&sessions)
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestListSessionsAfterCreate(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"repo":"owner/repo","prompt":"add tests"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w = httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	var sessions []*model.Session
	json.NewDecoder(w.Body).Decode(&sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Repo != "owner/repo" {
		t.Fatalf("expected repo 'owner/repo', got %q", sessions[0].Repo)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/nonexistent", nil)
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetSession(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"repo":"owner/repo","prompt":"fix it"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	var created createSessionResponse
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodGet, "/api/sessions/"+created.ID, nil)
	w = httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sess model.Session
	json.NewDecoder(w.Body).Decode(&sess)
	if sess.ID != created.ID {
		t.Fatalf("expected session ID %q, got %q", created.ID, sess.ID)
	}
}

func TestGetMessagesEmpty(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"repo":"owner/repo","prompt":"fix it"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	var created createSessionResponse
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodGet, "/api/sessions/"+created.ID+"/messages", nil)
	w = httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var msgs []*model.Message
	json.NewDecoder(w.Body).Decode(&msgs)
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestSendMessageEmptyContent(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"repo":"owner/repo","prompt":"fix","mode":"chat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	var created createSessionResponse
	json.NewDecoder(w.Body).Decode(&created)

	msgBody := `{"content":""}`
	req = httptest.NewRequest(http.MethodPost, "/api/sessions/"+created.ID+"/messages", strings.NewReader(msgBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStopSessionNotFound(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/nonexistent/stop", nil)
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestStopSession(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	body := `{"repo":"owner/repo","prompt":"do something"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	var created createSessionResponse
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodPost, "/api/sessions/"+created.ID+"/stop", nil)
	w = httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sess model.Session
	json.NewDecoder(w.Body).Decode(&sess)
	if sess.Status != model.StatusError {
		t.Fatalf("expected status 'error', got %q", sess.Status)
	}
	if sess.Error != "stopped by user" {
		t.Fatalf("expected error 'stopped by user', got %q", sess.Error)
	}
}

func TestPromptTooLong(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	longPrompt := strings.Repeat("x", 10001)
	body := `{"repo":"owner/repo","prompt":"` + longPrompt + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateTaskSessionWithAgent(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	// Create a session with an explicit agent.
	body := `{"repo":"owner/repo","prompt":"fix the bug","agent":"claude-code"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created createSessionResponse
	json.NewDecoder(w.Body).Decode(&created)

	// Retrieve the session and verify the agent field persisted.
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/"+created.ID, nil)
	w = httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sess model.Session
	json.NewDecoder(w.Body).Decode(&sess)
	if sess.Agent != "claude-code" {
		t.Fatalf("expected agent 'claude-code', got %q", sess.Agent)
	}
}

func TestCreateTaskSessionWithoutAgent(t *testing.T) {
	eng := testEngine(t)
	h := New(eng)

	// Create a session without specifying an agent.
	body := `{"repo":"owner/repo","prompt":"fix the bug"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created createSessionResponse
	json.NewDecoder(w.Body).Decode(&created)

	// Retrieve the session — agent should be empty (default).
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/"+created.ID, nil)
	w = httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	var sess model.Session
	json.NewDecoder(w.Body).Decode(&sess)
	if sess.Agent != "" {
		t.Fatalf("expected empty agent, got %q", sess.Agent)
	}
}

func TestIsValidRepo(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"owner/repo", true},
		{"a/b", true},
		{"noslash", false},
		{"/repo", false},
		{"owner/", false},
		{"a/b/c", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isValidRepo(tt.input)
		if got != tt.want {
			t.Errorf("isValidRepo(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
