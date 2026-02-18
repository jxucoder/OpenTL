package httpapi

import (
	"context"

	"github.com/jxucoder/TeleCoder/gitprovider"
	"github.com/jxucoder/TeleCoder/sandbox"
)

// stubLLM returns canned responses for pipeline stages.
type stubLLM struct{}

func (s *stubLLM) Complete(_ context.Context, _, _ string) (string, error) {
	return `[{"title":"Complete task","description":"do the thing"}]`, nil
}

// stubSandbox is a no-op sandbox runtime for testing HTTP handlers.
type stubSandbox struct{}

func (s *stubSandbox) Start(_ context.Context, _ sandbox.StartOptions) (string, error) {
	return "stub-container", nil
}
func (s *stubSandbox) Stop(_ context.Context, _ string) error { return nil }
func (s *stubSandbox) Wait(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (s *stubSandbox) StreamLogs(_ context.Context, _ string) (sandbox.LineScanner, error) {
	return &stubScanner{}, nil
}
func (s *stubSandbox) Exec(_ context.Context, _ string, _ []string) (sandbox.LineScanner, error) {
	return &stubScanner{}, nil
}
func (s *stubSandbox) ExecCollect(_ context.Context, _ string, _ []string) (string, error) {
	return "", nil
}
func (s *stubSandbox) CommitAndPush(_ context.Context, _, _, _ string) error { return nil }
func (s *stubSandbox) EnsureNetwork(_ context.Context, _ string) error      { return nil }
func (s *stubSandbox) IsRunning(_ context.Context, _ string) bool           { return true }

// stubScanner returns no lines.
type stubScanner struct{}

func (s *stubScanner) Scan() bool  { return false }
func (s *stubScanner) Text() string { return "" }
func (s *stubScanner) Err() error   { return nil }
func (s *stubScanner) Close() error { return nil }

// stubGitProvider is a no-op git provider for testing.
type stubGitProvider struct{}

func (s *stubGitProvider) CreatePR(_ context.Context, _ gitprovider.PROptions) (string, int, error) {
	return "https://github.com/test/repo/pull/1", 1, nil
}
func (s *stubGitProvider) GetDefaultBranch(_ context.Context, _ string) (string, error) {
	return "main", nil
}
func (s *stubGitProvider) IndexRepo(_ context.Context, _ string) (*gitprovider.RepoContext, error) {
	return &gitprovider.RepoContext{Tree: "README.md"}, nil
}
func (s *stubGitProvider) ReplyToPRComment(_ context.Context, _ string, _ int, _ string) error {
	return nil
}
