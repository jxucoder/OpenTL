// Package slack provides a Slack bot integration for OpenTL using Socket Mode.
//
// Socket Mode connects to Slack via WebSocket -- no public URL needed.
// The bot listens for @mentions, creates coding sessions, posts status
// updates in a Slack thread, uploads terminal logs, and delivers the PR link.
package slack

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/jxucoder/opentl/internal/session"
)

// SessionCreator is the interface used to create and run sessions.
// The server implements this so the bot doesn't depend on the full server.
type SessionCreator interface {
	CreateAndRunSession(repo, prompt string) (*session.Session, error)
}

// Bot is the Slack Socket Mode bot for OpenTL.
type Bot struct {
	api          *slack.Client
	socketClient *socketmode.Client
	store        *session.Store
	bus          *session.EventBus
	sessions     SessionCreator
	defaultRepo  string
}

// NewBot creates a new Slack Socket Mode bot.
func NewBot(botToken, appToken, defaultRepo string, store *session.Store, bus *session.EventBus, creator SessionCreator) *Bot {
	api := slack.New(
		botToken,
		slack.OptionAppLevelToken(appToken),
	)

	socketClient := socketmode.New(
		api,
		socketmode.OptionLog(log.New(log.Writer(), "slack-socketmode: ", log.LstdFlags)),
	)

	return &Bot{
		api:          api,
		socketClient: socketClient,
		store:        store,
		bus:          bus,
		sessions:     creator,
		defaultRepo:  defaultRepo,
	}
}

// Run connects to Slack via Socket Mode and processes events.
// It blocks until the context is canceled or a fatal error occurs.
func (b *Bot) Run(ctx context.Context) error {
	go b.eventLoop(ctx)
	log.Println("Slack bot connecting via Socket Mode...")
	return b.socketClient.RunContext(ctx)
}

// eventLoop reads events from the Socket Mode client and dispatches them.
func (b *Bot) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-b.socketClient.Events:
			if !ok {
				return
			}
			b.handleEvent(evt)
		}
	}
}

// handleEvent dispatches a single Socket Mode event.
func (b *Bot) handleEvent(evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		log.Println("Slack: connecting...")
	case socketmode.EventTypeConnected:
		log.Println("Slack: connected")
	case socketmode.EventTypeConnectionError:
		log.Println("Slack: connection error, will retry...")
	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		// Acknowledge immediately (Slack requires ack within 3 seconds).
		b.socketClient.Ack(*evt.Request)

		if eventsAPIEvent.Type == slackevents.CallbackEvent {
			b.handleCallbackEvent(eventsAPIEvent.InnerEvent)
		}
	case socketmode.EventTypeInteractive:
		// Acknowledge interactive events even if we don't handle them yet.
		b.socketClient.Ack(*evt.Request)
	}
}

// handleCallbackEvent routes inner Events API events.
func (b *Bot) handleCallbackEvent(innerEvent slackevents.EventsAPIInnerEvent) {
	switch ev := innerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		go b.handleMention(ev)
	}
}

// handleMention processes an @mention of the bot.
func (b *Bot) handleMention(ev *slackevents.AppMentionEvent) {
	// Strip the bot mention (<@U12345>) from the text to get the prompt.
	prompt := ev.Text
	if idx := strings.Index(prompt, ">"); idx >= 0 {
		prompt = strings.TrimSpace(prompt[idx+1:])
	}

	// Determine the thread timestamp -- reply in thread of the original message.
	threadTS := ev.TimeStamp
	if ev.ThreadTimeStamp != "" {
		threadTS = ev.ThreadTimeStamp
	}

	if prompt == "" {
		b.postThread(ev.Channel, threadTS,
			"Please provide a task description. Example:\n`@opentl add rate limiting to the users API --repo owner/repo`")
		return
	}

	prompt, repo := session.ParseRepoFlag(prompt, b.defaultRepo)

	if repo == "" {
		b.postThread(ev.Channel, threadTS,
			"I couldn't determine which repository to work in. Please specify:\n`@opentl [task] --repo owner/repo`")
		return
	}

	// Post initial acknowledgement.
	b.postThread(ev.Channel, threadTS,
		fmt.Sprintf(":rocket: *Starting task in `%s`...*\n> %s", repo, prompt))

	// Create and start the session.
	sess, err := b.sessions.CreateAndRunSession(repo, prompt)
	if err != nil {
		b.postThread(ev.Channel, threadTS,
			fmt.Sprintf(":x: Failed to start session: %s", err))
		return
	}

	b.postThread(ev.Channel, threadTS,
		fmt.Sprintf("Session `%s` created. I'll update you as it progresses.", sess.ID))

	// Monitor the session in the background and post updates.
	go b.monitorSession(sess, ev.Channel, threadTS)
}

// monitorSession subscribes to session events and posts key updates to the Slack thread.
// When the session completes, it uploads the full terminal log and posts the PR link.
func (b *Bot) monitorSession(sess *session.Session, channel, threadTS string) {
	ch := b.bus.Subscribe(sess.ID)
	defer b.bus.Unsubscribe(sess.ID, ch)

	for event := range ch {
		switch event.Type {
		case "status":
			b.postThread(channel, threadTS,
				fmt.Sprintf(":gear: %s", event.Data))

		case "error":
			b.postThread(channel, threadTS,
				fmt.Sprintf(":x: *Error:* %s", event.Data))

		case "done":
			// Upload the full terminal log.
			b.uploadSessionLog(channel, threadTS, sess.ID)

			// Refresh session to get PR info.
			updated, err := b.store.GetSession(sess.ID)
			if err != nil {
				log.Printf("Slack: failed to refresh session %s: %v", sess.ID, err)
				b.postThread(channel, threadTS,
					fmt.Sprintf(":white_check_mark: Session complete.\n%s", event.Data))
				return
			}

			b.postPRMessage(channel, threadTS, updated)
			return

			// Skip "output" events to avoid flooding the thread.
			// They are captured in the uploaded log file.
		}
	}
}

// uploadSessionLog fetches all events for a session, formats them as a log,
// and uploads the file to the Slack thread.
func (b *Bot) uploadSessionLog(channel, threadTS, sessionID string) {
	events, err := b.store.GetEvents(sessionID, 0)
	if err != nil {
		log.Printf("Slack: failed to get events for session %s: %v", sessionID, err)
		return
	}

	if len(events) == 0 {
		return
	}

	// Format the events as a readable log.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("OpenTL Session Log: %s\n", sessionID))
	sb.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")

	for _, e := range events {
		ts := e.CreatedAt.Format("15:04:05")
		tag := strings.ToUpper(e.Type)
		sb.WriteString(fmt.Sprintf("[%s] [%s] %s\n", ts, tag, e.Data))
	}

	content := sb.String()
	filename := fmt.Sprintf("opentl-session-%s.log", sessionID)

	_, err = b.api.UploadFileV2(slack.UploadFileV2Parameters{
		Content:         content,
		Filename:        filename,
		FileSize:        len(content),
		Title:           fmt.Sprintf("Terminal Output - Session %s", sessionID),
		Channel:         channel,
		ThreadTimestamp: threadTS,
	})
	if err != nil {
		log.Printf("Slack: failed to upload log file for session %s: %v", sessionID, err)
		// Fall back to posting a truncated log in the thread.
		truncated := content
		if len(truncated) > 3000 {
			truncated = "...(truncated)...\n" + truncated[len(truncated)-3000:]
		}
		b.postThread(channel, threadTS,
			fmt.Sprintf("*Terminal Output (truncated):*\n```\n%s\n```", truncated))
	}
}

// postPRMessage posts a rich message with the PR link to the Slack thread.
func (b *Bot) postPRMessage(channel, threadTS string, sess *session.Session) {
	if sess.PRUrl == "" {
		b.postThread(channel, threadTS, ":white_check_mark: Session complete (no PR created).")
		return
	}

	// Build a rich Block Kit message.
	headerText := slack.NewTextBlockObject(slack.MarkdownType,
		fmt.Sprintf(":white_check_mark: *PR Ready!*\n<%s|%s>",
			sess.PRUrl, fmt.Sprintf("PR #%d: %s", sess.PRNumber, session.Truncate(sess.Prompt, 60))),
		false, false)
	headerSection := slack.NewSectionBlock(headerText, nil, nil)

	contextElements := []slack.MixedElement{
		slack.NewTextBlockObject(slack.MarkdownType,
			fmt.Sprintf("Session `%s` | Repo `%s` | Branch `%s`", sess.ID, sess.Repo, sess.Branch),
			false, false),
	}
	contextBlock := slack.NewContextBlock("", contextElements...)

	_, _, err := b.api.PostMessage(channel,
		slack.MsgOptionBlocks(headerSection, slack.NewDividerBlock(), contextBlock),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		log.Printf("Slack: failed to post PR message: %v", err)
		// Fall back to plain text.
		b.postThread(channel, threadTS,
			fmt.Sprintf(":white_check_mark: *PR Ready!*\n%s", sess.PRUrl))
	}
}

// postThread sends a plain text message as a thread reply.
func (b *Bot) postThread(channel, threadTS, text string) {
	_, _, err := b.api.PostMessage(channel,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		log.Printf("Slack: failed to post message to %s: %v", channel, err)
	}
}
