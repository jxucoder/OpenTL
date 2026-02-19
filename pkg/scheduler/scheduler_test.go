package scheduler

import (
	"os"
	"path/filepath"
	"testing"
)

type mockCreator struct {
	calls []struct{ repo, prompt string }
}

func (m *mockCreator) CreateAndRunSession(repo, prompt string) error {
	m.calls = append(m.calls, struct{ repo, prompt string }{repo, prompt})
	return nil
}

func TestParseJobFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	content := `schedule: "0 9 * * MON"
repos:
  - org/api
  - org/frontend
prompt: "Audit for outdated deps and TODOs older than 90 days."
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	job, err := parseJobFile(path)
	if err != nil {
		t.Fatalf("parseJobFile: %v", err)
	}
	if job.Schedule != "0 9 * * MON" {
		t.Fatalf("expected schedule '0 9 * * MON', got %q", job.Schedule)
	}
	if len(job.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(job.Repos))
	}
	if job.Repos[0] != "org/api" {
		t.Fatalf("expected first repo 'org/api', got %q", job.Repos[0])
	}
	if job.Prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
}

func TestParseJobFile_MissingSchedule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	content := `repos: [org/api]
prompt: "test"
`
	os.WriteFile(path, []byte(content), 0644)
	_, err := parseJobFile(path)
	if err == nil {
		t.Fatal("expected error for missing schedule")
	}
}

func TestParseJobFile_MissingRepos(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	content := `schedule: "* * * * *"
prompt: "test"
`
	os.WriteFile(path, []byte(content), 0644)
	_, err := parseJobFile(path)
	if err == nil {
		t.Fatal("expected error for missing repos")
	}
}

func TestParseJobFile_MissingPrompt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	content := `schedule: "* * * * *"
repos: [org/api]
`
	os.WriteFile(path, []byte(content), 0644)
	_, err := parseJobFile(path)
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestLoadJobs(t *testing.T) {
	dir := t.TempDir()
	content := `schedule: "0 9 * * MON"
repos: [org/api, org/web]
prompt: "Run weekly audit"
`
	os.WriteFile(filepath.Join(dir, "weekly.yaml"), []byte(content), 0644)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0644)

	creator := &mockCreator{}
	s := New(dir, creator)

	if err := s.LoadJobs(); err != nil {
		t.Fatalf("LoadJobs: %v", err)
	}

	jobs := s.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "weekly" {
		t.Fatalf("expected job name 'weekly', got %q", jobs[0].Name)
	}
	if jobs[0].Schedule != "0 9 * * MON" {
		t.Fatalf("expected schedule, got %q", jobs[0].Schedule)
	}
}

func TestLoadJobs_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	creator := &mockCreator{}
	s := New(dir, creator)

	if err := s.LoadJobs(); err != nil {
		t.Fatalf("LoadJobs: %v", err)
	}
	if len(s.Jobs()) != 0 {
		t.Fatal("expected 0 jobs for empty dir")
	}
}

func TestLoadJobs_NonexistentDir(t *testing.T) {
	creator := &mockCreator{}
	s := New("/nonexistent/path", creator)

	if err := s.LoadJobs(); err != nil {
		t.Fatalf("LoadJobs should not error for nonexistent dir: %v", err)
	}
}

func TestRunJob(t *testing.T) {
	creator := &mockCreator{}
	s := New("", creator)

	job := Job{
		Name:     "test",
		Schedule: "* * * * *",
		Repos:    []string{"org/api", "org/web"},
		Prompt:   "run audit",
	}

	if err := s.RunJob(job); err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	if len(creator.calls) != 2 {
		t.Fatalf("expected 2 session calls, got %d", len(creator.calls))
	}
	if creator.calls[0].repo != "org/api" {
		t.Fatalf("expected first call repo 'org/api', got %q", creator.calls[0].repo)
	}
	if creator.calls[1].repo != "org/web" {
		t.Fatalf("expected second call repo 'org/web', got %q", creator.calls[1].repo)
	}
	if creator.calls[0].prompt != "run audit" {
		t.Fatalf("expected prompt 'run audit', got %q", creator.calls[0].prompt)
	}
}

func TestLoadJobs_WithName(t *testing.T) {
	dir := t.TempDir()
	content := `name: "dependency-check"
schedule: "0 0 * * *"
repos: [org/api]
prompt: "Check dependencies"
`
	os.WriteFile(filepath.Join(dir, "deps.yaml"), []byte(content), 0644)

	s := New(dir, &mockCreator{})
	s.LoadJobs()

	jobs := s.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "dependency-check" {
		t.Fatalf("expected name 'dependency-check', got %q", jobs[0].Name)
	}
}
