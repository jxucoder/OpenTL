// Package github provides webhook event parsing for GitHub PR comments.
package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// WebhookEvent represents a parsed GitHub webhook event relevant to PR comments.
type WebhookEvent struct {
	// Action is the webhook action (e.g. "created", "edited").
	Action string

	// Repo is the full repository name ("owner/repo").
	Repo string

	// PRNumber is the pull request number.
	PRNumber int

	// CommentBody is the text of the comment.
	CommentBody string

	// CommentUser is the GitHub login of the commenter.
	CommentUser string

	// CommentID is the GitHub ID of the comment.
	CommentID int64
}

// ParseWebhook parses a GitHub webhook request into a WebhookEvent.
// It supports "issue_comment" events on pull requests and
// "pull_request_review_comment" events.
//
// If secret is non-empty, the request signature is verified.
// Returns nil if the event is not a PR comment we care about.
func ParseWebhook(r *http.Request, secret string) (*WebhookEvent, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	// Verify signature if a secret is configured.
	if secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if sig == "" {
			return nil, fmt.Errorf("missing webhook signature")
		}
		if !verifySignature(body, sig, secret) {
			return nil, fmt.Errorf("invalid webhook signature")
		}
	}

	eventType := r.Header.Get("X-GitHub-Event")

	switch eventType {
	case "issue_comment":
		return parseIssueComment(body)
	case "pull_request_review_comment":
		return parseReviewComment(body)
	default:
		// Not an event we handle.
		return nil, nil
	}
}

func parseIssueComment(body []byte) (*WebhookEvent, error) {
	var payload struct {
		Action string `json:"action"`
		Issue  struct {
			Number      int `json:"number"`
			PullRequest *struct {
				URL string `json:"url"`
			} `json:"pull_request"`
		} `json:"issue"`
		Comment struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"comment"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing issue_comment payload: %w", err)
	}

	// Only handle comments on pull requests (not plain issues).
	if payload.Issue.PullRequest == nil {
		return nil, nil
	}

	// Only handle newly created comments.
	if payload.Action != "created" {
		return nil, nil
	}

	return &WebhookEvent{
		Action:      payload.Action,
		Repo:        payload.Repository.FullName,
		PRNumber:    payload.Issue.Number,
		CommentBody: payload.Comment.Body,
		CommentUser: payload.Comment.User.Login,
		CommentID:   payload.Comment.ID,
	}, nil
}

func parseReviewComment(body []byte) (*WebhookEvent, error) {
	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
		Comment struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"comment"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing pull_request_review_comment payload: %w", err)
	}

	// Only handle newly created comments.
	if payload.Action != "created" {
		return nil, nil
	}

	return &WebhookEvent{
		Action:      payload.Action,
		Repo:        payload.Repository.FullName,
		PRNumber:    payload.PullRequest.Number,
		CommentBody: payload.Comment.Body,
		CommentUser: payload.Comment.User.Login,
		CommentID:   payload.Comment.ID,
	}, nil
}

// verifySignature checks the HMAC-SHA256 signature from GitHub.
func verifySignature(payload []byte, signature, secret string) bool {
	sig := strings.TrimPrefix(signature, "sha256=")
	decoded, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(decoded, expected)
}
