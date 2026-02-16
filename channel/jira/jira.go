// Package jira provides a Jira webhook channel for TeleCoder.
//
// When a Jira issue is labeled with a trigger label (default: "telecoder"),
// TeleCoder creates a session from the issue summary+description and posts
// the result (PR link or text answer) back as a comment on the issue.
//
// Setup:
//  1. Create a Jira webhook pointing at <server>/api/webhooks/jira
//  2. Select "issue updated" events (or use automation to fire on label add)
//  3. Set JIRA_BASE_URL, JIRA_USER_EMAIL, and JIRA_API_TOKEN in your environment
//  4. Optionally set JIRA_TRIGGER_LABEL (default: "telecoder"),
//     JIRA_WEBHOOK_SECRET, and JIRA_DEFAULT_REPO
package jira

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

	"github.com/jxucoder/TeleCoder/eventbus"
	"github.com/jxucoder/TeleCoder/model"
	"github.com/jxucoder/TeleCoder/store"
)

// SessionCreator is the interface the engine implements for creating sessions.
type SessionCreator interface {
	CreateAndRunSession(repo, prompt string) (*model.Session, error)
}

// Channel is a webhook-based Jira channel for TeleCoder.
type Channel struct {
	baseURL      string // e.g. "https://yourcompany.atlassian.net"
	userEmail    string
	apiToken     string
	secret       string
	triggerLabel string
	defaultRepo  string
	store        store.SessionStore
	bus          eventbus.Bus
	sessions     SessionCreator
	srv          *http.Server
	addr         string
}

// Option configures the Jira channel.
type Option func(*Channel)

// WithAddr sets the listen address for the webhook server (default ":7091").
func WithAddr(addr string) Option {
	return func(c *Channel) { c.addr = addr }
}

// New creates a new Jira webhook channel.
func New(baseURL, userEmail, apiToken, secret, triggerLabel, defaultRepo string, st store.SessionStore, bus eventbus.Bus, creator SessionCreator, opts ...Option) *Channel {
	if triggerLabel == "" {
		triggerLabel = "telecoder"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	c := &Channel{
		baseURL:      baseURL,
		userEmail:    userEmail,
		apiToken:     apiToken,
		secret:       secret,
		triggerLabel: strings.ToLower(triggerLabel),
		defaultRepo:  defaultRepo,
		store:        st,
		bus:          bus,
		sessions:     creator,
		addr:         ":7091",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name returns the channel name.
func (c *Channel) Name() string { return "jira" }

// Run starts the webhook HTTP server. Blocks until ctx is done.
func (c *Channel) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/webhooks/jira", c.handleWebhook)

	c.srv = &http.Server{Addr: c.addr, Handler: mux}

	go func() {
		<-ctx.Done()
		c.srv.Close()
	}()

	log.Printf("Jira webhook listening on %s", c.addr)
	if err := c.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// --- Webhook handling ---

// jiraWebhookPayload is the subset of Jira webhook fields we use.
type jiraWebhookPayload struct {
	WebhookEvent string    `json:"webhookEvent"` // "jira:issue_updated", "jira:issue_created"
	Issue        jiraIssue `json:"issue"`
}

type jiraIssue struct {
	ID     string          `json:"id"`
	Key    string          `json:"key"` // e.g. "PROJ-123"
	Fields jiraIssueFields `json:"fields"`
}

type jiraIssueFields struct {
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Labels      []string     `json:"labels"`
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

	var payload jiraWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if !c.hasTriggerLabel(payload.Issue.Fields.Labels) {
		w.WriteHeader(http.StatusOK)
		return
	}

	go c.processIssue(payload.Issue)
	w.WriteHeader(http.StatusAccepted)
}

func (c *Channel) verifySignature(r *http.Request, body []byte) bool {
	sig := r.Header.Get("X-Hub-Signature")
	if sig == "" {
		return false
	}
	// Jira webhooks with secret use HMAC-SHA256 in "sha256=<hex>" format.
	sig = strings.TrimPrefix(sig, "sha256=")
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

func (c *Channel) hasTriggerLabel(labels []string) bool {
	for _, l := range labels {
		if strings.ToLower(l) == c.triggerLabel {
			return true
		}
	}
	return false
}

func (c *Channel) processIssue(issue jiraIssue) {
	prompt := issue.Fields.Summary
	if issue.Fields.Description != "" {
		prompt += "\n\n" + issue.Fields.Description
	}

	prompt, repo := model.ParseRepoFlag(prompt, c.defaultRepo)
	if repo == "" {
		log.Printf("Jira: no repo for issue %s (set JIRA_DEFAULT_REPO or use --repo in description)", issue.Key)
		c.postComment(issue.ID, "Could not determine repository. Add `--repo owner/repo` to the issue description or set JIRA_DEFAULT_REPO.")
		return
	}

	c.postComment(issue.ID, fmt.Sprintf("Starting TeleCoder session for %s...", repo))

	sess, err := c.sessions.CreateAndRunSession(repo, prompt)
	if err != nil {
		log.Printf("Jira: failed to create session for issue %s: %v", issue.Key, err)
		c.postComment(issue.ID, fmt.Sprintf("Failed to start session: %s", err))
		return
	}

	c.monitorSession(sess, issue.ID, issue.Key)
}

func (c *Channel) monitorSession(sess *model.Session, issueID, issueKey string) {
	ch := c.bus.Subscribe(sess.ID)
	defer c.bus.Unsubscribe(sess.ID, ch)

	for event := range ch {
		switch event.Type {
		case "error":
			c.postComment(issueID, fmt.Sprintf("Error: %s", event.Data))
			return
		case "done":
			updated, err := c.store.GetSession(sess.ID)
			if err != nil {
				c.postComment(issueID, "Session complete.")
				return
			}
			c.postResult(issueID, updated)
			return
		}
	}
}

func (c *Channel) postResult(issueID string, sess *model.Session) {
	var msg string
	switch {
	case sess.PRUrl != "":
		msg = fmt.Sprintf("PR ready: [#%d|%s]\n\nSession %s | Branch %s",
			sess.PRNumber, sess.PRUrl, sess.ID, sess.Branch)
	case sess.Result.Type == model.ResultText && sess.Result.Content != "":
		content := sess.Result.Content
		if len(content) > 2000 {
			content = content[:2000] + "\n...(truncated)"
		}
		msg = fmt.Sprintf("Result:\n\n%s\n\nSession %s", content, sess.ID)
	default:
		msg = fmt.Sprintf("Session %s complete (no PR created).", sess.ID)
	}
	c.postComment(issueID, msg)
}

// postComment adds a comment on a Jira issue via the REST API v3.
func (c *Channel) postComment(issueID, body string) {
	// Jira Cloud REST API v3 uses Atlassian Document Format (ADF).
	payload := map[string]any{
		"body": map[string]any{
			"version": 1,
			"type":    "doc",
			"content": []any{
				map[string]any{
					"type": "paragraph",
					"content": []any{
						map[string]any{
							"type": "text",
							"text": body,
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Jira: failed to marshal comment payload: %v", err)
		return
	}

	url := fmt.Sprintf("%s/rest/api/3/issue/%s/comment", c.baseURL, issueID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		log.Printf("Jira: failed to create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.userEmail, c.apiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Jira: failed to post comment on issue %s: %v", issueID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		log.Printf("Jira: comment API returned %d: %s", resp.StatusCode, respBody)
	}
}
