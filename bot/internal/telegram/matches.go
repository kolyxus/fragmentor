package telegram

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// recentMatches is how many matches /matches shows.
const recentMatches = 5

// moscow is the time zone of the match list in Russian: the players either
// live in it or are used to it. Russia has stayed at UTC+3 all year since
// 2014, and a fixed zone needs no tzdata on the server.
var moscow = time.FixedZone("MSK", 3*60*60)

// matches handles /matches: the last finished CS2 matches of the linked
// FACEIT account.
func (b *Bot) matches(ctx context.Context, msg *models.Message) error {
	lang := i18n.FromTelegram(msg.From.LanguageCode)
	texts := i18n.For(lang)
	acc, err := b.users.FaceitAccount(ctx, msg.From.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return b.reply(ctx, msg, texts.MatchesNoAccount)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("load faceit account: %w", err), b.reply(ctx, msg, texts.Failure))
	}

	b.typing(ctx, msg.Chat.ID)
	list, err := b.faceit.RecentMatches(ctx, acc.PlayerID, recentMatches)
	if err != nil {
		return errors.Join(fmt.Errorf("recent matches: %w", err), b.reply(ctx, msg, texts.FaceitDown))
	}
	if len(list) == 0 {
		return b.reply(ctx, msg, fmt.Sprintf(texts.MatchesEmpty, acc.Nickname))
	}
	return b.reply(ctx, msg, formatMatches(lang, acc.Nickname, list))
}

// formatMatches lays out the match list, a line per match with the start
// time, the map, the score of the player's team first and the result:
//
//	31.10 20:03 · Cache · 16:19 · поражение
//
// Times are in Moscow time in Russian, in UTC in English.
func formatMatches(lang i18n.Lang, nickname string, list []faceit.MatchSummary) string {
	texts := i18n.For(lang)
	loc := time.UTC
	if lang == i18n.RU {
		loc = moscow
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, texts.MatchesHeader, nickname)
	sb.WriteString("\n")
	for _, m := range list {
		result := texts.MatchLoss
		if m.Won {
			result = texts.MatchWin
		}
		fmt.Fprintf(&sb, "\n%s · %s · %d:%d · %s",
			m.StartedAt.In(loc).Format(texts.MatchTimeLayout),
			cmp.Or(m.MapName, texts.MatchNoMap),
			m.Score, m.OpponentScore, result)
	}
	return sb.String()
}
