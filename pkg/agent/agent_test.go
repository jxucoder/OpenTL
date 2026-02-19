package agent

import (
	"strings"
	"testing"
)

func TestOpenCodeCommand(t *testing.T) {
	a := &OpenCode{}
	if a.Name() != "opencode" {
		t.Fatalf("expected 'opencode', got %q", a.Name())
	}
	cmd := a.Command("fix the bug")
	if !strings.Contains(cmd, "opencode -p") {
		t.Fatalf("expected opencode command, got %q", cmd)
	}
	if !strings.Contains(cmd, "fix the bug") {
		t.Fatalf("expected prompt in command, got %q", cmd)
	}
}

func TestClaudeCodeCommand(t *testing.T) {
	a := &ClaudeCode{}
	if a.Name() != "claude-code" {
		t.Fatalf("expected 'claude-code', got %q", a.Name())
	}
	cmd := a.Command("add tests")
	if !strings.Contains(cmd, "claude --print") {
		t.Fatalf("expected claude command, got %q", cmd)
	}
}

func TestCodexCommand(t *testing.T) {
	a := &Codex{}
	if a.Name() != "codex" {
		t.Fatalf("expected 'codex', got %q", a.Name())
	}
	cmd := a.Command("refactor auth")
	if !strings.Contains(cmd, "codex exec --full-auto") {
		t.Fatalf("expected codex command, got %q", cmd)
	}
}

func TestPiCommand(t *testing.T) {
	a := &Pi{}
	if a.Name() != "pi" {
		t.Fatalf("expected 'pi', got %q", a.Name())
	}
	cmd := a.Command("deploy service")
	if !strings.Contains(cmd, "pi -p") {
		t.Fatalf("expected pi command, got %q", cmd)
	}
	if !strings.Contains(cmd, "--mode json") {
		t.Fatalf("expected json mode flag, got %q", cmd)
	}
}

func TestParseEvent_Status(t *testing.T) {
	a := &OpenCode{}
	ev := a.ParseEvent("###TELECODER_STATUS### Cloning repo")
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != "status" || ev.Data != "Cloning repo" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestParseEvent_Error(t *testing.T) {
	a := &ClaudeCode{}
	ev := a.ParseEvent("###TELECODER_ERROR### something failed")
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != "error" || ev.Data != "something failed" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestParseEvent_Done(t *testing.T) {
	a := &Codex{}
	ev := a.ParseEvent("###TELECODER_DONE### telecoder/abc123")
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != "done" || ev.Data != "telecoder/abc123" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestParseEvent_Result(t *testing.T) {
	a := &Pi{}
	ev := a.ParseEvent(`###TELECODER_RESULT### {"type":"text"}`)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != "result" {
		t.Fatalf("expected 'result' type, got %q", ev.Type)
	}
}

func TestParseEvent_RegularLine(t *testing.T) {
	a := &OpenCode{}
	ev := a.ParseEvent("just a regular log line")
	if ev != nil {
		t.Fatalf("expected nil for regular line, got %+v", ev)
	}
}

func TestRegistry(t *testing.T) {
	names := Names()
	if len(names) < 4 {
		t.Fatalf("expected at least 4 registered agents, got %d: %v", len(names), names)
	}

	for _, name := range []string{"opencode", "claude-code", "codex", "pi"} {
		a, err := Get(name)
		if err != nil {
			t.Fatalf("Get(%q) failed: %v", name, err)
		}
		if a.Name() != name {
			t.Fatalf("expected name %q, got %q", name, a.Name())
		}
	}
}

func TestGet_Unknown(t *testing.T) {
	_, err := Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestResolve(t *testing.T) {
	a := Resolve("claude-code")
	if a.Name() != "claude-code" {
		t.Fatalf("expected 'claude-code', got %q", a.Name())
	}

	a = Resolve("")
	if a.Name() != "opencode" {
		t.Fatalf("expected default 'opencode' for empty, got %q", a.Name())
	}

	a = Resolve("auto")
	if a.Name() != "opencode" {
		t.Fatalf("expected default 'opencode' for auto, got %q", a.Name())
	}

	a = Resolve("unknown-agent")
	if a.Name() != "opencode" {
		t.Fatalf("expected default 'opencode' for unknown, got %q", a.Name())
	}
}

func TestDefault(t *testing.T) {
	d := Default()
	if d == nil {
		t.Fatal("expected non-nil default agent")
	}
	if d.Name() != "opencode" {
		t.Fatalf("expected 'opencode' as default, got %q", d.Name())
	}
}

func TestCommandsNoDirectAgentReferences(t *testing.T) {
	for _, name := range []string{"opencode", "claude-code", "codex", "pi"} {
		a := Resolve(name)
		cmd := a.Command("test prompt")
		if cmd == "" {
			t.Fatalf("expected non-empty command for %q", name)
		}
		if !strings.HasPrefix(cmd, "cd /workspace/repo") {
			t.Fatalf("expected command to start with 'cd /workspace/repo' for %q, got %q", name, cmd)
		}
	}
}
