// Package telegram is the Telegram side of the bot: long polling, command
// and button handlers and the bot profile (command menu and descriptions).
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/analysis"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

const (
	// pollTimeout is passed to the library, which asks Telegram to hold each
	// getUpdates request for pollTimeout-1s. httpTimeout bounds every request
	// and leaves a margin over that.
	pollTimeout = 50 * time.Second
	httpTimeout = 60 * time.Second

	getMeTimeout   = 15 * time.Second
	profileTimeout = 30 * time.Second

	// handlerTimeout bounds one update, and so the wait for the updates in
	// progress on shutdown.
	handlerTimeout = 30 * time.Second

	// workers is how many updates are handled at the same time.
	workers = 4
)

// Users is the user storage the handlers need.
type Users interface {
	JoinWaitlist(ctx context.Context, u storage.User, now time.Time) (joined bool, err error)
	LinkFaceitAccount(ctx context.Context, u storage.User, acc storage.FaceitAccount, now time.Time) error
	FaceitAccount(ctx context.Context, telegramID int64) (storage.FaceitAccount, error)
	UTCOffset(ctx context.Context, telegramID int64) (minutes int, ok bool, err error)
	SetUTCOffset(ctx context.Context, u storage.User, minutes int, now time.Time) error
}

// Faceit is the FACEIT Data API the handlers need.
type Faceit interface {
	PlayerByNickname(ctx context.Context, nickname string) (faceit.Player, error)
	Player(ctx context.Context, playerID string) (faceit.Player, error)
	RecentMatches(ctx context.Context, playerID string, n int) ([]faceit.MatchSummary, error)
}

// Jobs is the queue of demo analyses the handlers need, an *analysis.Queue.
type Jobs interface {
	Submit(ctx context.Context, r analysis.Request) (analysis.Outcome, error)
	View(ctx context.Context, id int64) (analysis.View, error)
	SetMessage(ctx context.Context, id int64, messageID int) error
	Run(ctx context.Context, n analysis.Notifier) error
}

// Bot handles updates from Telegram.
type Bot struct {
	api     *bot.Bot
	users   Users
	faceit  Faceit
	jobs    Jobs
	admins  map[int64]bool
	pending *pendingLinks
	status  *statusUpdates
	log     *slog.Logger
	now     func() time.Time
}

// New prepares the bot without calling Telegram. Until the beta only the
// admins may send demos. Extra options are for tests.
func New(token string, users Users, fc Faceit, jobs Jobs, admins []int64, log *slog.Logger, opts ...bot.Option) (*Bot, error) {
	b := &Bot{
		users:   users,
		faceit:  fc,
		jobs:    jobs,
		admins:  make(map[int64]bool, len(admins)),
		pending: newPendingLinks(),
		status:  newStatusUpdates(),
		log:     log,
		now:     time.Now,
	}
	for _, id := range admins {
		b.admins[id] = true
	}
	options := []bot.Option{
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(pollTimeout, &http.Client{Timeout: httpTimeout}),
		// Handlers run inside the workers, so Start returns only after the
		// updates in progress are handled.
		bot.WithNotAsyncHandlers(),
		bot.WithWorkers(workers),
		bot.WithMiddlewares(b.recoverPanic, detachContext, privateChatsOnly),
		// Replaces the library default, which logs whole updates with user data.
		bot.WithDefaultHandler(b.handleDefault),
		bot.WithErrorsHandler(func(err error) {
			log.Error("telegram api", "err", err)
		}),
	}
	api, err := bot.New(token, append(options, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("create telegram client: %w", err)
	}
	api.RegisterHandler(bot.HandlerTypeMessageText, "start", bot.MatchTypeCommandStartOnly, b.handle("start", b.start))
	api.RegisterHandler(bot.HandlerTypeMessageText, "link", bot.MatchTypeCommandStartOnly, b.handle("link", b.link))
	api.RegisterHandler(bot.HandlerTypeMessageText, "matches", bot.MatchTypeCommandStartOnly, b.handle("matches", b.matches))
	api.RegisterHandler(bot.HandlerTypeMessageText, "help", bot.MatchTypeCommandStartOnly, b.handle("help", b.help))
	api.RegisterHandler(bot.HandlerTypeCallbackQueryData, linkButtonPrefix, bot.MatchTypePrefix, b.handleButton("link", b.linkButton))
	api.RegisterHandler(bot.HandlerTypeCallbackQueryData, tzButtonPrefix, bot.MatchTypePrefix, b.handleButton("timezone", b.timezoneButton))
	api.RegisterHandler(bot.HandlerTypeCallbackQueryData, demoButtonPrefix, bot.MatchTypePrefix, b.handleButton("demo", b.demoButton))
	b.api = api
	return b, nil
}

// Run checks the token, registers the bot profile and handles updates until
// ctx is done, with the queue of demo analyses running alongside. It returns
// once the updates in progress are handled, the queue has put back the jobs
// in progress, and the status messages left are drawn.
func (b *Bot) Run(ctx context.Context) error {
	meCtx, cancel := context.WithTimeout(ctx, getMeTimeout)
	me, err := b.api.GetMe(meCtx)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("get bot info: %w", err)
	}
	b.log.Info("telegram bot authorized", "username", me.Username)

	profileCtx, cancel := context.WithTimeout(ctx, profileTimeout)
	changed, err := b.syncProfile(profileCtx)
	cancel()
	switch {
	case err != nil && ctx.Err() == nil:
		b.log.Warn("bot profile not fully registered, the bot keeps working", "changed", changed, "err", err)
	case len(changed) > 0:
		b.log.Info("bot profile updated", "changed", changed)
	case err == nil:
		b.log.Info("bot profile up to date")
	}

	// The status messages outlive the polling: the queue changes jobs until
	// it stops.
	statusCtx, stopStatus := context.WithCancel(context.WithoutCancel(ctx))
	statusDone := make(chan struct{})
	go func() {
		defer close(statusDone)
		b.runStatus(statusCtx)
	}()
	queueDone := make(chan struct{})
	go func() {
		defer close(queueDone)
		if err := b.jobs.Run(ctx, b.status); err != nil {
			b.log.Error("analysis queue stopped", "err", err)
		}
	}()

	b.log.Info("polling for updates")
	b.api.Start(ctx)
	b.log.Info("polling stopped")
	<-queueDone
	stopStatus()
	<-statusDone
	return nil
}

// recoverPanic keeps a panicking handler from bringing the whole bot down.
func (b *Bot) recoverPanic(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, api *bot.Bot, upd *models.Update) {
		defer func() {
			if r := recover(); r != nil {
				b.log.Error("handler panic", "panic", r, "stack", string(debug.Stack()))
			}
		}()
		next(ctx, api, upd)
	}
}

// detachContext gives each update its own deadline in place of the polling
// context, so that on shutdown the updates in progress are finished rather
// than cut off.
func detachContext(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, api *bot.Bot, upd *models.Update) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), handlerTimeout)
		defer cancel()
		next(ctx, api, upd)
	}
}

// privateChatsOnly drops everything but messages and button presses in
// private chats: the bot does not work in groups and channels, and edits and
// other updates need no answer.
func privateChatsOnly(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, api *bot.Bot, upd *models.Update) {
		switch {
		case upd.Message != nil:
			if upd.Message.From == nil || upd.Message.Chat.Type != models.ChatTypePrivate {
				return
			}
		case upd.CallbackQuery != nil:
			if chat := buttonChat(upd.CallbackQuery); chat == nil || chat.Type != models.ChatTypePrivate {
				return
			}
		default:
			return
		}
		next(ctx, api, upd)
	}
}

// buttonChat returns the chat of the message with the pressed button, nil
// for a button under a message sent in inline mode.
func buttonChat(cq *models.CallbackQuery) *models.Chat {
	switch {
	case cq.Message.Message != nil:
		return &cq.Message.Message.Chat
	case cq.Message.InaccessibleMessage != nil:
		return &cq.Message.InaccessibleMessage.Chat
	}
	return nil
}
