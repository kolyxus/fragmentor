package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// The numbers of a parsed match, the metrics of its players and the facts of
// their reviews, are counted from the anonymous match of its journal, the
// same way after a parse and when a new version of the formulas or of the
// findings comes: then the match is counted again from its journal, with no
// new link (docs/DECISIONS.md, 2026-11-09, the storage). Only a new version
// of the journal itself needs the demo again.

// recountTimeout bounds counting a match again from its journal.
const recountTimeout = time.Minute

// errOldJournal means the journal of a match is of an older version: the
// match needs its demo again.
var errOldJournal = errors.New("the journal of the match is of an older version")

// storedMetrics is what is kept of the metrics of a player: the blocks for
// the match and for each side, without the SteamID and the team of
// metrics.Player, which the row has.
type storedMetrics struct {
	Match metrics.Stats `json:"match"`
	T     metrics.Stats `json:"t"`
	CT    metrics.Stats `json:"ct"`
}

// count fills in the metrics and the facts of the players of the match from
// anon, its anonymous match: players[i] is seat i+1 and anon.Players[i]. The
// facts of a player have his own past, his facts of the matches he played
// before this one; storage.SaveAnalysis keeps them only for the users of the
// bot.
func (q *Queue) count(ctx context.Context, match storage.ParsedMatch, anon *demo.Match, players []storage.MatchPlayer) error {
	if len(players) != len(anon.Players) {
		return fmt.Errorf("%d players kept, %d in the journal", len(players), len(anon.Players))
	}
	a := metrics.Analyze(anon)
	past := make(map[uint64]findings.History, len(players))
	for i, p := range players {
		if p.Seat != i+1 {
			return fmt.Errorf("player %d sits at %d", i, p.Seat)
		}
		list, err := q.store.PastFacts(ctx, p.SteamID64, match.MatchID, match.StartedAt, metrics.Version, findings.FactsVersion, findings.MaxHistory)
		if err != nil {
			return fmt.Errorf("load the past: %w", err)
		}
		var h findings.History
		for _, b := range list {
			var f findings.Facts
			if err := json.Unmarshal(b, &f); err != nil {
				return fmt.Errorf("decode past facts: %w", err)
			}
			h.AddFacts(f)
		}
		past[a.Players[i].SteamID64] = h
	}
	reports := findings.Analyze(a, past)
	c := content.Default()
	drills := func(rule findings.RuleID) []string { return c.DrillsFor(rule, anon.Map) }
	for i := range players {
		ap := a.Players[i]
		b, err := json.Marshal(storedMetrics{Match: ap.Match, T: ap.T, CT: ap.CT})
		if err != nil {
			return fmt.Errorf("encode metrics: %w", err)
		}
		f := findings.BuildFacts(findings.FactsInput{Match: anon, Analysis: a, Player: i, Report: reports[i],
			Level: players[i].SkillLevel, Drills: drills})
		fb, err := json.Marshal(f)
		if err != nil {
			return fmt.Errorf("encode facts: %w", err)
		}
		players[i].Metrics, players[i].Facts, players[i].FactsVersion = b, fb, findings.FactsVersion
	}
	return nil
}

// journal loads the parsed match and its players and reads its journal, the
// anonymous match: what counting the match again needs. A journal of an
// older version fails with errOldJournal, one that cannot be read with
// demo.ErrBadJournal: either way the match needs its demo again.
func (q *Queue) journal(ctx context.Context, matchID string) (storage.ParsedMatch, *demo.Match, []storage.MatchPlayer, error) {
	match, j, players, err := q.store.LoadAnalysis(ctx, matchID)
	if err != nil {
		return storage.ParsedMatch{}, nil, nil, fmt.Errorf("load the match: %w", err)
	}
	if j.Version != demo.JournalVersion {
		return storage.ParsedMatch{}, nil, nil, errOldJournal
	}
	anon, err := demo.DecodeJournal(j.Data)
	if err != nil {
		return storage.ParsedMatch{}, nil, nil, err
	}
	return match, anon, players, nil
}

// recount counts the metrics and the facts of a match again from its
// journal and keeps them.
func (q *Queue) recount(ctx context.Context, matchID string) error {
	match, anon, players, err := q.journal(ctx, matchID)
	if err != nil {
		return err
	}
	if err := q.count(ctx, match, anon, players); err != nil {
		return err
	}
	if err := q.store.SaveRecount(ctx, matchID, metrics.Version, players); err != nil {
		return fmt.Errorf("save the recount: %w", err)
	}
	return nil
}

// fresh reports whether the numbers of the match are of the current
// versions for the player: the metrics of the match and his facts.
func fresh(res storage.MatchResult) bool {
	return res.MetricsVersion == metrics.Version && res.FactsVersion == findings.FactsVersion
}

// recountStale counts again one match whose metrics, or the facts of a user
// of the bot in it, are of an older version: the earliest played first, so
// that the past of a player is counted before the matches that follow it.
// That is after a new version of the formulas or of the findings, and for a
// user who linked his account after his matches were parsed. A match that
// failed is not tried again until the bot restarts. It reports whether it
// counted one; one recount runs at a time.
func (q *Queue) recountStale(ctx context.Context) bool {
	if !q.recounting.TryLock() {
		return false
	}
	defer q.recounting.Unlock()
	ids, err := q.store.StaleMatches(ctx, demo.JournalVersion, metrics.Version, findings.FactsVersion, len(q.recountFailed)+1)
	if err != nil {
		if ctx.Err() == nil {
			q.log.Error("find the matches to count again", "err", err)
		}
		return false
	}
	for _, id := range ids {
		if q.recountFailed[id] {
			continue
		}
		start := q.now()
		rctx, cancel := context.WithTimeout(ctx, recountTimeout)
		err := q.recount(rctx, id)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			// The match is not named: its ID leads to the page of the
			// players.
			q.recountFailed[id] = true
			q.log.Error("count a match again", "err", err)
			return false
		}
		q.log.Info("match counted again", "duration", q.now().Sub(start).Round(time.Millisecond))
		return true
	}
	return false
}
