// Package scheduler provides batch/cron job scheduling for TeleCoder.
// Jobs are defined as YAML files in a configurable directory and executed
// on cron schedules, creating task-mode sessions via the engine.
package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// SessionCreator is the interface for creating task sessions.
type SessionCreator interface {
	CreateAndRunSession(repo, prompt string) error
}

// Job defines a scheduled task from a YAML file.
type Job struct {
	Name     string   `yaml:"name"`
	Schedule string   `yaml:"schedule"`
	Repos    []string `yaml:"repos"`
	Prompt   string   `yaml:"prompt"`
}

// Scheduler manages cron jobs and triggers sessions.
type Scheduler struct {
	mu       sync.Mutex
	jobs     []Job
	creator  SessionCreator
	stopCh   chan struct{}
	jobsDir  string
}

// New creates a new Scheduler that reads jobs from the given directory.
func New(jobsDir string, creator SessionCreator) *Scheduler {
	return &Scheduler{
		jobsDir: jobsDir,
		creator: creator,
		stopCh:  make(chan struct{}),
	}
}

// LoadJobs reads all .yaml files from the jobs directory.
func (s *Scheduler) LoadJobs() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs = nil

	entries, err := os.ReadDir(s.jobsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading jobs directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(s.jobsDir, name)
		job, err := parseJobFile(path)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", name, err)
		}
		if job.Name == "" {
			job.Name = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
		}
		s.jobs = append(s.jobs, *job)
	}

	return nil
}

// Jobs returns a copy of the loaded jobs.
func (s *Scheduler) Jobs() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]Job, len(s.jobs))
	copy(cp, s.jobs)
	return cp
}

// RunJob executes a single job by creating sessions for each repo.
func (s *Scheduler) RunJob(job Job) error {
	for _, repo := range job.Repos {
		if err := s.creator.CreateAndRunSession(repo, job.Prompt); err != nil {
			return fmt.Errorf("creating session for %s: %w", repo, err)
		}
	}
	return nil
}

func parseJobFile(path string) (*Job, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var job Job
	if err := yaml.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	if job.Schedule == "" {
		return nil, fmt.Errorf("schedule is required")
	}
	if len(job.Repos) == 0 {
		return nil, fmt.Errorf("at least one repo is required")
	}
	if job.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	return &job, nil
}
