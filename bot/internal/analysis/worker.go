package analysis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// Run parses the demos of the queue, Workers at a time, until ctx is done,
// and tells n of every change of a job. First it deals with the jobs a bot
// that died left running: a job taken MaxAttempts times fails, the rest go
// back to the queue. When ctx is done, the jobs in progress are stopped and
// put back in the queue for the next start, and Run returns once they are.
func (q *Queue) Run(ctx context.Context, n Notifier) error {
	recovered, err := q.store.RecoverJobs(ctx, MaxAttempts, q.now())
	if err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	}
	if len(recovered) > 0 {
		q.log.Info("jobs left running by the last run handled", "jobs", len(recovered))
	}
	// After a restart the status messages are drawn again: of the jobs
	// failed just now and of those in the queue.
	active, err := q.store.ActiveJobIDs(ctx)
	if err != nil {
		return fmt.Errorf("load active jobs: %w", err)
	}
	if ids := append(recovered, active...); len(ids) > 0 {
		n.JobsChanged(ids...)
	}

	var wg sync.WaitGroup
	for range Workers {
		wg.Go(func() { q.work(ctx, n) })
	}
	wg.Wait()
	return nil
}

// work takes the jobs one after another until ctx is done.
func (q *Queue) work(ctx context.Context, n Notifier) {
	for ctx.Err() == nil {
		job, ok, err := q.store.ClaimJob(ctx, q.now())
		if err != nil && ctx.Err() == nil {
			q.log.Error("take a job", "err", err)
		}
		if !ok {
			select {
			case <-ctx.Done():
			case <-q.wake:
			case <-time.After(pollInterval):
			}
			continue
		}
		// Another worker may find a job too.
		q.poke()
		// The job started, and the queue behind it moved.
		q.changed(ctx, n, job.ID)
		q.run(ctx, n, job)
	}
}

// changed tells n of the jobs and of all the jobs in the queue, whose
// places and waits changed with them.
func (q *Queue) changed(ctx context.Context, n Notifier, ids ...int64) {
	queued, err := q.store.QueuedJobIDs(ctx)
	if err != nil && ctx.Err() == nil {
		q.log.Error("load queued jobs", "err", err)
	}
	if ids = append(ids, queued...); len(ids) > 0 {
		n.JobsChanged(ids...)
	}
}

// run parses the demo of the job and keeps the outcome.
func (q *Queue) run(ctx context.Context, n Notifier, job storage.Job) {
	start := q.now()
	q.log.Info("job started", "job", job.ID, "attempt", job.Attempts)
	jobCtx, cancel := context.WithTimeout(ctx, q.jobTimeout)
	res := q.process(jobCtx, job)
	cancel()

	// The outcome is written even when the bot is stopping.
	wctx, wcancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer wcancel()
	var err error
	switch {
	case res.reason == "":
		err = q.store.FinishJob(wctx, job.ID, q.now())
		if res.parsed {
			q.durations.add(q.now().Sub(start))
		}
		q.log.Info("job done", "job", job.ID, "parsed", res.parsed, "map", res.mapName, "rounds", res.rounds,
			"suspicious", res.suspicious, "mib", res.bytes>>20, "duration", q.now().Sub(start).Round(time.Millisecond))
	case ctx.Err() != nil:
		// The bot is stopping, the demo is not to blame. The status
		// message keeps "parsing" and is redrawn after the restart.
		if err := q.store.RequeueJob(wctx, job.ID); err != nil {
			q.log.Error("put the job back in the queue", "job", job.ID, "err", err)
		} else {
			q.log.Info("job put back in the queue: the bot is stopping", "job", job.ID)
		}
		return
	default:
		err = q.store.FailJob(wctx, job.ID, res.reason, q.now())
		q.log.Warn("job failed", "job", job.ID, "reason", res.reason, "err", res.err,
			"duration", q.now().Sub(start).Round(time.Millisecond))
	}
	if err != nil {
		q.log.Error("save the outcome of the job", "job", job.ID, "err", err)
	}
	q.changed(wctx, n, job.ID)
}
