package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// result is the outcome of a job.
type result struct {
	reason string // why the job failed, "" if it is done
	err    error  // what went wrong, for the log

	// For a job done: whether the demo was parsed (or the match was parsed
	// already), and what it was.
	parsed     bool
	mapName    string
	rounds     int
	suspicious bool
	bytes      int64 // downloaded
}

// storedMetrics is what is kept of the metrics of a player: the blocks for
// the match and for each side, without the SteamID and the team of
// metrics.Player, since they go into the anonymous reference samples.
type storedMetrics struct {
	Match metrics.Stats `json:"match"`
	T     metrics.Stats `json:"t"`
	CT    metrics.Stats `json:"ct"`
}

// process does the job: if the match is parsed already with the user in
// it, nothing; otherwise it downloads and parses the demo, while the link
// lives, and saves the match with the metrics of its players.
func (q *Queue) process(ctx context.Context, job storage.Job) (res result) {
	defer func() {
		if r := recover(); r != nil {
			res = result{reason: ReasonInternal, err: fmt.Errorf("panic: %v\n%s", r, debug.Stack())}
		}
	}()

	acc, err := q.store.FaceitAccount(ctx, job.TelegramID)
	if errors.Is(err, storage.ErrNotFound) {
		return result{reason: ReasonNotLinked}
	}
	if err != nil {
		return result{reason: ReasonInternal, err: err}
	}
	// Another job of the match may have parsed it meanwhile.
	if _, ok, err := q.analyzed(ctx, job.MatchID, acc.PlayerID); err != nil {
		return result{reason: ReasonInternal, err: err}
	} else if ok {
		return result{}
	}

	var (
		m    *demo.Match
		size int64
	)
	for try := 1; ; try++ {
		if !job.LinkExpires.IsZero() && job.LinkExpires.Sub(q.now()) < LinkMargin {
			return result{reason: ReasonLinkExpired, err: err}
		}
		m, size, err = q.fetch(ctx, job.DemoURL)
		if err == nil {
			break
		}
		reason := failReason(ctx, err)
		if reason != ReasonDownload || try == downloadTries {
			return result{reason: reason, err: err, bytes: size}
		}
		q.log.Warn("download failed, trying again", "job", job.ID, "err", err)
		if err := q.sleep(ctx, retryPause); err != nil {
			return result{reason: ReasonTimeout, err: err}
		}
	}

	details, err := q.faceit.Match(ctx, job.MatchID)
	if err != nil {
		if ctx.Err() != nil {
			return result{reason: ReasonTimeout, err: err}
		}
		return result{reason: ReasonFaceit, err: err}
	}
	players, reason, err := playersOf(m, details, acc.PlayerID)
	if reason != "" {
		return result{reason: reason, err: err}
	}
	match := storage.ParsedMatch{
		MatchID:        job.MatchID,
		Map:            m.Map,
		StartedAt:      details.StartedAt,
		Score:          [2]int{m.Teams[0].Score, m.Teams[1].Score},
		Rounds:         len(m.Rounds),
		Suspicious:     m.Suspicious,
		Warnings:       m.Warnings,
		MetricsVersion: metrics.Version,
	}
	if err := q.store.SaveMatch(ctx, match, players, q.now()); err != nil {
		return result{reason: ReasonInternal, err: err}
	}
	return result{parsed: true, mapName: m.Map, rounds: len(m.Rounds), suspicious: m.Suspicious, bytes: size}
}

// fetch downloads the demo by the link and parses it on the way. It returns
// how much it downloaded, also on failure.
func (q *Queue) fetch(ctx context.Context, link string) (*demo.Match, int64, error) {
	body, err := q.download.open(ctx, link)
	if err != nil {
		return nil, 0, err
	}
	defer body.Close()
	m, err := q.parse(ctx, body)
	// The parser reports a failure of the network or of the limit in its
	// own way; the body knows what it was.
	if body.err != nil {
		return nil, body.n, body.err
	}
	if err != nil {
		return nil, body.n, err
	}
	return m, body.n, nil
}

// failReason tells why a download and parse failed.
func failReason(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, errLinkRejected):
		return ReasonLinkRejected
	case errors.Is(err, errDemoGone):
		return ReasonDemoGone
	case errors.Is(err, errTooLarge), errors.Is(err, demo.ErrTooLarge):
		return ReasonTooLarge
	case ctx.Err() != nil:
		return ReasonTimeout
	case errors.Is(err, errDownload):
		return ReasonDownload
	}
	return ReasonBadDemo
}

// playersOf makes the players of the parsed match m to save: every player
// of the demo with the metrics, and the FACEIT player ID and level from the
// roster of the match details, joined by SteamID64. Which of them are kept
// with the IDs is for storage.SaveMatch to decide. It checks that the demo
// is of the match: the map is the same, and most of the players of the demo
// are in the roster; and that the user's account is in the roster. On
// failure it returns the reason.
func playersOf(m *demo.Match, details faceit.Match, playerID string) ([]storage.PlayerMetrics, string, error) {
	if details.Map != "" && !sameMap(m.Map, details.Map) {
		return nil, ReasonBadDemo, fmt.Errorf("the demo is of %s, the match of %s", m.Map, details.Map)
	}
	if !inRoster(details, playerID) {
		return nil, ReasonNotInMatch, nil
	}
	roster := make(map[string]faceit.MatchPlayer)
	for _, t := range details.Teams {
		for _, p := range t.Players {
			roster[p.SteamID64] = p
		}
	}

	computed := metrics.Compute(m)
	out := make([]storage.PlayerMetrics, 0, len(m.Players))
	found := 0
	for i, p := range m.Players {
		steamID := strconv.FormatUint(p.SteamID64, 10)
		rp, ok := roster[steamID]
		if ok {
			found++
		}
		b, err := json.Marshal(storedMetrics{Match: computed[i].Match, T: computed[i].T, CT: computed[i].CT})
		if err != nil {
			return nil, ReasonInternal, fmt.Errorf("encode metrics: %w", err)
		}
		out = append(out, storage.PlayerMetrics{
			PlayerID:   rp.ID,
			SteamID64:  steamID,
			SkillLevel: rp.SkillLevel,
			Team:       p.Team,
			Metrics:    b,
		})
	}
	if found*2 < len(m.Players) {
		return nil, ReasonBadDemo, fmt.Errorf("%d of %d players of the demo are in the match", found, len(m.Players))
	}
	return out, "", nil
}

// sameMap compares map IDs such as "de_cache" and "workshop/123/de_cache".
func sameMap(a, b string) bool {
	return strings.EqualFold(a[strings.LastIndexByte(a, '/')+1:], b[strings.LastIndexByte(b, '/')+1:])
}
