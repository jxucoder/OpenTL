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

func splitRepo(fullName string) (owner, repo string, err error) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format %q, expected \"owner/repo\"", fullName)
	}
	return parts[0], parts[1], nil
}
