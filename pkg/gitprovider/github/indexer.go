package github

import (
	"context"
	"fmt"
	"sort"
	"strings"

	gogh "github.com/google/go-github/v68/github"

	"github.com/jxucoder/TeleCoder/gitprovider"
)

var keyFileNames = map[string]bool{
	"README.md":          true,
	"package.json":       true,
	"go.mod":             true,
	"pyproject.toml":     true,
	"Cargo.toml":         true,
	"Makefile":           true,
	"Dockerfile":         true,
	"docker-compose.yml": true,
	"compose.yml":        true,
	"requirements.txt":   true,
	"tsconfig.json":      true,
}

const maxTreeDepth = 3
const maxKeyFileLines = 100

// FormatRepoContext formats a RepoContext as a single block of text suitable
// for injection into an LLM prompt.
func FormatRepoContext(rc *gitprovider.RepoContext) string {
	var b strings.Builder

	if rc.Description != "" {
		fmt.Fprintf(&b, "### Description\n%s\n\n", rc.Description)
	}

	if len(rc.Languages) > 0 {
		fmt.Fprintf(&b, "### Languages\n")
		type langPct struct {
			name string
			pct  int
		}
		var langs []langPct
		for name, pct := range rc.Languages {
			langs = append(langs, langPct{name, pct})
		}
		sort.Slice(langs, func(i, j int) bool { return langs[i].pct > langs[j].pct })
		for _, l := range langs {
			fmt.Fprintf(&b, "- %s: %d%%\n", l.name, l.pct)
		}
		b.WriteString("\n")
	}

	if rc.Tree != "" {
		fmt.Fprintf(&b, "### File Tree (top %d levels)\n```\n%s\n```\n\n", maxTreeDepth, rc.Tree)
	}

	if len(rc.KeyFiles) > 0 {
		fmt.Fprintf(&b, "### Key Files\n")
		for name, content := range rc.KeyFiles {
			fmt.Fprintf(&b, "\n**%s**\n```\n%s\n```\n", name, content)
		}
	}

	return b.String()
}

// IndexRepo fetches repository metadata, file tree, and key files from the
// GitHub API and returns a structured RepoContext.
func (c *Client) IndexRepo(ctx context.Context, repo string) (*gitprovider.RepoContext, error) {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	rc := &gitprovider.RepoContext{
		Languages: make(map[string]int),
		KeyFiles:  make(map[string]string),
	}

	repoInfo, _, err := c.gh.Repositories.Get(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("fetching repo info: %w", err)
	}
	rc.Description = repoInfo.GetDescription()
	defaultBranch := repoInfo.GetDefaultBranch()
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	languages, _, err := c.gh.Repositories.ListLanguages(ctx, owner, repoName)
	if err == nil && len(languages) > 0 {
		var total int
		for _, bytes := range languages {
			total += bytes
		}
		if total > 0 {
			for lang, bytes := range languages {
				rc.Languages[lang] = (bytes * 100) / total
			}
		}
	}

	tree, _, err := c.gh.Git.GetTree(ctx, owner, repoName, defaultBranch, true)
	if err != nil {
		return nil, fmt.Errorf("fetching file tree: %w", err)
	}

	rc.Tree = buildTreeString(tree.Entries)

	for _, entry := range tree.Entries {
		path := entry.GetPath()
		baseName := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			baseName = path[idx+1:]
		}
		if !strings.Contains(path, "/") && keyFileNames[baseName] {
			content, err := fetchFileContent(ctx, c.gh, owner, repoName, path, defaultBranch)
			if err == nil && content != "" {
				rc.KeyFiles[path] = content
			}
		}
	}

	return rc, nil
}

func buildTreeString(entries []*gogh.TreeEntry) string {
	var lines []string
	for _, e := range entries {
		path := e.GetPath()
		depth := strings.Count(path, "/")
		if depth >= maxTreeDepth {
			continue
		}

		indent := strings.Repeat("  ", depth)
		name := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			name = path[idx+1:]
		}

		if e.GetType() == "tree" {
			lines = append(lines, fmt.Sprintf("%s%s/", indent, name))
		} else {
			lines = append(lines, fmt.Sprintf("%s%s", indent, name))
		}
	}
	return strings.Join(lines, "\n")
}

func fetchFileContent(ctx context.Context, gh *gogh.Client, owner, repo, path, ref string) (string, error) {
	opts := &gogh.RepositoryContentGetOptions{Ref: ref}
	file, _, _, err := gh.Repositories.GetContents(ctx, owner, repo, path, opts)
	if err != nil {
		return "", err
	}
	if file == nil {
		return "", nil
	}

	content, err := file.GetContent()
	if err != nil {
		return "", fmt.Errorf("decoding content for %s: %w", path, err)
	}

	return truncateLines(content, maxKeyFileLines), nil
}

func truncateLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
		lines = append(lines, "... (truncated)")
	}
	return strings.Join(lines, "\n")
}
