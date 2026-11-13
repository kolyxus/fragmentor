package findings

import (
	"math"
	"slices"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// The engine is tested on made-up measures: ten players, 1 to 5 of team 0
// and 6 to 10 of team 1, as indices 0 to 9. Player 1, index 0, is the one
// the tests look at.

const testTickRate = 64

// newData returns the data of ten players with every measure at zero.
func newData() *data {
	a := &metrics.Analysis{TickRate: testTickRate}
	for id := uint64(1); id <= 10; id++ {
		a.Players = append(a.Players, metrics.Player{SteamID64: id, Team: int((id - 1) / 5)})
	}
	d := &data{a: a, tallies: make(map[string][]tally, len(measures))}
	for _, m := range measures {
		d.tallies[m.id] = make([]tally, len(a.Players))
	}
	return d
}

// set gives player i x events of n opportunities of the measure.
func (d *data) set(id RuleID, i int, x, n float64) *tally {
	t := &d.tallies[string(id)][i]
	t.x, t.n, t.sample = x, n, n
	return t
}

// others gives each of the players 1 to 9, all but the first, x events of n.
func (d *data) others(id RuleID, x, n float64) {
	for i := 1; i < 10; i++ {
		d.set(id, i, x, n)
	}
}

func ruleOf(t *testing.T, id RuleID) rule {
	t.Helper()
	for _, r := range rules {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no rule %q", id)
	return rule{}
}

// events returns n events of a mistake that weigh one each.
func events(n int) []event {
	out := make([]event, n)
	for i := range out {
		out[i] = event{count: 1, impact: 1, ev: Evidence{Round: i + 1, Tick: 1000 * (i + 1)}}
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestShareOfOpportunities(t *testing.T) {
	d := newData()
	d.others(UntradedDeath, 7, 16)
	d.set(UntradedDeath, 0, 11, 16).events = events(11)
	f, ok := d.judge(ruleOf(t, UntradedDeath), 0, History{})
	if !ok {
		t.Fatal("11 untraded deaths of 16 against 7 of 16 in the lobby is no finding")
	}
	// The lobby is the other nine together, with one event and two
	// opportunities added: 64 of 146.
	rate := 64.0 / 146
	expected := 16 * rate
	spread := math.Sqrt(16 * rate * (1 - rate))
	z := (11 - expected) / spread
	if f.Rule != UntradedDeath || f.Kind != Mistake || f.Group != Trades || f.X != 11 || f.N != 16 || f.Sample != 16 {
		t.Errorf("finding = %+v", f)
	}
	if !near(f.BaseRate, rate) || !near(f.Expected, expected) || !near(f.Excess, 11-expected) || !near(f.Z, z) {
		t.Errorf("rate %v, expected %v, excess %v, z %v; want %v, %v, %v, %v", f.BaseRate, f.Expected, f.Excess, f.Z, rate, expected, 11-expected, z)
	}
	if !near(f.Impact, 1) || !near(f.Weight, z) || f.Habit {
		t.Errorf("impact %v, weight %v, habit %t; want 1, %v, false", f.Impact, f.Weight, f.Habit, z)
	}

	// The player himself is no part of his lobby: with the same lobby the
	// base is the same whatever he did.
	d.set(UntradedDeath, 0, 16, 16).events = events(16)
	if g, _ := d.judge(ruleOf(t, UntradedDeath), 0, History{}); !near(g.BaseRate, rate) {
		t.Errorf("the base moved with the player's own deaths: %v, want %v", g.BaseRate, rate)
	}
	// For another player he is: the base of player 2 holds his 16 of 16.
	if g, _ := d.judge(ruleOf(t, UntradedDeath), 1, History{}); g.Rule != "" {
		t.Errorf("player 2 has the share of the lobby and a finding: %+v", g)
	}
	if c := d.compare(measureOf[string(UntradedDeath)], 1, History{}); c.Lobby.X != 16+8*7 || c.Lobby.N != 9*16 {
		t.Errorf("the lobby of player 2 = %+v, want 72 of 144", c.Lobby)
	}
}

func TestFiringThresholds(t *testing.T) {
	for _, tc := range []struct {
		name         string
		x, n, ox, on float64
		want         bool
	}{
		// The lobby: 9·ox+1 of 9·on+2.
		{"two events more than expected", 10, 16, 7, 16, true},          // expected 7.01, excess 2.99
		{"less than two events more", 9, 16, 7, 16, false},              // excess 1.99
		{"just two events more", 9.02, 16, 7, 16, true},                 // excess 2.006
		{"below the minimal sample", 7, 7, 7, 16, false},                // 7 deaths, the rule needs 8
		{"at the minimal sample", 8, 8, 7, 16, true},                    // expected 3.51, excess 4.49
		{"two events more, within the spread", 43, 100, 40, 100, false}, // expected 40.02, spread 4.9: z 0.61
		{"fewer events than expected", 3, 16, 7, 16, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newData()
			d.others(UntradedDeath, tc.ox, tc.on)
			d.set(UntradedDeath, 0, tc.x, tc.n).events = events(int(tc.x))
			if _, ok := d.judge(ruleOf(t, UntradedDeath), 0, History{}); ok != tc.want {
				t.Errorf("fired %t, want %t", ok, tc.want)
			}
		})
	}
	// Excess over the lobby is not enough without the spread: 60 of 100
	// against 50 of 100 is two spreads, 52 of 100 less than one.
	d := newData()
	d.others(MovingShots, 50, 100)
	d.set(MovingShots, 0, 54, 100).events = events(54)
	if f, ok := d.judge(ruleOf(t, MovingShots), 0, History{}); ok {
		t.Errorf("54 of 100 against 50 of 100 fired: z %v", f.Z)
	}
	d.set(MovingShots, 0, 60, 100).events = events(60)
	if f, ok := d.judge(ruleOf(t, MovingShots), 0, History{}); !ok || f.Z < 1.9 || f.Z > 2.1 {
		t.Errorf("60 of 100 against 50 of 100: fired %t, z %v; want about 2", ok, f.Z)
	}
}

func TestEventsARound(t *testing.T) {
	d := newData()
	// Multikills: the others have 5 such rounds in 9·20 rounds played.
	d.others(MultiKills, 0, 20)
	d.set(MultiKills, 1, 5, 20)
	d.set(MultiKills, 0, 4, 22)
	f, ok := d.judge(ruleOf(t, MultiKills), 0, History{})
	rate := 6.0 / 182
	expected := 22 * rate
	if !ok || !near(f.BaseRate, rate) || !near(f.Expected, expected) || !near(f.Z, (4-expected)/math.Sqrt(expected)) {
		t.Errorf("fired %t: %+v; want the rate %v, expected %v, the spread its root", ok, f, rate, expected)
	}
	if f.Kind != Strength || !near(f.Impact, 1) || !near(f.Weight, f.Z) {
		t.Errorf("a strength weighs its z: %+v", f)
	}
	// A lobby without a single such round still expects some: one event
	// is added to it.
	d = newData()
	d.others(MultiKills, 0, 20)
	d.set(MultiKills, 0, 3, 20)
	f, ok = d.judge(ruleOf(t, MultiKills), 0, History{})
	if !ok || !near(f.BaseRate, 1.0/182) || math.IsInf(f.Z, 0) || math.IsNaN(f.Z) {
		t.Errorf("fired %t against an empty lobby: %+v", ok, f)
	}
	// And a lobby where all die untraded leaves room for more: never 1.
	d = newData()
	d.others(UntradedDeath, 16, 16)
	if got := measureOf[string(UntradedDeath)].rate(d.base(string(UntradedDeath), func(j int) bool { return j != 0 })); !near(got, 145.0/146) {
		t.Errorf("the rate of a lobby of 144 of 144 = %v, want 145/146", got)
	}
}

func TestCountAMatch(t *testing.T) {
	// The death spot is a count a match: the lobby gives the mean of the
	// worst places of the others, 18 of 9, and the spread is its root.
	d := newData()
	d.others(DeathSpot, 2, 1)
	tl := d.set(DeathSpot, 0, 5, 1)
	tl.sample, tl.place, tl.side, tl.events = 6, "Banana", "T", events(5)
	f, ok := d.judge(ruleOf(t, DeathSpot), 0, History{})
	if !ok || !near(f.BaseRate, 2) || !near(f.Expected, 2) || !near(f.Excess, 3) || !near(f.Z, 3/math.Sqrt2) {
		t.Errorf("fired %t: %+v; want the mean 2, excess 3, z 3/√2", ok, f)
	}
	if f.Place != "Banana" || f.Side != "T" || f.Sample != 6 || f.X != 5 || f.N != 1 {
		t.Errorf("the spot = %+v", f)
	}
	// The rule counts the worst place that is a spot, not the worst of all.
	tl.ruleX, tl.hasRuleX = 4, true
	if f, ok := d.judge(ruleOf(t, DeathSpot), 0, History{}); !ok || f.X != 4 || !near(f.Excess, 2) {
		t.Errorf("fired %t with x %v, excess %v; want the 4 of the spot", ok, f.X, f.Excess)
	}
	// No spot, no finding, whatever the worst place.
	tl.skip = true
	if _, ok := d.judge(ruleOf(t, DeathSpot), 0, History{}); ok {
		t.Error("a player without a spot has the finding")
	}
	// A lobby that never died untraded twice at a place has the mean of
	// one event, not of none.
	d = newData()
	d.others(DeathSpot, 0, 1)
	if got := measureOf[string(DeathSpot)].rate(d.base(string(DeathSpot), func(j int) bool { return j != 0 })); !near(got, 1.0/9) {
		t.Errorf("the mean of an empty lobby = %v, want 1/9", got)
	}
}

func TestImpact(t *testing.T) {
	for _, tc := range []struct {
		won     bool
		buy     metrics.Buy
		excused bool
		want    float64
	}{
		{true, metrics.BuyForce, false, 1},
		{true, metrics.BuyPistol, false, 1},
		{true, "", false, 1},
		{false, metrics.BuyForce, false, 1.5},
		{true, metrics.BuyFull, false, 1.25},
		{false, metrics.BuyFull, false, 1.875},
		{true, metrics.BuyEco, false, 0.5},
		{false, metrics.BuyEco, false, 0.75},
		{false, metrics.BuyEco, true, 0},
		{false, metrics.BuyFull, true, 0},
	} {
		if got := impactOf(tc.won, tc.buy, tc.excused); !near(got, tc.want) {
			t.Errorf("impactOf(won %t, %q, excused %t) = %v, want %v", tc.won, tc.buy, tc.excused, got, tc.want)
		}
	}

	// The weight of a mistake is its z by what its events weigh on
	// average; the events of a round of many shots count as many.
	d := newData()
	d.others(UntradedDeath, 7, 16)
	tl := d.set(UntradedDeath, 0, 11, 16)
	tl.events = events(11)
	tl.events[0].impact, tl.events[1].impact, tl.events[2].impact = 1.875, 1.5, 0
	f, _ := d.judge(ruleOf(t, UntradedDeath), 0, History{})
	if want := (1.875 + 1.5 + 0 + 8) / 11; !near(f.Impact, want) || !near(f.Weight, f.Z*want) {
		t.Errorf("impact %v, weight %v; want %v and z by it", f.Impact, f.Weight, want)
	}
	tl.events = []event{{count: 9, impact: 1.5}, {count: 2, impact: 0.5}}
	f, _ = d.judge(ruleOf(t, UntradedDeath), 0, History{})
	if want := (9*1.5 + 2*0.5) / 11; !near(f.Impact, want) {
		t.Errorf("impact of 9 events of 1.5 and 2 of 0.5 = %v, want %v", f.Impact, want)
	}
	// Events that all weigh nothing make no finding.
	tl.events = []event{{count: 11, impact: 0}}
	if f, ok := d.judge(ruleOf(t, UntradedDeath), 0, History{}); ok {
		t.Errorf("excused deaths alone made a finding: %+v", f)
	}
	// A strength weighs its z, whatever its events.
	d = newData()
	d.others(OpeningWins, 3, 6)
	d.set(OpeningWins, 0, 6, 6).events = []event{{count: 6, impact: 0, score: 1}}
	if f, ok := d.judge(ruleOf(t, OpeningWins), 0, History{}); !ok || !near(f.Impact, 1) || !near(f.Weight, f.Z) {
		t.Errorf("fired %t, impact %v, weight %v, z %v", ok, f.Impact, f.Weight, f.Z)
	}
}

func TestHabit(t *testing.T) {
	d := newData()
	d.others(UntradedDeath, 7, 16)
	d.set(UntradedDeath, 0, 11, 16).events = events(11)
	rl := ruleOf(t, UntradedDeath)
	plain, _ := d.judge(rl, 0, History{})
	past := func(matches int, x, n float64) History {
		return History{Matches: matches, Tallies: map[string]Tally{string(UntradedDeath): {X: x, N: n}}}
	}
	// The lobby's rate is 0.438: 30 of 48 is 9 events above it, three
	// spreads of 48 deaths.
	for _, tc := range []struct {
		name string
		h    History
		want bool
	}{
		{"three matches well above the lobby", past(3, 30, 48), true},
		{"two matches", past(2, 30, 48), false},
		{"three matches a hair above the lobby", past(3, 22, 48), false}, // expected 21.04, spread 3.4
		{"three matches below the lobby", past(3, 15, 48), false},
		{"three matches below the minimal sample", past(3, 7, 7), false},
		{"no past of the measure", History{Matches: 5}, false},
	} {
		f, ok := d.judge(rl, 0, tc.h)
		want := plain.Weight
		if tc.want {
			want *= HabitFactor
		}
		if !ok || f.Habit != tc.want || !near(f.Weight, want) {
			t.Errorf("%s: fired %t, habit %t, weight %v; want habit %t, weight %v", tc.name, ok, f.Habit, f.Weight, tc.want, want)
		}
	}
	// A habit of events a round has no minimal sample of opportunities.
	d = newData()
	d.others(MultiKills, 1, 20)
	d.set(MultiKills, 0, 4, 20)
	h := History{Matches: 3, Tallies: map[string]Tally{string(MultiKills): {X: 9, N: 60}}}
	if f, ok := d.judge(ruleOf(t, MultiKills), 0, h); !ok || !f.Habit {
		t.Errorf("9 such rounds in 60 against 10 in 182: fired %t, habit %t", ok, f.Habit)
	}
}

func TestEvidence(t *testing.T) {
	evs := []event{
		{count: 1, impact: 1, ev: Evidence{Round: 1, Tick: 1000}},
		{count: 1, impact: 1.875, ev: Evidence{Round: 9, Tick: 9000}},
		{count: 1, impact: 0, ev: Evidence{Round: 2, Tick: 2000}}, // excused: shows nothing
		{count: 1, impact: 1.5, ev: Evidence{Round: 7, Tick: 7000}},
		{count: 1, impact: 1.5, ev: Evidence{Round: 3, Tick: 3000}},
		{count: 1, impact: 0.5, ev: Evidence{Round: 4, Tick: 4000}},
		{count: 1, impact: 1.25, ev: Evidence{Round: 5, Tick: 5000}},
		{count: 1, impact: 1, ev: Evidence{Round: 6, Tick: 100}},
	}
	got := evidence(evs, Mistake, testTickRate)
	var rounds []int
	for _, e := range got {
		rounds = append(rounds, e.Round)
	}
	// The five heaviest, of equals the earlier ones, in the order of the
	// match: the round of 0.5 and the later round of 1 stay out.
	if want := []int{1, 3, 5, 7, 9}; !slices.Equal(rounds, want) {
		t.Errorf("rounds %v, want %v", rounds, want)
	}
	if e := got[4]; e.Tick != 9000 || e.Goto != 9000-5*testTickRate || !near(e.Impact, 1.875) {
		t.Errorf("the moment of round 9 = %+v, want the tick 5 s before it and its impact", e)
	}
	// A moment in the first seconds of a demo goes to its start.
	if e := evidence(evs[7:], Mistake, testTickRate)[0]; e.Goto != 0 {
		t.Errorf("goto = %d for the tick 100, want 0", e.Goto)
	}
	// A strength shows its biggest moments, and keeps those of no impact.
	strong := []event{
		{count: 1, score: 3, ev: Evidence{Round: 1}},
		{count: 1, score: 5, ev: Evidence{Round: 2}},
		{count: 1, score: 4, ev: Evidence{Round: 3}},
	}
	if got := evidence(strong, Strength, testTickRate); len(got) != 3 || got[0].Round != 1 || got[2].Round != 3 {
		t.Errorf("the moments of a strength = %+v", got)
	}
	// The smallest of seven stays out, though it comes first and in the
	// last round.
	seven := append([]event{{count: 1, score: 1, ev: Evidence{Round: 9}}}, strong...)
	seven = append(seven, strong...)
	if got := evidence(seven, Strength, testTickRate); len(got) != MaxEvidence || got[len(got)-1].Round == 9 {
		t.Errorf("%d moments, the smallest among them: %+v", len(got), got)
	}
	// An excused death shows no mistake, even when there is room for it.
	if got := evidence(evs[:3], Mistake, testTickRate); len(got) != 2 || got[0].Round != 1 || got[1].Round != 9 {
		t.Errorf("the moments of two deaths and an excused one = %+v, want rounds 1 and 9", got)
	}
	if got := evidence(nil, Mistake, testTickRate); len(got) != 0 {
		t.Errorf("moments of no events: %+v", got)
	}
}

func TestComparison(t *testing.T) {
	d := newData()
	// Untraded deaths of 16: the lower the better.
	for i, x := range []float64{8, 4, 6, 8, 10, 12, 14, 16, 2, 0} {
		d.set(UntradedDeath, i, x, 16)
	}
	m := measureOf[string(UntradedDeath)]
	c := d.compare(m, 0, History{})
	if c.Measure != string(UntradedDeath) || c.X != 8 || c.N != 16 || !near(c.Rate, 0.5) {
		t.Errorf("the player = %+v", c)
	}
	if c.Lobby != (Base{X: 72, N: 144, Rate: 0.5}) || c.Teammates != (Base{X: 28, N: 64, Rate: 28.0 / 64}) || c.Opponents != (Base{X: 44, N: 80, Rate: 0.55}) {
		t.Errorf("lobby %+v, teammates %+v, opponents %+v", c.Lobby, c.Teammates, c.Opponents)
	}
	// Four are strictly better than 8 of 16; the other with 8 shares the place.
	if c.Rank != 5 || c.Of != 10 || c.Own != nil {
		t.Errorf("place %d of %d, own %v; want 5 of 10, no past", c.Rank, c.Of, c.Own)
	}
	if c := d.compare(m, 3, History{}); c.Rank != 5 {
		t.Errorf("the other player with 8 of 16 is %d, want 5 too", c.Rank)
	}
	if c := d.compare(m, 9, History{}); c.Rank != 1 {
		t.Errorf("the player without untraded deaths is %d, want 1", c.Rank)
	}
	// For a strength more is better.
	for i, x := range []float64{3, 1, 5, 0, 0, 2, 2, 2, 4, 0} {
		d.set(OpeningWins, i, x, 6)
	}
	if c := d.compare(measureOf[string(OpeningWins)], 0, History{}); c.Rank != 3 {
		t.Errorf("3 opening wins of 6 is the place %d, want 3", c.Rank)
	}
	// Players without opportunities have no place and take none.
	d.set(OpeningWins, 2, 0, 0)
	d.set(OpeningWins, 4, 0, 0)
	if c := d.compare(measureOf[string(OpeningWins)], 0, History{}); c.Rank != 2 || c.Of != 8 {
		t.Errorf("place %d of %d, want 2 of 8", c.Rank, c.Of)
	}
	if c := d.compare(measureOf[string(OpeningWins)], 2, History{}); c.Rank != 0 || c.Of != 8 || c.Rate != 0 {
		t.Errorf("a player without opening duels: place %d of %d, rate %v", c.Rank, c.Of, c.Rate)
	}
	// His own past, from three matches on.
	h := History{Matches: 3, Tallies: map[string]Tally{string(UntradedDeath): {X: 30, N: 48}}}
	if c := d.compare(m, 0, h); c.Own == nil || *c.Own != (Base{X: 30, N: 48, Rate: 0.625}) {
		t.Errorf("own = %+v, want 30 of 48", c.Own)
	}
	h.Matches = 2
	if c := d.compare(m, 0, h); c.Own != nil {
		t.Errorf("own = %+v from two matches, want none", c.Own)
	}
}

func TestHistoryAdd(t *testing.T) {
	var h History
	h.Add(Report{Comparisons: []Comparison{{Measure: "a", X: 3, N: 10}, {Measure: "b", X: 1, N: 1}}})
	h.Add(Report{Comparisons: []Comparison{{Measure: "a", X: 5, N: 12}}})
	if h.Matches != 2 || h.Tallies["a"] != (Tally{8, 22}) || h.Tallies["b"] != (Tally{1, 1}) {
		t.Errorf("history = %+v", h)
	}
}

// The tables hold together: every rule has its measures, a nested rule a
// parent of its group, and the report lists every measure once.
func TestRuleTables(t *testing.T) {
	seen := make(map[RuleID]bool)
	for _, r := range rules {
		if seen[r.ID] || r.ID == "" {
			t.Errorf("rule %q twice or unnamed", r.ID)
		}
		seen[r.ID] = true
		if r.Kind != Mistake && r.Kind != Strength || !slices.Contains([]Group{Trades, Duels, Grenades, Shooting}, r.Group) {
			t.Errorf("rule %q of kind %q, group %q", r.ID, r.Kind, r.Group)
		}
		if len(r.measures) == 0 {
			t.Errorf("rule %q has no measure", r.ID)
		}
		for _, id := range r.measures {
			m, ok := measureOf[id]
			if !ok || m.good != (r.Kind == Strength) {
				t.Errorf("rule %q: measure %q, known %t, more is better %t", r.ID, id, ok, m.good)
			}
		}
		if r.Parent != "" {
			p := ruleOf(t, r.Parent)
			if p.Group != r.Group || p.Kind != r.Kind || p.Parent != "" {
				t.Errorf("rule %q is nested in %q of group %q, kind %q, parent %q", r.ID, p.ID, p.Group, p.Kind, p.Parent)
			}
		}
	}
	ids := Measures()
	if len(ids) != len(measureOf) {
		t.Errorf("%d measures listed, %d by name: one is there twice", len(ids), len(measureOf))
	}
	if got := Rules(); len(got) != 16 || got[0].ID != UntradedDeath || got[15].ID != TradeKills {
		t.Errorf("Rules() = %+v", got)
	}
	// The groups as docs/FINDINGS.md has them.
	groups := map[RuleID]Group{
		UntradedDeath: Trades, LoneDeath: Trades, AdvantageDeath: Trades, DryDeath: Trades, MissedTrade: Trades, DeathSpot: Trades,
		OpeningLoss: Duels, UnusedGrenades: Grenades, BadFlashes: Grenades, MovingShots: Shooting, FirstShot: Shooting,
		OpeningWins: Duels, MultiKills: Duels, Clutches: Duels, UsefulGrenades: Grenades, TradeKills: Trades,
	}
	for _, r := range Rules() {
		if r.Group != groups[r.ID] {
			t.Errorf("rule %q is of group %q, want %q", r.ID, r.Group, groups[r.ID])
		}
		if nested := r.ID == LoneDeath || r.ID == DryDeath || r.ID == DeathSpot; nested != (r.Parent == UntradedDeath) {
			t.Errorf("rule %q is nested in %q", r.ID, r.Parent)
		}
	}
}
