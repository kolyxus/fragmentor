// Package analysis turns the demo links users send into metrics in the
// database. It checks a link against FACEIT and the queue, keeps the queue
// of jobs in the database and parses the demos in workers of the process,
// Workers at a time. A demo goes from the network through the zstd decoder
// straight into the parser: it never touches the disk.
//
// A signed link lives for about five minutes, and the download must start
// before it runs out, so the queue knows how long it is: a link that would
// run out before its turn is refused at once, with the time to wait.
//
// The package knows nothing of Telegram: a Notifier hears of every change
// of a job, and the texts are the caller's business.
package analysis

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

const (
	// Workers is how many demos are parsed at the same time.
	Workers = 2
	// MaxActiveJobs is how many demos a user may have in the queue at once.
	MaxActiveJobs = 3
	// MaxAttempts is how many times a job is taken: a demo the bot died on
	// that many times is not tried again.
	MaxAttempts = 2
	// LinkMargin is how long before a link runs out its download has to
	// start: for the clocks of the bot and the storage, and the time to
	// ask FACEIT and to connect.
	LinkMargin = 30 * time.Second
)

const (
	// jobTimeout bounds a job: the download with the parse, FACEIT and the
	// database.
	jobTimeout = 5 * time.Minute
	// pollInterval is how often idle workers look into the queue without
	// being woken.
	pollInterval = 30 * time.Second
	// downloadTries is how many times a download that breaks is tried,
	// while the link lives.
	downloadTries = 2
	// retryPause is the pause before the second try.
	retryPause = 2 * time.Second
	// defaultLifetime is how long FACEIT signs a link for, for a link that
	// does not tell: 299 s on the links seen.
	defaultLifetime = 299 * time.Second
	// writeTimeout bounds writing the outcome of a job, which is done even
	// when the bot is stopping.
	writeTimeout = 10 * time.Second
)

// Reasons a job fails for, kept with the job.
const (
	// ReasonLinkExpired: the link ran out before the job's turn came.
	ReasonLinkExpired = "link_expired"
	// ReasonLinkRejected: the storage refused the link, 400, 401 or 403;
	// it answers 401 to a link that has run out.
	ReasonLinkRejected = "link_rejected"
	// ReasonDemoGone: the storage has no such demo, 404.
	ReasonDemoGone = "demo_gone"
	// ReasonDownload: the network failed, the storage answered 5xx or the
	// download broke off, twice.
	ReasonDownload = "download_failed"
	// ReasonTooLarge: the demo is larger than the limit.
	ReasonTooLarge = "too_large"
	// ReasonBadDemo: the parser refused the demo, or it is not of the match.
	ReasonBadDemo = "bad_demo"
	// ReasonFaceit: FACEIT did not give the details of the match.
	ReasonFaceit = "faceit_unavailable"
	// ReasonNotLinked: the user unlinked the FACEIT account meanwhile.
	ReasonNotLinked = "not_linked"
	// ReasonNotInMatch: the user linked an account that did not play the
	// match meanwhile.
	ReasonNotInMatch = "not_in_match"
	// ReasonTimeout: the job took longer than allowed.
	ReasonTimeout = "timeout"
	// ReasonInterrupted: the bot died on the job MaxAttempts times.
	ReasonInterrupted = storage.ReasonInterrupted
	// ReasonInternal: a failure of the bot, the database for one.
	ReasonInternal = "internal"
)

// Store is the database the queue needs.
type Store interface {
	FaceitAccount(ctx context.Context, telegramID int64) (storage.FaceitAccount, error)
	ActiveJobs(ctx context.Context, telegramID int64) ([]storage.Job, error)
	ActiveJobIDs(ctx context.Context) ([]int64, error)
	QueuedJobIDs(ctx context.Context) ([]int64, error)
	AddJob(ctx context.Context, j storage.NewJob, now time.Time, admit func(storage.QueueState) error) (storage.Job, error)
	Job(ctx context.Context, id int64) (storage.Job, error)
	QueueAhead(ctx context.Context, id int64) (storage.QueueState, error)
	ClaimJob(ctx context.Context, now time.Time) (storage.Job, bool, error)
	FinishJob(ctx context.Context, id int64, now time.Time) error
	FailJob(ctx context.Context, id int64, reason string, now time.Time) error
	RequeueJob(ctx context.Context, id int64) error
	RecoverJobs(ctx context.Context, maxAttempts int, now time.Time) ([]int64, error)
	SetJobMessage(ctx context.Context, id int64, messageID int) error
	MatchResult(ctx context.Context, matchID, playerID string) (storage.MatchResult, error)
	SaveMatch(ctx context.Context, m storage.ParsedMatch, players []storage.PlayerMetrics, now time.Time) error
}

// Faceit is the FACEIT Data API the queue needs.
type Faceit interface {
	Match(ctx context.Context, matchID string) (faceit.Match, error)
}

// Notifier hears that jobs changed: they joined the queue or moved in it,
// started or ended. JobsChanged must not block.
type Notifier interface {
	JobsChanged(ids ...int64)
}

// Queue is the queue of analysis jobs and its workers. It is safe for
// concurrent use.
type Queue struct {
	store     Store
	faceit    Faceit
	log       *slog.Logger
	download  *downloader
	durations *durations
	// wake tells idle workers that a job may be waiting.
	wake chan struct{}

	// Tests replace these.
	now        func() time.Time
	parse      func(ctx context.Context, r io.Reader) (*demo.Match, error)
	sleep      func(ctx context.Context, d time.Duration) error
	jobTimeout time.Duration
}

// New returns the queue of jobs kept in store. Run starts its workers.
func New(store Store, fc Faceit, log *slog.Logger) *Queue {
	return &Queue{
		store:     store,
		faceit:    fc,
		log:       log,
		download:  newDownloader(),
		durations: &durations{},
		wake:      make(chan struct{}, Workers),
		now:       time.Now,
		parse: func(ctx context.Context, r io.Reader) (*demo.Match, error) {
			return demo.Parse(ctx, r, demo.Options{})
		},
		sleep:      sleepCtx,
		jobTimeout: jobTimeout,
	}
}

// poke wakes an idle worker, if there is one.
func (q *Queue) poke() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
