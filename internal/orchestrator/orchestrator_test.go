package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jxucoder/opentl/internal/orchestrator"
)

// ---------------------------------------------------------------------------
// Mock LLM client
// ---------------------------------------------------------------------------

// mockLLMClient is a test double that records the arguments it receives and
// returns a canned response (or error).
type mockLLMClient struct {
	systemArg string
	userArg   string
	response  string
	err       error
}

func (m *mockLLMClient) Complete(_ context.Context, system, user string) (string, error) {
	m.systemArg = system
	m.userArg = user
	return m.response, m.err
}

// ---------------------------------------------------------------------------
// Plan tests
// ---------------------------------------------------------------------------

func TestPlan_CallsLLMWithCorrectPrompts(t *testing.T) {
	mock := &mockLLMClient{response: "step 1: do the thing"}
	o := orchestrator.New(mock)

	repo := "owner/repo"
	prompt := "add a feature"

	plan, err := o.Plan(context.Background(), repo, prompt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan != "step 1: do the thing" {
		t.Errorf("expected plan %q, got %q", "step 1: do the thing", plan)
	}

	// The user prompt should contain both the repository and the task.
	if !strings.Contains(mock.userArg, repo) {
		t.Errorf("user prompt should contain repo %q, got %q", repo, mock.userArg)
	}
	if !strings.Contains(mock.userArg, prompt) {
		t.Errorf("user prompt should contain task %q, got %q", prompt, mock.userArg)
	}

	// The system prompt should be non-empty (the planner system prompt).
	if mock.systemArg == "" {
		t.Error("system prompt should not be empty")
	}
}

func TestPlan_ReturnsErrorOnLLMFailure(t *testing.T) {
	mock := &mockLLMClient{err: errors.New("llm down")}
	o := orchestrator.New(mock)

	_, err := o.Plan(context.Background(), "owner/repo", "do stuff")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "planning") {
		t.Errorf("error should be wrapped with 'planning', got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// EnrichPrompt tests
// ---------------------------------------------------------------------------

func TestEnrichPrompt_CombinesPromptAndPlan(t *testing.T) {
	o := orchestrator.New(nil) // LLM not needed for EnrichPrompt

	original := "add dark mode"
	plan := "1. update CSS\n2. add toggle"

	result := o.EnrichPrompt(original, plan)

	if !strings.Contains(result, original) {
		t.Errorf("enriched prompt should contain original prompt %q", original)
	}
	if !strings.Contains(result, plan) {
		t.Errorf("enriched prompt should contain plan %q", plan)
	}
	if !strings.Contains(result, "## Task") {
		t.Error("enriched prompt should contain '## Task' header")
	}
	if !strings.Contains(result, "## Plan") {
		t.Error("enriched prompt should contain '## Plan' header")
	}
	if !strings.Contains(result, "## Instructions") {
		t.Error("enriched prompt should contain '## Instructions' header")
	}
}

// ---------------------------------------------------------------------------
// Review tests
// ---------------------------------------------------------------------------

func TestReview_ApprovedResponse(t *testing.T) {
	mock := &mockLLMClient{response: "APPROVED - looks great, all tests pass"}
	o := orchestrator.New(mock)

	result, err := o.Review(context.Background(), "task", "plan", "diff content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Approved {
		t.Error("expected Approved to be true for APPROVED response")
	}
	if result.Feedback != "APPROVED - looks great, all tests pass" {
		t.Errorf("unexpected feedback: %q", result.Feedback)
	}
}

func TestReview_RevisionNeededResponse(t *testing.T) {
	mock := &mockLLMClient{response: "REVISION NEEDED - missing error handling in handler.go"}
	o := orchestrator.New(mock)

	result, err := o.Review(context.Background(), "task", "plan", "diff content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Approved {
		t.Error("expected Approved to be false for REVISION NEEDED response")
	}
	if result.Feedback != "REVISION NEEDED - missing error handling in handler.go" {
		t.Errorf("unexpected feedback: %q", result.Feedback)
	}
}

func TestReview_LowercaseApprovedIsStillApproved(t *testing.T) {
	mock := &mockLLMClient{response: "approved with minor nits"}
	o := orchestrator.New(mock)

	result, err := o.Review(context.Background(), "task", "plan", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Approved {
		t.Error("expected Approved to be true for lowercase 'approved' response (case-insensitive)")
	}
}

func TestReview_WhitespaceBeforeApproved(t *testing.T) {
	mock := &mockLLMClient{response: "  APPROVED with whitespace prefix"}
	o := orchestrator.New(mock)

	result, err := o.Review(context.Background(), "task", "plan", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Approved {
		t.Error("expected Approved to be true when response has leading whitespace before APPROVED")
	}
}

func TestReview_ReturnsErrorOnLLMFailure(t *testing.T) {
	mock := &mockLLMClient{err: errors.New("network error")}
	o := orchestrator.New(mock)

	result, err := o.Review(context.Background(), "task", "plan", "diff")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
	if !strings.Contains(err.Error(), "reviewing") {
		t.Errorf("error should be wrapped with 'reviewing', got %q", err.Error())
	}
}

func TestReview_UserPromptContainsAllInputs(t *testing.T) {
	mock := &mockLLMClient{response: "APPROVED"}
	o := orchestrator.New(mock)

	prompt := "original task description"
	plan := "the plan"
	diff := "+added a line\n-removed a line"

	_, err := o.Review(context.Background(), prompt, plan, diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(mock.userArg, prompt) {
		t.Errorf("user prompt should contain the task prompt, got %q", mock.userArg)
	}
	if !strings.Contains(mock.userArg, plan) {
		t.Errorf("user prompt should contain the plan, got %q", mock.userArg)
	}
	if !strings.Contains(mock.userArg, diff) {
		t.Errorf("user prompt should contain the diff, got %q", mock.userArg)
	}
}

// ---------------------------------------------------------------------------
// NewLLMClientFromEnv tests
// ---------------------------------------------------------------------------

// clearLLMEnv unsets all keys that NewLLMClientFromEnv inspects.
func clearLLMEnv(t *testing.T) {
	t.Helper()
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("OPENTL_PLANNER_MODEL")
}

func TestNewLLMClientFromEnv_PrefersAnthropic(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	client, err := orchestrator.NewLLMClientFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := client.(*orchestrator.AnthropicClient); !ok {
		t.Errorf("expected *AnthropicClient, got %T", client)
	}
}

func TestNewLLMClientFromEnv_FallsBackToOpenAI(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-openai-test-key")

	client, err := orchestrator.NewLLMClientFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := client.(*orchestrator.OpenAIClient); !ok {
		t.Errorf("expected *OpenAIClient, got %T", client)
	}
}

func TestNewLLMClientFromEnv_AnthropicOverOpenAI(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
	t.Setenv("OPENAI_API_KEY", "sk-openai-test-key")

	client, err := orchestrator.NewLLMClientFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := client.(*orchestrator.AnthropicClient); !ok {
		t.Errorf("expected Anthropic to be preferred when both keys are set, got %T", client)
	}
}

func TestNewLLMClientFromEnv_NoKeysReturnsError(t *testing.T) {
	clearLLMEnv(t)

	client, err := orchestrator.NewLLMClientFromEnv()
	if err == nil {
		t.Fatal("expected error when no API keys are set, got nil")
	}
	if client != nil {
		t.Error("expected nil client on error")
	}
	if !strings.Contains(err.Error(), "no LLM API key found") {
		t.Errorf("error should mention missing keys, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// NewAnthropicClient / NewOpenAIClient constructor tests
// ---------------------------------------------------------------------------

func TestNewAnthropicClient_DefaultModel(t *testing.T) {
	client := orchestrator.NewAnthropicClient("sk-ant-key", "")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	// We cannot inspect the private model field directly, but we confirm
	// the constructor does not panic and returns a valid *AnthropicClient.
}

func TestNewAnthropicClient_CustomModel(t *testing.T) {
	client := orchestrator.NewAnthropicClient("sk-ant-key", "claude-opus-4-20250514")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewOpenAIClient_DefaultModel(t *testing.T) {
	client := orchestrator.NewOpenAIClient("sk-openai-key", "")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewOpenAIClient_CustomModel(t *testing.T) {
	client := orchestrator.NewOpenAIClient("sk-openai-key", "gpt-4-turbo")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
