package dispatcher

import (
	"context"
	"fmt"

	"github.com/jxucoder/TeleCoder/pkg/model"
)

// ChainEvaluator checks whether a completed session should trigger a follow-up.
type ChainEvaluator struct {
	dispatcher *Dispatcher
	maxDepth   int
}

// NewChainEvaluator creates a chain evaluator using the given dispatcher.
func NewChainEvaluator(d *Dispatcher, maxDepth int) *ChainEvaluator {
	if maxDepth <= 0 {
		maxDepth = model.MaxChainDepth
	}
	return &ChainEvaluator{dispatcher: d, maxDepth: maxDepth}
}

// Evaluate checks a completed session and returns a Decision if a follow-up
// should be spawned. Returns nil if no follow-up is needed.
func (ce *ChainEvaluator) Evaluate(ctx context.Context, sess *model.Session) (*Decision, error) {
	if sess.ChainDepth >= ce.maxDepth {
		return nil, fmt.Errorf("chain depth limit reached (%d)", ce.maxDepth)
	}

	event := formatCompletionEvent(sess)
	dec, err := ce.dispatcher.Dispatch(ctx, ChannelGeneric, event)
	if err != nil {
		return nil, err
	}

	if dec.Action != "spawn" {
		return nil, nil
	}

	if dec.Repo == "" {
		dec.Repo = sess.Repo
	}

	return dec, nil
}

// MaxDepth returns the configured maximum chain depth.
func (ce *ChainEvaluator) MaxDepth() int {
	return ce.maxDepth
}

func formatCompletionEvent(sess *model.Session) string {
	switch sess.Result.Type {
	case model.ResultPR:
		return fmt.Sprintf("Session %s completed with a PR: %s\nRepo: %s\nPrompt: %s",
			sess.ID, sess.Result.PRUrl, sess.Repo, sess.Prompt)
	case model.ResultText:
		content := sess.Result.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		return fmt.Sprintf("Session %s completed with text result.\nRepo: %s\nPrompt: %s\nResult: %s",
			sess.ID, sess.Repo, sess.Prompt, content)
	default:
		return fmt.Sprintf("Session %s completed.\nRepo: %s\nPrompt: %s",
			sess.ID, sess.Repo, sess.Prompt)
	}
}
