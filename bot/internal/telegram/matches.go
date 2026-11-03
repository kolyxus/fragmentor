package telegram

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// recentMatches is how many matches /matches shows.
const recentMatches = 5

// The time zone buttons carry "tz:menu" under the match list and
// "tz:<minutes east of UTC>" in the choice of zone.
const (
	tzButtonPrefix = "tz:"
	tzButtonMenu   = tzButtonPrefix + "menu"
)

// Marks of a won and a lost match.
const (
	winMark  = "🟩"
	lossMark = "🟥"
)

// utcOffsets are the time zones on offer, in minutes east of UTC: UTC and the
// eleven zones of Russia, from Kaliningrad (UTC+2) to Kamchatka (UTC+12).
// None of them moves the clock in summer, so a zone is just its offset.
var utcOffsets = []int{0, 120, 180, 240, 300, 360, 420, 480, 540, 600, 660, 720}

// defaultOffset is the time zone of a user who has not chosen one: Moscow time
// in Russian, UTC in English.
func defaultOffset(lang i18n.Lang) int {
	if lang == i18n.RU {
		return 180
	}
	return 0
}

// matches handles /matches: the last finished CS2 matches of the linked
// FACEIT account.
func (b *Bot) matches(ctx context.Context, msg *models.Message) error {
	text, markup, err := b.matchesView(ctx, msg.From, msg.Chat.ID)
	return errors.Join(err, b.send(ctx, msg.Chat.ID, text, markup))
}

// matchesView makes the answer to /matches: the match list with the time zone
// button, or, when there is no list to show, a text without buttons. A
// non-nil error is a failure to log; the text tells the user about it.
func (b *Bot) matchesView(ctx context.Context, user *models.User, chatID int64) (string, models.ReplyMarkup, error) {
	lang := i18n.FromTelegram(user.LanguageCode)
	texts := i18n.For(lang)
	acc, err := b.users.FaceitAccount(ctx, user.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return texts.MatchesNoAccount, nil, nil
	}
	if err != nil {
		return texts.Failure, nil, fmt.Errorf("load faceit account: %w", err)
	}
	offset, err := b.utcOffset(ctx, user.ID, lang)
	if err != nil {
		return texts.Failure, nil, err
	}

	b.typing(ctx, chatID)
	list, err := b.faceit.RecentMatches(ctx, acc.PlayerID, recentMatches)
	if err != nil {
		return texts.FaceitDown, nil, fmt.Errorf("recent matches: %w", err)
	}
	if len(list) == 0 {
		return fmt.Sprintf(texts.MatchesEmpty, acc.Nickname), nil, nil
	}
	buttons := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: texts.TimezoneButton, CallbackData: tzButtonMenu},
	}}}
	return formatMatches(lang, acc.Nickname, list, offset), buttons, nil
}

// utcOffset returns the time zone of the user in minutes east of UTC: the one
// they chose, or the default of the language.
func (b *Bot) utcOffset(ctx context.Context, telegramID int64, lang i18n.Lang) (int, error) {
	minutes, ok, err := b.users.UTCOffset(ctx, telegramID)
	if err != nil {
		return 0, fmt.Errorf("load utc offset: %w", err)
	}
	if !ok {
		return defaultOffset(lang), nil
	}
	return minutes, nil
}

// formatMatches lays out the match list, two lines per match: the result,
// the start time in the user's time zone, the map and the score of the
// player's team first, then the player's stats.
//
//	🟥 31.10 22:03 · Cache · 16:19
//	У/С/А 21/27/4 · ADR 64 · HS 43%
//
// A match FACEIT has no stats for yet goes without the map and the second line.
func formatMatches(lang i18n.Lang, nickname string, list []faceit.MatchSummary, utcOffset int) string {
	texts := i18n.For(lang)
	loc := time.FixedZone("", utcOffset*60)
	var sb strings.Builder
	fmt.Fprintf(&sb, texts.MatchesHeader, nickname)
	for _, m := range list {
		mark := lossMark
		if m.Won {
			mark = winMark
		}
		sb.WriteString("\n\n" + mark + " " + m.StartedAt.In(loc).Format(texts.MatchTimeLayout))
		if m.HasStats && m.Stats.Map != "" {
			sb.WriteString(" · " + faceit.MapName(m.Stats.Map))
		}
		fmt.Fprintf(&sb, " · %d:%d", m.Score, m.OpponentScore)
		if m.HasStats {
			s := m.Stats
			sb.WriteString("\n")
			fmt.Fprintf(&sb, texts.MatchStats, s.Kills, s.Deaths, s.Assists, int(math.Round(s.ADR)), s.HeadshotsPct)
		}
	}
	return sb.String()
}

// timezoneButton handles the time zone buttons. The one under the match list
// turns the message into the choice of time; a time saves its zone and turns
// the message back into the list, in the new time.
func (b *Bot) timezoneButton(ctx context.Context, cq *models.CallbackQuery) error {
	answerErr := b.answer(ctx, cq)
	lang := i18n.FromTelegram(cq.From.LanguageCode)
	texts := i18n.For(lang)

	data := strings.TrimPrefix(cq.Data, tzButtonPrefix)
	if data == "menu" {
		current, err := b.utcOffset(ctx, cq.From.ID, lang)
		if err != nil {
			return errors.Join(answerErr, err, b.sendToButtonChat(ctx, cq, texts.Failure))
		}
		return errors.Join(answerErr, b.editButtonMessage(ctx, cq, texts.TimezoneAsk, timezoneButtons(b.now(), current)))
	}
	minutes, err := strconv.Atoi(data)
	if err != nil || !slices.Contains(utcOffsets, minutes) {
		b.log.Warn("time zone button with bad data ignored")
		return answerErr
	}

	user := storage.User{TelegramID: cq.From.ID, Username: cq.From.Username, Language: string(lang)}
	if err := b.users.SetUTCOffset(ctx, user, minutes, b.now()); err != nil {
		return errors.Join(answerErr, fmt.Errorf("save utc offset: %w", err), b.sendToButtonChat(ctx, cq, texts.Failure))
	}
	b.log.Info("time zone chosen", "lang", lang)

	var chatID int64
	if chat := buttonChat(cq); chat != nil {
		chatID = chat.ID
	}
	text, markup, viewErr := b.matchesView(ctx, &cq.From, chatID)
	if markup == nil {
		// No list to go back to: say at least that the zone is saved.
		text = texts.TimezoneSaved + "\n\n" + text
	}
	return errors.Join(answerErr, viewErr, b.editButtonMessage(ctx, cq, text, markup))
}

// timezoneButtons lays out the current time in each zone on offer, four to a
// row, and marks the zone the user has now.
func timezoneButtons(now time.Time, current int) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	for i, minutes := range utcOffsets {
		if i%4 == 0 {
			rows = append(rows, nil)
		}
		label := now.In(time.FixedZone("", minutes*60)).Format("15:04")
		if minutes == current {
			label = "✓ " + label
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], models.InlineKeyboardButton{
			Text:         label,
			CallbackData: tzButtonPrefix + strconv.Itoa(minutes),
		})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}
