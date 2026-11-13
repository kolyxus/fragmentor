package findings

import (
	"cmp"
	"slices"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// The measures that are no rule of their own: the two of UsefulGrenades,
// and the scoreboard, which a review shows next to the lobby. The measure
// of every other rule has the name of the rule.
const (
	MeasureEnemyFlashes  = "enemy_flashes"  // enemies blinded and flash assists, a round
	MeasureGrenadeDamage = "grenade_damage" // grenade damage in DamageUnit, a round
	MeasureKD            = "kd"             // kills of deaths
	MeasureADR           = "adr"            // damage of rounds
	MeasureKAST          = "kast"           // KAST rounds of rounds played
	MeasureHeadshots     = "headshot_kills" // headshot kills of kills
	MeasureAccuracy      = "accuracy"       // hits of rifle shots in sight
)

// rule is a rule with the measures it fires on: the heaviest of them that
// fires makes the finding.
type rule struct {
	Rule
	measures []string
}

// rules is the rules of docs/FINDINGS.md, in its order. The minimal samples
// are fitted on the demos of bot/testdata, see "Проверка правил" there.
var rules = []rule{
	{Rule: Rule{ID: UntradedDeath, Kind: Mistake, Group: Trades, MinSample: 8}},
	{Rule: Rule{ID: LoneDeath, Kind: Mistake, Group: Trades, Parent: UntradedDeath, MinSample: 8}},
	{Rule: Rule{ID: OpeningLoss, Kind: Mistake, Group: Duels, MinSample: 4}},
	{Rule: Rule{ID: AdvantageDeath, Kind: Mistake, Group: Trades, MinSample: 8}},
	{Rule: Rule{ID: DryDeath, Kind: Mistake, Group: Trades, Parent: UntradedDeath, MinSample: 8}},
	{Rule: Rule{ID: UnusedGrenades, Kind: Mistake, Group: Grenades, MinSample: 8}},
	{Rule: Rule{ID: BadFlashes, Kind: Mistake, Group: Grenades, MinSample: 5}},
	{Rule: Rule{ID: MovingShots, Kind: Mistake, Group: Shooting, MinSample: 30}},
	{Rule: Rule{ID: FirstShot, Kind: Mistake, Group: Shooting, MinSample: 10}},
	{Rule: Rule{ID: MissedTrade, Kind: Mistake, Group: Trades, MinSample: 3}},
	{Rule: Rule{ID: DeathSpot, Kind: Mistake, Group: Trades, Parent: UntradedDeath, MinSample: SpotDeaths}},

	{Rule: Rule{ID: OpeningWins, Kind: Strength, Group: Duels, MinSample: 4}},
	{Rule: Rule{ID: MultiKills, Kind: Strength, Group: Duels}},
	{Rule: Rule{ID: Clutches, Kind: Strength, Group: Duels}},
	{Rule: Rule{ID: UsefulGrenades, Kind: Strength, Group: Grenades, MinSample: 5}, measures: []string{MeasureEnemyFlashes, MeasureGrenadeDamage}},
	{Rule: Rule{ID: TradeKills, Kind: Strength, Group: Trades, MinSample: 5}},
}

// measures is everything counted for every player, in the order a report
// lists its comparisons in: the measures of the rules, then the scoreboard.
var measures = []measure{
	{id: string(UntradedDeath)},
	{id: string(LoneDeath)},
	{id: string(OpeningLoss)},
	{id: string(AdvantageDeath)},
	{id: string(DryDeath)},
	{id: string(UnusedGrenades)},
	{id: string(BadFlashes)},
	{id: string(MovingShots)},
	{id: string(FirstShot)},
	{id: string(MissedTrade)},
	{id: string(DeathSpot), model: perMatch},

	{id: string(OpeningWins), good: true},
	{id: string(MultiKills), model: perRound, good: true},
	{id: string(Clutches), model: perRound, good: true},
	{id: MeasureEnemyFlashes, model: perRound, good: true},
	{id: MeasureGrenadeDamage, model: perRound, good: true},
	{id: string(TradeKills), good: true},

	{id: MeasureKD, good: true},
	{id: MeasureADR, good: true},
	{id: MeasureKAST, good: true},
	{id: MeasureHeadshots, good: true},
	{id: MeasureAccuracy, good: true},
}

// measureOf is the measures by name.
var measureOf = func() map[string]measure {
	out := make(map[string]measure, len(measures))
	for _, m := range measures {
		out[m.id] = m
	}
	return out
}()

func init() {
	// A rule without measures of its own fires on the measure of its name.
	for i := range rules {
		if rules[i].measures == nil {
			rules[i].measures = []string{string(rules[i].ID)}
		}
	}
}

// Rules returns the rules, in the order of docs/FINDINGS.md.
func Rules() []Rule {
	out := make([]Rule, len(rules))
	for i, r := range rules {
		out[i] = r.Rule
	}
	return out
}

// Measures returns the names of the measures, in the order a report lists
// its comparisons in.
func Measures() []string {
	out := make([]string, len(measures))
	for i, m := range measures {
		out[i] = m.id
	}
	return out
}

// parentOf returns the rule that id is nested in, "" if none.
func parentOf(id RuleID) RuleID {
	for _, r := range rules {
		if r.ID == id {
			return r.Parent
		}
	}
	return ""
}

// data is the measures of the players of a match.
type data struct {
	a *metrics.Analysis
	// tallies is each measure of each player, by the name of the measure
	// and the index of the player in a.Players.
	tallies map[string][]tally
}

// impactOf returns what an event of a mistake weighs: more in a round the
// team lost and on its full buy, less in its eco, nothing if it is excused.
func impactOf(won bool, buy metrics.Buy, excused bool) float64 {
	if excused {
		return 0
	}
	f := 1.0
	if !won {
		f *= LostRoundImpact
	}
	switch buy {
	case metrics.BuyFull:
		f *= FullBuyImpact
	case metrics.BuyEco:
		f *= EcoImpact
	}
	return f
}

// spot is a place of the map on a side, and cell the deaths of a player
// there.
type spot struct {
	place string
	side  demo.Side
}

type cell struct {
	deaths, untraded int
	events           []event
}

// collect counts the measures of every player of the match of a.
func collect(a *metrics.Analysis) *data {
	n := len(a.Players)
	d := &data{a: a, tallies: make(map[string][]tally, len(measures))}
	for _, m := range measures {
		d.tallies[m.id] = make([]tally, n)
	}
	index := make(map[uint64]int, n)
	for i, p := range a.Players {
		index[p.SteamID64] = i
	}
	rounds := make(map[int]metrics.RoundInfo, len(a.Rounds))
	for _, r := range a.Rounds {
		rounds[r.Number] = r
	}
	of := func(id RuleID, i int) *tally { return &d.tallies[string(id)][i] }
	// in returns how the round went for a team: whether it won it, how it
	// bought, and what an event of a mistake weighs in it.
	in := func(team, round int) (won bool, buy metrics.Buy, impact float64) {
		r, ok := rounds[round]
		if !ok {
			return false, "", 1
		}
		won, buy = r.Winner == team, r.Buy[team]
		return won, buy, impactOf(won, buy, false)
	}

	// The deaths: what the victim did wrong, and what the killer did well.
	type roundKiller struct {
		round  int
		killer uint64
	}
	firstKill := make(map[roundKiller]metrics.Death)
	flashAssists := make([]float64, n)
	spots := make([]map[spot]*cell, n)
	for i := range spots {
		spots[i] = make(map[spot]*cell)
	}
	for _, dt := range a.Deaths {
		ki, killer := index[dt.Killer]
		if killer {
			if _, ok := firstKill[roundKiller{dt.Round, dt.Killer}]; !ok {
				firstKill[roundKiller{dt.Round, dt.Killer}] = dt
			}
		}
		if ai, ok := index[dt.Assister]; ok && dt.FlashAssist {
			flashAssists[ai]++
		}
		// The kill as the killer saw it.
		kill := Evidence{
			Round: dt.Round, Clock: dt.Clock, Planted: dt.Planted, Tick: dt.Tick, Won: !dt.Won, Buy: dt.EnemyBuy,
			Place: dt.KillerPlace, EnemyPlace: dt.VictimPlace, Weapon: dt.Weapon,
		}
		if killer && dt.TradeKill {
			t := of(TradeKills, ki)
			t.x++
			t.events = append(t.events, event{count: 1, score: 1, ev: kill})
		}
		vi, ok := index[dt.Victim]
		if !ok {
			continue
		}
		impact := impactOf(dt.Won, dt.Buy, dt.Excused)
		death := Evidence{
			Round: dt.Round, Clock: dt.Clock, Planted: dt.Planted, Tick: dt.Tick, Won: dt.Won, Buy: dt.Buy,
			Place: dt.VictimPlace, EnemyPlace: dt.KillerPlace, TeammateMeters: meters(dt.TeammateDistance),
			Alive: dt.AliveOwn, AliveEnemy: dt.AliveEnemy, Weapon: dt.Weapon,
		}
		count := func(id RuleID, on bool, weight float64, ev Evidence) {
			t := of(id, vi)
			t.n++
			if on {
				t.x++
				t.events = append(t.events, event{count: 1, impact: impact * weight, ev: ev})
			}
		}
		count(UntradedDeath, !dt.Traded, 1, death)
		count(LoneDeath, dt.Lone, 1, death)
		count(AdvantageDeath, dt.Advantage, 1, death)
		count(DryDeath, dt.Dry, 1, death)
		left := death
		left.Grenades, left.GrenadeValue = dt.Grenades, dt.GrenadeValue
		count(UnusedGrenades, len(dt.Grenades) > 0, float64(dt.GrenadeValue)/GrenadeUnit, left)

		if dt.Opening {
			// An excused death is no opening duel lost, and no duel at all
			// for the mistake: neither for the victim nor for the killer.
			if !dt.Excused {
				count(OpeningLoss, true, 1, death)
				if killer {
					of(OpeningLoss, ki).n++
				}
			}
			of(OpeningWins, vi).n++
			if killer {
				t := of(OpeningWins, ki)
				t.n++
				t.x++
				t.events = append(t.events, event{count: 1, score: 1, ev: kill})
			}
		}
		// The deaths of a place on a side, those in a crowd of enemies aside.
		if dt.VictimPlace != "" && dt.EnemiesNear < SpotCrowd {
			key := spot{dt.VictimPlace, dt.Side}
			c := spots[vi][key]
			if c == nil {
				c = &cell{}
				spots[vi][key] = c
			}
			c.deaths++
			if !dt.Traded {
				c.untraded++
				c.events = append(c.events, event{count: 1, impact: impact, ev: death})
			}
		}
	}
	for i := range spots {
		deathSpot(of(DeathSpot, i), spots[i])
	}

	// The flashbangs: the bad ones are a mistake, those that blinded
	// enemies show the useful grenades.
	for _, fb := range a.Flashbangs {
		i, ok := index[fb.Thrower]
		if !ok {
			continue
		}
		won, buy, impact := in(a.Players[i].Team, fb.Round)
		ev := Evidence{
			Round: fb.Round, Clock: fb.Clock, Planted: fb.Planted, Tick: fb.Tick, Won: won, Buy: buy,
			Enemies: fb.Enemies, Teammates: fb.Teammates,
		}
		t := of(BadFlashes, i)
		t.n++
		if fb.Empty || fb.TeamFlash {
			t.x++
			t.events = append(t.events, event{count: 1, impact: impact, ev: ev})
		}
		if fb.Enemies > 0 {
			u := &d.tallies[MeasureEnemyFlashes][i]
			u.events = append(u.events, event{count: float64(fb.Enemies), score: float64(fb.Enemies), ev: ev})
		}
	}
	for _, g := range a.GrenadeHits {
		i, ok := index[g.Player]
		if !ok {
			continue
		}
		won, buy, _ := in(a.Players[i].Team, g.Round)
		ev := Evidence{
			Round: g.Round, Clock: g.Clock, Planted: g.Planted, Tick: g.Tick, Won: won, Buy: buy,
			Count: g.Damage, Enemies: g.Enemies, Detail: DetailHE,
		}
		if g.Fire {
			ev.Detail = DetailFire
		}
		t := &d.tallies[MeasureGrenadeDamage][i]
		t.events = append(t.events, event{count: float64(g.Damage) / DamageUnit, score: float64(g.Damage), ev: ev})
	}

	// The shooting.
	for _, rs := range a.RoundShots {
		i, ok := index[rs.Player]
		if !ok {
			continue
		}
		won, buy, impact := in(a.Players[i].Team, rs.Round)
		t := of(MovingShots, i)
		t.n += float64(rs.Shots)
		t.x += float64(rs.Moving)
		if rs.Moving > 0 {
			t.events = append(t.events, event{count: float64(rs.Moving), impact: impact, ev: Evidence{
				Round: rs.Round, Clock: rs.Clock, Planted: rs.Planted, Tick: rs.Tick, Won: won, Buy: buy,
				Weapon: rs.Weapon, Count: rs.Moving, Of: rs.Shots,
			}})
		}
	}
	for _, f := range a.Fights {
		i, ok := index[f.Player]
		if !ok {
			continue
		}
		won, buy, impact := in(a.Players[i].Team, f.Round)
		t := of(FirstShot, i)
		t.n++
		if !f.Hit {
			t.x++
			t.events = append(t.events, event{count: 1, impact: impact, ev: Evidence{
				Round: f.Round, Clock: f.Clock, Planted: f.Planted, Tick: f.Tick, Won: won, Buy: buy, Weapon: f.Weapon,
			}})
		}
	}

	// The chances to trade a teammate, and those missed.
	for _, ch := range a.Chances {
		i, ok := index[ch.Player]
		if !ok || ch.Death < 0 || ch.Death >= len(a.Deaths) {
			continue
		}
		t := of(MissedTrade, i)
		t.n++
		if !ch.Missed {
			continue
		}
		dt := a.Deaths[ch.Death]
		won, buy, impact := in(a.Players[i].Team, dt.Round)
		ev := Evidence{
			Round: dt.Round, Clock: dt.Clock, Planted: dt.Planted, Tick: dt.Tick, Won: won, Buy: buy,
			Place: dt.VictimPlace, EnemyPlace: dt.KillerPlace, Weapon: dt.Weapon, Detail: DetailNoShot,
		}
		switch {
		case ch.Hit:
			ev.Detail = DetailHit
		case ch.Shot:
			ev.Detail = DetailMissed
		}
		t.x++
		t.events = append(t.events, event{count: 1, impact: impact, ev: ev})
	}

	// The rounds of three kills and more, at the first of them.
	for _, r := range a.Rounds {
		for id, kills := range r.Kills {
			i, ok := index[id]
			if !ok || kills < 3 {
				continue
			}
			won, buy, _ := in(a.Players[i].Team, r.Number)
			ev := Evidence{Round: r.Number, Won: won, Buy: buy, Count: kills}
			if first, ok := firstKill[roundKiller{r.Number, id}]; ok {
				ev.Clock, ev.Planted, ev.Tick, ev.Weapon = first.Clock, first.Planted, first.Tick, first.Weapon
			}
			t := of(MultiKills, i)
			t.x++
			t.events = append(t.events, event{count: 1, score: float64(kills), ev: ev})
		}
	}
	// The clutches won: a 1vX weighs X.
	for _, cl := range a.Clutches {
		i, ok := index[cl.Player]
		if !ok || !cl.Won {
			continue
		}
		_, buy, _ := in(a.Players[i].Team, cl.Round)
		x := float64(min(cl.Enemies, 5))
		t := of(Clutches, i)
		t.x += x
		t.events = append(t.events, event{count: x, score: x, ev: Evidence{
			Round: cl.Round, Clock: cl.Clock, Planted: cl.Planted, Tick: cl.Tick, Won: true, Buy: buy, Count: cl.Enemies,
		}})
	}

	// What the metrics count already, and the samples.
	for i, p := range a.Players {
		st := p.Match
		played := float64(st.RoundsPlayed)
		thrown := float64(st.FlashesThrown + st.SmokesThrown + st.HEGrenadesThrown + st.FireGrenadesThrown + st.DecoysThrown)
		of(MultiKills, i).n = played
		of(Clutches, i).n = played
		of(TradeKills, i).n = float64(st.TeammateDeaths)
		flashes := &d.tallies[MeasureEnemyFlashes][i]
		flashes.x, flashes.n, flashes.sample, flashes.variant = float64(st.EnemiesFlashed)+flashAssists[i], played, thrown, VariantFlashes
		damage := &d.tallies[MeasureGrenadeDamage][i]
		damage.x, damage.n, damage.sample, damage.variant = float64(st.GrenadeDamage)/DamageUnit, played, thrown, VariantDamage

		d.tallies[MeasureKD][i] = tally{x: float64(st.Kills), n: float64(st.Deaths)}
		d.tallies[MeasureADR][i] = tally{x: float64(st.Damage), n: float64(st.Rounds)}
		d.tallies[MeasureKAST][i] = tally{x: float64(st.KASTRounds), n: float64(st.RoundsPlayed)}
		d.tallies[MeasureHeadshots][i] = tally{x: float64(st.HeadshotKills), n: float64(st.Kills)}
		d.tallies[MeasureAccuracy][i] = tally{x: float64(st.AccuracyHits), n: float64(st.AccuracyShots)}

		variants(of(MissedTrade, i), of(BadFlashes, i))
	}
	// The sample of a measure is its opportunities, unless set above.
	for id, ts := range d.tallies {
		if id == MeasureEnemyFlashes || id == MeasureGrenadeDamage || id == string(DeathSpot) {
			continue
		}
		for i := range ts {
			ts[i].sample = ts[i].n
		}
	}
	return d
}

// variants tells what most of the missed trades and of the bad flashbangs
// of a player were, for the text of the finding.
func variants(missed, flashes *tally) {
	var noShot, shot int
	for _, e := range missed.events {
		if e.ev.Detail == DetailNoShot {
			noShot++
		} else {
			shot++
		}
	}
	missed.variant = VariantNoKill
	if noShot > shot {
		missed.variant = VariantNoShot
	}
	var empty, team int
	for _, e := range flashes.events {
		if e.ev.Teammates > 0 {
			team++
		} else {
			empty++
		}
	}
	flashes.variant = VariantEmpty
	if team > empty {
		flashes.variant = VariantTeam
	}
}

// deathSpot fills the tally of the death spot of a player from his deaths
// by place. The measure is the untraded deaths at his worst place, whatever
// the place; the rule speaks of the worst place that is a spot: SpotDeaths
// deaths or more, SpotUntraded of them untraded.
func deathSpot(t *tally, cells map[spot]*cell) {
	t.n = 1
	t.skip = true
	keys := make([]spot, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	// The worst first: the most untraded deaths, then the most deaths.
	slices.SortFunc(keys, func(a, b spot) int {
		return cmp.Or(
			cmp.Compare(cells[b].untraded, cells[a].untraded),
			cmp.Compare(cells[b].deaths, cells[a].deaths),
			cmp.Compare(a.place, b.place),
			cmp.Compare(a.side, b.side),
		)
	})
	for i, k := range keys {
		c := cells[k]
		if i == 0 {
			t.x = float64(c.untraded)
		}
		if c.deaths >= SpotDeaths && float64(c.untraded) >= SpotUntraded*float64(c.deaths)-1e-9 {
			t.skip = false
			t.ruleX, t.hasRuleX = float64(c.untraded), true
			t.sample = float64(c.deaths)
			t.place, t.side = k.place, k.side
			t.events = c.events
			return
		}
	}
}

// meters returns a distance in units of the game in meters, 0 for none.
func meters(units float64) float64 {
	if units < 0 {
		return 0
	}
	return units / unitsPerMeter
}
