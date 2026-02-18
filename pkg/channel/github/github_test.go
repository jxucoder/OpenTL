package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jxucoder/TeleCoder/pkg/model"
)

// stubSessionCreator records calls to CreateAndRunSession.
type stubSessionCreator struct {
	called bool
	repo   string
	prompt string
}

func (s *stubSessionCreator) CreateAndRunSession(repo, prompt string) (*model.Session, error) {
	s.called = true
	s.repo = repo
	s.prompt = prompt
	return &model.Session{ID: "test-session"}, nil
}

// stubStore and stubBus satisfy the Channel constructor but are not exercised.
type stubStore struct{}

func (stubStore) CreateSession(s *model.Session) error                          { return nil }
func (stubStore) GetSession(id string) (*model.Session, error)                  { return nil, nil }
func (stubStore) UpdateSession(s *model.Session) error                          { return nil }
func (stubStore) ListSessions() ([]*model.Session, error)                       { return nil, nil }
func (stubStore) GetSessionByPR(repo string, prNumber int) (*model.Session, error) { return nil, nil }
func (stubStore) AddEvent(event *model.Event) error                             { return nil }
func (stubStore) GetEvents(sessionID string, afterID int64) ([]*model.Event, error) { return nil, nil }
func (stubStore) AddMessage(msg *model.Message) error                           { return nil }
func (stubStore) GetMessages(sessionID string) ([]*model.Message, error)        { return nil, nil }
func (stubStore) Close() error                                                  { return nil }

type stubBus struct{}

func (stubBus) Publish(sessionID string, event *model.Event)          {}
func (stubBus) Subscribe(sessionID string) chan *model.Event          { return make(chan *model.Event) }
func (stubBus) Unsubscribe(sessionID string, ch chan *model.Event)    {}

func makePayload(t *testing.T, action string, labels []string, labeledName string) []byte {
	t.Helper()
	var ghLabels []ghLabel
	for _, l := range labels {
		ghLabels = append(ghLabels, ghLabel{Name: l})
	}
	p := ghIssuesPayload{
		Action: action,
		Issue: ghIssue{
			Number: 42,
			Title:  "Test issue",
			Body:   "some body",
			Labels: ghLabels,
		},
		Repository: ghRepo{FullName: "owner/repo"},
	}
	if labeledName != "" {
		p.Label = &ghLabel{Name: labeledName}
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func signPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookSignatureVerification(t *testing.T) {
	creator := &stubSessionCreator{}
	ch := New("token", "my-secret", "telecoder", stubStore{}, stubBus{}, creator)

	body := makePayload(t, "opened", []string{"telecoder"}, "")

	// Missing signature should fail.
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing sig: got %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// Wrong signature should fail.
	req = httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	w = httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong sig: got %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// Correct signature should succeed.
	req = httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", signPayload("my-secret", body))
	w = httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("correct sig: got %d, want %d", w.Code, http.StatusAccepted)
	}
}

func TestWebhookLabelFiltering(t *testing.T) {
	creator := &stubSessionCreator{}
	// No secret so signature check is skipped.
	ch := New("token", "", "telecoder", stubStore{}, stubBus{}, creator)

	// Issue opened without trigger label → 200, no session created.
	body := makePayload(t, "opened", []string{"bug"}, "")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("no label: got %d, want %d", w.Code, http.StatusOK)
	}

	// Issue opened with trigger label → 202.
	body = makePayload(t, "opened", []string{"telecoder"}, "")
	req = httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	w = httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("with label: got %d, want %d", w.Code, http.StatusAccepted)
	}
}

func TestWebhookLabeledAction(t *testing.T) {
	creator := &stubSessionCreator{}
	ch := New("token", "", "telecoder", stubStore{}, stubBus{}, creator)

	// "labeled" with a non-trigger label → 200.
	body := makePayload(t, "labeled", []string{"bug", "telecoder"}, "bug")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("labeled non-trigger: got %d, want %d", w.Code, http.StatusOK)
	}

	// "labeled" with the trigger label → 202.
	body = makePayload(t, "labeled", []string{"bug", "telecoder"}, "telecoder")
	req = httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	w = httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("labeled trigger: got %d, want %d", w.Code, http.StatusAccepted)
	}
}

func TestWebhookActionFiltering(t *testing.T) {
	creator := &stubSessionCreator{}
	ch := New("token", "", "telecoder", stubStore{}, stubBus{}, creator)

	// "edited" action → 200 (ignored).
	body := makePayload(t, "edited", []string{"telecoder"}, "")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("edited action: got %d, want %d", w.Code, http.StatusOK)
	}

	// "closed" action → 200 (ignored).
	body = makePayload(t, "closed", []string{"telecoder"}, "")
	req = httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	w = httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("closed action: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestWebhookNonIssueEvent(t *testing.T) {
	creator := &stubSessionCreator{}
	ch := New("token", "", "telecoder", stubStore{}, stubBus{}, creator)

	body := makePayload(t, "opened", []string{"telecoder"}, "")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "push")
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("push event: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestWebhookMethodNotAllowed(t *testing.T) {
	creator := &stubSessionCreator{}
	ch := New("token", "", "telecoder", stubStore{}, stubBus{}, creator)

	req := httptest.NewRequest(http.MethodGet, "/api/webhooks/github-issues", nil)
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestNoSecretSkipsVerification(t *testing.T) {
	creator := &stubSessionCreator{}
	ch := New("token", "", "telecoder", stubStore{}, stubBus{}, creator)

	body := makePayload(t, "opened", []string{"telecoder"}, "")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github-issues", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	// No signature header — should still succeed since secret is empty.
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("no secret: got %d, want %d", w.Code, http.StatusAccepted)
	}
}
