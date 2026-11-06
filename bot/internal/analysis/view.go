package analysis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// View is a job as its status message shows it, as of now.
type View struct {
	Job storage.Job
	// For a queued job: its place in the queue from 1, and the time until
	// a worker should take it.
	Position int
	Wait     time.Duration
	// For a job done: the match, as the user saw it. HasResult is false if
	// the match is gone from the database.
	Result    storage.MatchResult
	HasResult bool
}

// View returns the job with the ID as of now.
func (q *Queue) View(ctx context.Context, id int64) (View, error) {
	j, err := q.store.Job(ctx, id)
	if err != nil {
		return View{}, fmt.Errorf("load job: %w", err)
	}
	v := View{Job: j}
	switch j.Status {
	case storage.JobQueued:
		state, err := q.store.QueueAhead(ctx, id)
		if err != nil {
			return View{}, fmt.Errorf("load queue: %w", err)
		}
		v.Position = state.Ahead + 1
		v.Wait = startIn(q.now(), state.Running, state.Ahead, Workers, q.durations.estimate())
	case storage.JobDone:
		var playerID string
		acc, err := q.store.FaceitAccount(ctx, j.TelegramID)
		switch {
		case err == nil:
			playerID = acc.PlayerID
		case !errors.Is(err, storage.ErrNotFound):
			return View{}, fmt.Errorf("load faceit account: %w", err)
		}
		res, err := q.store.MatchResult(ctx, j.MatchID, playerID)
		switch {
		case err == nil:
			v.Result, v.HasResult = res, true
		case !errors.Is(err, storage.ErrNotFound):
			return View{}, fmt.Errorf("load match: %w", err)
		}
	}
	return v, nil
}

// SetMessage remembers the message that shows the status of the job.
func (q *Queue) SetMessage(ctx context.Context, id int64, messageID int) error {
	return q.store.SetJobMessage(ctx, id, messageID)
}
