package agent

import (
	"fmt"

	"github.com/jxucoder/TeleCoder/pkg/model"
)

// Pi wraps the Pi CLI agent.
// Pi is model-agnostic (15+ providers), MIT licensed, and produces rich JSONL output.
type Pi struct{}

func (a *Pi) Name() string { return "pi" }

func (a *Pi) Command(prompt string) string {
	return fmt.Sprintf("cd /workspace/repo && pi -p %q --mode json 2>&1", prompt)
}

func (a *Pi) ParseEvent(line string) *model.Event {
	return parseGenericLine(line)
}
