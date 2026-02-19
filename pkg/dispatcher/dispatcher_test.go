package dispatcher

import (
	"context"
	"fmt"
	"testing"
)

type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Complete(_ context.Context, _, _ string) (string, error) {
	return m.response, m.err
}

func TestDispatch_Spawn(t *testing.T) {
	llm := &mockLLM{response: `{"action":"spawn","repo":"myorg/myapp","prompt":"fix the login bug","agent":"auto"}`}
	d := New(llm)

	dec, err := d.Dispatch(context.Background(), ChannelSlack, "fix the login bug in myorg/myapp")
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if dec.Action != "spawn" {
		t.Fatalf("expected action 'spawn', got %q", dec.Action)
	}
	if dec.Repo != "myorg/myapp" {
		t.Fatalf("expected repo 'myorg/myapp', got %q", dec.Repo)
	}
	if dec.Prompt != "fix the login bug" {
		t.Fatalf("expected prompt 'fix the login bug', got %q", dec.Prompt)
	}
}

func TestDispatch_Reply(t *testing.T) {
	llm := &mockLLM{response: `{"action":"reply","reply":"The project uses Go 1.22"}`}
	d := New(llm)

	dec, err := d.Dispatch(context.Background(), ChannelTelegram, "what language is this project?")
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if dec.Action != "reply" {
		t.Fatalf("expected action 'reply', got %q", dec.Action)
	}
	if dec.Reply == "" {
		t.Fatal("expected non-empty reply")
	}
}

func TestDispatch_Ignore(t *testing.T) {
	llm := &mockLLM{response: `{"action":"ignore"}`}
	d := New(llm)

	dec, err := d.Dispatch(context.Background(), ChannelSlack, "good morning everyone!")
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if dec.Action != "ignore" {
		t.Fatalf("expected action 'ignore', got %q", dec.Action)
	}
}

func TestDispatch_GitHubIssue(t *testing.T) {
	llm := &mockLLM{response: `{"action":"spawn","repo":"myorg/api","prompt":"Add rate limiting to /users endpoint","agent":"pi"}`}
	d := New(llm)

	dec, err := d.Dispatch(context.Background(), ChannelGitHub, "Issue #42: Add rate limiting to /users endpoint")
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if dec.Action != "spawn" {
		t.Fatalf("expected action 'spawn', got %q", dec.Action)
	}
	if dec.Agent != "pi" {
		t.Fatalf("expected agent 'pi', got %q", dec.Agent)
	}
}

func TestDispatch_SentryAlert(t *testing.T) {
	llm := &mockLLM{response: `{"action":"spawn","repo":"myorg/api","prompt":"Investigate NullPointerException in UserService.java:42"}`}
	d := New(llm)

	dec, err := d.Dispatch(context.Background(), ChannelGeneric, "Sentry alert: NullPointerException in UserService.java:42")
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if dec.Action != "spawn" {
		t.Fatalf("expected action 'spawn', got %q", dec.Action)
	}
}

func TestDispatch_IgnoreProducesNoSession(t *testing.T) {
	llm := &mockLLM{response: `{"action":"ignore"}`}
	d := New(llm)

	dec, err := d.Dispatch(context.Background(), ChannelSlack, "brb lunch")
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if dec.Action != "ignore" {
		t.Fatalf("expected 'ignore', got %q", dec.Action)
	}
	if dec.Repo != "" || dec.Prompt != "" {
		t.Fatalf("ignore should have no repo/prompt, got repo=%q prompt=%q", dec.Repo, dec.Prompt)
	}
}

func TestDispatch_LLMError(t *testing.T) {
	llm := &mockLLM{err: fmt.Errorf("API rate limit")}
	d := New(llm)

	_, err := d.Dispatch(context.Background(), ChannelSlack, "fix the bug")
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

func TestDispatch_MalformedResponse(t *testing.T) {
	llm := &mockLLM{response: "I'm not sure what you want"}
	d := New(llm)

	dec, err := d.Dispatch(context.Background(), ChannelSlack, "hello")
	if err != nil {
		t.Fatalf("expected no error for malformed response, got %v", err)
	}
	if dec.Action != "ignore" {
		t.Fatalf("expected fallback to 'ignore', got %q", dec.Action)
	}
}

func TestDispatch_InvalidAction(t *testing.T) {
	llm := &mockLLM{response: `{"action":"delete"}`}
	d := New(llm)

	dec, err := d.Dispatch(context.Background(), ChannelSlack, "test")
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if dec.Action != "ignore" {
		t.Fatalf("expected invalid action to become 'ignore', got %q", dec.Action)
	}
}

func TestDispatch_CodeFencedResponse(t *testing.T) {
	llm := &mockLLM{response: "```json\n{\"action\":\"spawn\",\"repo\":\"org/repo\",\"prompt\":\"do thing\"}\n```"}
	d := New(llm)

	dec, err := d.Dispatch(context.Background(), ChannelSlack, "do thing in org/repo")
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if dec.Action != "spawn" {
		t.Fatalf("expected 'spawn', got %q", dec.Action)
	}
}

func TestSetPrompt(t *testing.T) {
	llm := &mockLLM{response: `{"action":"ignore"}`}
	d := New(llm)

	d.SetPrompt(ChannelSlack, "Custom slack prompt")
	if d.prompts[ChannelSlack] != "Custom slack prompt" {
		t.Fatalf("expected custom prompt, got %q", d.prompts[ChannelSlack])
	}
}

func TestDefaultPrompts(t *testing.T) {
	prompts := defaultPrompts()
	for _, ch := range []ChannelType{ChannelSlack, ChannelTelegram, ChannelGitHub, ChannelGeneric} {
		if _, ok := prompts[ch]; !ok {
			t.Fatalf("missing default prompt for channel %q", ch)
		}
	}
}
