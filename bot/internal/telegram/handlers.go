package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// handle adapts a message handler to the library and logs the outcome. Logs
// carry no user data: no IDs, usernames or message texts. The raw Telegram
// language_code is logged at debug level to check how the language is picked.
func (b *Bot) handle(name string, h func(ctx context.Context, msg *models.Message) error) bot.HandlerFunc {
	return func(ctx context.Context, _ *bot.Bot, upd *models.Update) {
		start := time.Now()
		if strings.HasPrefix(upd.Message.Text, "/") {
			// A command ends the wait for a nickname; /link starts it again.
			b.pending.clear(upd.Message.From.ID)
		}
		if err := h(ctx, upd.Message); err != nil {
			b.log.Error("handle message", "handler", name, "err", err)
			return
		}
		b.log.Debug("message handled", "handler", name, "lang_code", upd.Message.From.LanguageCode, "duration", time.Since(start))
	}
}

// handleButton adapts a button handler to the library and logs the outcome
// with no user data.
func (b *Bot) handleButton(name string, h func(ctx context.Context, cq *models.CallbackQuery) error) bot.HandlerFunc {
	return func(ctx context.Context, _ *bot.Bot, upd *models.Update) {
		start := time.Now()
		if err := h(ctx, upd.CallbackQuery); err != nil {
			b.log.Error("handle button", "handler", name, "err", err)
			return
		}
		b.log.Debug("button handled", "handler", name, "duration", time.Since(start))
	}
}

// handleDefault gets the updates no other handler took: messages that are not
// known commands, and presses of buttons the bot no longer has.
func (b *Bot) handleDefault(ctx context.Context, api *bot.Bot, upd *models.Update) {
	if upd.CallbackQuery != nil {
		b.handleButton("unknown", func(ctx context.Context, cq *models.CallbackQuery) error {
			return b.answer(ctx, cq)
		})(ctx, api, upd)
		return
	}
	b.handle("default", b.text)(ctx, api, upd)
}

// start puts the user on the beta waitlist. A deep-link payload after
// /start is ignored for now.
func (b *Bot) start(ctx context.Context, msg *models.Message) error {
	lang := i18n.FromTelegram(msg.From.LanguageCode)
	texts := i18n.For(lang)
	joined, err := b.users.JoinWaitlist(ctx, storage.User{
		TelegramID: msg.From.ID,
		Username:   msg.From.Username,
		Language:   string(lang),
	}, b.now())
	if err != nil {
		return errors.Join(fmt.Errorf("join waitlist: %w", err), b.reply(ctx, msg, texts.Failure))
	}
	if !joined {
		return b.reply(ctx, msg, texts.StartAlready)
	}
	b.log.Info("user joined the waitlist", "lang", lang)
	return b.reply(ctx, msg, texts.StartJoined)
}

func (b *Bot) help(ctx context.Context, msg *models.Message) error {
	return b.reply(ctx, msg, messagesFor(msg.From).Help)
}

// text handles a message that is not a known command: the nickname the bot
// asked for after /link, a FACEIT profile link sent on its own, or anything
// else, which gets a hint.
func (b *Bot) text(ctx context.Context, msg *models.Message) error {
	text := strings.TrimSpace(msg.Text)
	if text != "" && !strings.HasPrefix(text, "/") {
		if b.pending.take(msg.From.ID, b.now()) {
			return b.offerLink(ctx, msg, text)
		}
		if _, ok := faceit.ParseProfileLink(text); ok {
			return b.offerLink(ctx, msg, text)
		}
	}
	return b.reply(ctx, msg, messagesFor(msg.From).Unknown)
}

// messagesFor returns the texts in the language of the user.
func messagesFor(u *models.User) i18n.Messages {
	return i18n.For(i18n.FromTelegram(u.LanguageCode))
}

// reply sends plain text to the chat of msg, without link previews.
func (b *Bot) reply(ctx context.Context, msg *models.Message, text string) error {
	return b.send(ctx, msg.Chat.ID, text, nil)
}

// send sends plain text without link previews, with buttons if markup is
// not nil.
func (b *Bot) send(ctx context.Context, chatID int64, text string, markup models.ReplyMarkup) error {
	params := &bot.SendMessageParams{
		ChatID:             chatID,
		Text:               text,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
	}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	if _, err := b.api.SendMessage(ctx, params); err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}

// replaceButtonMessage replaces the text of the message with the pressed
// button, which also removes its buttons. If the message can no longer be
// edited, the text comes as a new message.
func (b *Bot) replaceButtonMessage(ctx context.Context, cq *models.CallbackQuery, text string) error {
	msg := cq.Message.Message
	if msg == nil {
		chat := buttonChat(cq)
		if chat == nil {
			return nil
		}
		return b.send(ctx, chat.ID, text, nil)
	}
	_, err := b.api.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:             msg.Chat.ID,
		MessageID:          msg.ID,
		Text:               text,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
	})
	// A second press of the same button would set the same text again.
	if err != nil && !(errors.Is(err, bot.ErrorBadRequest) && strings.Contains(err.Error(), "message is not modified")) {
		return fmt.Errorf("edit message: %w", err)
	}
	return nil
}

// answer stops the loading animation on the pressed button.
func (b *Bot) answer(ctx context.Context, cq *models.CallbackQuery) error {
	if _, err := b.api.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID}); err != nil {
		return fmt.Errorf("answer callback query: %w", err)
	}
	return nil
}

// typing shows "typing…" while a slow answer is on its way. A failure only
// loses the hint, so it is logged and ignored.
func (b *Bot) typing(ctx context.Context, chatID int64) {
	if _, err := b.api.SendChatAction(ctx, &bot.SendChatActionParams{ChatID: chatID, Action: models.ChatActionTyping}); err != nil {
		b.log.Debug("send chat action", "err", err)
	}
}
