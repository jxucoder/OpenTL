package pipeline

import (
	"fmt"
	"strings"

	"github.com/jxucoder/TeleCoder/pkg/llm"
)

// VerifyResult is the outcome of running tests/linting inside the sandbox.
type VerifyResult struct {
	Passed   bool
	Output   string
	Feedback string
}

// VerifyStage runs test and lint commands inside a sandbox and analyzes
// the results using an LLM. It is not invoked via the standard pipeline
// Run() flow — instead, the engine calls Verify() directly after a sandbox
// round completes (similar to how ReviewStage.Review works).
type VerifyStage struct {
	llm          llm.Client
	systemPrompt string
}

// NewVerifyStage creates a verify stage. Pass empty systemPrompt to use the default.
func NewVerifyStage(client llm.Client, systemPrompt string) *VerifyStage {
	if systemPrompt == "" {
		systemPrompt = DefaultVerifyPrompt
	}
	return &VerifyStage{llm: client, systemPrompt: systemPrompt}
}

func (s *VerifyStage) Name() string { return "verify" }

// Execute is a no-op — the engine calls Verify() directly.
func (s *VerifyStage) Execute(ctx *Context) error {
	return nil
}

// Verify analyzes test/lint output and returns a VerifyResult.
// If output is empty, it assumes tests passed.
func (s *VerifyStage) Verify(ctx *Context, testOutput string) (*VerifyResult, error) {
	if strings.TrimSpace(testOutput) == "" {
		return &VerifyResult{Passed: true, Output: "", Feedback: ""}, nil
	}

	user := fmt.Sprintf("## Task\n%s\n\n## Test / Lint Output\n```\n%s\n```", ctx.Prompt, testOutput)

	response, err := s.llm.Complete(ctx.Ctx, s.systemPrompt, user)
	if err != nil {
		return nil, fmt.Errorf("verify analysis: %w", err)
	}

	passed := strings.HasPrefix(strings.ToUpper(strings.TrimSpace(response)), "PASSED")

	return &VerifyResult{
		Passed:   passed,
		Output:   testOutput,
		Feedback: response,
	}, nil
}

// DetectVerifyCommands returns shell commands to run tests and linting based
// on which project files exist. The caller is expected to check file existence
// inside the sandbox via sandbox.ExecCollect before calling this.
func DetectVerifyCommands(existingFiles map[string]bool) []string {
	var cmds []string

	// Test commands.
	switch {
	case existingFiles["go.mod"]:
		cmds = append(cmds, "go test ./... 2>&1")
	case existingFiles["package.json"]:
		cmds = append(cmds, "npm test --if-present 2>&1")
	case existingFiles["Cargo.toml"]:
		cmds = append(cmds, "cargo test 2>&1")
	case existingFiles["requirements.txt"] || existingFiles["pyproject.toml"] || existingFiles["setup.py"]:
		cmds = append(cmds, "python -m pytest 2>&1 || python -m unittest discover 2>&1")
	case existingFiles["Makefile"]:
		cmds = append(cmds, "make test 2>&1")
	}

	// Lint commands.
	switch {
	case existingFiles["go.mod"]:
		cmds = append(cmds, "go vet ./... 2>&1")
	case existingFiles[".eslintrc.js"] || existingFiles[".eslintrc.json"] || existingFiles["eslint.config.js"] || existingFiles["eslint.config.mjs"]:
		cmds = append(cmds, "npx eslint . 2>&1")
	}

	return cmds
}
