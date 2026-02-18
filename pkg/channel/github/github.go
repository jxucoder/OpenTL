// Package github provides a GitHub Issues webhook channel for TeleCoder.
//
// When a GitHub issue is opened or labeled with a trigger label (default:
// "telecoder"), TeleCoder creates a session from the issue title+body and
// posts the result (PR link or text answer) back as a comment on the issue.
//
// Setup:
//  1. Create a GitHub webhook pointing at <server>/api/webhooks/github-issues
//  2. Select "Issues" events
//  3. Set GITHUB_TOKEN and optionally GITHUB_WEBHOOK_SECRET in your environment
//  4. Optionally set GITHUB_ISSUES_TRIGGER_LABEL (default: "telecoder")
package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/jxucoder/TeleCoder/pkg/eventbus"
	"github.com/jxucoder/TeleCoder/pkg/model"
	"github.com/jxucoder/TeleCoder/pkg/store"
)

// SessionCreator is the interface the engine implements for creating sessions.
type SessionCreator interface {
	CreateAndRunSession(repo, prompt string) (*model.Session, error)
}

// Channel is a webhook-based GitHub Issues channel for TeleCoder.
type Channel struct {
	token        string // GitHub API token
	secret       string // webhook HMAC secret
	triggerLabel string
	store        store.SessionStore
	bus          eventbus.Bus
	sessions     SessionCreator
	srv          *http.Server
	addr         string
}

// Option configures the GitHub Issues channel.
type Option func(*Channel)

// WithAddr sets the listen address for the webhook server (default ":7092").
func WithAddr(addr string) Option {
	return func(c *Channel) { c.addr = addr }
}

// New creates a new GitHub Issues webhook channel.
func New(token, secret, triggerLabel string, st store.SessionStore, bus eventbus.Bus, creator SessionCreator, opts ...Option) *Channel {
	if triggerLabel == "" {
		triggerLabel = "telecoder"
	}
	c := &Channel{
		token:        token,
		secret:       secret,
		triggerLabel: strings.ToLower(triggerLabel),
		store:        st,
		bus:          bus,
		sessions:     creator,
		addr:         ":7092",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name returns the channel name.
func (c *Channel) Name() string { return "github-issues" }

// Run starts the webhook HTTP server. Blocks until ctx is done.
func (c *Channel) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/webhooks/github-issues", c.handleWebhook)

	c.srv = &http.Server{Addr: c.addr, Handler: mux}

	go func() {
		<-ctx.Done()
		c.srv.Close()
	}()

	log.Printf("GitHub Issues webhook listening on %s", c.addr)
	if err := c.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// --- Webhook handling ---

// ghIssuesPayload is the subset of GitHub issues webhook fields we use.
type ghIssuesPayload struct {
	Action string  `json:"action"` // "opened", "labeled", "edited", etc.
	Issue  ghIssue `json:"issue"`
	Label  *ghLabel `json:"label,omitempty"` // present when action is "labeled"
	Repository ghRepo `json:"repository"`
}

type ghIssue struct {
	Number int       `json:"number"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Labels []ghLabel `json:"labels"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghRepo struct {
	FullName string `json:"full_name"`
}

func (c *Channel) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if c.secret != "" && !c.verifySignature(r, body) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Only handle "issues" events.
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType != "issues" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload ghIssuesPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Only act on "opened" or "labeled" actions.
	switch payload.Action {
	case "opened":
		if !c.hasTriggerLabel(payload.Issue.Labels) {
			w.WriteHeader(http.StatusOK)
			return
		}
	case "labeled":
		// When labeled, check the specific label that was just added.
		if payload.Label == nil || strings.ToLower(payload.Label.Name) != c.triggerLabel {
			w.WriteHeader(http.StatusOK)
			return
		}
	default:
		w.WriteHeader(http.StatusOK)
		return
	}

	go c.processIssue(payload.Repository.FullName, payload.Issue)
	w.WriteHeader(http.StatusAccepted)
}

func (c *Channel) verifySignature(r *http.Request, body []byte) bool {
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		return false
	}
	sig = strings.TrimPrefix(sig, "sha256=")
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

func (c *Channel) hasTriggerLabel(labels []ghLabel) bool {
	for _, l := range labels {
		if strings.ToLower(l.Name) == c.triggerLabel {
			return true
		}
	}
	return false
}

func (c *Channel) processIssue(repo string, issue ghIssue) {
	prompt := issue.Title
	if issue.Body != "" {
		prompt += "\n\n" + issue.Body
	}

	// Allow --repo override in body, but default to the webhook's repo.
	prompt, repoOverride := model.ParseRepoFlag(prompt, repo)
	repo = repoOverride

	c.postComment(repo, issue.Number, fmt.Sprintf("Starting TeleCoder session for `%s`...", repo))

	sess, err := c.sessions.CreateAndRunSession(repo, prompt)
	if err != nil {
		log.Printf("GitHub Issues: failed to create session for %s#%d: %v", repo, issue.Number, err)
		c.postComment(repo, issue.Number, fmt.Sprintf("Failed to start session: %s", err))
		return
	}

	c.monitorSession(sess, repo, issue.Number)
}

func (c *Channel) monitorSession(sess *model.Session, repo string, issueNumber int) {
	ch := c.bus.Subscribe(sess.ID)
	defer c.bus.Unsubscribe(sess.ID, ch)

	for event := range ch {
		switch event.Type {
		case "error":
			c.postComment(repo, issueNumber, fmt.Sprintf("Error: %s", event.Data))
			return
		case "done":
			updated, err := c.store.GetSession(sess.ID)
			if err != nil {
				c.postComment(repo, issueNumber, "Session complete.")
				return
			}
			c.postResult(repo, issueNumber, updated)
			return
		}
	}
}

func (c *Channel) postResult(repo string, issueNumber int, sess *model.Session) {
	var msg string
	switch {
	case sess.PRUrl != "":
		msg = fmt.Sprintf("PR ready: [#%d](%s)\n\nSession `%s` | Branch `%s`",
			sess.PRNumber, sess.PRUrl, sess.ID, sess.Branch)
	case sess.Result.Type == model.ResultText && sess.Result.Content != "":
		content := sess.Result.Content
		if len(content) > 2000 {
			content = content[:2000] + "\n...(truncated)"
		}
		msg = fmt.Sprintf("Result:\n\n%s\n\nSession `%s`", content, sess.ID)
	default:
		msg = fmt.Sprintf("Session `%s` complete (no PR created).", sess.ID)
	}
	c.postComment(repo, issueNumber, msg)
}

// postComment posts a comment on a GitHub issue via the REST API.
func (c *Channel) postComment(repo string, issueNumber int, body string) {
	payload := map[string]string{"body": body}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("GitHub Issues: failed to marshal comment payload: %v", err)
		return
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repo, issueNumber)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		log.Printf("GitHub Issues: failed to create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("GitHub Issues: failed to post comment on %s#%d: %v", repo, issueNumber, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		log.Printf("GitHub Issues: comment API returned %d: %s", resp.StatusCode, respBody)
	}
}
