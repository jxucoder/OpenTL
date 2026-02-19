// Package dispatcher provides LLM-powered event routing for TeleCoder.
// Instead of keyword matching, it uses a lightweight LLM to decide whether
// an incoming event should spawn a session, get a reply, or be ignored.
package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LLM is a minimal interface for the dispatcher's routing decisions.
// Implementations call a lightweight model (e.g. Haiku, GPT-4o-mini).
type LLM interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Decision is the structured output from the dispatcher.
type Decision struct {
	Action string `json:"action"` // "spawn", "reply", "ignore"
	Repo   string `json:"repo,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	Agent  string `json:"agent,omitempty"`
	Reply  string `json:"reply,omitempty"`
}

// ChannelType identifies the source channel for system prompt selection.
type ChannelType string

const (
	ChannelSlack    ChannelType = "slack"
	ChannelTelegram ChannelType = "telegram"
	ChannelGitHub   ChannelType = "github"
	ChannelGeneric  ChannelType = "generic"
)

// Dispatcher routes incoming events using an LLM.
type Dispatcher struct {
	llm     LLM
	prompts map[ChannelType]string
}

// New creates a new Dispatcher with the given LLM client.
func New(llm LLM) *Dispatcher {
	return &Dispatcher{
		llm:     llm,
		prompts: defaultPrompts(),
	}
}

// SetPrompt overrides the system prompt for a specific channel type.
func (d *Dispatcher) SetPrompt(ch ChannelType, prompt string) {
	d.prompts[ch] = prompt
}

// Dispatch evaluates an incoming event and returns a routing decision.
func (d *Dispatcher) Dispatch(ctx context.Context, channel ChannelType, event string) (*Decision, error) {
	prompt, ok := d.prompts[channel]
	if !ok {
		prompt = d.prompts[ChannelGeneric]
	}

	response, err := d.llm.Complete(ctx, prompt, event)
	if err != nil {
		return nil, fmt.Errorf("dispatcher LLM call failed: %w", err)
	}

	decision, err := parseDecision(response)
	if err != nil {
		return &Decision{Action: "ignore"}, nil
	}

	return decision, nil
}

func parseDecision(response string) (*Decision, error) {
	response = strings.TrimSpace(response)

	if strings.HasPrefix(response, "```") {
		if idx := strings.Index(response, "\n"); idx >= 0 {
			response = response[idx+1:]
		}
		if idx := strings.LastIndex(response, "```"); idx >= 0 {
			response = response[:idx]
		}
		response = strings.TrimSpace(response)
	}

	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	response = response[start : end+1]

	var d Decision
	if err := json.Unmarshal([]byte(response), &d); err != nil {
		return nil, fmt.Errorf("parsing decision JSON: %w", err)
	}

	switch d.Action {
	case "spawn", "reply", "ignore":
	default:
		d.Action = "ignore"
	}

	return &d, nil
}

func defaultPrompts() map[ChannelType]string {
	base := `You are a routing engine for TeleCoder, a background coding agent.

You receive events from %s. For each event, decide:
- "spawn": create a coding session (requires repo and prompt)
- "reply": respond directly without a session (provide reply text)
- "ignore": do nothing

Return ONLY a JSON object:
{"action": "spawn"|"reply"|"ignore", "repo": "owner/repo", "prompt": "task description", "agent": "auto", "reply": "text"}

Rules:
- If the event is a clear coding task (bug fix, feature, refactor), use "spawn"
- If the event is a question that can be answered without code, use "reply"
- If the event is irrelevant (greetings, off-topic, spam), use "ignore"
- For "spawn", repo and prompt are required
- For "reply", reply is required
- agent is optional (default "auto"); set to "pi", "opencode", "claude-code", or "codex" if specified`

	return map[ChannelType]string{
		ChannelSlack:    fmt.Sprintf(base, "Slack messages"),
		ChannelTelegram: fmt.Sprintf(base, "Telegram messages"),
		ChannelGitHub:   fmt.Sprintf(base, "GitHub issues and comments"),
		ChannelGeneric:  fmt.Sprintf(base, "an external source"),
	}
}
