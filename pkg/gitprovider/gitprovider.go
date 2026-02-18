// Package gitprovider defines the git hosting provider interface for TeleCoder.
package gitprovider

import "context"

// PROptions configures a new pull request.
type PROptions struct {
	Repo   string // "owner/repo"
	Branch string // source branch
	Base   string // target branch (default: "main")
	Title  string
	Body   string
}

// PRComment represents a comment on a pull request.
type PRComment struct {
	ID        int64
	Body      string
	User      string
	Path      string // file path (only for review comments)
	Line      int    // line number (only for review comments)
	InReplyTo int64
}

// RepoContext holds the structural summary of a repository.
type RepoContext struct {
	Description string
	Tree        string
	Languages   map[string]int
	KeyFiles    map[string]string
}

// WebhookEvent represents a parsed webhook event relevant to PR comments.
type WebhookEvent struct {
	Action      string
	Repo        string
	PRNumber    int
	CommentBody string
	CommentUser string
	CommentID   int64
}

// Provider is the interface for git hosting operations.
type Provider interface {
	CreatePR(ctx context.Context, opts PROptions) (url string, number int, err error)
	GetDefaultBranch(ctx context.Context, repo string) (string, error)
	IndexRepo(ctx context.Context, repo string) (*RepoContext, error)
	ReplyToPRComment(ctx context.Context, repo string, prNumber int, body string) error
}
