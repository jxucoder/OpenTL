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

func TestExtractJSON(t *testing.T) {
	raw := "```json\n[{\"title\":\"A\",\"description\":\"B\"}]\n```"
	got := extractJSON(raw)
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Fatalf("failed to extract json array: %q", got)
	}
}
