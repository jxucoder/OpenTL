package agent

import (
	"fmt"

	"github.com/jxucoder/TeleCoder/pkg/model"
)

// ClaudeCode wraps the Claude Code CLI agent.
// Claude Code supports Anthropic models only and is proprietary.
type ClaudeCode struct{}

func (a *ClaudeCode) Name() string { return "claude-code" }

func (a *ClaudeCode) Command(prompt string) string {
	return fmt.Sprintf("cd /workspace/repo && claude --print %q 2>&1", prompt)
}

func (a *ClaudeCode) ParseEvent(line string) *model.Event {
	return parseGenericLine(line)
}
