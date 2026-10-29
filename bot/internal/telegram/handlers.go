package telegram

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// handle adapts a message handler to the library and logs the outcome. Logs
// carry no user data: no IDs, usernames or message texts. The raw Telegram
// language_code is logged at debug level to check how the language is picked.
func (b *Bot) handle(name string, h func(ctx context.Context, msg *models.Message) error) bot.HandlerFunc {
	return func(ctx context.Context, _ *bot.Bot, upd *models.Update) {
		start := time.Now()
		if err := h(ctx, upd.Message); err != nil {
			b.log.Error("handle message", "handler", name, "err", err)
			return
		}
		b.log.Debug("message handled", "handler", name, "lang_code", upd.Message.From.LanguageCode, "duration", time.Since(start))
	}
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
	return b.reply(ctx, msg, i18n.For(i18n.FromTelegram(msg.From.LanguageCode)).Help)
}

func (b *Bot) unknown(ctx context.Context, msg *models.Message) error {
	return b.reply(ctx, msg, i18n.For(i18n.FromTelegram(msg.From.LanguageCode)).Unknown)
}

// reply sends plain text to the chat of msg, without link previews.
func (b *Bot) reply(ctx context.Context, msg *models.Message, text string) error {
	_, err := b.api.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:             msg.Chat.ID,
		Text:               text,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
	})
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}
