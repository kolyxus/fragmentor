package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// The card buttons carry "link:<FACEIT player ID>" for Yes and "link:no" for
// No. Telegram allows 64 bytes of button data; a player ID takes 36.
const (
	linkButtonPrefix = "link:"
	linkButtonNo     = linkButtonPrefix + "no"
)

// link handles /link. With a nickname or a profile link after the command it
// shows the "is this you?" card; without one it asks for it and takes the
// next message as the answer.
func (b *Bot) link(ctx context.Context, msg *models.Message) error {
	input := commandArgs(msg.Text)
	if input == "" {
		b.pending.set(msg.From.ID, b.now())
		return b.reply(ctx, msg, messagesFor(msg.From).LinkAsk)
	}
	return b.offerLink(ctx, msg, input)
}

// commandArgs returns what follows the command: "kolyx" for "/link kolyx".
func commandArgs(text string) string {
	i := strings.IndexFunc(text, unicode.IsSpace)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(text[i:])
}

// offerLink finds the FACEIT account by the nickname or profile link in input
// and shows the card with its nickname, level and ELO, and the Yes and No
// buttons: as the caption of the player's avatar, or as text if there is no
// avatar or Telegram could not take it. If the input is wrong, the user may
// send another one straight away, without /link.
func (b *Bot) offerLink(ctx context.Context, msg *models.Message, input string) error {
	user := msg.From
	texts := messagesFor(user)
	nickname, err := faceit.ParseNickname(input)
	if err != nil {
		b.pending.set(user.ID, b.now())
		return b.reply(ctx, msg, texts.LinkBadInput)
	}

	b.typing(ctx, msg.Chat.ID)
	player, err := b.faceit.PlayerByNickname(ctx, nickname)
	switch {
	case errors.Is(err, faceit.ErrNotFound):
		b.pending.set(user.ID, b.now())
		return b.reply(ctx, msg, fmt.Sprintf(texts.LinkNotFound, nickname))
	case err != nil:
		return errors.Join(fmt.Errorf("find player: %w", err), b.reply(ctx, msg, texts.FaceitDown))
	case !player.HasCS2:
		b.pending.set(user.ID, b.now())
		return b.reply(ctx, msg, fmt.Sprintf(texts.LinkNoCS2, player.Nickname))
	}

	current, err := b.users.FaceitAccount(ctx, user.ID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
	case err != nil:
		return errors.Join(fmt.Errorf("load faceit account: %w", err), b.reply(ctx, msg, texts.Failure))
	case current.PlayerID == player.ID:
		return b.reply(ctx, msg, fmt.Sprintf(texts.LinkAlready, player.Nickname))
	}

	card := fmt.Sprintf(texts.LinkCard, player.Nickname, player.SkillLevel, player.Elo)
	if current.PlayerID != "" {
		card += "\n\n" + fmt.Sprintf(texts.LinkCardReplace, current.Nickname)
	}
	buttons := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: texts.LinkYes, CallbackData: linkButtonPrefix + player.ID},
		{Text: texts.LinkNo, CallbackData: linkButtonNo},
	}}}
	if player.Avatar != "" {
		// Telegram fetches the picture by the link; the bot neither
		// downloads nor keeps it.
		err := b.sendPhoto(ctx, msg.Chat.ID, player.Avatar, card, buttons)
		if err == nil {
			return nil
		}
		b.log.Warn("card with the avatar not sent, sending it as text", "err", err)
	}
	return b.send(ctx, msg.Chat.ID, card, buttons)
}

// linkButton handles the Yes and No buttons of the card. The button data is
// not trusted: the player ID is checked, and the player is fetched from
// FACEIT again, so what gets saved is what FACEIT has now. If FACEIT or the
// database fails, the card stays, and Yes may be pressed again.
func (b *Bot) linkButton(ctx context.Context, cq *models.CallbackQuery) error {
	// Stop the loading animation at once: FACEIT may take a few seconds.
	answerErr := b.answer(ctx, cq)
	lang := i18n.FromTelegram(cq.From.LanguageCode)
	texts := i18n.For(lang)

	data := strings.TrimPrefix(cq.Data, linkButtonPrefix)
	if data == "no" {
		b.pending.set(cq.From.ID, b.now())
		return errors.Join(answerErr, b.editButtonMessage(ctx, cq, texts.LinkDeclined, nil))
	}
	if !faceit.ValidPlayerID(data) {
		b.log.Warn("link button with bad data ignored")
		return answerErr
	}

	player, err := b.faceit.Player(ctx, data)
	switch {
	case errors.Is(err, faceit.ErrNotFound), err == nil && !player.HasCS2:
		return errors.Join(answerErr, b.editButtonMessage(ctx, cq, texts.LinkGone, nil))
	case err != nil:
		return errors.Join(answerErr, fmt.Errorf("get player: %w", err), b.sendToButtonChat(ctx, cq, texts.FaceitDown))
	}

	err = b.users.LinkFaceitAccount(ctx,
		storage.User{TelegramID: cq.From.ID, Username: cq.From.Username, Language: string(lang)},
		storage.FaceitAccount{PlayerID: player.ID, Nickname: player.Nickname, SteamID64: player.SteamID64},
		b.now())
	if err != nil {
		return errors.Join(answerErr, fmt.Errorf("link faceit account: %w", err), b.sendToButtonChat(ctx, cq, texts.Failure))
	}
	b.pending.clear(cq.From.ID)
	b.log.Info("faceit account linked", "lang", lang)
	return errors.Join(answerErr, b.editButtonMessage(ctx, cq, fmt.Sprintf(texts.LinkDone, player.Nickname), nil))
}

// sendToButtonChat sends a new message to the chat of the pressed button.
func (b *Bot) sendToButtonChat(ctx context.Context, cq *models.CallbackQuery, text string) error {
	chat := buttonChat(cq)
	if chat == nil {
		return nil
	}
	return b.send(ctx, chat.ID, text, nil)
}
