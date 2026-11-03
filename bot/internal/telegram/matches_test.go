package telegram

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

var testMatches = []faceit.MatchSummary{
	{ID: "1-00000000-0000-4000-8000-000000000004", StartedAt: time.Date(2026, 10, 31, 17, 3, 25, 0, time.UTC),
		Score: 16, OpponentScore: 19, HasStats: true,
		Stats: faceit.PlayerMatchStats{MatchID: "1-00000000-0000-4000-8000-000000000004", Map: "de_cache",
			Kills: 21, Deaths: 27, Assists: 4, Headshots: 9, HeadshotsPct: 43, ADR: 63.8}},
	// Late at night in UTC, so the date moves in the zones east of it.
	{ID: "1-00000000-0000-4000-8000-000000000044", StartedAt: time.Date(2026, 10, 19, 21, 18, 32, 0, time.UTC),
		Score: 13, OpponentScore: 10, Won: true, HasStats: true,
		Stats: faceit.PlayerMatchStats{MatchID: "1-00000000-0000-4000-8000-000000000044", Map: "de_mirage",
			Kills: 13, Deaths: 18, Assists: 4, Headshots: 7, HeadshotsPct: 54, ADR: 73.5, Won: true}},
	// FACEIT has no stats of this one yet.
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

// matchesButtons is the keyboard under the match list.
func matchesButtons(texts i18n.Messages) [][]models.InlineKeyboardButton {
	return [][]models.InlineKeyboardButton{{{Text: texts.TimezoneButton, CallbackData: "tz:menu"}}}
}

func TestFormatMatches(t *testing.T) {
	tests := []struct {
		name   string
		lang   i18n.Lang
		offset int
		want   string
	}{
		{"ru, Moscow", i18n.RU, 180, `Последние матчи kolyx:

🟥 31.10 20:03 · Cache · 16:19
У/С/А 21/27/4 · ADR 64 · HS 43%

🟩 20.10 00:18 · Mirage · 13:10
У/С/А 13/18/4 · ADR 74 · HS 54%

🟩 12.10 20:06 · 13:11`},
		{"ru, UTC+5", i18n.RU, 300, `Последние матчи kolyx:

🟥 31.10 22:03 · Cache · 16:19
У/С/А 21/27/4 · ADR 64 · HS 43%

🟩 20.10 02:18 · Mirage · 13:10
У/С/А 13/18/4 · ADR 74 · HS 54%

🟩 12.10 22:06 · 13:11`},
		{"en, UTC", i18n.EN, 0, `Latest matches of kolyx:

🟥 Oct 31 17:03 · Cache · 16:19
K/D/A 21/27/4 · ADR 64 · HS 43%

🟩 Oct 19 21:18 · Mirage · 13:10
K/D/A 13/18/4 · ADR 74 · HS 54%

🟩 Oct 12 17:06 · 13:11`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatMatches(tt.lang, "kolyx", testMatches, tt.offset); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestDefaultOffset(t *testing.T) {
	if got := defaultOffset(i18n.RU); got != 180 {
		t.Errorf("defaultOffset(ru) = %d, want Moscow, 180", got)
	}
	if got := defaultOffset(i18n.EN); got != 0 {
		t.Errorf("defaultOffset(en) = %d, want UTC, 0", got)
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name, langCode string
		chosen         *int
		lang           i18n.Lang
		offset         int
	}{
		{"ru by default in Moscow time", "ru", nil, i18n.RU, 180},
		{"en by default in UTC", "en", nil, i18n.EN, 0},
		{"the chosen zone", "ru", ptr(300), i18n.RU, 300},
		{"UTC chosen in Russian", "ru", ptr(0), i18n.RU, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, api, store, fc := newLinkBot(t)
			linkKolyx(t, store)
			fc.matches = testMatches
			if tt.chosen != nil {
				if err := store.SetUTCOffset(context.Background(), storage.User{TelegramID: 42, Language: "ru"}, *tt.chosen, testNow); err != nil {
					t.Fatal(err)
				}
			}

			process(b, privateMessage("/matches", tt.langCode))

			if got, want := api.sentTexts(), []string{formatMatches(tt.lang, "kolyx", testMatches, tt.offset)}; !slices.Equal(got, want) {
				t.Errorf("sent %q, want %q", got, want)
			}
			if got, want := cardButtons(t, api), matchesButtons(i18n.For(tt.lang)); !slices.EqualFunc(got, want, slices.Equal) {
				t.Errorf("buttons %+v, want %+v", got, want)
			}
			if got, want := fc.callList(), []string{fmt.Sprintf("recent %s %d", playerKolyx.ID, recentMatches)}; !slices.Equal(got, want) {
				t.Errorf("FACEIT calls %q, want %q", got, want)
			}
			if m := api.methods(); !slices.Equal(m, []string{"sendChatAction", "sendMessage"}) {
				t.Errorf("Bot API calls %v, want typing, then the list", m)
			}
		})
	}
}

func ptr(n int) *int { return &n }

func TestMatchesWithoutAccount(t *testing.T) {
	b, api, _, fc := newLinkBot(t)

	process(b, privateMessage("/matches", "en"))

	if got, want := api.sentTexts(), []string{i18n.For(i18n.EN).MatchesNoAccount}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
	if markup := api.callsTo("sendMessage")[0].params["reply_markup"]; markup != "" {
		t.Errorf("buttons %s under a text with nothing to list", markup)
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

func TestTimezoneButtons(t *testing.T) {
	got := timezoneButtons(testNow, 180) // testNow is 12:00 UTC
	wantLabels := [][]string{
		{"12:00", "14:00", "✓ 15:00", "16:00"},
		{"17:00", "18:00", "19:00", "20:00"},
		{"21:00", "22:00", "23:00", "00:00"},
	}
	if len(got.InlineKeyboard) != len(wantLabels) {
		t.Fatalf("%d rows, want %d", len(got.InlineKeyboard), len(wantLabels))
	}
	for i, row := range got.InlineKeyboard {
		if len(row) != 4 {
			t.Errorf("row %d has %d buttons, want 4", i, len(row))
			continue
		}
		for j, button := range row {
			offset := utcOffsets[i*4+j]
			if button.Text != wantLabels[i][j] || button.CallbackData != "tz:"+strconv.Itoa(offset) {
				t.Errorf("button %d,%d = %q %q, want %q tz:%d", i, j, button.Text, button.CallbackData, wantLabels[i][j], offset)
			}
		}
	}
}

func TestTimezoneMenu(t *testing.T) {
	b, api, store, _ := newLinkBot(t)
	linkKolyx(t, store)
	ru := i18n.For(i18n.RU)

	process(b, buttonPress("tz:menu", "ru"))

	edits := api.callsTo("editMessageText")
	if len(edits) != 1 || edits[0].params["text"] != ru.TimezoneAsk || edits[0].params["message_id"] != "7" {
		t.Fatalf("edits %+v, want the time question in the list message", edits)
	}
	if got, want := buttonsOf(t, edits[0]), timezoneButtons(testNow, 180).InlineKeyboard; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("buttons %+v, want %+v", got, want)
	}
	if n := len(api.callsTo("answerCallbackQuery")); n != 1 {
		t.Errorf("%d answers, want 1", n)
	}
}

func TestTimezoneChoice(t *testing.T) {
	b, api, store, fc := newLinkBot(t)
	linkKolyx(t, store)
	fc.matches = testMatches
	ru := i18n.For(i18n.RU)

	process(b, buttonPress("tz:300", "ru"))

	if minutes, ok, err := store.UTCOffset(context.Background(), 42); err != nil || !ok || minutes != 300 {
		t.Fatalf("UTCOffset() = %d, %t, %v, want 300 saved", minutes, ok, err)
	}
	edits := api.callsTo("editMessageText")
	if len(edits) != 1 {
		t.Fatalf("%d edits, want 1", len(edits))
	}
	if got, want := edits[0].params["text"], formatMatches(i18n.RU, "kolyx", testMatches, 300); got != want {
		t.Errorf("edited to:\n%s\nwant the list in UTC+5:\n%s", got, want)
	}
	if got, want := buttonsOf(t, edits[0]), matchesButtons(ru); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("buttons %+v, want %+v", got, want)
	}

	// The menu now marks the new zone, and /matches uses it.
	process(b, buttonPress("tz:menu", "ru"))
	if got, want := buttonsOf(t, api.callsTo("editMessageText")[1]), timezoneButtons(testNow, 300).InlineKeyboard; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("menu buttons %+v, want %+v", got, want)
	}
	process(b, privateMessage("/matches", "ru"))
	if got, want := api.sentTexts(), []string{formatMatches(i18n.RU, "kolyx", testMatches, 300)}; !slices.Equal(got, want) {
		t.Errorf("/matches sent %q, want %q", got, want)
	}
}

func TestTimezoneChoiceWithoutList(t *testing.T) {
	ru := i18n.For(i18n.RU)
	t.Run("FACEIT down", func(t *testing.T) {
		b, api, store, fc := newLinkBot(t)
		linkKolyx(t, store)
		fc.err = context.DeadlineExceeded

		process(b, buttonPress("tz:600", "ru"))

		if got, want := api.editedTexts(), []string{ru.TimezoneSaved + "\n\n" + ru.FaceitDown}; !slices.Equal(got, want) {
			t.Errorf("edited to %q, want %q", got, want)
		}
		if _, ok := api.callsTo("editMessageText")[0].params["reply_markup"]; ok {
			t.Error("buttons kept without a list")
		}
		if minutes, ok, _ := store.UTCOffset(context.Background(), 42); !ok || minutes != 600 {
			t.Errorf("UTCOffset() = %d, %t, want 600 saved anyway", minutes, ok)
		}
	})
	t.Run("no account", func(t *testing.T) {
		b, api, _, _ := newLinkBot(t)

		process(b, buttonPress("tz:0", "ru"))

		if got, want := api.editedTexts(), []string{ru.TimezoneSaved + "\n\n" + ru.MatchesNoAccount}; !slices.Equal(got, want) {
			t.Errorf("edited to %q, want %q", got, want)
		}
	})
}

func TestTimezoneButtonBadData(t *testing.T) {
	b, api, store, fc := newLinkBot(t)
	linkKolyx(t, store)

	for _, data := range []string{"tz:", "tz:abc", "tz:999", "tz:-180", "tz:60", "tz:180.5"} {
		process(b, buttonPress(data, "ru"))
	}

	if n := len(api.callsTo("answerCallbackQuery")); n != 6 {
		t.Errorf("%d presses answered, want 6", n)
	}
	if m := api.methods(); slices.Contains(m, "editMessageText") || slices.Contains(m, "sendMessage") {
		t.Errorf("Bot API calls %v, want only answers", m)
	}
	if _, ok, _ := store.UTCOffset(context.Background(), 42); ok {
		t.Error("a zone is saved")
	}
	if calls := fc.callList(); len(calls) != 0 {
		t.Errorf("FACEIT called: %q", calls)
	}
}
