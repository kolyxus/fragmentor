package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

var (
	playerKolyx = faceit.Player{ID: "00000000-0000-4000-8000-000000000001", Nickname: "kolyx", Country: "ru",
		HasCS2: true, SteamID64: "76561190000000001", SkillLevel: 8, Elo: 1850}
	playerOther = faceit.Player{ID: "00000000-0000-4000-8000-000000000002", Nickname: "s1mple", Country: "ua",
		HasCS2: true, SteamID64: "76561190000000002", SkillLevel: 10, Elo: 3200}
	playerNoCS2 = faceit.Player{ID: "00000000-0000-4000-8000-000000000003", Nickname: "oldtimer", Country: "ru"}
)

// cardMessageID is the ID of the card message in button presses.
const cardMessageID = 7

// stubFaceit stands in for the FACEIT client with a few known players.
type stubFaceit struct {
	mu      sync.Mutex
	players []faceit.Player
	matches []faceit.MatchSummary
	err     error // returned by every call if set
	calls   []string
}

func newStubFaceit() *stubFaceit {
	return &stubFaceit{players: []faceit.Player{playerKolyx, playerOther, playerNoCS2}}
}

func (s *stubFaceit) PlayerByNickname(_ context.Context, nickname string) (faceit.Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "find "+nickname)
	if s.err != nil {
		return faceit.Player{}, s.err
	}
	for _, p := range s.players {
		if strings.EqualFold(p.Nickname, nickname) {
			return p, nil
		}
	}
	return faceit.Player{}, fmt.Errorf("faceit: find player: %w", faceit.ErrNotFound)
}

func (s *stubFaceit) Player(_ context.Context, playerID string) (faceit.Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "get "+playerID)
	if s.err != nil {
		return faceit.Player{}, s.err
	}
	for _, p := range s.players {
		if p.ID == playerID {
			return p, nil
		}
	}
	return faceit.Player{}, &faceit.StatusError{Op: "get player", Status: 404}
}

func (s *stubFaceit) RecentMatches(_ context.Context, playerID string, n int) ([]faceit.MatchSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, fmt.Sprintf("recent %s %d", playerID, n))
	if s.err != nil {
		return nil, s.err
	}
	return s.matches, nil
}

func (s *stubFaceit) callList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// newLinkBot returns a bot with a real database and the stub FACEIT.
func newLinkBot(t *testing.T) (*Bot, *fakeAPI, *storage.Store, *stubFaceit) {
	t.Helper()
	store, _ := newTestStore(t)
	b, api := newTestBot(t, store)
	fc := newStubFaceit()
	b.faceit = fc
	return b, api, store, fc
}

// buttonPress builds a press by user 42 of a button under the card message in
// the private chat.
func buttonPress(data, languageCode string) *models.Update {
	return &models.Update{ID: 2, CallbackQuery: &models.CallbackQuery{
		ID:   "press-1",
		From: models.User{ID: 42, FirstName: "Player", Username: "player", LanguageCode: languageCode},
		Message: models.MaybeInaccessibleMessage{
			Type:    models.MaybeInaccessibleMessageTypeMessage,
			Message: &models.Message{ID: cardMessageID, Chat: models.Chat{ID: 42, Type: models.ChatTypePrivate}, Text: "card"},
		},
		Data: data,
	}}
}

func linkedAccount(t *testing.T, store *storage.Store) (storage.FaceitAccount, bool) {
	t.Helper()
	acc, err := store.FaceitAccount(context.Background(), 42)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.FaceitAccount{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return acc, true
}

func card(texts i18n.Messages, p faceit.Player) string {
	return fmt.Sprintf(texts.LinkCard, p.Nickname, p.SkillLevel, p.Elo)
}

// cardButtons returns the inline keyboard of the last sent message.
func cardButtons(t *testing.T, api *fakeAPI) [][]models.InlineKeyboardButton {
	t.Helper()
	calls := api.callsTo("sendMessage")
	if len(calls) == 0 {
		t.Fatal("no message sent")
	}
	var markup models.InlineKeyboardMarkup
	if err := json.Unmarshal([]byte(calls[len(calls)-1].params["reply_markup"]), &markup); err != nil {
		t.Fatalf("reply_markup %q: %v", calls[len(calls)-1].params["reply_markup"], err)
	}
	return markup.InlineKeyboard
}

func TestLinkShowsCard(t *testing.T) {
	b, api, store, _ := newLinkBot(t)
	ru := i18n.For(i18n.RU)

	process(b, privateMessage("/link kolyx", "ru"))

	if got, want := api.sentTexts(), []string{card(ru, playerKolyx)}; !slices.Equal(got, want) {
		t.Fatalf("sent %q, want %q", got, want)
	}
	want := [][]models.InlineKeyboardButton{{
		{Text: ru.LinkYes, CallbackData: "link:" + playerKolyx.ID},
		{Text: ru.LinkNo, CallbackData: "link:no"},
	}}
	if got := cardButtons(t, api); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("buttons %+v, want %+v", got, want)
	}
	if len(api.callsTo("sendChatAction")) != 1 {
		t.Error("no typing action while FACEIT is asked")
	}
	if _, ok := linkedAccount(t, store); ok {
		t.Error("the account is linked before Yes")
	}
}

func TestLinkConfirm(t *testing.T) {
	b, api, store, fc := newLinkBot(t)
	ru := i18n.For(i18n.RU)

	process(b, privateMessage("/link https://www.faceit.com/ru/players/kolyx/stats/cs2", "ru"))
	process(b, buttonPress("link:"+playerKolyx.ID, "ru"))

	answers := api.callsTo("answerCallbackQuery")
	if len(answers) != 1 || answers[0].params["callback_query_id"] != "press-1" {
		t.Errorf("answerCallbackQuery calls %+v, want one for the press", answers)
	}
	edits := api.callsTo("editMessageText")
	if len(edits) != 1 {
		t.Fatalf("%d edits, want 1", len(edits))
	}
	p := edits[0].params
	if p["text"] != fmt.Sprintf(ru.LinkDone, "kolyx") || p["chat_id"] != "42" || p["message_id"] != "7" {
		t.Errorf("edit %v, want the done text in the card message", p)
	}
	if _, ok := p["reply_markup"]; ok {
		t.Errorf("edit keeps buttons: %s", p["reply_markup"])
	}
	acc, ok := linkedAccount(t, store)
	want := storage.FaceitAccount{PlayerID: playerKolyx.ID, Nickname: "kolyx", SteamID64: playerKolyx.SteamID64, LinkedAt: testNow}
	if !ok || acc != want {
		t.Errorf("linked %+v, want %+v", acc, want)
	}
	// The player is fetched again by the ID from the button.
	if got, want := fc.callList(), []string{"find kolyx", "get " + playerKolyx.ID}; !slices.Equal(got, want) {
		t.Errorf("FACEIT calls %q, want %q", got, want)
	}
}

func TestLinkDecline(t *testing.T) {
	b, api, store, _ := newLinkBot(t)
	en := i18n.For(i18n.EN)

	process(b, privateMessage("/link kolyx", "en"))
	process(b, buttonPress(linkButtonNo, "en"))
	// No asks for another nickname, which may come without /link.
	process(b, privateMessage("s1mple", "en"))

	if got, want := api.editedTexts(), []string{en.LinkDeclined}; !slices.Equal(got, want) {
		t.Errorf("edited to %q, want %q", got, want)
	}
	if got, want := api.sentTexts(), []string{card(en, playerKolyx), card(en, playerOther)}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
	if _, ok := linkedAccount(t, store); ok {
		t.Error("an account is linked after No")
	}
}

func TestLinkAsksForNickname(t *testing.T) {
	b, api, _, _ := newLinkBot(t)
	ru := i18n.For(i18n.RU)

	process(b, privateMessage("/link", "ru"))
	process(b, privateMessage("kolyx", "ru"))
	// The wait is over: the next text is not a nickname.
	process(b, privateMessage("s1mple", "ru"))

	want := []string{ru.LinkAsk, card(ru, playerKolyx), ru.Unknown}
	if got := api.sentTexts(); !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestLinkWaitRunsOut(t *testing.T) {
	b, api, _, _ := newLinkBot(t)
	ru := i18n.For(i18n.RU)

	process(b, privateMessage("/link", "ru"))
	b.now = func() time.Time { return testNow.Add(pendingTTL) }
	process(b, privateMessage("kolyx", "ru"))

	if got, want := api.sentTexts(), []string{ru.LinkAsk, ru.Unknown}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestCommandEndsLinkWait(t *testing.T) {
	b, api, _, _ := newLinkBot(t)
	ru := i18n.For(i18n.RU)

	process(b, privateMessage("/link", "ru"))
	process(b, privateMessage("/help", "ru"))
	process(b, privateMessage("kolyx", "ru"))

	if got, want := api.sentTexts(), []string{ru.LinkAsk, ru.Help, ru.Unknown}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestProfileLinkWithoutCommand(t *testing.T) {
	b, api, _, _ := newLinkBot(t)
	en := i18n.For(i18n.EN)

	process(b, privateMessage("https://www.faceit.com/en/players/kolyx", "en"))
	// A bare nickname is not taken without /link.
	process(b, privateMessage("kolyx", "en"))

	if got, want := api.sentTexts(), []string{card(en, playerKolyx), en.Unknown}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestLinkMistakes(t *testing.T) {
	ru := i18n.For(i18n.RU)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"not a nickname", "/link https://steamcommunity.com/id/kolyx", ru.LinkBadInput},
		{"not found", "/link nobody", fmt.Sprintf(ru.LinkNotFound, "nobody")},
		{"no CS2", "/link oldtimer", fmt.Sprintf(ru.LinkNoCS2, "oldtimer")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, api, _, _ := newLinkBot(t)

			process(b, privateMessage(tt.input, "ru"))
			// The fix may come as a plain message.
			process(b, privateMessage("kolyx", "ru"))

			if got, want := api.sentTexts(), []string{tt.want, card(ru, playerKolyx)}; !slices.Equal(got, want) {
				t.Errorf("sent %q, want %q", got, want)
			}
		})
	}
}

func TestLinkFaceitDown(t *testing.T) {
	b, api, _, fc := newLinkBot(t)
	fc.err = &faceit.StatusError{Op: "find player", Status: 503}

	process(b, privateMessage("/link kolyx", "ru"))

	if got, want := api.sentTexts(), []string{i18n.For(i18n.RU).FaceitDown}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestLinkSameAccountAgain(t *testing.T) {
	b, api, store, _ := newLinkBot(t)
	err := store.LinkFaceitAccount(context.Background(), storage.User{TelegramID: 42, Language: "ru"},
		storage.FaceitAccount{PlayerID: playerKolyx.ID, Nickname: "kolyx", SteamID64: playerKolyx.SteamID64}, testNow)
	if err != nil {
		t.Fatal(err)
	}

	process(b, privateMessage("/link KOLYX", "ru"))

	if got, want := api.sentTexts(), []string{fmt.Sprintf(i18n.For(i18n.RU).LinkAlready, "kolyx")}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestLinkReplacesAccount(t *testing.T) {
	b, api, store, _ := newLinkBot(t)
	ru := i18n.For(i18n.RU)
	err := store.LinkFaceitAccount(context.Background(), storage.User{TelegramID: 42, Language: "ru"},
		storage.FaceitAccount{PlayerID: playerOther.ID, Nickname: "s1mple", SteamID64: playerOther.SteamID64}, testNow)
	if err != nil {
		t.Fatal(err)
	}

	process(b, privateMessage("/link kolyx", "ru"))
	process(b, buttonPress("link:"+playerKolyx.ID, "ru"))

	wantCard := card(ru, playerKolyx) + "\n\n" + fmt.Sprintf(ru.LinkCardReplace, "s1mple")
	if got := api.sentTexts(); !slices.Equal(got, []string{wantCard}) {
		t.Errorf("sent %q, want %q", got, wantCard)
	}
	if acc, _ := linkedAccount(t, store); acc.PlayerID != playerKolyx.ID {
		t.Errorf("linked %+v, want kolyx", acc)
	}
}

func TestLinkButtonBadData(t *testing.T) {
	b, api, store, fc := newLinkBot(t)

	for _, data := range []string{"link:", "link:../../players", "link:" + playerKolyx.ID + "/x"} {
		process(b, buttonPress(data, "ru"))
	}

	if n := len(api.callsTo("answerCallbackQuery")); n != 3 {
		t.Errorf("%d presses answered, want 3", n)
	}
	if m := api.methods(); slices.Contains(m, "editMessageText") || slices.Contains(m, "sendMessage") {
		t.Errorf("Bot API calls %v, want only answers", m)
	}
	if calls := fc.callList(); len(calls) != 0 {
		t.Errorf("FACEIT called: %q", calls)
	}
	if _, ok := linkedAccount(t, store); ok {
		t.Error("an account is linked")
	}
}

func TestLinkButtonFailuresKeepCard(t *testing.T) {
	t.Run("FACEIT down", func(t *testing.T) {
		b, api, store, fc := newLinkBot(t)
		fc.err = &faceit.StatusError{Op: "get player", Status: 502}

		process(b, buttonPress("link:"+playerKolyx.ID, "ru"))

		if got, want := api.sentTexts(), []string{i18n.For(i18n.RU).FaceitDown}; !slices.Equal(got, want) {
			t.Errorf("sent %q, want %q", got, want)
		}
		if edits := api.editedTexts(); len(edits) != 0 {
			t.Errorf("card edited to %q, want it kept for another press", edits)
		}
		if _, ok := linkedAccount(t, store); ok {
			t.Error("an account is linked")
		}
	})
	t.Run("database down", func(t *testing.T) {
		users := &stubUsers{err: errors.New("disk is full")}
		b, api := newTestBot(t, users)

		process(b, buttonPress("link:"+playerKolyx.ID, "en"))

		if got, want := api.sentTexts(), []string{i18n.For(i18n.EN).Failure}; !slices.Equal(got, want) {
			t.Errorf("sent %q, want %q", got, want)
		}
		if edits := api.editedTexts(); len(edits) != 0 {
			t.Errorf("card edited to %q", edits)
		}
	})
}

func TestLinkButtonAccountGone(t *testing.T) {
	for name, id := range map[string]string{
		"deleted": "00000000-0000-4000-8000-000000000099",
		"no CS2":  playerNoCS2.ID,
	} {
		t.Run(name, func(t *testing.T) {
			b, api, store, _ := newLinkBot(t)

			process(b, buttonPress("link:"+id, "ru"))

			if got, want := api.editedTexts(), []string{i18n.For(i18n.RU).LinkGone}; !slices.Equal(got, want) {
				t.Errorf("edited to %q, want %q", got, want)
			}
			if _, ok := linkedAccount(t, store); ok {
				t.Error("an account is linked")
			}
		})
	}
}

func TestUnknownButton(t *testing.T) {
	b, api, _, _ := newLinkBot(t)

	process(b, buttonPress("rate:5", "ru"))

	if got := api.methods(); !slices.Equal(got, []string{"answerCallbackQuery"}) {
		t.Errorf("Bot API calls %v, want just the answer", got)
	}
}

func TestCommandArgs(t *testing.T) {
	for text, want := range map[string]string{
		"/link":                   "",
		"/link kolyx":             "kolyx",
		"/link   kolyx  ":         "kolyx",
		"/link\nkolyx":            "kolyx",
		"/link two words":         "two words",
		"/link https://x.com/a b": "https://x.com/a b",
	} {
		if got := commandArgs(text); got != want {
			t.Errorf("commandArgs(%q) = %q, want %q", text, got, want)
		}
	}
}
