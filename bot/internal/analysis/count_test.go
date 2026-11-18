package analysis

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// factsOf returns the facts kept of the player at the seat of the match.
func (f *fixture) factsOf(matchID string, seat int) findings.Facts {
	f.t.Helper()
	_, _, players, err := f.store.LoadAnalysis(f.ctx, matchID)
	if err != nil {
		f.t.Fatal(err)
	}
	var facts findings.Facts
	if err := json.Unmarshal(players[seat-1].Facts, &facts); err != nil {
		f.t.Fatalf("facts of seat %d: %v", seat, err)
	}
	return facts
}

// openingsDemo is a match of 10 rounds on Mirage, all won by team 1: in
// each player 6 kills player 1 first, and nobody else dies. Player 1 loses
// every opening duel.
func openingsDemo() *demo.Match {
	m := testDemo()
	m.Teams[0].Score, m.Teams[0].FirstHalf = 0, 0
	m.Teams[1].Score, m.Teams[1].FirstHalf = 10, 10
	alive := m.Rounds[0].Alive
	m.Rounds, m.Kills = nil, nil
	for n := 1; n <= 10; n++ {
		start := n * 10000
		m.Rounds = append(m.Rounds, demo.Round{Number: n, Winner: 1, WinnerSide: demo.T, Reason: "t_win", Score: [2]int{0, n},
			FreezeEndTick: start, EndTick: start + 5000, Alive: alive})
		m.Kills = append(m.Kills, demo.Kill{Tick: start + 500, Round: n, Killer: &demo.Ref{ID: steamOf(6), Side: demo.T},
			Victim: demo.Ref{ID: steamOf(1), Side: demo.CT}, Weapon: "AK-47", Class: demo.ClassRifle})
	}
	return m
}

// TestFactsDrillsOfTheMap checks that the drills of the facts are those
// that fit the map of the match: against the opening duels lost on Mirage,
// the prefires of 5E for Mirage come first.
func TestFactsDrillsOfTheMap(t *testing.T) {
	f := newFixture(t)
	f.q.parse = func(_ context.Context, r io.Reader) (*demo.Match, error) {
		if _, err := io.Copy(io.Discard, r); err != nil {
			return nil, err
		}
		return openingsDemo(), nil
	}
	f.accept(userP1, testMatchID)
	if got := f.step(); got.Status != storage.JobDone {
		t.Fatalf("job = %+v, want done", got)
	}
	facts := f.factsOf(testMatchID, 1)
	i := slices.IndexFunc(facts.Findings, func(ff findings.FactFinding) bool { return ff.Rule == findings.OpeningLoss })
	if i < 0 {
		t.Fatalf("no finding of the opening duels lost: %+v", facts.Findings)
	}
	want := content.Default().DrillsFor(findings.OpeningLoss, "de_mirage")
	if !slices.Equal(facts.Findings[i].Drills, want) || len(want) == 0 || want[0] != "prefire_5e" {
		t.Errorf("drills %v, want %v, the prefires of Mirage first", facts.Findings[i].Drills, want)
	}
}

// TestOwnPast checks the past of a user in his facts: his own average is
// made of the facts of his matches played before, from three of them on.
// The four matches are the same demo, so the past of the last one is three
// times its numbers.
func TestOwnPast(t *testing.T) {
	f := newFixture(t)
	ids := []string{testMatchID, otherMatchID, thirdMatchID, fourthMatchID}
	for i, id := range ids {
		d := testDetails()
		d.ID, d.StartedAt = id, t0.Add(time.Duration(i-len(ids))*time.Hour)
		f.faceit.set(id, d)
		f.accept(userP1, id)
		if got := f.step(); got.Status != storage.JobDone {
			t.Fatalf("job of match %d = %+v, want done", i+1, got)
		}
	}
	for _, c := range f.factsOf(thirdMatchID, 1).Comparisons {
		if c.Own != nil {
			t.Errorf("%s: own average of two past matches: %+v", c.Measure, c.Own)
		}
	}
	counted := 0
	for _, c := range f.factsOf(fourthMatchID, 1).Comparisons {
		if c.N == 0 {
			continue
		}
		counted++
		if c.Own == nil || c.Own.N != 3*c.N || c.Own.X != 3*c.X {
			t.Errorf("%s: own average %+v, want 3 times %v of %v", c.Measure, c.Own, c.X, c.N)
		}
	}
	if counted == 0 {
		t.Error("no measure has opportunities")
	}
}

// TestRecountChecksSeats corrupts the players kept of a match: the recount
// fails rather than give a player the numbers of another.
func TestRecountChecksSeats(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	f.step()
	// Nine players kept, ten in the journal.
	if _, err := f.db().Exec(`DELETE FROM match_players WHERE seat = 10`); err != nil {
		t.Fatal(err)
	}
	if err := f.q.recount(f.ctx, testMatchID); err == nil {
		t.Error("recount() with 9 players of 10 kept")
	}
	// Ten again, the last at seat 11.
	if _, err := f.db().Exec(`INSERT INTO match_players (match_id, seat, steam_id64, team, metrics) VALUES (?, 11, ?, 1, '{}')`,
		testMatchID, strconv.FormatUint(steamOf(10), 10)); err != nil {
		t.Fatal(err)
	}
	if err := f.q.recount(f.ctx, testMatchID); err == nil {
		t.Error("recount() with a player out of his seat")
	}
}

// TestRecountAfterFailure checks that a match that failed to be counted
// again does not hold up the others: the idle worker goes on to the next.
func TestRecountAfterFailure(t *testing.T) {
	f := newFixture(t)
	later := testDetails()
	later.ID, later.StartedAt = otherMatchID, t0.Add(-30*time.Minute)
	f.faceit.set(otherMatchID, later)
	f.accept(userP1, testMatchID)
	f.step()
	f.accept(userP1, otherMatchID)
	f.step()
	// Both of older formulas; the one played first cannot be read.
	for _, q := range []string{
		`UPDATE matches SET metrics_version = metrics_version - 1`,
		`UPDATE match_journals SET journal = x'00010203' WHERE match_id = '` + testMatchID + `'`,
	} {
		if _, err := f.db().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if f.q.recountStale(f.ctx) {
		t.Error("a journal that cannot be read was counted again")
	}
	if !f.q.recountStale(f.ctx) {
		t.Error("the match after the one that failed was not counted again")
	}
	if res, err := f.store.MatchResult(f.ctx, otherMatchID, strconv.FormatUint(steamOf(1), 10)); err != nil || res != done(1, 0) {
		t.Errorf("MatchResult() of the second match = %+v, %v; want the current versions", res, err)
	}
}
