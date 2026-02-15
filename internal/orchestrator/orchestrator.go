// Package orchestrator implements the plan-then-code-then-review pipeline.
//
// Instead of sending raw prompts directly to the sandbox agent, the orchestrator:
//  1. PLAN  - Analyzes the task and generates a structured plan
//  2. CODE  - Sends the enriched plan to the sandbox agent (OpenCode)
//  3. REVIEW - Reviews the resulting diff and optionally requests one revision
//
// This is not a multi-agent framework -- it's three sequential LLM calls
// that wrap the sandbox execution with intelligence.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LLMClient is a minimal interface for making LLM API calls.
// The orchestrator doesn't care about the provider -- it just needs
// a function that takes a system prompt + user prompt and returns text.
type LLMClient interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Orchestrator manages the plan/code/review pipeline.
type Orchestrator struct {
	llm LLMClient
}

// New creates an Orchestrator with the given LLM client.
func New(llm LLMClient) *Orchestrator {
	return &Orchestrator{llm: llm}
}

// Plan generates a structured plan for a task.
// If repoContext is non-empty it is included so the planner knows the codebase
// structure (file tree, key files, languages, etc.).
func (o *Orchestrator) Plan(ctx context.Context, repo, prompt, repoContext string) (string, error) {
	system := plannerSystemPrompt
	user := fmt.Sprintf("Repository: %s\n\nTask: %s", repo, prompt)
	if repoContext != "" {
		user = fmt.Sprintf("Repository: %s\n\n## Codebase Context\n%s\n\nTask: %s", repo, repoContext, prompt)
	}

	plan, err := o.llm.Complete(ctx, system, user)
	if err != nil {
		return "", fmt.Errorf("planning: %w", err)
	}

	return plan, nil
}

// EnrichPrompt combines the original prompt with a generated plan
// into a detailed instruction for the coding agent.
func (o *Orchestrator) EnrichPrompt(originalPrompt, plan string) string {
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

// RevisePrompt builds an instruction for a revision round. The coding agent
// receives the original task, the plan, and specific reviewer feedback so it
// can fix the issues without starting from scratch.
func (o *Orchestrator) RevisePrompt(originalPrompt, plan, feedback string) string {
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

// ReviewResult is the outcome of a code review.
type ReviewResult struct {
	Approved bool   // true if changes look correct
	Feedback string // specific feedback if not approved
}

// Review examines a diff against the original plan and provides feedback.
func (o *Orchestrator) Review(ctx context.Context, prompt, plan, diff string) (*ReviewResult, error) {
	system := reviewerSystemPrompt
	user := fmt.Sprintf("## Original Task\n%s\n\n## Plan\n%s\n\n## Diff\n```diff\n%s\n```", prompt, plan, diff)

	response, err := o.llm.Complete(ctx, system, user)
	if err != nil {
		return nil, fmt.Errorf("reviewing: %w", err)
	}

	// Parse the response. Convention: starts with "APPROVED" or "REVISION NEEDED"
	approved := strings.HasPrefix(strings.ToUpper(strings.TrimSpace(response)), "APPROVED")

	return &ReviewResult{
		Approved: approved,
		Feedback: response,
	}, nil
}

// SubTask is a single step in a decomposed task.
type SubTask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Decompose uses the LLM to break a task into ordered sub-tasks.
// If the task is simple, returns a single SubTask wrapping the original prompt.
func (o *Orchestrator) Decompose(ctx context.Context, prompt, repoContext string) ([]SubTask, error) {
	user := fmt.Sprintf("Task: %s", prompt)
	if repoContext != "" {
		user = fmt.Sprintf("## Codebase Context\n%s\n\nTask: %s", repoContext, prompt)
	}

	response, err := o.llm.Complete(ctx, decomposerSystemPrompt, user)
	if err != nil {
		return nil, fmt.Errorf("decomposing task: %w", err)
	}

	tasks, err := parseSubTasks(response)
	if err != nil || len(tasks) == 0 {
		return []SubTask{{Title: "Complete task", Description: prompt}}, nil
	}
	return tasks, nil
}

// parseSubTasks extracts the JSON array from the LLM response.
// The response may contain markdown fences or extra text around the JSON.
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

// extractJSON finds the first JSON array in the text, handling optional
// markdown code fences.
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

// --- System Prompts ---

const plannerSystemPrompt = `You are a senior software engineer planning a code change.

Given a repository name, optional codebase context (file tree, key config files,
language breakdown), and a task description, create a structured plan.

Your plan should include:
1. **Files to modify** - List specific files that need changes (use the codebase
   context to identify real paths when available)
2. **Approach** - Step-by-step approach to implement the change
3. **Testing** - How to verify the changes work
4. **Risks** - Any potential issues or edge cases to watch for

Keep the plan concise and actionable. Focus on WHAT to change and WHY,
not the exact code (the coding agent will handle implementation details).

Output the plan in markdown format.`

const reviewerSystemPrompt = `You are a senior software engineer reviewing a code change.

You will receive:
1. The original task description
2. The plan that was created for the task
3. The diff of changes made

Review the diff against the plan and task. Check for:
- Does the diff address the original task?
- Does it follow the plan?
- Are there any bugs, security issues, or missing edge cases?
- Are there any unnecessary or unrelated changes?

Respond with one of:
- "APPROVED" followed by a brief summary of why the changes look good
- "REVISION NEEDED" followed by specific, actionable feedback

Keep your response concise and focused on the most important issues.`

const decomposerSystemPrompt = `You are a task decomposition engine for a coding agent.

Given a task description (and optionally codebase context), decide whether the
task should be executed as a single step or broken into multiple ordered steps.

Rules:
- For simple, focused tasks (e.g. "fix the typo in README", "add a unit test
  for function X"), return a SINGLE sub-task.
- For complex, multi-concern tasks (e.g. "add user authentication with login,
  signup, and password reset"), break into 2-5 ordered steps.
- Each step should be independently executable and testable.
- Steps are executed sequentially on the same git branch -- later steps can
  depend on earlier steps' changes.
- Keep step descriptions specific and actionable.

Return ONLY a JSON array (no other text) in this exact format:

[
  {"title": "Short title", "description": "Detailed description of what to do"},
  {"title": "Short title", "description": "Detailed description of what to do"}
]

For a simple task, return a single-element array.`
