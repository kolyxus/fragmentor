package findings

import (
	"cmp"
	"slices"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// fired returns the findings of the rules with the weights, the heaviest
// first, as a report has them.
func fired(t *testing.T, weights map[RuleID]float64) []Finding {
	t.Helper()
	var fs []Finding
	for _, r := range Rules() {
		if w, ok := weights[r.ID]; ok {
			fs = append(fs, Finding{Rule: r.ID, Kind: r.Kind, Group: r.Group, Weight: w})
		}
	}
	if len(fs) != len(weights) {
		t.Fatalf("unknown rules in %v", weights)
	}
	slices.SortStableFunc(fs, func(a, b Finding) int { return cmp.Compare(b.Weight, a.Weight) })
	return fs
}

// told returns the rules of the picks, and the rules each is told with.
func told(fs []Finding, picks []Pick) (rules []RuleID, with [][]RuleID) {
	for _, p := range picks {
		rules = append(rules, fs[p.Finding].Rule)
		var w []RuleID
		for _, j := range p.With {
			w = append(w, fs[j].Rule)
		}
		with = append(with, w)
	}
	return rules, with
}

func TestPickOneOfAGroup(t *testing.T) {
	fs := fired(t, map[RuleID]float64{
		UnusedGrenades: 5, BadFlashes: 4, // grenades
		MovingShots: 3, FirstShot: 2.5, // shooting
		OpeningLoss:    2,   // duels
		AdvantageDeath: 1.5, // trades: the fourth group stays out
		OpeningWins:    9, TradeKills: 1,
	})
	mistakes, strength := pick(fs)
	rules, with := told(fs, mistakes)
	if want := []RuleID{UnusedGrenades, MovingShots, OpeningLoss}; !slices.Equal(rules, want) {
		t.Errorf("mistakes %v, want %v", rules, want)
	}
	for i, p := range mistakes {
		if p.Weight != fs[p.Finding].Weight || with[i] != nil {
			t.Errorf("mistake %d = %+v with %v", i, p, with[i])
		}
	}
	if len(mistakes) != MaxMistakes {
		t.Errorf("%d mistakes, want %d", len(mistakes), MaxMistakes)
	}
	// The strength is the heaviest one, and takes no place of a mistake.
	if strength == nil || fs[strength.Finding].Rule != OpeningWins || strength.Weight != 9 || strength.With != nil {
		t.Errorf("strength = %+v", strength)
	}

	// Fewer findings, fewer mistakes: nothing is added to make three.
	fs = fired(t, map[RuleID]float64{BadFlashes: 2, UnusedGrenades: 1})
	mistakes, strength = pick(fs)
	if rules, _ := told(fs, mistakes); !slices.Equal(rules, []RuleID{BadFlashes}) || strength != nil {
		t.Errorf("mistakes %v, strength %+v; want the bad flashes alone", rules, strength)
	}
	mistakes, strength = pick(nil)
	if mistakes == nil || len(mistakes) != 0 || strength != nil {
		t.Errorf("of no findings: %v, %+v", mistakes, strength)
	}
	// Strengths alone make no mistakes.
	fs = fired(t, map[RuleID]float64{Clutches: 3, MultiKills: 4})
	mistakes, strength = pick(fs)
	if len(mistakes) != 0 || strength == nil || fs[strength.Finding].Rule != MultiKills {
		t.Errorf("mistakes %v, strength %+v", mistakes, strength)
	}
}

func TestPickTheMostExact(t *testing.T) {
	for _, tc := range []struct {
		name    string
		weights map[RuleID]float64
		rule    RuleID
		weight  float64
		with    []RuleID
	}{
		{"the general rule alone", map[RuleID]float64{UntradedDeath: 3}, UntradedDeath, 3, nil},
		{"a nested rule lighter than the general one", map[RuleID]float64{UntradedDeath: 3, LoneDeath: 1.5}, LoneDeath, 3, []RuleID{UntradedDeath}},
		{"a nested rule heavier than the general one", map[RuleID]float64{UntradedDeath: 2, LoneDeath: 3}, LoneDeath, 3, []RuleID{UntradedDeath}},
		{"two nested rules: the heavier one", map[RuleID]float64{UntradedDeath: 3, LoneDeath: 1.5, DryDeath: 2}, DryDeath, 3, []RuleID{UntradedDeath, LoneDeath}},
		{"nested rules without the general one", map[RuleID]float64{LoneDeath: 2, DeathSpot: 1}, LoneDeath, 2, []RuleID{DeathSpot}},
		{"another rule of the group leads", map[RuleID]float64{AdvantageDeath: 4, UntradedDeath: 3, LoneDeath: 2}, AdvantageDeath, 4, nil},
		{"another rule of the group follows", map[RuleID]float64{UntradedDeath: 3, AdvantageDeath: 2.5, LoneDeath: 1}, LoneDeath, 3, []RuleID{UntradedDeath}},
		{"a missed trade is nested in nothing", map[RuleID]float64{MissedTrade: 5, DryDeath: 3, UntradedDeath: 2}, MissedTrade, 5, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A finding of another group is picked beside it, untouched.
			tc.weights[MovingShots] = 0.5
			fs := fired(t, tc.weights)
			mistakes, _ := pick(fs)
			rules, with := told(fs, mistakes)
			if len(mistakes) != 2 || rules[0] != tc.rule || mistakes[0].Weight != tc.weight || !slices.Equal(with[0], tc.with) {
				t.Errorf("picked %v with %v by %v; want %v with %v by %v", rules, with, mistakes[0].Weight, tc.rule, tc.with, tc.weight)
			}
			if rules[1] != MovingShots || with[1] != nil {
				t.Errorf("the second mistake = %v with %v", rules[1], with[1])
			}
		})
	}
	// The family is picked by its heaviest finding: a light nested rule
	// stands before a mistake of another group that is heavier than itself.
	fs := fired(t, map[RuleID]float64{UntradedDeath: 3, LoneDeath: 1, BadFlashes: 2})
	mistakes, _ := pick(fs)
	if rules, _ := told(fs, mistakes); !slices.Equal(rules, []RuleID{LoneDeath, BadFlashes}) {
		t.Errorf("mistakes %v, want the lone death first", rules)
	}
}

// A match in which several rules fire for player 1, from the events to the
// report.
func TestAnalyze(t *testing.T) {
	g := newGame(20)
	g.lobby(16, 4, untraded)
	// 12 untraded deaths of 16, 9 of them alone, in rounds the team lost.
	for n := 1; n <= 16; n++ {
		g.round(n).Winner = 1
	}
	// Each at a place of its own, so that no place is a death spot.
	for n := 1; n <= 16; n++ {
		place := at(string(rune('A'+n)), demo.CT)
		switch {
		case n <= 9:
			g.death(n, 1, 6, lone, place)
		case n <= 12:
			g.death(n, 1, 6, untraded, place)
		default:
			g.death(n, 1, 6, place)
		}
	}
	// Six bad flashbangs of six.
	for id := uint64(2); id <= 10; id++ {
		for n := 1; n <= 6; n++ {
			g.flashbang(n, id, 1, 0)
		}
	}
	for n := 1; n <= 6; n++ {
		g.flashbang(n, 1, 0, 0)
	}
	// Three rounds of three kills.
	for n := 17; n <= 19; n++ {
		g.round(n).Kills[1] = 3
	}
	history := map[uint64]History{
		1: {Matches: 4, Tallies: map[string]Tally{string(BadFlashes): {X: 30, N: 32}}},
		2: {Matches: 1},
	}
	reports := Analyze(&g.a, history)
	if len(reports) != 10 {
		t.Fatalf("%d reports, want one a player", len(reports))
	}
	r := reports[0]
	var rules []RuleID
	for i, f := range r.Findings {
		rules = append(rules, f.Rule)
		if i > 0 && f.Weight > r.Findings[i-1].Weight {
			t.Errorf("finding %d is heavier than the one before: %v", i, rules)
		}
	}
	slices.Sort(rules)
	if want := []RuleID{BadFlashes, LoneDeath, MultiKills, UntradedDeath}; !slices.Equal(rules, want) {
		t.Fatalf("fired %v, want %v", rules, want)
	}
	// The bad flashes are a habit: his past matches show them too.
	flashes := finding(r, BadFlashes)
	if !flashes.Habit || !near(flashes.Weight, flashes.Z*flashes.Impact*HabitFactor) {
		t.Errorf("the bad flashes = %+v, want a habit", flashes)
	}
	if c := comparison(t, r, string(BadFlashes)); c.Own == nil || c.Own.X != 30 || c.Own.N != 32 {
		t.Errorf("own = %+v", c.Own)
	}
	if c := comparison(t, reports[1], string(BadFlashes)); c.Own != nil {
		t.Errorf("player 2 has one past match and own = %+v", c.Own)
	}
	// Two mistakes: the deaths, told by the lone ones with the numbers of
	// the untraded, and the flashes; and the strength.
	picked, with := told(r.Findings, r.Mistakes)
	slices.Sort(picked)
	if !slices.Equal(picked, []RuleID{BadFlashes, LoneDeath}) {
		t.Fatalf("mistakes %v", picked)
	}
	for i, p := range r.Mistakes {
		if r.Findings[p.Finding].Rule == LoneDeath {
			untraded := finding(r, UntradedDeath)
			if !slices.Equal(with[i], []RuleID{UntradedDeath}) || !near(p.Weight, max(untraded.Weight, r.Findings[p.Finding].Weight)) {
				t.Errorf("the lone deaths are told with %v by %v", with[i], p.Weight)
			}
		}
		if i > 0 && p.Weight > r.Mistakes[i-1].Weight {
			t.Errorf("mistake %d is heavier than the one before", i)
		}
	}
	if r.Strength == nil || r.Findings[r.Strength.Finding].Rule != MultiKills {
		t.Errorf("strength = %+v", r.Strength)
	}
	// Every death of his was in a lost round: each weighs 1.5.
	if f := finding(r, LoneDeath); f.X != 9 || f.N != 16 || !near(f.Impact, 1.5) {
		t.Errorf("the lone deaths = %+v", f)
	}

	// A match of nobody makes no reports, and a player of no events an
	// empty one.
	if got := Analyze(&metrics.Analysis{}, nil); len(got) != 0 {
		t.Errorf("reports of no players: %+v", got)
	}
	empty := Analyze(&newGame(1).a, nil)[0]
	if empty.Findings == nil || len(empty.Findings) != 0 || empty.Mistakes == nil || len(empty.Mistakes) != 0 || empty.Strength != nil {
		t.Errorf("the report of a player without events = %+v", empty)
	}
}
