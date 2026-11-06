package telegram

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// statusDrainTimeout bounds drawing the status messages left when the bot
// stops.
const statusDrainTimeout = 10 * time.Second

// statusUpdates collects the jobs whose status messages are to be drawn.
// It is the analysis.Notifier of the queue: every change of a job, from the
// handlers or the workers, ends up here, and one goroutine draws the
// messages from the state of the jobs in the database. With one drawer the
// edits of a message never overtake each other, and a message is never sent
// twice.
type statusUpdates struct {
	mu      sync.Mutex
	pending map[int64]bool
	// drawn is the text each message shows now, so that the same text is
	// not sent again; jobs that are over leave it.
	drawn map[int64]string
	wake  chan struct{}
}

func newStatusUpdates() *statusUpdates {
	return &statusUpdates{pending: map[int64]bool{}, drawn: map[int64]string{}, wake: make(chan struct{}, 1)}
}

// JobsChanged asks to draw the status messages of the jobs. It does not
// block.
func (s *statusUpdates) JobsChanged(ids ...int64) {
	s.mu.Lock()
	for _, id := range ids {
		s.pending[id] = true
	}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// take returns the jobs to draw, in order, and forgets them.
func (s *statusUpdates) take() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int64, 0, len(s.pending))
	for id := range s.pending {
		ids = append(ids, id)
	}
	clear(s.pending)
	slices.Sort(ids)
	return ids
}

func (s *statusUpdates) shown(id int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drawn[id]
}

func (s *statusUpdates) setShown(id int64, text string, over bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if over {
		delete(s.drawn, id)
		return
	}
	s.drawn[id] = text
}

// runStatus draws the status messages as the jobs change until ctx is
// done; then it draws what is left, within statusDrainTimeout.
func (b *Bot) runStatus(ctx context.Context) {
	for {
		select {
		case <-b.status.wake:
			b.drawStatus(ctx)
		case <-ctx.Done():
			drain, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusDrainTimeout)
			b.drawStatus(drain)
			cancel()
			return
		}
	}
}

// drawStatus draws the status messages of the jobs that changed, until none
// is left.
func (b *Bot) drawStatus(ctx context.Context) {
	for {
		ids := b.status.take()
		if len(ids) == 0 {
			return
		}
		for _, id := range ids {
			if err := b.drawJob(ctx, id); err != nil {
				b.log.Warn("draw the status of a job", "job", id, "err", err)
			}
		}
	}
}

// drawJob brings the status message of the job up to date: it sends the
// message the first time, and edits it after. A message the user deleted is
// sent again.
func (b *Bot) drawJob(ctx context.Context, id int64) error {
	v, err := b.jobs.View(ctx, id)
	if err != nil {
		return err
	}
	text, markup := statusText(v)
	if text == "" {
		return errors.New("a job done without its match")
	}
	over := v.Job.Status == storage.JobDone || v.Job.Status == storage.JobFailed
	if v.Job.StatusMessageID != 0 {
		if b.status.shown(id) == text {
			b.status.setShown(id, text, over)
			return nil
		}
		err := b.editText(ctx, v.Job.TelegramID, v.Job.StatusMessageID, text, markup)
		if err == nil {
			b.status.setShown(id, text, over)
			return nil
		}
		if !errors.Is(err, bot.ErrorBadRequest) || !strings.Contains(err.Error(), "message to edit not found") {
			return err
		}
	}
	msg, err := b.sendMessage(ctx, v.Job.TelegramID, text, markup)
	if err != nil {
		return err
	}
	if err := b.jobs.SetMessage(ctx, id, msg.ID); err != nil {
		return fmt.Errorf("save the status message: %w", err)
	}
	b.status.setShown(id, text, over)
	return nil
}

// editText replaces the text of a message the bot sent, and its buttons
// with markup; nil markup removes them. The same text again is no error.
func (b *Bot) editText(ctx context.Context, chatID int64, messageID int, text string, markup models.ReplyMarkup) error {
	params := &bot.EditMessageTextParams{
		ChatID:             chatID,
		MessageID:          messageID,
		Text:               text,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
	}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	_, err := b.api.EditMessageText(ctx, params)
	if err != nil && !(errors.Is(err, bot.ErrorBadRequest) && strings.Contains(err.Error(), "message is not modified")) {
		return fmt.Errorf("edit message: %w", err)
	}
	return nil
}
