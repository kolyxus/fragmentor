package demo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs"
)

func TestParseNotZstd(t *testing.T) {
	_, err := Parse(context.Background(), strings.NewReader("a page of HTML instead of a demo"), Options{})
	if !errors.Is(err, ErrNotZstd) {
		t.Errorf("err = %v, want ErrNotZstd", err)
	}
}

func TestParseNotDemo(t *testing.T) {
	zst := compress(t, []byte("HL2DEMO\x00 a CS:GO demo, or anything but a CS2 one"))
	_, err := Parse(context.Background(), bytes.NewReader(zst), Options{})
	if !errors.Is(err, ErrNotDemo) {
		t.Errorf("err = %v, want ErrNotDemo", err)
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := Parse(context.Background(), bytes.NewReader(compress(t, nil)), Options{})
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("err = %v, want ErrTruncated", err)
	}
}

func TestParseCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Parse(ctx, bytes.NewReader(compress(t, []byte("PBDEMS2\x00"))), Options{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestCatch(t *testing.T) {
	err := catch(func() error { panic("boom") })
	var pe *PanicError
	if !errors.As(err, &pe) || pe.Value != "boom" || len(pe.Stack) == 0 {
		t.Errorf("err = %#v, want a *PanicError with the value and a stack", err)
	}
	// demoinfocs raises a handler's panic again wrapped in its own type.
	err = catch(func() error { panic(&demoinfocs.PanicError{Value: "handler", Stack: []byte("stack")}) })
	if !errors.As(err, &pe) || pe.Value != "handler" || string(pe.Stack) != "stack" {
		t.Errorf("err = %#v, want the handler's value and stack", err)
	}
	if err := catch(func() error { return io.EOF }); err != io.EOF {
		t.Errorf("err = %v, want the error of f", err)
	}
}

// testDemos returns the demos in bot/testdata. They are not in git: the test
// skips without them, and with -short, as each takes seconds.
func testDemos(t *testing.T) []string {
	t.Helper()
	if testing.Short() {
		t.Skip("parsing real demos takes seconds")
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "*.dem.zst"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no demos in bot/testdata")
	}
	return files
}

// TestParseTestdata checks what holds for any FACEIT match. That the numbers
// are FACEIT's is checked by go run ./cmd/parsedemo -faceit testdata.
func TestParseTestdata(t *testing.T) {
	twoFlashbangs := 0 // deaths with two flashbangs in all the demos
	for _, file := range testDemos(t) {
		t.Run(filepath.Base(file)[:10], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			m, err := ParseFile(ctx, file, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if m.Suspicious || len(m.Warnings) != 0 {
				t.Errorf("suspicious %t, warnings: %q", m.Suspicious, m.Warnings)
			}
			if !strings.HasPrefix(m.Map, "de_") || m.TickRate != 64 {
				t.Errorf("map %q, tick rate %v; want de_*, 64", m.Map, m.TickRate)
			}
			a, b := m.Teams[0], m.Teams[1]
			if a.Score+b.Score != len(m.Rounds) || max(a.Score, b.Score) < 13 {
				t.Errorf("score %d:%d in %d rounds", a.Score, b.Score, len(m.Rounds))
			}
			for _, team := range m.Teams {
				if team.FirstHalf+team.SecondHalf+team.Overtime != team.Score {
					t.Errorf("halves of %+v do not add up", team)
				}
			}
			if a.FirstHalf+b.FirstHalf != 12 {
				t.Errorf("first half %d:%d, want 12 rounds", a.FirstHalf, b.FirstHalf)
			}
			for i, r := range m.Rounds {
				if r.Number != i+1 || r.Score[r.Winner] < 1 || (i > 0 && r.EndTick <= m.Rounds[i-1].EndTick) {
					t.Errorf("round %d = %+v", i+1, r)
				}
			}
			if last := m.Rounds[len(m.Rounds)-1].Score; last != [2]int{a.Score, b.Score} {
				t.Errorf("score after the last round %v, teams %d:%d", last, a.Score, b.Score)
			}
			var perTeam [2]int
			for _, p := range m.Players {
				perTeam[p.Team]++
				if p.SteamID64 == 0 || p.Name == "" || p.Headshots > p.Kills || p.Damage <= 0 || p.StartSide != m.Teams[p.Team].StartSide {
					t.Errorf("player %+v", p)
				}
			}
			if perTeam != [2]int{5, 5} {
				t.Errorf("players per team %v, want 5 and 5", perTeam)
			}
			checkEvents(t, m)
			twoFlashbangs += checkJournals(t, m)
		})
	}
	// The ammo of the pawn counts the second flashbang: in a few matches
	// someone dies with two.
	if !t.Failed() && twoFlashbangs == 0 {
		t.Error("nobody died with two flashbangs in any demo: the second flashbang is lost")
	}
}

// checkJournals checks the shots, sightings, flashes and grenades of a real
// match, and returns how many died with two flashbangs.
func checkJournals(t *testing.T, m *Match) int {
	t.Helper()
	inRound := func(round int) bool { return round >= 1 && round <= len(m.Rounds) }
	last := 0
	for i, s := range m.Shots {
		if !inRound(s.Round) || s.Tick < last || !gun(s.Class) || s.Weapon == "" || s.Shooter.Side == "" ||
			s.MaxSpeed <= 0 || s.Speed < 0 || s.Speed > 1000 {
			t.Errorf("shot %d = %+v", i, s)
		}
		last = s.Tick
	}
	if len(m.Shots) < 1000 {
		t.Errorf("%d shots in a match", len(m.Shots))
	}
	for i, s := range m.Sightings {
		bad := !inRound(s.Round) || s.From > s.To || s.Spotter.Side == "" || s.Target.Side == "" || s.Spotter.Side == s.Target.Side
		// A sighting ends when the next round starts, before its freeze time ends.
		if !bad && s.Round < len(m.Rounds) && s.To > m.Rounds[s.Round].FreezeEndTick {
			bad = true
		}
		if bad {
			t.Errorf("sighting %d = %+v", i, s)
		}
	}
	if len(m.Sightings) < 100 {
		t.Errorf("%d sightings in a match", len(m.Sightings))
	}
	for i, f := range m.Flashes {
		if !inRound(f.Round) || f.Duration <= 0 || f.Duration > 10 || f.Victim.Side == "" || f.Thrower == nil {
			t.Errorf("flash %d = %+v", i, f)
		}
	}
	if len(m.Flashes) == 0 {
		t.Error("no flashes in a match")
	}
	two := 0
	for i, k := range m.Kills {
		flashbangs := 0
		for _, g := range k.Grenades {
			if GrenadePrice(g) == 0 {
				t.Errorf("kill %d: %q is no grenade", i, g)
			}
			if g == Flashbang {
				flashbangs++
			}
		}
		if flashbangs > 2 {
			t.Errorf("kill %d: %d flashbangs", i, flashbangs)
		}
		if flashbangs == 2 {
			two++
		}
	}
	return two
}

// checkEvents checks the events of a real match: every kill and hit falls
// in a round, in order; every round knows who was alive in it; and the
// deaths, kills and damage of the log add up to the summary.
func checkEvents(t *testing.T, m *Match) {
	t.Helper()
	inRound := func(what string, i, tick, round int, after bool) {
		if round < 1 || round > len(m.Rounds) {
			t.Errorf("%s %d at tick %d in round %d of %d", what, i, tick, round, len(m.Rounds))
			return
		}
		r := m.Rounds[round-1]
		if after && tick < r.EndTick || !after && (tick > r.EndTick || round > 1 && tick < m.Rounds[round-2].EndTick) {
			t.Errorf("%s %d at tick %d, after the end %t, round %d ended at %d", what, i, tick, after, round, r.EndTick)
		}
	}
	players := make(map[uint64]Player)
	for _, p := range m.Players {
		players[p.SteamID64] = p
	}
	var deaths, kills, damage int
	last := 0
	for i, k := range m.Kills {
		inRound("kill", i, k.Tick, k.Round, k.AfterRoundEnd)
		if k.Tick < last {
			t.Errorf("kill %d at tick %d comes after tick %d", i, k.Tick, last)
		}
		last = k.Tick
		if _, ok := players[k.Victim.ID]; ok && !k.Victim.Bot {
			deaths++
		}
		if k.Killer != nil && !k.Killer.Bot && k.Killer.Side != k.Victim.Side && players[k.Killer.ID].SteamID64 != 0 {
			kills++
		}
		if k.Disconnect && (k.Killer == nil || k.Killer.ID != k.Victim.ID || k.Class != ClassWorld) {
			t.Errorf("kill %d: a disconnect not by the victim's own hand: %+v", i, k)
		}
	}
	for i, h := range m.Hits {
		inRound("hit", i, h.Tick, h.Round, h.AfterRoundEnd)
		if h.Attacker != nil && !h.Attacker.Bot && h.Attacker.Side != h.Victim.Side && players[h.Attacker.ID].SteamID64 != 0 {
			damage += h.Damage
		}
	}
	var sum Player
	for _, p := range m.Players {
		sum.Deaths += p.Deaths
		sum.Kills += p.Kills
		sum.Damage += p.Damage
	}
	if deaths != sum.Deaths || kills != sum.Kills || damage != sum.Damage {
		t.Errorf("the log has %d deaths, %d kills, %d damage; the summary %d, %d, %d", deaths, kills, damage, sum.Deaths, sum.Kills, sum.Damage)
	}
	for _, r := range m.Rounds {
		var alive [2]int
		for _, a := range r.Alive {
			switch {
			case a.Side == CT:
				alive[0]++
			case a.Side == T:
				alive[1]++
			default:
				t.Errorf("round %d: alive on no side: %+v", r.Number, a)
			}
			if _, ok := players[a.ID]; !a.Bot && !ok {
				t.Errorf("round %d: alive is not a player of the match: %+v", r.Number, a)
			}
		}
		if alive[0] < 1 || alive[0] > 5 || alive[1] < 1 || alive[1] > 5 || r.FreezeEndTick <= 0 || r.FreezeEndTick >= r.EndTick {
			t.Errorf("round %d: alive CT %d, T %d; freeze time ended at %d, the round at %d", r.Number, alive[0], alive[1], r.FreezeEndTick, r.EndTick)
		}
	}
}

func TestParseTestdataFailures(t *testing.T) {
	file := testDemos(t)[0]
	open := func(t *testing.T) *os.File {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}

	t.Run("truncated", func(t *testing.T) {
		_, err := Parse(context.Background(), io.LimitReader(open(t), 8<<20), Options{})
		if !errors.Is(err, ErrTruncated) {
			t.Errorf("err = %v, want ErrTruncated", err)
		}
	})
	t.Run("too large", func(t *testing.T) {
		_, err := Parse(context.Background(), open(t), Options{MaxSize: 16 << 20})
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := Parse(ctx, open(t), Options{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want context.DeadlineExceeded", err)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("stopped after %v", d)
		}
	})
}
