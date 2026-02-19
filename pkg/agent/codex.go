package agent

import (
	"fmt"

	"github.com/jxucoder/TeleCoder/pkg/model"
)

// Codex wraps the OpenAI Codex CLI agent.
// Codex supports OpenAI models only and is Apache 2.0 licensed.
type Codex struct{}

func (a *Codex) Name() string { return "codex" }

func (a *Codex) Command(prompt string) string {
	return fmt.Sprintf("cd /workspace/repo && codex exec --full-auto --ephemeral %q 2>&1", prompt)
}

func (a *Codex) ParseEvent(line string) *model.Event {
	return parseGenericLine(line)
}
