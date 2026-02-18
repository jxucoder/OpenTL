// Package slack provides a Slack bot channel for TeleCoder using Socket Mode.
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

	"github.com/jxucoder/TeleCoder/pkg/eventbus"
	"github.com/jxucoder/TeleCoder/pkg/model"
	"github.com/jxucoder/TeleCoder/pkg/store"
)

// SessionCreator is the interface used to create and run sessions.
type SessionCreator interface {
	CreateAndRunSession(repo, prompt string) (*model.Session, error)
}

// Bot is the Slack Socket Mode bot for TeleCoder.
type Bot struct {
	api          *slack.Client
	socketClient *socketmode.Client
	store        store.SessionStore
	bus          eventbus.Bus
	sessions     SessionCreator
	defaultRepo  string
}

// NewBot creates a new Slack Socket Mode bot.
func NewBot(botToken, appToken, defaultRepo string, st store.SessionStore, bus eventbus.Bus, creator SessionCreator) *Bot {
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
		store:        st,
		bus:          bus,
		sessions:     creator,
		defaultRepo:  defaultRepo,
	}
}

// Name returns the channel name.
func (b *Bot) Name() string { return "slack" }

// Run connects to Slack via Socket Mode and processes events.
func (b *Bot) Run(ctx context.Context) error {
	go b.eventLoop(ctx)
	log.Println("Slack bot connecting via Socket Mode...")
	return b.socketClient.RunContext(ctx)
}

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
		b.socketClient.Ack(*evt.Request)

		if eventsAPIEvent.Type == slackevents.CallbackEvent {
			b.handleCallbackEvent(eventsAPIEvent.InnerEvent)
		}
	case socketmode.EventTypeInteractive:
		b.socketClient.Ack(*evt.Request)
	}
}

func (b *Bot) handleCallbackEvent(innerEvent slackevents.EventsAPIInnerEvent) {
	switch ev := innerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		go b.handleMention(ev)
	}
}

func (b *Bot) handleMention(ev *slackevents.AppMentionEvent) {
	prompt := ev.Text
	if idx := strings.Index(prompt, ">"); idx >= 0 {
		prompt = strings.TrimSpace(prompt[idx+1:])
	}

	threadTS := ev.TimeStamp
	if ev.ThreadTimeStamp != "" {
		threadTS = ev.ThreadTimeStamp
	}

	if prompt == "" {
		b.postThread(ev.Channel, threadTS,
			"Please provide a task description. Example:\n`@telecoder add rate limiting to the users API --repo owner/repo`")
		return
	}

	prompt, repo := model.ParseRepoFlag(prompt, b.defaultRepo)

	if repo == "" {
		b.postThread(ev.Channel, threadTS,
			"I couldn't determine which repository to work in. Please specify:\n`@telecoder [task] --repo owner/repo`")
		return
	}

	b.postThread(ev.Channel, threadTS,
		fmt.Sprintf(":rocket: *Starting task in `%s`...*\n> %s", repo, prompt))

	sess, err := b.sessions.CreateAndRunSession(repo, prompt)
	if err != nil {
		b.postThread(ev.Channel, threadTS,
			fmt.Sprintf(":x: Failed to start session: %s", err))
		return
	}

	b.postThread(ev.Channel, threadTS,
		fmt.Sprintf("Session `%s` created. I'll update you as it progresses.", sess.ID))

	go b.monitorSession(sess, ev.Channel, threadTS)
}

func (b *Bot) monitorSession(sess *model.Session, channel, threadTS string) {
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
			b.uploadSessionLog(channel, threadTS, sess.ID)

			updated, err := b.store.GetSession(sess.ID)
			if err != nil {
				log.Printf("Slack: failed to refresh session %s: %v", sess.ID, err)
				b.postThread(channel, threadTS,
					fmt.Sprintf(":white_check_mark: Session complete.\n%s", event.Data))
				return
			}

			b.postPRMessage(channel, threadTS, updated)
			return
		}
	}
}

func (b *Bot) uploadSessionLog(channel, threadTS, sessionID string) {
	events, err := b.store.GetEvents(sessionID, 0)
	if err != nil {
		log.Printf("Slack: failed to get events for session %s: %v", sessionID, err)
		return
	}

	if len(events) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("TeleCoder Session Log: %s\n", sessionID))
	sb.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")

	for _, e := range events {
		ts := e.CreatedAt.Format("15:04:05")
		tag := strings.ToUpper(e.Type)
		sb.WriteString(fmt.Sprintf("[%s] [%s] %s\n", ts, tag, e.Data))
	}

	content := sb.String()
	filename := fmt.Sprintf("telecoder-session-%s.log", sessionID)

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
		truncated := content
		if len(truncated) > 3000 {
			truncated = "...(truncated)...\n" + truncated[len(truncated)-3000:]
		}
		b.postThread(channel, threadTS,
			fmt.Sprintf("*Terminal Output (truncated):*\n```\n%s\n```", truncated))
	}
}

func (b *Bot) postPRMessage(channel, threadTS string, sess *model.Session) {
	if sess.PRUrl == "" {
		b.postThread(channel, threadTS, ":white_check_mark: Session complete (no PR created).")
		return
	}

	headerText := slack.NewTextBlockObject(slack.MarkdownType,
		fmt.Sprintf(":white_check_mark: *PR Ready!*\n<%s|%s>",
			sess.PRUrl, fmt.Sprintf("PR #%d: %s", sess.PRNumber, model.Truncate(sess.Prompt, 60))),
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
		b.postThread(channel, threadTS,
			fmt.Sprintf(":white_check_mark: *PR Ready!*\n%s", sess.PRUrl))
	}
}

func (b *Bot) postThread(channel, threadTS, text string) {
	_, _, err := b.api.PostMessage(channel,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		log.Printf("Slack: failed to post message to %s: %v", channel, err)
	}
}
