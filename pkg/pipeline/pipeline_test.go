package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeLLM struct {
	response string
	err      error
}

func (f *fakeLLM) Complete(ctx context.Context, system, user string) (string, error) {
	return f.response, f.err
}

func TestPlanStage(t *testing.T) {
	stage := NewPlanStage(&fakeLLM{response: "Plan output"}, "")
	ctx := &Context{
		Ctx:     context.Background(),
		Repo:    "owner/repo",
		Prompt:  "fix bug",
		RepoCtx: "repo ctx",
	}
	if err := stage.Execute(ctx); err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if ctx.Plan != "Plan output" {
		t.Fatalf("unexpected plan: %s", ctx.Plan)
	}
}

func TestReviewApproved(t *testing.T) {
	stage := NewReviewStage(&fakeLLM{response: "APPROVED: looks good"}, "")
	res, err := stage.Review(context.Background(), "task", "plan", "diff")
	if err != nil {
		t.Fatalf("review error: %v", err)
	}
	if !res.Approved {
		t.Fatal("expected approved review")
	}
}

func TestReviewRevisionNeeded(t *testing.T) {
	stage := NewReviewStage(&fakeLLM{response: "REVISION NEEDED: add test"}, "")
	res, err := stage.Review(context.Background(), "task", "plan", "diff")
	if err != nil {
		t.Fatalf("review error: %v", err)
	}
	if res.Approved {
		t.Fatal("expected non-approved review")
	}
}

func TestEnrichAndRevisePrompt(t *testing.T) {
	enriched := EnrichPrompt("task", "plan")
	if !strings.Contains(enriched, "## Plan") {
		t.Fatalf("missing plan section: %s", enriched)
	}
	revised := RevisePrompt("task", "plan", "feedback")
	if !strings.Contains(revised, "Revision Instructions") {
		t.Fatalf("missing revision instructions: %s", revised)
	}
}

func TestDecomposeMultipleTasks(t *testing.T) {
	stage := NewDecomposeStage(&fakeLLM{response: `[{"title":"T1","description":"D1"},{"title":"T2","description":"D2"}]`}, "")
	ctx := &Context{Ctx: context.Background(), Prompt: "task"}
	if err := stage.Execute(ctx); err != nil {
		t.Fatalf("decompose error: %v", err)
	}
	if len(ctx.SubTasks) != 2 || ctx.SubTasks[0].Title != "T1" {
		t.Fatalf("unexpected tasks: %+v", ctx.SubTasks)
	}
}

func TestDecomposeFallsBackOnBadJSON(t *testing.T) {
	stage := NewDecomposeStage(&fakeLLM{response: "not json"}, "")
	ctx := &Context{Ctx: context.Background(), Prompt: "original prompt"}
	if err := stage.Execute(ctx); err != nil {
		t.Fatalf("decompose error: %v", err)
	}
	if len(ctx.SubTasks) != 1 || ctx.SubTasks[0].Description != "original prompt" {
		t.Fatalf("expected fallback task, got: %+v", ctx.SubTasks)
	}
}

func TestVerifyPassed(t *testing.T) {
	stage := NewVerifyStage(&fakeLLM{response: "PASSED: all tests pass"}, "")
	ctx := &Context{Ctx: context.Background(), Prompt: "fix bug"}
	res, err := stage.Verify(ctx, "ok  	github.com/foo/bar	0.012s")
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !res.Passed {
		t.Fatal("expected passed verify")
	}
}

func TestVerifyFailed(t *testing.T) {
	stage := NewVerifyStage(&fakeLLM{response: "FAILED: TestFoo assertion error"}, "")
	ctx := &Context{Ctx: context.Background(), Prompt: "fix bug"}
	res, err := stage.Verify(ctx, "--- FAIL: TestFoo (0.00s)")
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected failed verify")
	}
}

func TestVerifyEmptyOutput(t *testing.T) {
	stage := NewVerifyStage(&fakeLLM{response: "should not be called"}, "")
	ctx := &Context{Ctx: context.Background(), Prompt: "fix bug"}
	res, err := stage.Verify(ctx, "")
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !res.Passed {
		t.Fatal("expected passed for empty output")
	}
}

func TestDetectVerifyCommands(t *testing.T) {
	cmds := DetectVerifyCommands(map[string]bool{"go.mod": true})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands for go.mod, got %d: %v", len(cmds), cmds)
	}
	if !strings.Contains(cmds[0], "go test") {
		t.Fatalf("expected go test command, got: %s", cmds[0])
	}
	if !strings.Contains(cmds[1], "go vet") {
		t.Fatalf("expected go vet command, got: %s", cmds[1])
	}
}

func TestDetectVerifyCommandsNode(t *testing.T) {
	cmds := DetectVerifyCommands(map[string]bool{"package.json": true, ".eslintrc.json": true})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(cmds), cmds)
	}
}

func TestDetectVerifyCommandsNone(t *testing.T) {
	cmds := DetectVerifyCommands(map[string]bool{})
	if len(cmds) != 0 {
		t.Fatalf("expected 0 commands, got %d: %v", len(cmds), cmds)
	}
}

func TestExtractJSON(t *testing.T) {
	raw := "```json\n[{\"title\":\"A\",\"description\":\"B\"}]\n```"
	got := extractJSON(raw)
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Fatalf("failed to extract json array: %q", got)
	}
}

// --- Progress helper tests ---

func TestFormatProgressJSON(t *testing.T) {
	statuses := []SubTaskStatus{
		{Title: "Add auth", Description: "Add authentication", Status: "completed", CommitHash: "abc123"},
		{Title: "Add tests", Description: "Add unit tests", Status: "running"},
		{Title: "Add docs", Description: "Add documentation", Status: "pending"},
	}

	out, err := FormatProgressJSON(statuses)
	if err != nil {
		t.Fatalf("FormatProgressJSON error: %v", err)
	}
	if !strings.Contains(out, "Add auth") {
		t.Fatalf("expected 'Add auth' in output: %s", out)
	}
	if !strings.Contains(out, "abc123") {
		t.Fatalf("expected commit hash in output: %s", out)
	}
	if !strings.Contains(out, "running") {
		t.Fatalf("expected 'running' status in output: %s", out)
	}

	// Should be valid JSON that round-trips.
	var parsed []SubTaskStatus
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("failed to parse output JSON: %v", err)
	}
	if len(parsed) != 3 {
		t.Fatalf("expected 3 items, got %d", len(parsed))
	}
	if parsed[0].CommitHash != "abc123" {
		t.Fatalf("expected commit hash 'abc123', got %q", parsed[0].CommitHash)
	}
}

func TestFormatProgressJSON_Empty(t *testing.T) {
	out, err := FormatProgressJSON([]SubTaskStatus{})
	if err != nil {
		t.Fatalf("FormatProgressJSON error: %v", err)
	}
	if out != "[]" {
		t.Fatalf("expected empty array, got %q", out)
	}
}

func TestProgressContext_FirstStep(t *testing.T) {
	statuses := []SubTaskStatus{
		{Title: "Step 1", Description: "Do thing 1", Status: "running"},
		{Title: "Step 2", Description: "Do thing 2", Status: "pending"},
	}

	ctx := ProgressContext(statuses, 0)
	if ctx != "" {
		t.Fatalf("expected empty context for first step, got %q", ctx)
	}
}

func TestProgressContext_SecondStep(t *testing.T) {
	statuses := []SubTaskStatus{
		{Title: "Add auth", Description: "Add authentication module", Status: "completed", CommitHash: "abc123"},
		{Title: "Add tests", Description: "Add unit tests", Status: "running"},
		{Title: "Add docs", Description: "Add documentation", Status: "pending"},
	}

	ctx := ProgressContext(statuses, 1)
	if !strings.Contains(ctx, "Previous Steps") {
		t.Fatalf("expected 'Previous Steps' header: %s", ctx)
	}
	if !strings.Contains(ctx, "Add auth") {
		t.Fatalf("expected completed step in context: %s", ctx)
	}
	if !strings.Contains(ctx, "Current Step (2/3)") {
		t.Fatalf("expected current step marker: %s", ctx)
	}
	if !strings.Contains(ctx, "Add tests") {
		t.Fatalf("expected current step title: %s", ctx)
	}
	// Should NOT contain the third step.
	if strings.Contains(ctx, "Add docs") {
		t.Fatalf("should not contain future step: %s", ctx)
	}
}

func TestProgressContext_FailedStep(t *testing.T) {
	statuses := []SubTaskStatus{
		{Title: "Step 1", Description: "First step", Status: "failed"},
		{Title: "Step 2", Description: "Second step", Status: "running"},
	}

	ctx := ProgressContext(statuses, 1)
	if !strings.Contains(ctx, "❌") {
		t.Fatalf("expected failure icon for failed step: %s", ctx)
	}
}

func TestDecomposeWithMaxSubTasks(t *testing.T) {
	// When maxSubTasks > 5, the user message should include the max instruction.
	var capturedUser string
	stage := NewDecomposeStage(&fakeLLM{response: `[{"title":"T1","description":"D1"}]`}, "")

	// Override LLM to capture the user message.
	capturingLLM := &capturingFakeLLM{response: `[{"title":"T1","description":"D1"}]`}
	stage.llm = capturingLLM

	ctx := &Context{Ctx: context.Background(), Prompt: "complex task"}
	if err := stage.ExecuteWithMaxSubTasks(ctx, 10); err != nil {
		t.Fatalf("decompose error: %v", err)
	}
	capturedUser = capturingLLM.lastUser
	if !strings.Contains(capturedUser, "up to 10 steps") {
		t.Fatalf("expected max steps instruction in user message: %s", capturedUser)
	}
}

func TestDecomposeWithDefaultMax(t *testing.T) {
	capturingLLM := &capturingFakeLLM{response: `[{"title":"T1","description":"D1"}]`}
	stage := NewDecomposeStage(capturingLLM, "")

	ctx := &Context{Ctx: context.Background(), Prompt: "simple task"}
	if err := stage.ExecuteWithMaxSubTasks(ctx, 5); err != nil {
		t.Fatalf("decompose error: %v", err)
	}
	if strings.Contains(capturingLLM.lastUser, "up to") {
		t.Fatalf("should not include max steps instruction for default (5): %s", capturingLLM.lastUser)
	}
}

// capturingFakeLLM records the user message.
type capturingFakeLLM struct {
	response string
	lastUser string
}

func (f *capturingFakeLLM) Complete(_ context.Context, _, user string) (string, error) {
	f.lastUser = user
	return f.response, nil
}
