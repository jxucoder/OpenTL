// Package github provides GitHub API integration for PR creation.
package github

import (
	"context"
	"fmt"
	"strings"

	gogh "github.com/google/go-github/v68/github"
)

// Client wraps the GitHub API for OpenTL operations.
type Client struct {
	gh *gogh.Client
}

// NewClient creates a GitHub client authenticated with the given token.
func NewClient(token string) *Client {
	return &Client{
		gh: gogh.NewClient(nil).WithAuthToken(token),
	}
}

// PROptions configures a new pull request.
type PROptions struct {
	Repo   string // "owner/repo"
	Branch string // source branch
	Base   string // target branch (default: "main")
	Title  string
	Body   string
}

// CreatePR opens a pull request and returns the PR URL and number.
func (c *Client) CreatePR(ctx context.Context, opts PROptions) (string, int, error) {
	owner, repo, err := splitRepo(opts.Repo)
	if err != nil {
		return "", 0, err
	}

	base := opts.Base
	if base == "" {
		base = "main"
	}

	pr, _, err := c.gh.PullRequests.Create(ctx, owner, repo, &gogh.NewPullRequest{
		Title: gogh.Ptr(opts.Title),
		Body:  gogh.Ptr(opts.Body),
		Head:  gogh.Ptr(opts.Branch),
		Base:  gogh.Ptr(base),
	})
	if err != nil {
		return "", 0, fmt.Errorf("creating pull request: %w", err)
	}

	return pr.GetHTMLURL(), pr.GetNumber(), nil
}

// GetDefaultBranch returns the default branch for a repository.
func (c *Client) GetDefaultBranch(ctx context.Context, repoFullName string) (string, error) {
	owner, repo, err := splitRepo(repoFullName)
	if err != nil {
		return "", err
	}

	r, _, err := c.gh.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", fmt.Errorf("getting repository: %w", err)
	}

	return r.GetDefaultBranch(), nil
}

// PRComment represents a comment on a pull request.
type PRComment struct {
	ID     int64  // GitHub comment ID
	Body   string // comment text
	User   string // GitHub login of the commenter
	Path   string // file path (only for review comments, empty for issue comments)
	Line   int    // line number (only for review comments)
	InReplyTo int64 // parent comment ID for threaded replies
}

// ListPRComments returns all issue comments on a pull request.
func (c *Client) ListPRComments(ctx context.Context, repoFullName string, prNumber int) ([]PRComment, error) {
	owner, repo, err := splitRepo(repoFullName)
	if err != nil {
		return nil, err
	}

	ghComments, _, err := c.gh.Issues.ListComments(ctx, owner, repo, prNumber, &gogh.IssueListCommentsOptions{
		Sort:      gogh.Ptr("created"),
		Direction: gogh.Ptr("desc"),
		ListOptions: gogh.ListOptions{PerPage: 30},
	})
	if err != nil {
		return nil, fmt.Errorf("listing PR comments: %w", err)
	}

	var comments []PRComment
	for _, gc := range ghComments {
		comments = append(comments, PRComment{
			ID:   gc.GetID(),
			Body: gc.GetBody(),
			User: gc.GetUser().GetLogin(),
		})
	}
	return comments, nil
}

// ListPRReviewComments returns all review (inline) comments on a pull request.
func (c *Client) ListPRReviewComments(ctx context.Context, repoFullName string, prNumber int) ([]PRComment, error) {
	owner, repo, err := splitRepo(repoFullName)
	if err != nil {
		return nil, err
	}

	ghComments, _, err := c.gh.PullRequests.ListComments(ctx, owner, repo, prNumber, &gogh.PullRequestListCommentsOptions{
		Sort:      "created",
		Direction: "desc",
		ListOptions: gogh.ListOptions{PerPage: 30},
	})
	if err != nil {
		return nil, fmt.Errorf("listing PR review comments: %w", err)
	}

	var comments []PRComment
	for _, gc := range ghComments {
		comments = append(comments, PRComment{
			ID:        gc.GetID(),
			Body:      gc.GetBody(),
			User:      gc.GetUser().GetLogin(),
			Path:      gc.GetPath(),
			Line:      gc.GetLine(),
			InReplyTo: gc.GetInReplyTo(),
		})
	}
	return comments, nil
}

// ReplyToPRComment posts a reply as an issue comment on the pull request.
func (c *Client) ReplyToPRComment(ctx context.Context, repoFullName string, prNumber int, body string) error {
	owner, repo, err := splitRepo(repoFullName)
	if err != nil {
		return err
	}

	_, _, err = c.gh.Issues.CreateComment(ctx, owner, repo, prNumber, &gogh.IssueComment{
		Body: gogh.Ptr(body),
	})
	if err != nil {
		return fmt.Errorf("posting PR comment: %w", err)
	}
	return nil
}

// GetPRDiff returns the diff of a pull request.
func (c *Client) GetPRDiff(ctx context.Context, repoFullName string, prNumber int) (string, error) {
	owner, repo, err := splitRepo(repoFullName)
	if err != nil {
		return "", err
	}

	diff, _, err := c.gh.PullRequests.GetRaw(ctx, owner, repo, prNumber, gogh.RawOptions{
		Type: gogh.Diff,
	})
	if err != nil {
		return "", fmt.Errorf("getting PR diff: %w", err)
	}
	return diff, nil
}

func splitRepo(fullName string) (owner, repo string, err error) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format %q, expected \"owner/repo\"", fullName)
	}
	return parts[0], parts[1], nil
}
