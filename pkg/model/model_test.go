package model

import "testing"

func TestTruncateShortString(t *testing.T) {
	got := Truncate("hello", 10)
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestTruncateExactLength(t *testing.T) {
	got := Truncate("hello", 5)
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestTruncateLongString(t *testing.T) {
	got := Truncate("hello world", 8)
	if got != "hello..." {
		t.Fatalf("expected 'hello...', got %q", got)
	}
}

func TestTruncateVerySmallMaxLen(t *testing.T) {
	got := Truncate("hello", 2)
	if got != "he" {
		t.Fatalf("expected 'he', got %q", got)
	}
}

func TestTruncateMaxLenThree(t *testing.T) {
	got := Truncate("hello", 3)
	if got != "hel" {
		t.Fatalf("expected 'hel', got %q", got)
	}
}

func TestTruncateEmptyString(t *testing.T) {
	got := Truncate("", 10)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestTruncateUnicode(t *testing.T) {
	got := Truncate("こんにちは世界", 6)
	if got != "こんに..." {
		t.Fatalf("expected 'こんに...', got %q", got)
	}
}

func TestStatusConstants(t *testing.T) {
	statuses := []Status{StatusPending, StatusRunning, StatusComplete, StatusError, StatusIdle}
	expected := []string{"pending", "running", "complete", "error", "idle"}
	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Fatalf("expected %q, got %q", expected[i], s)
		}
	}
}

func TestModeConstants(t *testing.T) {
	if string(ModeTask) != "task" {
		t.Fatalf("expected 'task', got %q", ModeTask)
	}
	if string(ModeChat) != "chat" {
		t.Fatalf("expected 'chat', got %q", ModeChat)
	}
}

func TestResultTypeConstants(t *testing.T) {
	if string(ResultPR) != "pr" {
		t.Fatalf("expected 'pr', got %q", ResultPR)
	}
	if string(ResultText) != "text" {
		t.Fatalf("expected 'text', got %q", ResultText)
	}
	if string(ResultNone) != "" {
		t.Fatalf("expected empty string, got %q", ResultNone)
	}
}
