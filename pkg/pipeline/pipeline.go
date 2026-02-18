// Package pipeline defines the Stage/Pipeline interfaces and built-in stages
// for the TeleCoder plan-code-review pipeline.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jxucoder/TeleCoder/pkg/llm"
)

// Context carries data through pipeline stages.
type Context struct {
	Ctx        context.Context
	Repo       string
	Prompt     string
	RepoCtx    string // formatted repo context from indexer
	Plan       string
	Feedback   string
	SubTasks   []SubTask
}

// SubTask is a single step in a decomposed task.
type SubTask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ReviewResult is the outcome of a code review.
type ReviewResult struct {
	Approved bool
	Feedback string
}

// Stage is a single step in a pipeline.
type Stage interface {
	Name() string
	Execute(ctx *Context) error
}

// Pipeline executes a sequence of stages.
type Pipeline interface {
	Run(ctx *Context) error
}

// DefaultPipeline runs stages sequentially.
type DefaultPipeline struct {
	stages []Stage
}

// NewPipeline creates a pipeline from the given stages.
func NewPipeline(stages ...Stage) *DefaultPipeline {
	return &DefaultPipeline{stages: stages}
}

// Run executes all stages in order.
func (p *DefaultPipeline) Run(ctx *Context) error {
	for _, s := range p.stages {
		if err := s.Execute(ctx); err != nil {
			return fmt.Errorf("stage %s: %w", s.Name(), err)
		}
	}
	return nil
}

// --- Built-in stages ---

// PlanStage generates a structured plan using an LLM.
type PlanStage struct {
	llm          llm.Client
	systemPrompt string
}

// NewPlanStage creates a plan stage. Pass empty systemPrompt to use the default.
func NewPlanStage(client llm.Client, systemPrompt string) *PlanStage {
	if systemPrompt == "" {
		systemPrompt = DefaultPlannerPrompt
	}
	return &PlanStage{llm: client, systemPrompt: systemPrompt}
}

func (s *PlanStage) Name() string { return "plan" }

func (s *PlanStage) Execute(ctx *Context) error {
	user := fmt.Sprintf("Repository: %s\n\nTask: %s", ctx.Repo, ctx.Prompt)
	if ctx.RepoCtx != "" {
		user = fmt.Sprintf("Repository: %s\n\n## Codebase Context\n%s\n\nTask: %s", ctx.Repo, ctx.RepoCtx, ctx.Prompt)
	}

	plan, err := s.llm.Complete(ctx.Ctx, s.systemPrompt, user)
	if err != nil {
		return fmt.Errorf("planning: %w", err)
	}

	ctx.Plan = plan
	return nil
}

// ReviewStage reviews a diff against the plan and task.
type ReviewStage struct {
	llm          llm.Client
	systemPrompt string
}

// NewReviewStage creates a review stage. Pass empty systemPrompt to use the default.
func NewReviewStage(client llm.Client, systemPrompt string) *ReviewStage {
	if systemPrompt == "" {
		systemPrompt = DefaultReviewerPrompt
	}
	return &ReviewStage{llm: client, systemPrompt: systemPrompt}
}

func (s *ReviewStage) Name() string { return "review" }

func (s *ReviewStage) Execute(ctx *Context) error {
	// Review is invoked directly by the engine, not via pipeline Run.
	return nil
}

// Review examines a diff and returns a ReviewResult.
func (s *ReviewStage) Review(ctx context.Context, prompt, plan, diff string) (*ReviewResult, error) {
	user := fmt.Sprintf("## Original Task\n%s\n\n## Plan\n%s\n\n## Diff\n```diff\n%s\n```", prompt, plan, diff)

	response, err := s.llm.Complete(ctx, s.systemPrompt, user)
	if err != nil {
		return nil, fmt.Errorf("reviewing: %w", err)
	}

	approved := strings.HasPrefix(strings.ToUpper(strings.TrimSpace(response)), "APPROVED")

	return &ReviewResult{
		Approved: approved,
		Feedback: response,
	}, nil
}

// DecomposeStage breaks a task into ordered sub-tasks using an LLM.
type DecomposeStage struct {
	llm          llm.Client
	systemPrompt string
}

// NewDecomposeStage creates a decompose stage. Pass empty systemPrompt to use the default.
func NewDecomposeStage(client llm.Client, systemPrompt string) *DecomposeStage {
	if systemPrompt == "" {
		systemPrompt = DefaultDecomposerPrompt
	}
	return &DecomposeStage{llm: client, systemPrompt: systemPrompt}
}

func (s *DecomposeStage) Name() string { return "decompose" }

func (s *DecomposeStage) Execute(ctx *Context) error {
	return s.ExecuteWithMaxSubTasks(ctx, 0)
}

// ExecuteWithMaxSubTasks decomposes the task allowing up to maxSubTasks steps.
// If maxSubTasks is 0 or <= 5, uses the default prompt. If > 5, appends an
// instruction allowing more steps.
func (s *DecomposeStage) ExecuteWithMaxSubTasks(ctx *Context, maxSubTasks int) error {
	user := fmt.Sprintf("Task: %s", ctx.Prompt)
	if ctx.RepoCtx != "" {
		user = fmt.Sprintf("## Codebase Context\n%s\n\nTask: %s", ctx.RepoCtx, ctx.Prompt)
	}

	if maxSubTasks > 5 {
		user += fmt.Sprintf("\n\nYou may return up to %d steps.", maxSubTasks)
	}

	response, err := s.llm.Complete(ctx.Ctx, s.systemPrompt, user)
	if err != nil {
		return fmt.Errorf("decomposing task: %w", err)
	}

	tasks, err := parseSubTasks(response)
	if err != nil || len(tasks) == 0 {
		ctx.SubTasks = []SubTask{{Title: "Complete task", Description: ctx.Prompt}}
		return nil
	}
	ctx.SubTasks = tasks
	return nil
}

// EnrichPrompt combines the original prompt with a generated plan.
func EnrichPrompt(originalPrompt, plan string) string {
	return fmt.Sprintf(`## Task
%s

## Plan
The following plan was generated for this task. Follow it closely.

%s

## Instructions
- Follow the plan step by step
- Run tests after making changes if a test suite exists
- If tests fail, fix the issues before proceeding
- Keep changes minimal and focused on the task
- Do not make unrelated changes`, originalPrompt, plan)
}

// RevisePrompt builds an instruction for a revision round.
func RevisePrompt(originalPrompt, plan, feedback string) string {
	return fmt.Sprintf(`## Task
%s

## Plan
%s

## Revision Instructions
A code review found issues with the previous attempt. Address the following
feedback carefully. Only change what the reviewer flagged -- do not redo work
that was already approved.

%s

## General Rules
- Run tests after making changes if a test suite exists
- Keep changes minimal and focused on the feedback
- Do not make unrelated changes`, originalPrompt, plan, feedback)
}

// --- Progress helpers for long-running multi-step tasks ---

// SubTaskStatus tracks the state of a single sub-task during multi-step execution.
type SubTaskStatus struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"` // "pending", "running", "completed", "failed"
	CommitHash  string `json:"commit_hash,omitempty"`
}

// FormatProgressJSON serializes the current progress state as JSON for writing
// into the sandbox as .telecoder-progress.json.
func FormatProgressJSON(statuses []SubTaskStatus) (string, error) {
	data, err := json.MarshalIndent(statuses, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshalling progress: %w", err)
	}
	return string(data), nil
}

// ProgressContext builds a markdown summary of completed/failed steps to prepend
// to the agent's prompt, giving it awareness of what has been done so far.
func ProgressContext(statuses []SubTaskStatus, currentIndex int) string {
	if currentIndex == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Previous Steps\n\n")
	b.WriteString("The following steps have already been completed on this branch.\n")
	b.WriteString("Do NOT redo any of this work — build on top of it.\n\n")

	for i := 0; i < currentIndex && i < len(statuses); i++ {
		s := statuses[i]
		icon := "✅"
		if s.Status == "failed" {
			icon = "❌"
		}
		b.WriteString(fmt.Sprintf("%d. %s **%s** — %s\n", i+1, icon, s.Title, s.Description))
	}

	if currentIndex < len(statuses) {
		b.WriteString(fmt.Sprintf("\n## Current Step (%d/%d)\n\n", currentIndex+1, len(statuses)))
		b.WriteString(fmt.Sprintf("**%s**: %s\n", statuses[currentIndex].Title, statuses[currentIndex].Description))
	}

	return b.String()
}

// --- JSON parsing helpers ---

func parseSubTasks(response string) ([]SubTask, error) {
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON array found in response")
	}

	var tasks []SubTask
	if err := json.Unmarshal([]byte(jsonStr), &tasks); err != nil {
		return nil, fmt.Errorf("parsing sub-tasks JSON: %w", err)
	}
	return tasks, nil
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}

	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}
