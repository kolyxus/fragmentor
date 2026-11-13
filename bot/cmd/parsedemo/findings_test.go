package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

func TestClockText(t *testing.T) {
	for _, tc := range []struct {
		clock   float64
		planted bool
		want    string
	}{
		{115, false, "1:55"},
		{94.58, false, "1:35"}, // the game rounds up: checked in CS2
		{94, false, "1:34"},
		{60, false, "1:00"},
		{59.01, false, "1:00"},
		{0.5, false, "0:01"},
		{0, false, "0:00"},
		{40, true, "bomb 0:40"},
		{24.2, true, "bomb 0:25"},
	} {
		if got := clockText(tc.clock, tc.planted); got != tc.want {
			t.Errorf("clockText(%v, %t) = %q, want %q", tc.clock, tc.planted, got, tc.want)
		}
	}
}

func TestRateAndNumber(t *testing.T) {
	for _, tc := range []struct {
		measure string
		v       float64
		want    string
	}{
		{string(findings.UntradedDeath), 0.6875, "69%"},
		{string(findings.MultiKills), 0.1724, "0.17/r"},
		{findings.MeasureGrenadeDamage, 0.034, "0.03/r"},
		{string(findings.DeathSpot), 2.4444, "2.44"},
		{findings.MeasureKD, 1.7, "1.70"},
		{findings.MeasureADR, 104.655, "104.7"},
		{findings.MeasureKAST, 0.724, "72%"},
	} {
		if got := rate(tc.measure, tc.v); got != tc.want {
			t.Errorf("rate(%s, %v) = %q, want %q", tc.measure, tc.v, got, tc.want)
		}
	}
	for v, want := range map[float64]string{11: "11", 0: "0", 3.3: "3.3", 2.05: "2.0", 3035: "3035"} {
		if got := number(v); got != want {
			t.Errorf("number(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestMoment(t *testing.T) {
	death := findings.Evidence{
		Round: 7, Clock: 85.3, Tick: 51314, Goto: 50994, Buy: metrics.BuyFull, Impact: 1.875,
		Place: "Bridge", EnemyPlace: "Middle", TeammateMeters: 27.6, Alive: 2, AliveEnemy: 1, Weapon: "AWP",
	}
	got := moment(findings.Finding{Rule: findings.UntradedDeath, Kind: findings.Mistake}, death)
	if want := "round 7, 1:26, Bridge <- Middle, teammate 28 m, 2v1, AWP, lost, full, impact 1.88: demo_gototick 50994"; got != want {
		t.Errorf("a death:\n%s\nwant\n%s", got, want)
	}
	left := death
	left.Grenades, left.GrenadeValue, left.Planted, left.Clock = []string{demo.Flashbang, demo.SmokeGrenade}, 500, true, 12
	got = moment(findings.Finding{Rule: findings.UnusedGrenades, Kind: findings.Mistake}, left)
	if !strings.Contains(got, "bomb 0:12") || !strings.Contains(got, "$500 left: Flashbang, Smoke Grenade") {
		t.Errorf("a death with grenades: %s", got)
	}
	// A flashbang that blinded nobody tells so; shots are counted of shots.
	flash := findings.Evidence{Round: 3, Clock: 100, Tick: 9000, Goto: 8680, Won: true, Buy: metrics.BuyForce, Impact: 1}
	got = moment(findings.Finding{Rule: findings.BadFlashes, Kind: findings.Mistake}, flash)
	if want := "round 3, 1:40, enemies 0, teammates 0, won, force, impact 1.00: demo_gototick 8680"; got != want {
		t.Errorf("a flashbang:\n%s\nwant\n%s", got, want)
	}
	shots := findings.Evidence{Round: 9, Clock: 16.7, Planted: true, Tick: 64924, Goto: 64604, Won: true, Impact: 1, Weapon: "AK-47", Count: 4, Of: 9}
	if got := moment(findings.Finding{Rule: findings.MovingShots, Kind: findings.Mistake}, shots); !strings.Contains(got, "AK-47, 4 of 9, won, impact 1.00") {
		t.Errorf("shots on the move: %s", got)
	}
	// A strength shows no impact; a round of kills the demo tells no moment
	// of has no command.
	round := findings.Evidence{Round: 5, Won: true, Buy: metrics.BuyPistol, Count: 4}
	got = moment(findings.Finding{Rule: findings.MultiKills, Kind: findings.Strength}, round)
	if want := "round 5, 0:00, x4, won, pistol"; got != want {
		t.Errorf("a round of four kills:\n%s\nwant\n%s", got, want)
	}
	missed := findings.Evidence{Round: 2, Clock: 50, Tick: 5000, Goto: 4680, Place: "Banana", Detail: findings.DetailNoShot, Impact: 1.5}
	if got := moment(findings.Finding{Rule: findings.MissedTrade, Kind: findings.Mistake}, missed); !strings.Contains(got, "Banana <- ?, no_shot, lost") {
		t.Errorf("a missed trade: %s", got)
	}
}

// lobby returns a made-up demo of ten players, 101 to 110, in which each
// dies 16 times, 4 of them untraded, and the first one untraded times.
func lobby(file string, untraded int) analyzed {
	a := &metrics.Analysis{TickRate: 64}
	d := analyzed{file: file, mapID: "de_mirage", rounds: 16, score: [2]int{13, 3}, teams: [2]string{"team_a", "team_b"},
		names: make(map[uint64]string), a: a}
	for n := 1; n <= 16; n++ {
		a.Rounds = append(a.Rounds, metrics.RoundInfo{Number: n, Winner: 0, Buy: [2]metrics.Buy{metrics.BuyForce, metrics.BuyForce}})
	}
	for i := range 10 {
		id := uint64(101 + i)
		a.Players = append(a.Players, metrics.Player{SteamID64: id, Team: i / 5, Match: metrics.Stats{
			Rounds: 16, RoundsPlayed: 16, Kills: 12, Deaths: 16, Assists: 3, ADR: 75.5, KAST: 0.7, OpeningAttempts: 2, FlashesThrown: 5,
		}})
		d.names[id] = fmt.Sprintf("player%d", i+1)
		own := 4
		if i == 0 {
			own = untraded
		}
		for n := 1; n <= 16; n++ {
			a.Deaths = append(a.Deaths, metrics.Death{
				Round: n, Tick: 1000*n + i, Clock: 80, Victim: id, Team: i / 5, Side: demo.CT, Killer: uint64(101 + (i+5)%10),
				Won: i < 5, Buy: metrics.BuyForce, Traded: n > own, AliveOwn: 3, AliveEnemy: 3, TeammateDistance: 400,
				VictimPlace: fmt.Sprintf("Place%d", n), KillerPlace: "Window", Weapon: "AWP",
			})
		}
	}
	return d
}

func TestFindingsReport(t *testing.T) {
	// In the first demo player1 has two mistakes: 12 untraded deaths of 16,
	// and six flashbangs that blinded nobody of six, where the others have
	// three of six.
	first := lobby("first.dem.zst", 12)
	for i := range 10 {
		for n := 1; n <= 6; n++ {
			enemies := 1
			if i == 0 || n <= 3 {
				enemies = 0
			}
			first.a.Flashbangs = append(first.a.Flashbangs, metrics.Flashbang{
				Round: n, Tick: 2000*n + i, Clock: 100, Thrower: uint64(101 + i), Enemies: enemies, Empty: enemies == 0,
			})
		}
	}
	var out bytes.Buffer
	findingsReport(&out, []analyzed{first, lobby("second.dem.zst", 4)})
	text := out.String()
	for _, want := range []string{
		"=== first.dem.zst: de_mirage, 16 rounds, 13:3",
		"--- player1, team_a: K/D/A 12/16/3, ADR 75.5, KAST 70%",
		"role: AWP kills 16 of 12, opening duels 2, flashbangs 5, flash assists 0",
		"untraded_death   12 of 16  75%",
		"mistake  untraded_death [trades]: 12 of 16, the lobby's 25% gives 4.1: +7.9",
		"round 1, 1:20, Place1 <- Window, teammate 10 m, 3v3, AWP, won, force, impact 1.00: demo_gototick 680",
		"mistake 1: untraded_death, weight",
		"mistake  bad_flashes [grenades] empty: 6 of 6, the lobby's 50% gives 3.0: +3.0",
		"round 2, 1:40, enemies 0, teammates 0, won, force, impact 1.00: demo_gototick 3680",
		"mistake 2: bad_flashes, weight",
		"strength: none",
		"--- player6, team_b:",
		"=== second.dem.zst",
		"findings: none",
		"=== the rules over 20 players of 2 demos",
		"mistakes picked: 0 for 19 players, 1 for 0 players, 2 for 1 players, 3 for 0 players, a strength for 0",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in the report", want)
		}
	}
	// The summary counts each rule once, fired and picked, and the heavier
	// of the two as the first mistake.
	summary := func(rule string) []string {
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, rule+" ") && strings.Contains(l, " mistake ") {
				return strings.Fields(l)
			}
		}
		return nil
	}
	for rule, first := range map[string]string{"untraded_death": "1", "bad_flashes": "0"} {
		if got := summary(rule); len(got) < 7 || got[3] != "1" || got[4] != "1" || got[5] != first || got[6] != "0" {
			t.Errorf("the summary of %s = %q, want fired 1, picked 1, first %s, habits 0", rule, got, first)
		}
	}
	// The report of no demos is the summary of nothing.
	out.Reset()
	findingsReport(&out, nil)
	if !strings.Contains(out.String(), "the rules over 0 players of 0 demos") {
		t.Errorf("the report of no demos: %q", out.String())
	}
}

func TestWithPast(t *testing.T) {
	// The other demos of a run are the past of a player: three make his own
	// rate, and with many untraded deaths in them the finding a habit.
	demos := []analyzed{lobby("1", 12), lobby("2", 12), lobby("3", 12), lobby("4", 12)}
	reports := withPast(demos)
	own := func(r findings.Report) *findings.Base {
		for _, c := range r.Comparisons {
			if c.Measure == string(findings.UntradedDeath) {
				return c.Own
			}
		}
		return nil
	}
	for i := range demos {
		r := reports[i][0]
		if o := own(r); o == nil || o.X != 36 || o.N != 48 {
			t.Fatalf("demo %d: own = %+v, want 36 of 48 from the three other demos", i, o)
		}
		if len(r.Findings) != 1 || !r.Findings[0].Habit {
			t.Errorf("demo %d: findings %+v, want the untraded deaths as a habit", i, r.Findings)
		}
		if o := own(reports[i][1]); o == nil || o.X != 12 || o.N != 48 {
			t.Errorf("demo %d: own of the second player = %+v, want 12 of 48", i, o)
		}
	}
	// Two other demos are not enough, and a player of other demos has none.
	reports = withPast(demos[:3])
	if o := own(reports[0][0]); o != nil || reports[0][0].Findings[0].Habit {
		t.Errorf("own = %+v from two other demos, habit %t", o, reports[0][0].Findings[0].Habit)
	}
	stranger := lobby("5", 12)
	for i := range stranger.a.Players {
		stranger.a.Players[i].SteamID64 += 100
	}
	for i := range stranger.a.Deaths {
		stranger.a.Deaths[i].Victim += 100
		stranger.a.Deaths[i].Killer += 100
	}
	reports = withPast(append([]analyzed{stranger}, demos...))
	if o := own(reports[0][0]); o != nil {
		t.Errorf("own = %+v of a player who is in one demo", o)
	}
	// No more than findings.MaxHistory demos count.
	var many []analyzed
	for i := range findings.MaxHistory + 3 {
		many = append(many, lobby(fmt.Sprint(i), 12))
	}
	if o := own(withPast(many)[0][0]); o == nil || o.N != 16*findings.MaxHistory {
		t.Errorf("own = %+v of %d other demos, want the deaths of %d", o, len(many)-1, findings.MaxHistory)
	}
}
