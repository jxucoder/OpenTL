package agent

import (
	"fmt"
	"time"

	"github.com/jxucoder/TeleCoder/pkg/model"
)

// OpenCode wraps the OpenCode CLI agent.
// OpenCode is model-agnostic (15+ providers) and MIT licensed.
type OpenCode struct{}

func (a *OpenCode) Name() string { return "opencode" }

func (a *OpenCode) Command(prompt string) string {
	return fmt.Sprintf("cd /workspace/repo && opencode -p %q 2>&1", prompt)
}

func (a *OpenCode) ParseEvent(line string) *model.Event {
	return parseGenericLine(line)
}

// parseGenericLine handles the common ###TELECODER_ marker protocol.
// Returns nil for lines that are not markers (regular output).
func parseGenericLine(line string) *model.Event {
	now := time.Now().UTC()
	switch {
	case len(line) > 23 && line[:23] == "###TELECODER_STATUS### ":
		return &model.Event{Type: "status", Data: line[23:], CreatedAt: now}
	case len(line) > 22 && line[:22] == "###TELECODER_ERROR### ":
		return &model.Event{Type: "error", Data: line[22:], CreatedAt: now}
	case len(line) > 21 && line[:21] == "###TELECODER_DONE### ":
		return &model.Event{Type: "done", Data: line[21:], CreatedAt: now}
	case len(line) > 23 && line[:23] == "###TELECODER_RESULT### ":
		return &model.Event{Type: "result", Data: line[23:], CreatedAt: now}
	}
	return nil
}
