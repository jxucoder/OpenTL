// Package telegram provides a Telegram bot channel for TeleCoder.
package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/jxucoder/TeleCoder/pkg/eventbus"
	"github.com/jxucoder/TeleCoder/pkg/model"
	"github.com/jxucoder/TeleCoder/pkg/store"
)

// ChatSessionCreator is the interface the engine implements for chat sessions.
type ChatSessionCreator interface {
	CreateAndRunSession(repo, prompt string) (*model.Session, error)
	CreateChatSession(repo string) (*model.Session, error)
	SendChatMessage(sessionID, content string) (*model.Message, error)
	CreatePRFromChat(sessionID string) (string, int, error)
}

type chatState struct {
	sessionID string
	repo      string
}

// Bot is the Telegram bot for TeleCoder.
type Bot struct {
	api          *tgbotapi.BotAPI
	store        store.SessionStore
	bus          eventbus.Bus
	sessions     ChatSessionCreator
	defaultRepo  string
	chatMu       sync.RWMutex
	chatSessions map[int64]*chatState
}

// NewBot creates a new Telegram bot.
func NewBot(token, defaultRepo string, st store.SessionStore, bus eventbus.Bus, creator ChatSessionCreator) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("creating Telegram bot: %w", err)
	}

	log.Printf("Telegram bot authorized as @%s", api.Self.UserName)

	return &Bot{
		api:          api,
		store:        st,
		bus:          bus,
		sessions:     creator,
		defaultRepo:  defaultRepo,
		chatSessions: make(map[int64]*chatState),
	}, nil
}

// Name returns the channel name.
func (b *Bot) Name() string { return "telegram" }

// Run starts the long-polling loop. Blocks until ctx is canceled.
func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	log.Println("Telegram bot listening for messages...")

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			if update.Message != nil {
				go b.handleMessage(update.Message)
			}
		}
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	chatID := msg.Chat.ID

	if strings.HasPrefix(text, "/") {
		b.handleCommand(chatID, msg.MessageID, text)
		return
	}

	b.handleChatMessage(chatID, msg.MessageID, text)
}

func (b *Bot) handleCommand(chatID int64, replyTo int, text string) {
	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])
	if at := strings.Index(cmd, "@"); at >= 0 {
		cmd = cmd[:at]
	}

	switch cmd {
	case "/start", "/help":
		b.sendHelp(chatID, replyTo)
	case "/new":
		args := strings.Join(parts[1:], " ")
		_, repo := model.ParseRepoFlag(args, b.defaultRepo)
		if repo == b.defaultRepo && len(parts) > 1 && strings.Contains(parts[1], "/") {
			repo = parts[1]
		}
		b.startNewSession(chatID, replyTo, repo)
	case "/pr":
		b.handlePR(chatID, replyTo)
	case "/diff":
		b.handleDiff(chatID, replyTo)
	case "/status":
		b.handleStatus(chatID, replyTo)
	case "/stop":
		b.handleStop(chatID, replyTo)
	case "/run":
		prompt := strings.TrimSpace(strings.TrimPrefix(text, parts[0]))
		if prompt == "" {
			b.sendReply(chatID, replyTo, "Usage: `/run fix the typo \\-\\-repo owner/repo`")
			return
		}
		b.handleLegacyRun(chatID, replyTo, prompt)
	default:
		b.sendReply(chatID, replyTo, fmt.Sprintf("Unknown command `%s`\\. Try /help", escapeMarkdown(cmd)))
	}
}

func (b *Bot) handleChatMessage(chatID int64, replyTo int, text string) {
	b.chatMu.RLock()
	state := b.chatSessions[chatID]
	b.chatMu.RUnlock()

	if state == nil {
		prompt, repo := model.ParseRepoFlag(text, b.defaultRepo)
		if repo == "" {
			b.sendReply(chatID, replyTo,
				"No active session\\. Start one with:\n"+
					"`/new \\-\\-repo owner/repo`\n\n"+
					"Or send a message with `\\-\\-repo`:\n"+
					"`fix the bug \\-\\-repo owner/repo`")
			return
		}
		b.startNewSessionWithMessage(chatID, replyTo, repo, prompt)
		return
	}

	b.sendChatAction(chatID)

	_, err := b.sessions.SendChatMessage(state.sessionID, text)
	if err != nil {
		b.sendReply(chatID, replyTo,
			fmt.Sprintf("⚠️ %s", escapeMarkdown(err.Error())))
		return
	}

	b.monitorEvents(state.sessionID, chatID, replyTo, monitorOpts{
		stopOnIdle:   true,
		bufferOutput: true,
		showDone:     true,
	})
}

func (b *Bot) startNewSession(chatID int64, replyTo int, repo string) {
	if repo == "" {
		b.sendReply(chatID, replyTo,
			"Please specify a repo:\n`/new \\-\\-repo owner/repo`\n\nor set `TELEGRAM_DEFAULT_REPO`")
		return
	}

	b.detachSession(chatID)

	b.sendReply(chatID, replyTo,
		fmt.Sprintf("⚙ Starting new session for `%s`\\.\\.\\.", escapeMarkdown(repo)))

	sess, err := b.sessions.CreateChatSession(repo)
	if err != nil {
		b.sendReply(chatID, replyTo,
			fmt.Sprintf("❌ Failed to create session: %s", escapeMarkdown(err.Error())))
		return
	}

	b.chatMu.Lock()
	b.chatSessions[chatID] = &chatState{sessionID: sess.ID, repo: repo}
	b.chatMu.Unlock()

	b.monitorEvents(sess.ID, chatID, replyTo, monitorOpts{
		stopOnIdle:   true,
		bufferOutput: true,
		showDone:     true,
	})
}

func (b *Bot) startNewSessionWithMessage(chatID int64, replyTo int, repo, prompt string) {
	b.detachSession(chatID)

	b.sendReply(chatID, replyTo,
		fmt.Sprintf("⚙ Starting session for `%s`\\.\\.\\.", escapeMarkdown(repo)))
	b.sendChatAction(chatID)

	sess, err := b.sessions.CreateChatSession(repo)
	if err != nil {
		b.sendReply(chatID, replyTo,
			fmt.Sprintf("❌ Failed to create session: %s", escapeMarkdown(err.Error())))
		return
	}

	b.chatMu.Lock()
	b.chatSessions[chatID] = &chatState{sessionID: sess.ID, repo: repo}
	b.chatMu.Unlock()

	b.monitorEvents(sess.ID, chatID, replyTo, monitorOpts{stopOnIdle: true})

	_, err = b.sessions.SendChatMessage(sess.ID, prompt)
	if err != nil {
		b.sendReply(chatID, replyTo,
			fmt.Sprintf("⚠️ %s", escapeMarkdown(err.Error())))
		return
	}

	b.monitorEvents(sess.ID, chatID, replyTo, monitorOpts{
		stopOnIdle:   true,
		bufferOutput: true,
		showDone:     true,
	})
}

func (b *Bot) handlePR(chatID int64, replyTo int) {
	state := b.getChatState(chatID)
	if state == nil {
		b.sendReply(chatID, replyTo, "No active session\\. Start one first with /new")
		return
	}

	b.sendReply(chatID, replyTo, "⚙ Creating pull request\\.\\.\\.")
	b.sendChatAction(chatID)

	prURL, prNumber, err := b.sessions.CreatePRFromChat(state.sessionID)
	if err != nil {
		b.sendReply(chatID, replyTo,
			fmt.Sprintf("❌ %s", escapeMarkdown(err.Error())))
		return
	}

	b.sendReply(chatID, replyTo,
		fmt.Sprintf("✅ *PR Ready\\!*\n\n[PR \\#%d](%s)\n\nSession `%s`",
			prNumber, escapeMarkdown(prURL), state.sessionID))

	b.detachSession(chatID)
}

func (b *Bot) handleDiff(chatID int64, replyTo int) {
	state := b.getChatState(chatID)
	if state == nil {
		b.sendReply(chatID, replyTo, "No active session\\.")
		return
	}

	sess, err := b.store.GetSession(state.sessionID)
	if err != nil || sess.ContainerID == "" {
		b.sendReply(chatID, replyTo, "Session has no active container\\.")
		return
	}

	b.sendReply(chatID, replyTo, "⚙ Fetching diff\\.\\.\\.")

	events, _ := b.store.GetEvents(state.sessionID, 0)
	var lastOutput string
	for _, e := range events {
		if e.Type == "output" {
			lastOutput = e.Data
		}
	}
	if lastOutput != "" && len(lastOutput) > 3500 {
		lastOutput = lastOutput[:3500] + "\n... (truncated)"
	}
	if lastOutput == "" {
		lastOutput = "(no changes detected yet)"
	}

	b.sendReply(chatID, replyTo,
		fmt.Sprintf("```\n%s\n```", escapeMarkdown(lastOutput)))
}

func (b *Bot) handleStatus(chatID int64, replyTo int) {
	state := b.getChatState(chatID)
	if state == nil {
		b.sendReply(chatID, replyTo, "No active session\\. Start one with /new")
		return
	}

	sess, err := b.store.GetSession(state.sessionID)
	if err != nil {
		b.sendReply(chatID, replyTo, "❌ Could not fetch session info\\.")
		return
	}

	msgs, _ := b.store.GetMessages(state.sessionID)
	userMsgCount := 0
	for _, m := range msgs {
		if m.Role == "user" {
			userMsgCount++
		}
	}

	b.sendReply(chatID, replyTo, fmt.Sprintf(
		"*Session* `%s`\n"+
			"*Repo:* `%s`\n"+
			"*Status:* `%s`\n"+
			"*Branch:* `%s`\n"+
			"*Messages:* %d",
		sess.ID,
		escapeMarkdown(sess.Repo),
		escapeMarkdown(string(sess.Status)),
		escapeMarkdown(sess.Branch),
		userMsgCount,
	))
}

func (b *Bot) handleStop(chatID int64, replyTo int) {
	state := b.getChatState(chatID)
	if state == nil {
		b.sendReply(chatID, replyTo, "No active session\\.")
		return
	}

	b.detachSession(chatID)
	b.sendReply(chatID, replyTo, "✅ Session stopped\\.")
}

func (b *Bot) handleLegacyRun(chatID int64, replyTo int, text string) {
	prompt, repo := model.ParseRepoFlag(text, b.defaultRepo)

	if repo == "" {
		b.sendReply(chatID, replyTo,
			"Please specify a repo: `/run fix typo \\-\\-repo owner/repo`")
		return
	}

	b.sendReply(chatID, replyTo,
		fmt.Sprintf("⚙ Starting task in `%s`\\.\\.\\.\n> %s",
			escapeMarkdown(repo), escapeMarkdown(prompt)))

	sess, err := b.sessions.CreateAndRunSession(repo, prompt)
	if err != nil {
		b.sendReply(chatID, replyTo,
			fmt.Sprintf("❌ Failed: %s", escapeMarkdown(err.Error())))
		return
	}

	b.sendReply(chatID, replyTo,
		fmt.Sprintf("Session `%s` created\\. I'll send the PR when it's done\\.", sess.ID))

	go b.monitorEvents(sess.ID, chatID, replyTo, monitorOpts{showDonePR: true})
}

// --- Event monitoring ---

type monitorOpts struct {
	stopOnIdle   bool
	bufferOutput bool
	showDone     bool
	showDonePR   bool
}

func (b *Bot) monitorEvents(sessionID string, chatID int64, replyTo int, opts monitorOpts) {
	ch := b.bus.Subscribe(sessionID)
	defer b.bus.Unsubscribe(sessionID, ch)

	var outputBuf strings.Builder
	var flushTicker *time.Ticker
	if opts.bufferOutput {
		flushTicker = time.NewTicker(2 * time.Second)
		defer flushTicker.Stop()
	}

	flush := func() {
		if outputBuf.Len() == 0 {
			return
		}
		text := outputBuf.String()
		outputBuf.Reset()
		if len(text) > 3800 {
			text = text[len(text)-3800:]
			text = "\\.\\.\\.\n" + text
		}
		b.sendReply(chatID, replyTo, fmt.Sprintf("```\n%s\n```", escapeMarkdown(text)))
	}

	flushC := (<-chan time.Time)(nil)
	if flushTicker != nil {
		flushC = flushTicker.C
	}

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				flush()
				return
			}

			switch event.Type {
			case "status":
				flush()
				if opts.stopOnIdle && (event.Data == "Ready" || strings.HasPrefix(event.Data, "Ready")) {
					b.sendReply(chatID, replyTo,
						fmt.Sprintf("✅ %s", escapeMarkdown(event.Data)))
					return
				}
				b.sendReply(chatID, replyTo,
					fmt.Sprintf("⚙ %s", escapeMarkdown(event.Data)))
				b.sendChatAction(chatID)

			case "output":
				outputBuf.WriteString(event.Data)
				outputBuf.WriteString("\n")

			case "error":
				flush()
				b.sendReply(chatID, replyTo,
					fmt.Sprintf("❌ %s", escapeMarkdown(event.Data)))
				return

			case "done":
				flush()
				if opts.showDonePR {
					updated, err := b.store.GetSession(sessionID)
					if err != nil {
						b.sendReply(chatID, replyTo, fmt.Sprintf("✅ Done\\.\n%s", escapeMarkdown(event.Data)))
						return
					}
					b.sendPRMessage(chatID, replyTo, updated)
					return
				}
				if opts.showDone {
					b.sendReply(chatID, replyTo, fmt.Sprintf("✅ Done: %s", escapeMarkdown(event.Data)))
				}
				return
			}

		case <-flushC:
			flush()
		}
	}
}

// --- Helpers ---

func (b *Bot) getChatState(chatID int64) *chatState {
	b.chatMu.RLock()
	defer b.chatMu.RUnlock()
	return b.chatSessions[chatID]
}

func (b *Bot) detachSession(chatID int64) {
	b.chatMu.Lock()
	delete(b.chatSessions, chatID)
	b.chatMu.Unlock()
}

func (b *Bot) sendHelp(chatID int64, replyTo int) {
	b.sendReply(chatID, replyTo, ""+
		"*TeleCoder* — Your AI coding agent\\.\n\n"+
		"*Chat mode \\(multi\\-turn\\):*\n"+
		"Just send a message\\! The first message starts a session\\.\n"+
		"`fix the login bug \\-\\-repo owner/repo`\n"+
		"Then send follow\\-ups:\n"+
		"`also add tests for the fix`\n\n"+
		"*In a forum group:* each topic is a separate session\\.\n"+
		"Create a new topic \\= start a new session\\.\n\n"+
		"*Commands:*\n"+
		"/new \\-\\- Start a fresh session\n"+
		"/pr \\-\\- Create a PR from current changes\n"+
		"/diff \\-\\- Show recent output\n"+
		"/status \\-\\- Show session info\n"+
		"/stop \\-\\- Stop the current session\n"+
		"/run \\<task\\> \\-\\- One\\-shot mode \\(task → PR\\)\n"+
		"/help \\-\\- Show this message")
}

func (b *Bot) sendPRMessage(chatID int64, replyTo int, sess *model.Session) {
	if sess.PRUrl == "" {
		b.sendReply(chatID, replyTo, "✅ Session complete \\(no PR created\\)\\.")
		return
	}

	text := fmt.Sprintf(
		"✅ *PR Ready\\!*\n\n"+
			"[PR \\#%d: %s](%s)\n\n"+
			"Session `%s` \\| Repo `%s` \\| Branch `%s`",
		sess.PRNumber,
		escapeMarkdown(model.Truncate(sess.Prompt, 60)),
		escapeMarkdown(sess.PRUrl),
		sess.ID,
		escapeMarkdown(sess.Repo),
		escapeMarkdown(sess.Branch),
	)

	b.sendReply(chatID, replyTo, text)
}

func (b *Bot) sendChatAction(chatID int64) {
	action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	b.api.Send(action)
}

func (b *Bot) sendReply(chatID int64, replyTo int, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyToMessageID = replyTo
	msg.ParseMode = "MarkdownV2"

	if _, err := b.api.Send(msg); err != nil {
		log.Printf("Telegram: failed to send message: %v", err)
		msg.ParseMode = ""
		msg.Text = stripMarkdown(text)
		b.api.Send(msg)
	}
}

func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]",
		"(", "\\(", ")", "\\)", "~", "\\~", "`", "\\`",
		">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-",
		"=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}",
		".", "\\.", "!", "\\!",
	)
	return replacer.Replace(s)
}

func stripMarkdown(s string) string {
	r := strings.NewReplacer(
		"\\*", "*", "\\_", "_", "\\[", "[", "\\]", "]",
		"\\(", "(", "\\)", ")", "\\~", "~", "\\`", "`",
		"\\>", ">", "\\#", "#", "\\+", "+", "\\-", "-",
		"\\=", "=", "\\|", "|", "\\{", "{", "\\}", "}",
		"\\.", ".", "\\!", "!",
	)
	return r.Replace(s)
}
