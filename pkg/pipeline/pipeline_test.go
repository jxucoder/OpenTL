package pipeline

import (
	"context"
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
