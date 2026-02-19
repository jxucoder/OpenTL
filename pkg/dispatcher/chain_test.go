package dispatcher

import (
	"context"
	"fmt"
	"testing"

	"github.com/jxucoder/TeleCoder/pkg/model"
)

func TestChainEvaluator_SpawnsFollowUp(t *testing.T) {
	llm := &mockLLM{response: `{"action":"spawn","repo":"myorg/myapp","prompt":"review the PR changes"}`}
	d := New(llm)
	ce := NewChainEvaluator(d, 3)

	sess := &model.Session{
		ID:     "abc123",
		Repo:   "myorg/myapp",
		Prompt: "add auth",
		Result: model.Result{Type: model.ResultPR, PRUrl: "https://github.com/myorg/myapp/pull/42"},
	}

	dec, err := ce.Evaluate(context.Background(), sess)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if dec == nil {
		t.Fatal("expected non-nil decision for spawn")
	}
	if dec.Action != "spawn" {
		t.Fatalf("expected 'spawn', got %q", dec.Action)
	}
	if dec.Prompt != "review the PR changes" {
		t.Fatalf("expected review prompt, got %q", dec.Prompt)
	}
}

func TestChainEvaluator_NoFollowUp(t *testing.T) {
	llm := &mockLLM{response: `{"action":"ignore"}`}
	d := New(llm)
	ce := NewChainEvaluator(d, 3)

	sess := &model.Session{
		ID:     "abc123",
		Repo:   "myorg/myapp",
		Prompt: "fix typo",
		Result: model.Result{Type: model.ResultText, Content: "Fixed the typo."},
	}

	dec, err := ce.Evaluate(context.Background(), sess)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if dec != nil {
		t.Fatalf("expected nil decision for ignore, got %+v", dec)
	}
}

func TestChainEvaluator_DepthLimitReached(t *testing.T) {
	llm := &mockLLM{response: `{"action":"spawn","repo":"myorg/myapp","prompt":"next step"}`}
	d := New(llm)
	ce := NewChainEvaluator(d, 3)

	sess := &model.Session{
		ID:         "abc123",
		Repo:       "myorg/myapp",
		ChainDepth: 3,
	}

	_, err := ce.Evaluate(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error for chain depth limit")
	}
}

func TestChainEvaluator_Depth4Rejected(t *testing.T) {
	llm := &mockLLM{response: `{"action":"spawn","repo":"myorg/myapp","prompt":"step 5"}`}
	d := New(llm)
	ce := NewChainEvaluator(d, 3)

	sess := &model.Session{
		ID:         "abc123",
		Repo:       "myorg/myapp",
		ChainDepth: 4,
	}

	_, err := ce.Evaluate(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error for depth 4 with limit 3")
	}
}

func TestChainEvaluator_InheritsRepo(t *testing.T) {
	llm := &mockLLM{response: `{"action":"spawn","prompt":"review changes"}`}
	d := New(llm)
	ce := NewChainEvaluator(d, 3)

	sess := &model.Session{
		ID:     "abc123",
		Repo:   "myorg/myapp",
		Prompt: "add feature",
		Result: model.Result{Type: model.ResultPR, PRUrl: "https://github.com/myorg/myapp/pull/1"},
	}

	dec, err := ce.Evaluate(context.Background(), sess)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if dec.Repo != "myorg/myapp" {
		t.Fatalf("expected inherited repo 'myorg/myapp', got %q", dec.Repo)
	}
}

func TestChainEvaluator_LLMError(t *testing.T) {
	llm := &mockLLM{err: fmt.Errorf("LLM down")}
	d := New(llm)
	ce := NewChainEvaluator(d, 3)

	sess := &model.Session{ID: "abc123", Repo: "myorg/myapp"}

	_, err := ce.Evaluate(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

func TestChainEvaluator_DefaultMaxDepth(t *testing.T) {
	llm := &mockLLM{response: `{"action":"ignore"}`}
	d := New(llm)
	ce := NewChainEvaluator(d, 0)

	if ce.MaxDepth() != model.MaxChainDepth {
		t.Fatalf("expected default max depth %d, got %d", model.MaxChainDepth, ce.MaxDepth())
	}
}
