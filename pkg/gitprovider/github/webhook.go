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

	"github.com/jxucoder/TeleCoder/pkg/gitprovider"
)

// ParseWebhook parses a GitHub webhook request into a WebhookEvent.
// If secret is non-empty, the request signature is verified.
// Returns nil if the event is not a PR comment we care about.
func ParseWebhook(r *http.Request, secret string) (*gitprovider.WebhookEvent, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

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
	case "pull_request_review":
		return parseReview(body)
	default:
		return nil, nil
	}
}

func parseIssueComment(body []byte) (*gitprovider.WebhookEvent, error) {
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

	if payload.Issue.PullRequest == nil {
		return nil, nil
	}
	if payload.Action != "created" {
		return nil, nil
	}

	return &gitprovider.WebhookEvent{
		Action:      payload.Action,
		Repo:        payload.Repository.FullName,
		PRNumber:    payload.Issue.Number,
		CommentBody: payload.Comment.Body,
		CommentUser: payload.Comment.User.Login,
		CommentID:   payload.Comment.ID,
	}, nil
}

func parseReviewComment(body []byte) (*gitprovider.WebhookEvent, error) {
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

	if payload.Action != "created" {
		return nil, nil
	}

	return &gitprovider.WebhookEvent{
		Action:      payload.Action,
		Repo:        payload.Repository.FullName,
		PRNumber:    payload.PullRequest.Number,
		CommentBody: payload.Comment.Body,
		CommentUser: payload.Comment.User.Login,
		CommentID:   payload.Comment.ID,
	}, nil
}

func parseReview(body []byte) (*gitprovider.WebhookEvent, error) {
	var payload struct {
		Action string `json:"action"`
		Review struct {
			ID    int64  `json:"id"`
			Body  string `json:"body"`
			State string `json:"state"`
			User  struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"review"`
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing pull_request_review payload: %w", err)
	}

	if payload.Action != "submitted" {
		return nil, nil
	}

	switch payload.Review.State {
	case "changes_requested":
	case "commented":
		if strings.TrimSpace(payload.Review.Body) == "" {
			return nil, nil
		}
	default:
		return nil, nil
	}

	return &gitprovider.WebhookEvent{
		Action:      payload.Action,
		Repo:        payload.Repository.FullName,
		PRNumber:    payload.PullRequest.Number,
		CommentBody: payload.Review.Body,
		CommentUser: payload.Review.User.Login,
		CommentID:   payload.Review.ID,
	}, nil
}

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
