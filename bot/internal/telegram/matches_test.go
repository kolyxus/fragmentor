package telegram

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

var testMatches = []faceit.MatchSummary{
	{ID: "1-00000000-0000-4000-8000-000000000004", StartedAt: time.Date(2026, 10, 31, 17, 3, 25, 0, time.UTC),
		Map: "de_cache", MapName: "Cache", Score: 16, OpponentScore: 19},
	{ID: "1-00000000-0000-4000-8000-000000000044", StartedAt: time.Date(2026, 10, 19, 21, 18, 32, 0, time.UTC),
		Map: "de_mirage", MapName: "Mirage", Score: 13, OpponentScore: 10, Won: true},
	{ID: "1-00000000-0000-4000-8000-000000000054", StartedAt: time.Date(2026, 10, 12, 17, 6, 54, 0, time.UTC),
		Score: 13, OpponentScore: 11, Won: true},
}

func linkKolyx(t *testing.T, store *storage.Store) {
	t.Helper()
	err := store.LinkFaceitAccount(context.Background(), storage.User{TelegramID: 42, Language: "ru"},
		storage.FaceitAccount{PlayerID: playerKolyx.ID, Nickname: "kolyx", SteamID64: playerKolyx.SteamID64}, testNow)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFormatMatches(t *testing.T) {
	ru := `Последние матчи kolyx (время московское):

31.10 20:03 · Cache · 16:19 · поражение
20.10 00:18 · Mirage · 13:10 · победа
12.10 20:06 · карта неизвестна · 13:11 · победа`
	if got := formatMatches(i18n.RU, "kolyx", testMatches); got != ru {
		t.Errorf("ru:\n%s\nwant\n%s", got, ru)
	}
	en := `Latest matches of kolyx (times in UTC):

Oct 31 17:03 · Cache · 16:19 · loss
Oct 19 21:18 · Mirage · 13:10 · win
Oct 12 17:06 · unknown map · 13:11 · win`
	if got := formatMatches(i18n.EN, "kolyx", testMatches); got != en {
		t.Errorf("en:\n%s\nwant\n%s", got, en)
	}
}

func TestMatches(t *testing.T) {
	b, api, store, fc := newLinkBot(t)
	linkKolyx(t, store)
	fc.matches = testMatches

	process(b, privateMessage("/matches", "ru"))

	if got, want := api.sentTexts(), []string{formatMatches(i18n.RU, "kolyx", testMatches)}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
	if got, want := fc.callList(), []string{fmt.Sprintf("recent %s %d", playerKolyx.ID, recentMatches)}; !slices.Equal(got, want) {
		t.Errorf("FACEIT calls %q, want %q", got, want)
	}
	if m := api.methods(); !slices.Equal(m, []string{"sendChatAction", "sendMessage"}) {
		t.Errorf("Bot API calls %v, want typing, then the list", m)
	}
}

func TestMatchesWithoutAccount(t *testing.T) {
	b, api, _, fc := newLinkBot(t)

	process(b, privateMessage("/matches", "en"))

	if got, want := api.sentTexts(), []string{i18n.For(i18n.EN).MatchesNoAccount}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
	if calls := fc.callList(); len(calls) != 0 {
		t.Errorf("FACEIT called: %q", calls)
	}
}

func TestMatchesEmpty(t *testing.T) {
	b, api, store, _ := newLinkBot(t)
	linkKolyx(t, store)

	process(b, privateMessage("/matches", "ru"))

	if got, want := api.sentTexts(), []string{fmt.Sprintf(i18n.For(i18n.RU).MatchesEmpty, "kolyx")}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestMatchesFaceitDown(t *testing.T) {
	b, api, store, fc := newLinkBot(t)
	linkKolyx(t, store)
	fc.err = context.DeadlineExceeded

	process(b, privateMessage("/matches", "ru"))

	if got, want := api.sentTexts(), []string{i18n.For(i18n.RU).FaceitDown}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}
