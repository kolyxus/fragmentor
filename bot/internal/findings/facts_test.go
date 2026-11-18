package findings

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// factsGame is a made-up match of 16 rounds in which player 1, of team 0,
// has untraded deaths, some of them alone, and bad flashbangs; team 0 lost
// it 7:13 and started it on T.
func factsGame() (*game, *demo.Match) {
	g := newGame(20)
	g.lobby(16, 4, untraded)
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
	for id := uint64(2); id <= 10; id++ {
		for n := 1; n <= 6; n++ {
			g.flashbang(n, id, 1, 0)
		}
	}
	for n := 1; n <= 6; n++ {
		g.flashbang(n, 1, 0, 0)
	}
	m := &demo.Match{
		Map: "de_mirage", TickRate: testTickRate, Rounds: make([]demo.Round, 20),
		Teams: [2]demo.Team{{StartSide: demo.T, Score: 7}, {StartSide: demo.CT, Score: 13}},
	}
	return g, m
}

func TestBuildFacts(t *testing.T) {
	g, m := factsGame()
	a := &g.a
	// The events of player 1 besides the deaths: a kill, a grenade of each
	// kind, the damage of an HE. His team bought in full in round 17, the
	// victim's team saved.
	g.round(17).Buy = [2]metrics.Buy{metrics.BuyFull, metrics.BuyEco}
	k := g.death(17, 7, 1, opening, func(d *metrics.Death) {
		d.Headshot, d.Weapon, d.VictimPlace, d.KillerPlace, d.Clock, d.Planted = true, "AK-47", "Apartments", "Underpass", 31.5, true
	})
	a.Deaths[k].Won = false // the victim's team lost: player 1 won it
	g.round(17).Winner = 0
	a.Throws = append(a.Throws,
		metrics.Throw{Grenade: 1, Round: 2, Tick: 900, Clock: 101, Thrower: 1, Weapon: demo.Flashbang},
		metrics.Throw{Grenade: 99, Round: 3, Tick: 1900, Clock: 80, Thrower: 1, Weapon: demo.SmokeGrenade},
		metrics.Throw{Grenade: 100, Round: 3, Tick: 1950, Clock: 79, Thrower: 2, Weapon: demo.HEGrenade}, // of player 2
	)
	a.Flashbangs[len(a.Flashbangs)-6].Grenade = 1 // the first flashbang of player 1
	a.Flashbangs[len(a.Flashbangs)-6].Teammates = 2
	a.GrenadeHits = []metrics.GrenadeHit{
		{Round: 4, Tick: 3000, Clock: 70, Player: 1, Damage: 57, Enemies: 2},
		{Round: 4, Tick: 3100, Clock: 69, Player: 6, Fire: true, Damage: 20, Enemies: 1},
	}
	st := g.stats(1)
	st.Kills, st.Deaths, st.Assists, st.ADR, st.KAST, st.HeadshotKillRate = 1, 16, 2, 61.5, 0.55, 1
	reports := Analyze(a, nil)
	// What the facts tell and the findings do not count: the rounds he
	// played, who was alive at his first death and what he carried, and how
	// the teams bought in round 2.
	st.RoundsPlayed = 19
	first := slices.IndexFunc(a.Deaths, func(d metrics.Death) bool { return d.Victim == 1 })
	a.Deaths[first].AliveEnemy = 4
	carrying(demo.Flashbang, demo.Flashbang)(&a.Deaths[first])
	g.round(2).Buy = [2]metrics.Buy{metrics.BuyEco, metrics.BuyFull}

	drills := map[RuleID][]string{LoneDeath: {"demo_review"}, BadFlashes: {"yprac_flashes", "other"}}
	f := BuildFacts(FactsInput{Match: m, Analysis: a, Player: 0, Report: reports[0], Level: 6,
		Drills: func(r RuleID) []string { return drills[r] }})

	if f.Version != FactsVersion {
		t.Errorf("version %d", f.Version)
	}
	if want := (MatchFacts{Map: "de_mirage", Rounds: 20, Score: [2]int{7, 13}, Result: ResultLoss, StartSide: demo.T, TickRate: testTickRate}); f.Match != want {
		t.Errorf("match = %+v, want %+v", f.Match, want)
	}
	if want := (PlayerFacts{Level: 6, Kills: 1, Deaths: 16, Assists: 2, ADR: 61.5, KAST: 0.55, HeadshotKills: 1, RoundsPlayed: 19}); f.Player != want {
		t.Errorf("player = %+v, want %+v", f.Player, want)
	}
	if len(f.Comparisons) != len(Measures()) || f.Comparisons[0].Measure != Measures()[0] {
		t.Errorf("%d comparisons", len(f.Comparisons))
	}
	// The findings in the order of the report, with IDs from f1.
	if len(f.Findings) != len(reports[0].Findings) {
		t.Fatalf("%d findings, the report has %d", len(f.Findings), len(reports[0].Findings))
	}
	ids := make(map[RuleID]string)
	for i, ff := range f.Findings {
		if ff.ID != "f"+string(rune('1'+i)) || ff.Rule != reports[0].Findings[i].Rule || ff.Weight != reports[0].Findings[i].Weight {
			t.Errorf("finding %d = %+v", i, ff)
		}
		if !slices.Equal(ff.Drills, drills[ff.Rule]) {
			t.Errorf("finding %s: drills %v, want %v", ff.Rule, ff.Drills, drills[ff.Rule])
		}
		ids[ff.Rule] = ff.ID
	}
	// The untraded deaths are told by the lone ones; the task is against the
	// heaviest mistake that has a drill.
	var picked []string
	for _, pk := range f.Review.Mistakes {
		picked = append(picked, pk.Finding)
		if pk.Finding == ids[LoneDeath] && !slices.Equal(pk.With, []string{ids[UntradedDeath]}) {
			t.Errorf("the lone deaths are told with %v", pk.With)
		}
	}
	if !slices.Contains(picked, ids[LoneDeath]) || !slices.Contains(picked, ids[BadFlashes]) || len(picked) != 2 {
		t.Errorf("picked %v of %v", picked, ids)
	}
	if f.Review.Task == nil || f.Review.Task.Drill != drills[f.findingRule(t, f.Review.Mistakes[0].Finding)][0] ||
		f.Review.Task.Finding != f.Review.Mistakes[0].Finding {
		t.Errorf("task = %+v, the first mistake %+v", f.Review.Task, f.Review.Mistakes[0])
	}

	// The events of player 1, and nobody else's: the lobby of the game has
	// him kill the players of team 1 too.
	kills := 0
	for _, d := range a.Deaths {
		if d.Killer == 1 {
			kills++
		}
	}
	if len(f.Events.Deaths) != 16 || len(f.Events.Kills) != kills || len(f.Events.Grenades) != 2 || len(f.Events.GrenadeDamage) != 1 {
		t.Fatalf("events = %d deaths, %d kills of %d, %d grenades, %d grenade damage", len(f.Events.Deaths), len(f.Events.Kills), kills,
			len(f.Events.Grenades), len(f.Events.GrenadeDamage))
	}
	kill := f.Events.Kills[slices.IndexFunc(f.Events.Kills, func(k KillFact) bool { return k.Round == 17 })]
	if want := (Moment{Round: 17, Clock: 31.5, Planted: true, Tick: kill.Tick, Won: true, Buy: metrics.BuyFull}); kill.Moment != want ||
		kill.EnemyBuy != metrics.BuyEco || kill.Place != "Underpass" || kill.VictimPlace != "Apartments" || kill.Weapon != "AK-47" ||
		!kill.Headshot || !kill.Opening || kill.TradeKill {
		t.Errorf("kill = %+v", kill)
	}
	d := f.Events.Deaths[0]
	if d.Round != 1 || d.Traded || !d.Lone || d.Place != "B" || d.KillerPlace != "Window" || d.TeammateMeters != 50.8 ||
		d.Alive != 3 || d.AliveEnemy != 4 || !d.Won || !slices.Equal(d.Grenades, []string{demo.Flashbang, demo.Flashbang}) || d.GrenadeValue != 400 {
		t.Errorf("death = %+v", d)
	}
	if g0 := f.Events.Grenades[0]; g0.Weapon != demo.Flashbang || g0.Teammates != 2 || g0.Enemies != 0 || g0.Round != 2 || g0.Clock != 101 ||
		g0.Buy != metrics.BuyEco {
		t.Errorf("flashbang = %+v", g0)
	}
	if g1 := f.Events.Grenades[1]; g1.Weapon != demo.SmokeGrenade || g1.Enemies != 0 || g1.Round != 3 {
		t.Errorf("smoke = %+v", g1)
	}
	if h := f.Events.GrenadeDamage[0]; h.Damage != 57 || h.Enemies != 2 || h.Fire || h.Round != 4 || h.Won != (g.round(4).Winner == 0) {
		t.Errorf("grenade damage = %+v", h)
	}

	// The facts go into the database as JSON: the same back.
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var back Facts
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if b2, _ := json.Marshal(back); string(b2) != string(b) {
		t.Errorf("the facts change through JSON:\n%s\n%s", b, b2)
	}
	for _, key := range []string{`"version":1`, `"findings":[{"id":"f1","rule":`, `"review":{"mistakes":[{"finding":"f`, `"task":{"drill":`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("no %s in the JSON of the facts", key)
		}
	}

	// A player of team 1, which won it 13:7 from CT; the summary of the demo
	// may be wrong.
	m.Suspicious = true
	f = BuildFacts(FactsInput{Match: m, Analysis: a, Player: 5, Report: reports[5]})
	if want := (MatchFacts{Map: "de_mirage", Rounds: 20, Score: [2]int{13, 7}, Result: ResultWin, StartSide: demo.CT, TickRate: testTickRate,
		Suspicious: true}); f.Match != want {
		t.Errorf("match of team 1 = %+v, want %+v", f.Match, want)
	}
}

// findingRule returns the rule of the finding with the ID.
func (f Facts) findingRule(t *testing.T, id string) RuleID {
	t.Helper()
	for _, ff := range f.Findings {
		if ff.ID == id {
			return ff.Rule
		}
	}
	t.Fatalf("no finding %s", id)
	return ""
}

func TestFactsTask(t *testing.T) {
	g, m := factsGame()
	r := Analyze(&g.a, nil)[0]
	build := func(drills map[RuleID][]string) *TaskFacts {
		return BuildFacts(FactsInput{Match: m, Analysis: &g.a, Report: r, Drills: func(id RuleID) []string { return drills[id] }}).Review.Task
	}
	ids := make(map[RuleID]string)
	for i, f := range r.Findings {
		ids[f.Rule] = "f" + string(rune('1'+i))
	}
	first := r.Findings[r.Mistakes[0].Finding].Rule
	// Only the general rule told with the lone deaths has a drill: it is the
	// task, against the finding the review tells.
	if task := build(map[RuleID][]string{UntradedDeath: {"retakes"}}); first == LoneDeath &&
		(task == nil || task.Drill != "retakes" || task.Finding != ids[LoneDeath]) {
		t.Errorf("task = %+v", task)
	}
	// The first mistake has no drill: the task is against the next one.
	if task := build(map[RuleID][]string{BadFlashes: {"yprac_flashes"}}); task == nil || task.Drill != "yprac_flashes" || task.Finding != ids[BadFlashes] {
		t.Errorf("task = %+v", task)
	}
	// No drill at all, no task; no drills func, no drills.
	if task := build(nil); task != nil {
		t.Errorf("task = %+v without drills", task)
	}
	f := BuildFacts(FactsInput{Match: m, Analysis: &g.a, Report: r})
	if f.Review.Task != nil || slices.ContainsFunc(f.Findings, func(ff FactFinding) bool { return ff.Drills != nil }) {
		t.Errorf("facts without drills: task %+v", f.Review.Task)
	}
	// Strengths have no drills, and a draw is a draw.
	m.Teams[0].Score, m.Teams[1].Score = 12, 12
	if f := BuildFacts(FactsInput{Match: m, Analysis: &g.a, Report: r}); f.Match.Result != ResultDraw {
		t.Errorf("result of 12:12 = %s", f.Match.Result)
	}

	// A mistake and a strength, every rule with two drills: the drills go to
	// the mistake only, the task is its first drill, the strength is told.
	two := func(id RuleID) []string { return []string{string(id) + "_1", string(id) + "_2"} }
	f = BuildFacts(FactsInput{Match: m, Analysis: &g.a, Drills: two, Report: Report{
		Findings: []Finding{{Rule: LoneDeath, Kind: Mistake}, {Rule: MultiKills, Kind: Strength}},
		Mistakes: []Pick{{Finding: 0}},
		Strength: &Pick{Finding: 1},
	}})
	if !slices.Equal(f.Findings[0].Drills, two(LoneDeath)) || f.Findings[1].Drills != nil {
		t.Errorf("drills of the mistake %v, of the strength %v", f.Findings[0].Drills, f.Findings[1].Drills)
	}
	if task := f.Review.Task; task == nil || *task != (TaskFacts{Drill: "lone_death_1", Finding: "f1"}) {
		t.Errorf("task = %+v", task)
	}
	if s := f.Review.Strength; s == nil || s.Finding != "f2" || len(s.With) != 0 {
		t.Errorf("strength = %+v", s)
	}
	// Nothing to tell: the mistakes are an empty list, not null.
	b, err := json.Marshal(BuildFacts(FactsInput{Match: m, Analysis: &g.a}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"review":{"mistakes":[]}`) {
		t.Errorf("the review of facts without findings: %s", b[strings.Index(string(b), `"review"`):][:40])
	}
}

func TestGotoTick(t *testing.T) {
	for _, tc := range [][3]float64{{1000, 64, 680}, {100, 64, 0}, {320, 64, 0}, {2000, 128, 1360}} {
		if got := GotoTick(int(tc[0]), tc[1]); got != int(tc[2]) {
			t.Errorf("GotoTick(%v, %v) = %d, want %v", tc[0], tc[1], got, tc[2])
		}
	}
	var h History
	h.AddFacts(Facts{Comparisons: []Comparison{{Measure: "a", X: 2, N: 5}}})
	h.AddFacts(Facts{Comparisons: []Comparison{{Measure: "a", X: 1, N: 3}}})
	if h.Matches != 2 || h.Tallies["a"] != (Tally{3, 8}) {
		t.Errorf("history of facts = %+v", h)
	}
}
