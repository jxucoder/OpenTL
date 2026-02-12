package server

import (
	"testing"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate_ShortString(t *testing.T) {
	input := "hello"
	got := truncate(input, 10)
	if got != input {
		t.Errorf("truncate(%q, 10) = %q; want %q", input, got, input)
	}
}

func TestTruncate_LongASCII(t *testing.T) {
	input := "abcdefghijklmnopqrstuvwxyz"
	got := truncate(input, 10)
	want := "abcdefg..."
	if got != want {
		t.Errorf("truncate(%q, 10) = %q; want %q", input, got, want)
	}
	if runeCount := utf8.RuneCountInString(got); runeCount != 10 {
		t.Errorf("truncated result has %d runes; want 10", runeCount)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	input := "exactly10!" // 10 runes
	got := truncate(input, 10)
	if got != input {
		t.Errorf("truncate(%q, 10) = %q; want %q (unchanged)", input, got, input)
	}
}

func TestTruncate_MultiByte_Emoji(t *testing.T) {
	// Each emoji is one rune but multiple bytes.
	input := "Hello, World! 🌍🌎🌏🚀🛸" // 20 runes
	got := truncate(input, 15)

	if runeCount := utf8.RuneCountInString(got); runeCount != 15 {
		t.Errorf("truncated result has %d runes; want 15", runeCount)
	}
	// Last three characters should be "..."
	runes := []rune(got)
	suffix := string(runes[len(runes)-3:])
	if suffix != "..." {
		t.Errorf("truncated result should end with '...'; got suffix %q", suffix)
	}
}

func TestTruncate_MultiByte_CJK(t *testing.T) {
	// CJK characters: each is one rune (3 bytes in UTF-8).
	input := "Go语言编程设计模式与实践分享"
	runeLen := utf8.RuneCountInString(input)
	maxLen := 8

	if runeLen <= maxLen {
		t.Fatalf("test setup error: input has %d runes, need more than %d", runeLen, maxLen)
	}

	got := truncate(input, maxLen)
	gotRuneLen := utf8.RuneCountInString(got)
	if gotRuneLen != maxLen {
		t.Errorf("truncated result has %d runes; want %d", gotRuneLen, maxLen)
	}

	runes := []rune(got)
	suffix := string(runes[len(runes)-3:])
	if suffix != "..." {
		t.Errorf("truncated result should end with '...'; got suffix %q", suffix)
	}
}

func TestTruncate_EmptyString(t *testing.T) {
	got := truncate("", 10)
	if got != "" {
		t.Errorf("truncate(\"\", 10) = %q; want \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// isValidRepoFormat
// ---------------------------------------------------------------------------

func TestIsValidRepoFormat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "valid owner/repo", input: "owner/repo", want: true},
		{name: "valid with hyphens", input: "my-org/my-repo", want: true},
		{name: "extra slash", input: "owner/repo/extra", want: false},
		{name: "empty string", input: "", want: false},
		{name: "trailing slash", input: "owner/", want: false},
		{name: "leading slash", input: "/repo", want: false},
		{name: "no slash", input: "owner", want: false},
		{name: "only slash", input: "/", want: false},
		{name: "double slash", input: "owner//repo", want: false},
		{name: "multiple segments", input: "a/b/c/d", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidRepoFormat(tt.input)
			if got != tt.want {
				t.Errorf("isValidRepoFormat(%q) = %v; want %v", tt.input, got, tt.want)
			}
		})
	}
}
