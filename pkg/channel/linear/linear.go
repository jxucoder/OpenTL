// Package linear provides a Linear webhook channel for TeleCoder.
//
// When a Linear issue is labeled with a trigger label (default: "telecoder"),
// TeleCoder creates a session from the issue title+description and posts the
// result (PR link or text answer) back as a comment on the issue.
//
// Setup:
//  1. Create a Linear webhook pointing at <server>/api/webhooks/linear
//  2. Select "Issues" events
//  3. Set LINEAR_API_KEY and LINEAR_WEBHOOK_SECRET in your environment
//  4. Optionally set LINEAR_TRIGGER_LABEL (default: "telecoder") and LINEAR_DEFAULT_REPO
package linear

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

// Channel is a webhook-based Linear channel for TeleCoder.
type Channel struct {
	apiKey       string
	secret       string
	triggerLabel string
	defaultRepo  string
	store        store.SessionStore
	bus          eventbus.Bus
	sessions     SessionCreator
	srv          *http.Server
	addr         string
}

// Option configures the Linear channel.
type Option func(*Channel)

// WithAddr sets the listen address for the webhook server (default ":7090").
func WithAddr(addr string) Option {
	return func(c *Channel) { c.addr = addr }
}

// New creates a new Linear webhook channel.
func New(apiKey, secret, triggerLabel, defaultRepo string, st store.SessionStore, bus eventbus.Bus, creator SessionCreator, opts ...Option) *Channel {
	if triggerLabel == "" {
		triggerLabel = "telecoder"
	}
	c := &Channel{
		apiKey:       apiKey,
		secret:       secret,
		triggerLabel: strings.ToLower(triggerLabel),
		defaultRepo:  defaultRepo,
		store:        st,
		bus:          bus,
		sessions:     creator,
		addr:         ":7090",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name returns the channel name.
func (c *Channel) Name() string { return "linear" }

// Run starts the webhook HTTP server. Blocks until ctx is done.
func (c *Channel) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/webhooks/linear", c.handleWebhook)

	c.srv = &http.Server{Addr: c.addr, Handler: mux}

	go func() {
		<-ctx.Done()
		c.srv.Close()
	}()

	log.Printf("Linear webhook listening on %s", c.addr)
	if err := c.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// --- Webhook handling ---

// linearWebhookPayload is the subset of Linear webhook fields we care about.
type linearWebhookPayload struct {
	Action string      `json:"action"` // "create", "update", "remove"
	Type   string      `json:"type"`   // "Issue", "Comment", etc.
	Data   linearIssue `json:"data"`
}

type linearIssue struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Labels      []linearLabel `json:"labels"`
	Team        linearTeam    `json:"team"`
}

type linearLabel struct {
	Name string `json:"name"`
}

type linearTeam struct {
	Key string `json:"key"`
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

	var payload linearWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if payload.Type != "Issue" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if !c.hasTriggerLabel(payload.Data.Labels) {
		w.WriteHeader(http.StatusOK)
		return
	}

	go c.processIssue(payload.Data)
	w.WriteHeader(http.StatusAccepted)
}

func (c *Channel) verifySignature(r *http.Request, body []byte) bool {
	sig := r.Header.Get("Linear-Signature")
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

func (c *Channel) hasTriggerLabel(labels []linearLabel) bool {
	for _, l := range labels {
		if strings.ToLower(l.Name) == c.triggerLabel {
			return true
		}
	}
	return false
}

func (c *Channel) processIssue(issue linearIssue) {
	prompt := issue.Title
	if issue.Description != "" {
		prompt += "\n\n" + issue.Description
	}

	prompt, repo := model.ParseRepoFlag(prompt, c.defaultRepo)
	if repo == "" {
		log.Printf("Linear: no repo for issue %s (set LINEAR_DEFAULT_REPO or use --repo in description)", issue.ID)
		c.postComment(issue.ID, "Could not determine repository. Add `--repo owner/repo` to the issue description or set LINEAR_DEFAULT_REPO.")
		return
	}

	c.postComment(issue.ID, fmt.Sprintf("Starting TeleCoder session for `%s`...", repo))

	sess, err := c.sessions.CreateAndRunSession(repo, prompt)
	if err != nil {
		log.Printf("Linear: failed to create session for issue %s: %v", issue.ID, err)
		c.postComment(issue.ID, fmt.Sprintf("Failed to start session: %s", err))
		return
	}

	c.monitorSession(sess, issue.ID)
}

func (c *Channel) monitorSession(sess *model.Session, issueID string) {
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
	c.postComment(issueID, msg)
}

// postComment posts a comment on a Linear issue via the GraphQL API.
func (c *Channel) postComment(issueID, body string) {
	query := `mutation($issueID: String!, $body: String!) {
		commentCreate(input: { issueId: $issueID, body: $body }) {
			success
		}
	}`

	payload := map[string]any{
		"query": query,
		"variables": map[string]string{
			"issueID": issueID,
			"body":    body,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Linear: failed to marshal comment payload: %v", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.linear.app/graphql", bytes.NewReader(data))
	if err != nil {
		log.Printf("Linear: failed to create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Linear: failed to post comment on issue %s: %v", issueID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		log.Printf("Linear: comment API returned %d: %s", resp.StatusCode, respBody)
	}
}
