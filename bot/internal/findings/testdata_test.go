package findings

import (
	"context"
	"math"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// testDemos returns the demos in bot/testdata and in its folders, the *.zst
// files. They are not in git: the test skips without them, and with -short,
// as each takes seconds.
func testDemos(t *testing.T) []string {
	t.Helper()
	if testing.Short() {
		t.Skip("parsing real demos takes seconds")
	}
	var files []string
	for _, pattern := range []string{"*.zst", filepath.Join("*", "*.zst")} {
		found, err := filepath.Glob(filepath.Join("..", "..", "testdata", pattern))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, found...)
	}
	if len(files) == 0 {
		t.Skip("no demos in bot/testdata")
	}
	return files
}

// demoName names the subtest of a demo: its folder in testdata, if any, and
// the start of its name.
func demoName(file string) string {
	name := filepath.Base(file)
	if len(name) > 10 {
		name = name[:10]
	}
	if dir := filepath.Base(filepath.Dir(file)); dir != "testdata" {
		name = dir + "/" + name
	}
	return name
}

// The bounds of the rules over the player-matches of bot/testdata, see
// docs/FINDINGS.md, "Проверка правил": no rule fires for everybody or, unless
// it is marked rare, for nobody, and the weights of the rules stay
// comparable. They are checked from minPlayerMatches on: a handful of demos
// tells nothing of a distribution.
const (
	minPlayerMatches = 100
	// A weight above maxWeight means a rule counts in other units than the
	// rest: the heaviest finding of the demos weighs 12.
	maxWeight = 25.0
	// The median weight of a rule that fired medianFrom times or more lies
	// between minMedian and maxMedian: the medians of the demos are 1.9 to
	// 3.5.
	medianFrom = 5
	minMedian  = 1.0
	maxMedian  = 5.0
	// No rule is the first mistake of more than maxTopShare of the players
	// who have mistakes: in the demos the most frequent one is of 18%.
	maxTopShare = 0.35
	// At least minWithMistake of the players have a mistake to hear about,
	// and minWithStrength a strength: in the demos 78% and 43%.
	minWithMistake  = 0.5
	minWithStrength = 0.25
)

// TestRulesOnTestdata runs the rules on every player of every demo in
// bot/testdata, first without a past and then with the other demos of the
// same player as his past, as go run ./cmd/parsedemo -findings does. It
// checks every report, the measures against the metrics they are counted
// from, and the bounds above.
func TestRulesOnTestdata(t *testing.T) {
	files := testDemos(t)
	analyses := make([]*metrics.Analysis, len(files))
	// The demos are parsed in parallel, as each takes seconds; the group
	// ends when all of them are.
	t.Run("parse", func(t *testing.T) {
		for i, file := range files {
			t.Run(demoName(file), func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				m, err := demo.ParseFile(ctx, file, demo.Options{})
				if err != nil {
					t.Fatal(err)
				}
				analyses[i] = metrics.Analyze(m)
			})
		}
	})
	if slices.Contains(analyses, nil) {
		t.Fatal("not every demo was parsed")
	}

	first := make([][]Report, len(analyses))
	for i, a := range analyses {
		first[i] = Analyze(a, nil)
		if len(first[i]) != len(a.Players) {
			t.Fatalf("%s: %d reports of %d players", demoName(files[i]), len(first[i]), len(a.Players))
		}
		for k, r := range first[i] {
			checkMeasures(t, demoName(files[i]), k, a.Players[k].Match, r)
			checkReport(t, demoName(files[i]), k, a, r, false)
		}
	}

	var (
		total, withMistake, withStrength int
		firedBy                          = make(map[RuleID]int)
		weights                          = make(map[RuleID][]float64)
		topBy                            = make(map[RuleID]int)
	)
	for i, a := range analyses {
		past := make(map[uint64]History)
		for j, other := range analyses {
			if j == i {
				continue
			}
			for k, p := range other.Players {
				h := past[p.SteamID64]
				h.Add(first[j][k])
				past[p.SteamID64] = h
			}
		}
		for k, r := range Analyze(a, past) {
			checkReport(t, demoName(files[i]), k, a, r, true)
			total++
			for _, f := range r.Findings {
				firedBy[f.Rule]++
				weights[f.Rule] = append(weights[f.Rule], f.Weight)
			}
			if len(r.Mistakes) > 0 {
				withMistake++
				topBy[r.Findings[r.Mistakes[0].Finding].Rule]++
			}
			if r.Strength != nil {
				withStrength++
			}
		}
	}

	for _, rl := range Rules() {
		n := firedBy[rl.ID]
		t.Logf("%-16s fired for %3d of %d, the first mistake of %d", rl.ID, n, total, topBy[rl.ID])
		if n == total {
			t.Errorf("%s fires for every player: it tells nothing", rl.ID)
		}
	}
	t.Logf("%d of %d players have a mistake, %d a strength", withMistake, total, withStrength)
	if total < minPlayerMatches {
		t.Skipf("%d player-matches: the bounds of the rules need %d", total, minPlayerMatches)
	}
	for _, rl := range Rules() {
		ws := weights[rl.ID]
		if len(ws) == 0 {
			if !rl.Rare {
				t.Errorf("%s fires for nobody and is not marked rare", rl.ID)
			}
			continue
		}
		slices.Sort(ws)
		median := ws[len(ws)/2]
		if ws[len(ws)-1] > maxWeight {
			t.Errorf("%s: the heaviest finding weighs %.1f, more than %v", rl.ID, ws[len(ws)-1], maxWeight)
		}
		if len(ws) >= medianFrom && (median < minMedian || median > maxMedian) {
			t.Errorf("%s: the median weight of %d findings is %.2f, out of %v to %v", rl.ID, len(ws), median, minMedian, maxMedian)
		}
		if share := float64(topBy[rl.ID]) / float64(withMistake); share > maxTopShare {
			t.Errorf("%s is the first mistake of %.0f%% of the players with mistakes, more than %.0f%%", rl.ID, 100*share, 100*maxTopShare)
		}
	}
	if share := float64(withMistake) / float64(total); share < minWithMistake {
		t.Errorf("%.0f%% of the players have a mistake, less than %.0f%%", 100*share, 100*minWithMistake)
	}
	if share := float64(withStrength) / float64(total); share < minWithStrength {
		t.Errorf("%.0f%% of the players have a strength, less than %.0f%%", 100*share, 100*minWithStrength)
	}
}

// checkMeasures checks the measures of player k against the metrics they
// are counted from.
func checkMeasures(t *testing.T, name string, k int, st metrics.Stats, r Report) {
	t.Helper()
	deaths := float64(st.TradedDeaths + st.UntradedDeaths)
	played := float64(st.RoundsPlayed)
	var clutches float64
	for x, c := range st.Clutches {
		clutches += float64((x + 1) * c.Wins)
	}
	want := map[string][2]float64{
		string(UntradedDeath):  {float64(st.UntradedDeaths), deaths},
		string(LoneDeath):      {float64(st.LoneDeaths), deaths},
		string(AdvantageDeath): {float64(st.AdvantageDeaths), deaths},
		string(DryDeath):       {float64(st.DryDeaths), deaths},
		string(UnusedGrenades): {float64(st.DeathsWithGrenades), deaths},
		string(BadFlashes):     {float64(st.BadFlashes), float64(st.FlashesThrown)},
		string(MovingShots):    {float64(st.MovingShots), float64(st.MovingBase)},
		string(FirstShot):      {float64(st.FirstShotFights - st.FirstShotHits), float64(st.FirstShotFights)},
		string(MissedTrade):    {float64(st.MissedTrades), float64(st.TradeChances)},
		string(OpeningWins):    {float64(st.OpeningWins), float64(st.OpeningAttempts)},
		string(MultiKills):     {float64(st.MultiKills[1] + st.MultiKills[2] + st.MultiKills[3]), played},
		string(Clutches):       {clutches, played},
		MeasureGrenadeDamage:   {float64(st.GrenadeDamage) / DamageUnit, played},
		string(TradeKills):     {float64(st.TradeKills), float64(st.TeammateDeaths)},
		MeasureKD:              {float64(st.Kills), float64(st.Deaths)},
		MeasureADR:             {float64(st.Damage), float64(st.Rounds)},
		MeasureKAST:            {float64(st.KASTRounds), played},
		MeasureHeadshots:       {float64(st.HeadshotKills), float64(st.Kills)},
		MeasureAccuracy:        {float64(st.AccuracyHits), float64(st.AccuracyShots)},
	}
	if len(r.Comparisons) != len(Measures()) {
		t.Fatalf("%s, player %d: %d comparisons, want %d", name, k, len(r.Comparisons), len(Measures()))
	}
	for _, c := range r.Comparisons {
		if w, ok := want[c.Measure]; ok {
			if !near(c.X, w[0]) || !near(c.N, w[1]) {
				t.Errorf("%s, player %d: %s is %v of %v, the metrics %v of %v", name, k, c.Measure, c.X, c.N, w[0], w[1])
			}
			continue
		}
		// The measures with rules of their own on top of the metrics.
		lost := float64(st.OpeningAttempts - st.OpeningWins)
		switch c.Measure {
		case string(OpeningLoss): // the excused duels are left out, for the loser and for the winner
			if c.X > lost || c.N-c.X > float64(st.OpeningWins) || c.X > c.N {
				t.Errorf("%s, player %d: %v of %v opening duels lost, the metrics %v of %d", name, k, c.X, c.N, lost, st.OpeningAttempts)
			}
		case MeasureEnemyFlashes: // with the flash assists
			if c.X < float64(st.EnemiesFlashed) || c.X > float64(st.EnemiesFlashed+st.Assists) || c.N != played {
				t.Errorf("%s, player %d: %v enemies blinded with the flash assists in %v rounds, the metrics %d and %d assists", name, k, c.X, c.N, st.EnemiesFlashed, st.Assists)
			}
		case string(DeathSpot):
			if c.N != 1 || c.X > float64(st.UntradedDeaths) || st.UntradedDeaths > 0 && c.X < 1 {
				t.Errorf("%s, player %d: %v untraded deaths at the worst place of %v, the metrics %d untraded", name, k, c.X, c.N, st.UntradedDeaths)
			}
		default:
			t.Errorf("%s, player %d: the measure %s is checked against nothing", name, k, c.Measure)
		}
	}
}

// checkReport checks what holds for the report of any player: the
// comparisons, the findings and what is picked of them. past tells whether
// the report was made with a past of the player.
func checkReport(t *testing.T, name string, k int, a *metrics.Analysis, r Report, past bool) {
	t.Helper()
	bad := func(format string, args ...any) {
		t.Helper()
		t.Errorf("%s, player %d: "+format, append([]any{name, k}, args...)...)
	}
	for _, c := range r.Comparisons {
		if c.Of > len(a.Players) || c.Rank < 0 || c.Rank > c.Of || (c.Rank == 0) != (c.N == 0) ||
			!near(c.Lobby.X, c.Teammates.X+c.Opponents.X) || !near(c.Lobby.N, c.Teammates.N+c.Opponents.N) ||
			c.X < 0 || c.N < 0 || math.IsNaN(c.Rate) || !past && c.Own != nil {
			bad("comparison %+v", c)
		}
	}
	byID := make(map[RuleID]Rule)
	for _, rl := range Rules() {
		byID[rl.ID] = rl
	}
	lead := int(math.Round(GotoLead.Seconds() * a.TickRate))
	seen := make(map[RuleID]bool)
	var heaviestStrength = -1
	for i, f := range r.Findings {
		rl, ok := byID[f.Rule]
		if !ok || seen[f.Rule] || f.Kind != rl.Kind || f.Group != rl.Group {
			bad("finding %d of the rule %q, kind %q, group %q", i, f.Rule, f.Kind, f.Group)
			continue
		}
		seen[f.Rule] = true
		want := f.Z * f.Impact
		if f.Habit {
			want *= HabitFactor
		}
		if !(f.Weight > 0) || math.IsInf(f.Weight, 0) || !near(f.Weight, want) || f.Excess < MinExcess || f.Z < MinZ ||
			f.Sample < rl.MinSample || !near(f.X-f.Expected, f.Excess) || !near(f.N*f.BaseRate, f.Expected) ||
			f.Impact <= 0 || f.Kind == Strength && f.Impact != 1 || f.Habit && !past ||
			i > 0 && f.Weight > r.Findings[i-1].Weight || (f.Rule == DeathSpot) != (f.Place != "") {
			bad("finding %+v", f)
		}
		if f.Kind == Strength && heaviestStrength < 0 {
			heaviestStrength = i
		}
		// A finding shows its moments; only blindings by flashbangs the demo
		// does not name leave the useful grenades without one.
		if len(f.Evidence) > MaxEvidence || len(f.Evidence) == 0 && f.Rule != UsefulGrenades {
			bad("%s shows %d moments", f.Rule, len(f.Evidence))
		}
		for j, e := range f.Evidence {
			if e.Round < 1 || e.Round > len(a.Rounds) || e.Clock < 0 || e.Tick < 0 || e.Goto != max(e.Tick-lead, 0) ||
				f.Kind == Mistake && e.Impact <= 0 || e.TeammateMeters < 0 ||
				j > 0 && (e.Round < f.Evidence[j-1].Round || e.Round == f.Evidence[j-1].Round && e.Tick < f.Evidence[j-1].Tick) {
				bad("%s: moment %d = %+v", f.Rule, j, e)
			}
		}
	}
	if len(r.Mistakes) > MaxMistakes {
		bad("%d mistakes picked", len(r.Mistakes))
	}
	groups := make(map[Group]bool)
	mistakes := 0
	for _, f := range r.Findings {
		if f.Kind == Mistake {
			mistakes++
		}
	}
	if (len(r.Mistakes) == 0) != (mistakes == 0) {
		bad("%d mistakes fired, %d picked", mistakes, len(r.Mistakes))
	}
	for i, p := range r.Mistakes {
		if p.Finding < 0 || p.Finding >= len(r.Findings) {
			bad("mistake %d is the finding %d of %d", i, p.Finding, len(r.Findings))
			continue
		}
		f := r.Findings[p.Finding]
		if f.Kind != Mistake || groups[f.Group] || p.Weight < f.Weight || i > 0 && p.Weight > r.Mistakes[i-1].Weight {
			bad("mistake %d = %+v, the finding %+v", i, p, f)
		}
		groups[f.Group] = true
		heaviest := f.Weight
		for _, j := range p.With {
			if j < 0 || j >= len(r.Findings) || j == p.Finding {
				bad("mistake %d is told with the finding %d", i, j)
				continue
			}
			g := r.Findings[j]
			// Told with it are the findings nested with it; it is itself
			// nested, the most exact of them.
			if g.Group != f.Group || g.Kind != Mistake || rootOf(g.Rule) != rootOf(f.Rule) || byID[f.Rule].Parent == "" {
				bad("mistake %d, %s, is told with %s", i, f.Rule, g.Rule)
			}
			heaviest = max(heaviest, g.Weight)
		}
		if !near(p.Weight, heaviest) && len(p.With) > 0 {
			bad("mistake %d is picked by %v, the heaviest of its family weighs %v", i, p.Weight, heaviest)
		}
	}
	switch {
	case heaviestStrength < 0 && r.Strength != nil, heaviestStrength >= 0 && r.Strength == nil:
		bad("the strength %+v, the heaviest one fired is the finding %d", r.Strength, heaviestStrength)
	case r.Strength != nil && (r.Strength.Finding != heaviestStrength || r.Strength.Weight != r.Findings[heaviestStrength].Weight || r.Strength.With != nil):
		bad("the strength %+v, want the finding %d", *r.Strength, heaviestStrength)
	}
}
