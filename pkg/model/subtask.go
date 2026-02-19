package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SubTask is a single step in a decomposed task.
type SubTask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

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
