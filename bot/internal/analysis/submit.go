package analysis

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// ErrFaceit means FACEIT did not answer, so the match could not be checked.
var ErrFaceit = errors.New("analysis: FACEIT unavailable")

// errQueueTooLong refuses a job whose link would run out in the queue.
var errQueueTooLong = errors.New("analysis: the link would run out in the queue")

// Request is a demo link a user sent.
type Request struct {
	TelegramID int64
	Language   string // of the bot, "ru" or "en"
	Link       faceit.DemoLink
}

// Verdict is what became of a request.
type Verdict int

// The verdicts. Only Accepted adds a job.
const (
	Accepted         Verdict = iota // the job joined the queue
	Analyzed                        // the match is parsed already, the user in it
	Duplicate                       // the user has the demo in the queue already
	TooMany                         // the user has MaxActiveJobs demos in the queue
	NotLinked                       // the user has not linked a FACEIT account
	LinkExpired                     // the link has run out, or is about to
	QueueTooLong                    // the link would run out before its turn
	MatchNotFound                   // FACEIT has no such match
	MatchNotFinished                // the match is not over
	MultiMap                        // the match has more than one map
	NotInMatch                      // the linked account did not play the match
)

// Outcome is the answer to a request.
type Outcome struct {
	Verdict Verdict
	Job     storage.Job           // Accepted: the job added
	Result  storage.MatchResult   // Analyzed: the match
	Account storage.FaceitAccount // NotInMatch: the account linked
	// Position is the place of the job in the queue from 1, and Wait the
	// time until a worker should take it: of the job added for Accepted,
	// of the job that would be for QueueTooLong.
	Position int
	Wait     time.Duration
	// For QueueTooLong: how long the link lives yet, how long FACEIT signs
	// a link for, and when a fresh link would last until its turn if the
	// queue only shrinks.
	LinkLeft time.Duration
	Lifetime time.Duration
	RetryIn  time.Duration
}

// Submit checks a demo link and puts its job at the end of the queue. The
// link must not be about to run out; the user must have linked a FACEIT
// account, have the demo not in the queue already and fewer than
// MaxActiveJobs demos there; FACEIT must know the match, finished, of one
// map, with the user's account in it. A match parsed already with the
// user in it is not parsed again. Last, the job must get its turn while its
// link lives, by the estimate of the queue; the check and the insert are
// one transaction. A verdict other than Accepted tells why no job was added;
// an error is a failure of the database or of FACEIT (ErrFaceit).
func (q *Queue) Submit(ctx context.Context, r Request) (Outcome, error) {
	now := q.now()
	link := r.Link
	if !link.Expires.IsZero() && link.Expires.Sub(now) < LinkMargin {
		return Outcome{Verdict: LinkExpired}, nil
	}

	acc, err := q.store.FaceitAccount(ctx, r.TelegramID)
	if errors.Is(err, storage.ErrNotFound) {
		return Outcome{Verdict: NotLinked}, nil
	}
	if err != nil {
		return Outcome{}, fmt.Errorf("load faceit account: %w", err)
	}
	active, err := q.store.ActiveJobs(ctx, r.TelegramID)
	if err != nil {
		return Outcome{}, fmt.Errorf("load active jobs: %w", err)
	}
	for _, j := range active {
		if j.MatchID == link.MatchID {
			return Outcome{Verdict: Duplicate, Job: j}, nil
		}
	}
	if len(active) >= MaxActiveJobs {
		return Outcome{Verdict: TooMany}, nil
	}
	if res, ok, err := q.analyzed(ctx, link.MatchID, acc.PlayerID); err != nil {
		return Outcome{}, err
	} else if ok {
		return Outcome{Verdict: Analyzed, Result: res}, nil
	}

	details, err := q.faceit.Match(ctx, link.MatchID)
	if errors.Is(err, faceit.ErrNotFound) {
		return Outcome{Verdict: MatchNotFound}, nil
	}
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: %w", ErrFaceit, err)
	}
	if v := checkMatch(details, acc.PlayerID); v != Accepted {
		return Outcome{Verdict: v, Account: acc}, nil
	}

	var out Outcome
	d := q.durations.estimate()
	job, err := q.store.AddJob(ctx, storage.NewJob{
		TelegramID:  r.TelegramID,
		MatchID:     link.MatchID,
		DemoURL:     link.URL,
		LinkExpires: link.Expires,
		Language:    r.Language,
	}, now, func(state storage.QueueState) error {
		out.Position = state.Ahead + 1
		out.Wait = startIn(now, state.Running, state.Ahead, Workers, d)
		if !link.Expires.IsZero() && now.Add(out.Wait).After(link.Expires.Add(-LinkMargin)) {
			return errQueueTooLong
		}
		return nil
	})
	switch {
	case errors.Is(err, errQueueTooLong):
		out.Verdict = QueueTooLong
		out.LinkLeft = link.Expires.Sub(now)
		out.Lifetime = cmp.Or(link.Lifetime, defaultLifetime)
		out.RetryIn = max(out.Wait-out.Lifetime+LinkMargin, time.Minute)
		q.log.Info("demo refused: the link would run out in the queue",
			"position", out.Position, "wait", out.Wait.Round(time.Second), "link_left", out.LinkLeft.Round(time.Second))
		return out, nil
	case errors.Is(err, storage.ErrJobExists):
		return Outcome{Verdict: Duplicate}, nil
	case err != nil:
		return Outcome{}, fmt.Errorf("add job: %w", err)
	}
	out.Verdict, out.Job = Accepted, job
	q.log.Info("job queued", "job", job.ID, "position", out.Position, "wait", out.Wait.Round(time.Second),
		"link_left", linkLeft(link.Expires, now))
	q.poke()
	return out, nil
}

// analyzed reports whether the match is parsed with the current formulas
// and the FACEIT player is kept with it: then a new link changes nothing.
// A player of the match who began to use the bot after it was parsed is not
// kept with it, and the match is parsed again for them.
func (q *Queue) analyzed(ctx context.Context, matchID, playerID string) (storage.MatchResult, bool, error) {
	res, err := q.store.MatchResult(ctx, matchID, playerID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.MatchResult{}, false, nil
	}
	if err != nil {
		return storage.MatchResult{}, false, fmt.Errorf("load match: %w", err)
	}
	return res, res.MetricsVersion == metrics.Version && res.Team >= 0, nil
}

// checkMatch tells whether the match can be parsed for the FACEIT player:
// Accepted if it can.
func checkMatch(m faceit.Match, playerID string) Verdict {
	switch {
	case !strings.EqualFold(m.Status, "FINISHED"):
		return MatchNotFinished
	case m.BestOf > 1:
		return MultiMap
	case !inRoster(m, playerID):
		return NotInMatch
	}
	return Accepted
}

// inRoster reports whether the FACEIT player is in a team of the match.
func inRoster(m faceit.Match, playerID string) bool {
	for _, t := range m.Teams {
		for _, p := range t.Players {
			if p.ID == playerID {
				return true
			}
		}
	}
	return false
}

// linkLeft is how long a link lives yet, for the log: -1s if unknown.
func linkLeft(expires, now time.Time) time.Duration {
	if expires.IsZero() {
		return -time.Second
	}
	return expires.Sub(now).Round(time.Second)
}
