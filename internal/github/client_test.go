package github

import (
	"testing"
)

func TestSplitRepo(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "standard owner/repo",
			input:     "owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "hyphenated org and repo",
			input:     "my-org/my-repo",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
			wantErr:   false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no slash",
			input:   "noslash",
			wantErr: true,
		},
		{
			name:    "empty owner",
			input:   "/repo",
			wantErr: true,
		},
		{
			name:    "empty repo",
			input:   "owner/",
			wantErr: true,
		},
		{
			name:      "extra slash kept in repo via SplitN",
			input:     "a/b/c",
			wantOwner: "a",
			wantRepo:  "b/c",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := splitRepo(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitRepo(%q) expected error, got owner=%q repo=%q", tt.input, owner, repo)
				}
				return
			}

			if err != nil {
				t.Fatalf("splitRepo(%q) unexpected error: %v", tt.input, err)
			}
			if owner != tt.wantOwner {
				t.Errorf("splitRepo(%q) owner = %q, want %q", tt.input, owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("splitRepo(%q) repo = %q, want %q", tt.input, repo, tt.wantRepo)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient("test-token")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
}
