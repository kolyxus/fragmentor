package telegram

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

var testNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// newTestBot returns a bot talking to a fake Bot API, with FACEIT stubbed
// by a newStubFaceit; tests that need other players replace b.faceit.
func newTestBot(t *testing.T, users Users) (*Bot, *fakeAPI) {
	t.Helper()
	api := newFakeAPI(t)
	b, err := New(testToken, users, newStubFaceit(), newStubJobs(), nil, slog.New(slog.DiscardHandler), bot.WithServerURL(api.URL))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	b.now = func() time.Time { return testNow }
	return b, api
}

// newTestStore opens a real database in a temporary directory.
func newTestStore(t *testing.T) (*storage.Store, string) {
	t.Helper()
	path := filepath.Join(tempDir(t), "test.db")
	store, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("storage.Open() error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close() error: %v", err)
		}
	})
	return store, path
}

// stubUsers records the users who joined and can fail on demand. It has no
// linked FACEIT accounts.
type stubUsers struct {
	mu     sync.Mutex
	joined []storage.User
	linked []storage.FaceitAccount
	err    error
}

func (s *stubUsers) JoinWaitlist(_ context.Context, u storage.User, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	s.joined = append(s.joined, u)
	return true, nil
}

func (s *stubUsers) LinkFaceitAccount(_ context.Context, _ storage.User, acc storage.FaceitAccount, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.linked = append(s.linked, acc)
	return nil
}

func (s *stubUsers) FaceitAccount(context.Context, int64) (storage.FaceitAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return storage.FaceitAccount{}, s.err
	}
	return storage.FaceitAccount{}, storage.ErrNotFound
}

func (s *stubUsers) UTCOffset(context.Context, int64) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return 0, false, s.err
}

func (s *stubUsers) SetUTCOffset(context.Context, storage.User, int, time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// privateMessage builds an update with a message from user 42 in a private
// chat. A leading /command gets its bot_command entity, as Telegram sends it.
func privateMessage(text, languageCode string) *models.Update {
	msg := &models.Message{
		ID:   1,
		From: &models.User{ID: 42, FirstName: "Player", Username: "player", LanguageCode: languageCode},
		Chat: models.Chat{ID: 42, Type: models.ChatTypePrivate},
		Text: text,
	}
	if strings.HasPrefix(text, "/") {
		command, _, _ := strings.Cut(text, " ")
		msg.Entities = []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len(command)}}
	}
	return &models.Update{ID: 1, Message: msg}
}

func process(b *Bot, upd *models.Update) {
	// Handlers are synchronous (WithNotAsyncHandlers), so the update is
	// handled when ProcessUpdate returns.
	b.api.ProcessUpdate(context.Background(), upd)
}

func countUsers(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStartTwiceKeepsOneUser(t *testing.T) {
	store, path := newTestStore(t)
	b, api := newTestBot(t, store)
	ru := i18n.For(i18n.RU)

	process(b, privateMessage("/start", "ru"))
	process(b, privateMessage("/start", "ru"))

	want := []string{ru.StartJoined, ru.StartAlready}
	if got := api.sentTexts(); !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
	if n := countUsers(t, path); n != 1 {
		t.Errorf("users has %d rows after two /start, want 1", n)
	}
}

func TestRepliesInUserLanguage(t *testing.T) {
	tests := []struct {
		code string
		want i18n.Lang
	}{
		{"ru", i18n.RU},
		{"en", i18n.EN},
		{"uk", i18n.EN},
		{"", i18n.EN},
	}
	for _, tt := range tests {
		t.Run("code "+tt.code, func(t *testing.T) {
			users := &stubUsers{}
			b, api := newTestBot(t, users)
			texts := i18n.For(tt.want)

			process(b, privateMessage("/start", tt.code))
			process(b, privateMessage("/help", tt.code))

			want := []string{texts.StartJoined, texts.Help}
			if got := api.sentTexts(); !slices.Equal(got, want) {
				t.Errorf("sent %q, want %q", got, want)
			}
			wantUser := storage.User{TelegramID: 42, Username: "player", Language: string(tt.want)}
			if len(users.joined) != 1 || users.joined[0] != wantUser {
				t.Errorf("joined %+v, want %+v", users.joined, wantUser)
			}
		})
	}
}

func TestReplyParams(t *testing.T) {
	b, api := newTestBot(t, &stubUsers{})
	process(b, privateMessage("/help", "ru"))

	calls := api.callsTo("sendMessage")
	if len(calls) != 1 {
		t.Fatalf("sendMessage called %d times, want 1", len(calls))
	}
	p := calls[0].params
	if p["chat_id"] != "42" {
		t.Errorf("chat_id = %q, want 42", p["chat_id"])
	}
	if _, ok := p["parse_mode"]; ok {
		t.Errorf("parse_mode = %q, want plain text", p["parse_mode"])
	}
	if !strings.Contains(p["link_preview_options"], `"is_disabled":true`) {
		t.Errorf("link_preview_options = %q, want previews disabled", p["link_preview_options"])
	}
}

func TestStartWithPayload(t *testing.T) {
	users := &stubUsers{}
	b, api := newTestBot(t, users)

	process(b, privateMessage("/start abc123", "ru"))

	if got, want := api.sentTexts(), []string{i18n.For(i18n.RU).StartJoined}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestUnknownMessages(t *testing.T) {
	for _, text := range []string{"привет", "/stats", "/help@fragmentor_ai_bot", "help", ""} {
		t.Run(text, func(t *testing.T) {
			users := &stubUsers{}
			b, api := newTestBot(t, users)

			process(b, privateMessage(text, "en"))

			if got, want := api.sentTexts(), []string{i18n.For(i18n.EN).Unknown}; !slices.Equal(got, want) {
				t.Errorf("sent %q, want %q", got, want)
			}
			if len(users.joined) != 0 {
				t.Errorf("unknown message joined the waitlist: %+v", users.joined)
			}
		})
	}
}

func TestIgnoresEverythingButPrivateMessages(t *testing.T) {
	group := privateMessage("/start", "ru")
	group.Message.Chat.Type = models.ChatTypeGroup
	supergroup := privateMessage("/start", "ru")
	supergroup.Message.Chat.Type = models.ChatTypeSupergroup
	noSender := privateMessage("/start", "ru")
	noSender.Message.From = nil
	edited := &models.Update{ID: 2, EditedMessage: privateMessage("/start", "ru").Message}
	channelPost := &models.Update{ID: 3, ChannelPost: &models.Message{ID: 1, Chat: models.Chat{ID: -100, Type: models.ChatTypeChannel}, Text: "/start"}}
	groupButton := buttonPress(linkButtonNo, "ru")
	groupButton.CallbackQuery.Message.Message.Chat.Type = models.ChatTypeGroup
	inlineButton := buttonPress(linkButtonNo, "ru")
	inlineButton.CallbackQuery.Message = models.MaybeInaccessibleMessage{}
	inlineButton.CallbackQuery.InlineMessageID = "inline-1"

	for name, upd := range map[string]*models.Update{
		"group":         group,
		"supergroup":    supergroup,
		"no sender":     noSender,
		"edited":        edited,
		"channel post":  channelPost,
		"group button":  groupButton,
		"inline button": inlineButton,
	} {
		t.Run(name, func(t *testing.T) {
			users := &stubUsers{}
			b, api := newTestBot(t, users)

			process(b, upd)

			if m := api.methods(); len(m) != 0 {
				t.Errorf("Bot API called: %v", m)
			}
			if len(users.joined) != 0 {
				t.Errorf("joined the waitlist: %+v", users.joined)
			}
		})
	}
}

func TestStartStorageFailure(t *testing.T) {
	b, api := newTestBot(t, &stubUsers{err: errors.New("disk is full")})

	process(b, privateMessage("/start", "ru"))

	if got, want := api.sentTexts(), []string{i18n.For(i18n.RU).Failure}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestSyncProfile(t *testing.T) {
	b, api := newTestBot(t, &stubUsers{})
	ctx := context.Background()

	changed, err := b.syncProfile(ctx)
	if err != nil {
		t.Fatalf("syncProfile() error: %v", err)
	}
	if len(changed) != 6 {
		t.Errorf("first sync changed %q, want commands and both descriptions in two languages", changed)
	}
	for code, lang := range map[string]i18n.Lang{"": i18n.EN, "ru": i18n.RU} {
		texts := i18n.For(lang)
		wantCommands := `[{"command":"start","description":"` + texts.CommandStart + `"},` +
			`{"command":"link","description":"` + texts.CommandLink + `"},` +
			`{"command":"matches","description":"` + texts.CommandMatches + `"},` +
			`{"command":"help","description":"` + texts.CommandHelp + `"}]`
		if got := api.commands[code]; got != wantCommands {
			t.Errorf("commands[%q] = %s, want %s", code, got, wantCommands)
		}
		if got := api.descriptions[code]; got != texts.Description {
			t.Errorf("descriptions[%q] = %q, want %q", code, got, texts.Description)
		}
		if got := api.shortDescriptions[code]; got != texts.ShortDescription {
			t.Errorf("shortDescriptions[%q] = %q, want %q", code, got, texts.ShortDescription)
		}
	}

	before := len(api.methods())
	changed, err = b.syncProfile(ctx)
	if err != nil {
		t.Fatalf("second syncProfile() error: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("second sync changed %q, want nothing", changed)
	}
	for _, m := range api.methods()[before:] {
		if strings.HasPrefix(m, "set") {
			t.Errorf("second sync called %s", m)
		}
	}
}

func TestRunHandlesUpdatesAndStops(t *testing.T) {
	store, path := newTestStore(t)
	b, api := newTestBot(t, store)
	api.queueUpdates(privateMessage("/start", "en"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for len(api.sentTexts()) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no reply within 5 s, Bot API calls: %v", api.methods())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after cancel")
	}

	if got, want := api.sentTexts(), []string{i18n.For(i18n.EN).StartJoined}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
	if n := countUsers(t, path); n != 1 {
		t.Errorf("users has %d rows, want 1", n)
	}
	if len(api.callsTo("setMyCommands")) != 2 {
		t.Errorf("setMyCommands called %d times, want 2", len(api.callsTo("setMyCommands")))
	}
}

func TestErrorsDoNotLeakToken(t *testing.T) {
	// The server drops the connection, so the error carries the request URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	b, err := New(testToken, &stubUsers{}, newStubFaceit(), newStubJobs(), nil, slog.New(slog.DiscardHandler), bot.WithServerURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	err = b.reply(context.Background(), privateMessage("hi", "en").Message, "text")
	if err == nil {
		t.Fatal("reply() succeeded, want a transport error")
	}
	if !strings.Contains(err.Error(), "/bot***/sendMessage") {
		t.Errorf("error %q does not show the masked URL, the test lost its point", err)
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "TEST-token") {
		t.Errorf("error leaks the token: %q", err)
	}
}

func TestDetachContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	var ctxErr error
	var hasDeadline bool
	detachContext(func(ctx context.Context, _ *bot.Bot, _ *models.Update) {
		ctxErr = ctx.Err()
		_, hasDeadline = ctx.Deadline()
	})(parent, nil, nil)

	if ctxErr != nil {
		t.Errorf("handler context is done: %v", ctxErr)
	}
	if !hasDeadline {
		t.Error("handler context has no deadline")
	}
}

func TestRecoverPanic(t *testing.T) {
	var logs bytes.Buffer
	b := &Bot{log: slog.New(slog.NewTextHandler(&logs, nil))}

	b.recoverPanic(func(context.Context, *bot.Bot, *models.Update) {
		panic("boom")
	})(context.Background(), nil, nil)

	if !strings.Contains(logs.String(), "handler panic") || !strings.Contains(logs.String(), "boom") {
		t.Errorf("panic not logged: %s", logs.String())
	}
}
