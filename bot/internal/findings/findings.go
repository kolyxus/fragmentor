// Package findings turns the metrics of a match into findings: what a player
// did worse or better than the other nine players of the match, how sure
// that is, how much it weighs, and the moments of the match that show it.
// From the findings it picks the mistakes and the strength a review tells
// about.
//
// docs/FINDINGS.md is the contract: the rules, what each is compared with,
// when it fires, what it weighs and how the review is picked. docs/METRICS.md
// defines what is measured; here it is only judged. A finding is never made
// without something to compare with: the lobby, the other nine players of
// the match, is what every rule fires against.
//
// The package counts from a metrics.Analysis and knows nothing of the demo
// parser, Telegram or the database. A Report holds no names and no IDs.
package findings

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// When a rule fires, see docs/FINDINGS.md, "Срабатывание и вес". The player
// must have at least MinExcess events more than the share of the lobby
// would give him, and the excess must be at least MinZ times the spread
// that chance alone makes at his sample.
const (
	MinExcess = 2.0
	MinZ      = 1.0
)

// What an event of a mistake weighs. A lost round makes it heavier, a full
// buy of the player's team too, an eco lighter; an excused death, a pistol
// against a rifle in an eco, weighs nothing. The events of a strength all
// weigh one.
const (
	LostRoundImpact = 1.5
	FullBuyImpact   = 1.25
	EcoImpact       = 0.5
)

// HabitFactor is how much heavier a finding is when the past matches of the
// player show the same: a habit is worth more training than a bad match.
// MinHistory is how many past matches that takes, and MaxHistory how many
// of them count at most: the last ones before the match.
const (
	HabitFactor = 1.5
	MinHistory  = 3
	MaxHistory  = 10
)

// GrenadeUnit is the worth of the grenades left at a death, in dollars of
// the game, that makes the death weigh one for the unused grenades.
// DamageUnit is the grenade damage that counts as one event of the useful
// grenades: the health of one enemy.
const (
	GrenadeUnit = 500
	DamageUnit  = 100
)

// A death spot is a place of the map where the player died SpotDeaths times
// or more on one side, at least SpotUntraded of them untraded. Deaths with
// SpotCrowd enemies or more near are left out: an anchor a whole team walks
// over did not choose the place badly.
const (
	SpotDeaths   = 3
	SpotUntraded = 2.0 / 3
	SpotCrowd    = 3
)

// MaxMistakes is how many mistakes a review tells about at most, MaxEvidence
// how many moments show a finding, and GotoLead how long before a moment
// its demo_gototick tick is, to see how it came about.
const (
	MaxMistakes = 3
	MaxEvidence = 5
	GotoLead    = 5 * time.Second
)

// unitsPerMeter is the units of the game in a meter: a unit is an inch.
const unitsPerMeter = 1 / 0.0254

// Kind tells a mistake from a strength.
type Kind string

// The kinds.
const (
	Mistake  Kind = "mistake"
	Strength Kind = "strength"
)

// Group is what a rule is about. A review tells of one mistake of a group
// at most: the rules of a group are much about the same events.
type Group string

// The groups.
const (
	Trades   Group = "trades" // trades and positioning
	Duels    Group = "duels"
	Grenades Group = "grenades"
	Shooting Group = "shooting"
)

// RuleID names a rule.
type RuleID string

// The mistakes and the strengths, in the order of docs/FINDINGS.md.
const (
	UntradedDeath  RuleID = "untraded_death"
	LoneDeath      RuleID = "lone_death"
	OpeningLoss    RuleID = "opening_loss"
	AdvantageDeath RuleID = "advantage_death"
	DryDeath       RuleID = "dry_death"
	UnusedGrenades RuleID = "unused_grenades"
	BadFlashes     RuleID = "bad_flashes"
	MovingShots    RuleID = "moving_shots"
	FirstShot      RuleID = "first_shot"
	MissedTrade    RuleID = "missed_trade"
	DeathSpot      RuleID = "death_spot"

	OpeningWins    RuleID = "opening_wins"
	MultiKills     RuleID = "multikills"
	Clutches       RuleID = "clutches"
	UsefulGrenades RuleID = "useful_grenades"
	TradeKills     RuleID = "trade_kills"
)

// The variants of a finding: which measure of the rule fired, or what most
// of its events were.
const (
	VariantFlashes = "flashes" // UsefulGrenades: the enemies blinded
	VariantDamage  = "damage"  // UsefulGrenades: the grenade damage
	VariantNoShot  = "no_shot" // MissedTrade: mostly he did not shoot at the killer
	VariantNoKill  = "no_kill" // MissedTrade: mostly he shot and did not kill
	VariantEmpty   = "empty"   // BadFlashes: mostly flashbangs that blinded no enemy
	VariantTeam    = "team"    // BadFlashes: mostly flashbangs that blinded teammates
)

// Rule describes a rule of docs/FINDINGS.md.
type Rule struct {
	ID    RuleID
	Kind  Kind
	Group Group
	// Parent is the rule this one is nested in, "" if none: its events are
	// among the events of the parent, so it tells the same more exactly.
	Parent RuleID
	// MinSample is the fewest opportunities the rule speaks from: deaths,
	// flashbangs and the like, see docs/FINDINGS.md. 0 is no limit.
	MinSample float64
	// Rare marks a rule that may fire for nobody in the demos of
	// bot/testdata: a rare situation is no fault of the rule.
	Rare bool
}

// Report is the findings of one player of a match.
type Report struct {
	// Comparisons puts what the player did next to the lobby, his
	// teammates, his opponents and his own past, measure by measure.
	Comparisons []Comparison `json:"comparisons"`
	// Findings is every rule that fired, the heaviest first.
	Findings []Finding `json:"findings"`
	// Mistakes is the mistakes picked for the review, MaxMistakes at most
	// and one of a group at most, the heaviest first. Strength is the
	// strength picked, nil if none fired.
	Mistakes []Pick `json:"mistakes"`
	Strength *Pick  `json:"strength,omitempty"`
}

// Pick is a finding picked for the review.
type Pick struct {
	// Finding is the index in Report.Findings.
	Finding int `json:"finding"`
	// Weight is what it was picked by: the heaviest weight among the finding
	// and the findings of With.
	Weight float64 `json:"weight"`
	// With is the other findings nested with it, indices in
	// Report.Findings: of them the review tells this one, the most exact,
	// and names their numbers in its text, "11 untraded, 6 of them alone".
	With []int `json:"with,omitempty"`
}

// Comparison is a measure of the player next to what he is compared with.
// A measure is events of opportunities, like untraded deaths of deaths, or
// events of rounds.
type Comparison struct {
	Measure string `json:"measure"`
	// X and N are the events and the opportunities of the player, Rate
	// X / N, 0 without opportunities.
	X    float64 `json:"x"`
	N    float64 `json:"n"`
	Rate float64 `json:"rate"`
	// Lobby is the other nine players of the match together, Teammates and
	// Opponents the four and the five of them: the events of them all by
	// the opportunities of them all, not the mean of their rates.
	Lobby     Base `json:"lobby"`
	Teammates Base `json:"teammates"`
	Opponents Base `json:"opponents"`
	// Rank is the place of the player among the Of players of the match
	// that have opportunities, 1 the best; 0 if he has none himself.
	Rank int `json:"rank"`
	Of   int `json:"of"`
	// Own is the past matches of the player, nil without MinHistory of
	// them.
	Own *Base `json:"own,omitempty"`
}

// Base is the events and the opportunities of some players together, and
// the rate they make: X / N, 0 without opportunities.
type Base struct {
	X    float64 `json:"x"`
	N    float64 `json:"n"`
	Rate float64 `json:"rate"`
}

// Finding is a rule that fired for the player.
type Finding struct {
	Rule    RuleID `json:"rule"`
	Kind    Kind   `json:"kind"`
	Group   Group  `json:"group"`
	Variant string `json:"variant,omitempty"`
	// X is the events of the player and N his opportunities, or the rounds
	// he played for a rule that counts by the round; for DeathSpot X is the
	// untraded deaths at the spot and N one, the match.
	X float64 `json:"x"`
	N float64 `json:"n"`
	// Sample is what the minimal sample of the rule was checked against: N
	// for most rules, the grenades thrown for UsefulGrenades, the deaths at
	// the spot for DeathSpot.
	Sample float64 `json:"sample"`
	// BaseRate is the rate of the lobby the player is measured against,
	// Expected the events it would give him, and Excess how many more he
	// has.
	BaseRate float64 `json:"base_rate"`
	Expected float64 `json:"expected"`
	Excess   float64 `json:"excess"`
	// Z is how many times the excess is the spread chance alone makes.
	Z float64 `json:"z"`
	// Impact is what an event of the finding weighs on average, 1 for a
	// strength. Habit means the past matches of the player show the same.
	Impact float64 `json:"impact"`
	Habit  bool    `json:"habit,omitempty"`
	// Weight is Z by Impact, by HabitFactor for a habit: what the findings
	// are ordered and picked by.
	Weight float64 `json:"weight"`
	// Place and Side are the spot of DeathSpot: the place name of the game,
	// like "TopofMid", and the side the player died there on.
	Place string    `json:"place,omitempty"`
	Side  demo.Side `json:"side,omitempty"`
	// Evidence is the moments that show the finding, MaxEvidence at most:
	// the heaviest ones, in the order of the match.
	Evidence []Evidence `json:"evidence,omitempty"`
}

// Evidence is a moment of the match that shows a finding. Which of its
// fields are set depends on the rule.
type Evidence struct {
	Round int `json:"round"`
	// Clock is what the round clock showed, in seconds: the time left of
	// the round, of the bomb if Planted. A text rounds it up, as the game
	// does.
	Clock   float64 `json:"clock"`
	Planted bool    `json:"planted,omitempty"`
	// Tick is the moment and Goto the tick for demo_gototick, GotoLead
	// before it.
	Tick int `json:"tick"`
	Goto int `json:"goto"`
	// Won means the team of the player won the round; Buy is how it bought.
	Won bool        `json:"won"`
	Buy metrics.Buy `json:"buy,omitempty"`
	// Impact is what the moment weighs, as in Finding.Impact.
	Impact float64 `json:"impact"`
	// Place is where the player stood and EnemyPlace where his enemy did,
	// the place names of the game; for a missed trade Place is where the
	// teammate died.
	Place      string `json:"place,omitempty"`
	EnemyPlace string `json:"enemy_place,omitempty"`
	// TeammateMeters is how far the nearest live teammate stood as the
	// player died, 0 if none was alive.
	TeammateMeters float64 `json:"teammate_m,omitempty"`
	// Alive and AliveEnemy are how many were alive on the two sides right
	// before a death, the player among them.
	Alive      int `json:"alive,omitempty"`
	AliveEnemy int `json:"alive_enemy,omitempty"`
	// Weapon is the weapon of the moment: what killed, what was fired.
	Weapon string `json:"weapon,omitempty"`
	// Count and Of count what the moment stands for: the shots on the move
	// of the shots in sight of a round, the kills of a round, the X of a
	// 1vX clutch, the damage of a grenade.
	Count int `json:"count,omitempty"`
	Of    int `json:"of,omitempty"`
	// Enemies and Teammates are whom a flashbang blinded, or how many
	// enemies a grenade hurt.
	Enemies   int `json:"enemies,omitempty"`
	Teammates int `json:"teammates,omitempty"`
	// Grenades is what the player carried as he died, GrenadeValue its
	// worth in dollars of the game.
	Grenades     []string `json:"grenades,omitempty"`
	GrenadeValue int      `json:"grenade_value,omitempty"`
	// Detail tells the moment more exactly: for a missed trade "no_shot",
	// "missed" or "hit"; for grenade damage "he" or "fire".
	Detail string `json:"detail,omitempty"`
}

// The details of Evidence.
const (
	DetailNoShot = "no_shot" // he saw the killer and did not shoot
	DetailMissed = "missed"  // he shot at the killer and did not hit
	DetailHit    = "hit"     // he hit the killer and did not kill
	DetailHE     = "he"
	DetailFire   = "fire"
)

// ClockSeconds returns the whole seconds a round clock shows for clock
// seconds left: the game rounds up, so with 94.58 s left it shows 1:35.
// Checked in CS2, see docs/DECISIONS.md, 2026-11-12.
func ClockSeconds(clock float64) int { return int(math.Ceil(clock)) }

// History is what the past matches of a player add up to: what his own
// rate of a measure is taken from, see docs/FINDINGS.md, "своё среднее".
// Which matches count, the last MaxHistory before this one, is up to the
// caller.
type History struct {
	Matches int
	// Tallies is the events and the opportunities of each measure, by its
	// name, summed over the matches.
	Tallies map[string]Tally
}

// Tally is the events and the opportunities of a measure.
type Tally struct{ X, N float64 }

// Add adds a match of the player, the report of it, to the history.
func (h *History) Add(r Report) {
	if h.Tallies == nil {
		h.Tallies = make(map[string]Tally)
	}
	h.Matches++
	for _, c := range r.Comparisons {
		t := h.Tallies[c.Measure]
		h.Tallies[c.Measure] = Tally{X: t.X + c.X, N: t.N + c.N}
	}
}

// Analyze returns the findings of every player of the match of a, in the
// order of a.Players. history is the past of the players by SteamID64; a
// player without one has no habits.
func Analyze(a *metrics.Analysis, history map[uint64]History) []Report {
	d := collect(a)
	out := make([]Report, len(a.Players))
	for i, p := range a.Players {
		out[i] = d.report(i, history[p.SteamID64])
	}
	return out
}

// report makes the report of player i.
func (d *data) report(i int, h History) Report {
	r := Report{Comparisons: make([]Comparison, 0, len(measures)), Findings: []Finding{}, Mistakes: []Pick{}}
	for _, m := range measures {
		r.Comparisons = append(r.Comparisons, d.compare(m, i, h))
	}
	for _, rl := range rules {
		if f, ok := d.judge(rl, i, h); ok {
			r.Findings = append(r.Findings, f)
		}
	}
	// The heaviest first; the rules of one weight stay in their order.
	slices.SortStableFunc(r.Findings, func(a, b Finding) int { return cmp.Compare(b.Weight, a.Weight) })
	r.Mistakes, r.Strength = pick(r.Findings)
	return r
}

// model is how the events of a measure are expected.
type model int

const (
	// share: events of opportunities. The lobby gives the share p, the
	// player is expected to have n·p events, give or take √(n·p·(1−p)).
	share model = iota
	// perRound: events a round. The lobby gives the rate, the player is
	// expected to have his rounds by the rate, give or take the root of it.
	perRound
	// perMatch: a count a match, like the deaths at the worst spot. The
	// lobby gives the mean, give or take its root.
	perMatch
)

// measure is something counted for every player: the events and the
// opportunities of a rule, or of the scoreboard.
type measure struct {
	id    string
	model model
	// good means more is better: a strength, the scoreboard.
	good bool
}

// rate returns the rate of the base for the model, never 0 or 1: for
// events of opportunities and events a round one event and two
// opportunities are added, as docs/FINDINGS.md tells; the mean of a count a
// match is that of at least one event.
func (m measure) rate(b Base) float64 {
	if m.model == perMatch {
		if b.N <= 0 {
			return 0
		}
		return max(b.X, 1) / b.N
	}
	return (b.X + 1) / (b.N + 2)
}

// spread returns how much chance alone moves the events of a player with n
// opportunities around the expected: one standard deviation.
func (m measure) spread(n, rate float64) float64 {
	if m.model == share {
		return math.Sqrt(n * rate * (1 - rate))
	}
	return math.Sqrt(n * rate)
}

// tally is a measure of one player.
type tally struct {
	x, n float64
	// sample is what the minimal sample of the rule is checked against.
	sample float64
	// skip means the rule of the measure cannot fire, whatever the numbers:
	// the player has no death spot.
	skip bool
	// ruleX is the events the rule counts when they are not x: for the
	// death spot x is the worst place of all and ruleX the worst of those
	// that are spots. Used when hasRuleX.
	ruleX    float64
	hasRuleX bool
	variant  string
	place    string
	side     demo.Side
	events   []event
}

// event is an event of a tally with what it weighs.
type event struct {
	// count is how many events of the measure it stands for: the shots on
	// the move of a round are one event of many.
	count float64
	// impact is what one of them weighs for a mistake.
	impact float64
	// score orders the events of a strength, which all weigh one: the kills
	// of a round, the enemies blinded.
	score float64
	ev    Evidence
}

// meanImpact returns what an event of the tally weighs on average.
func (t tally) meanImpact() float64 {
	var sum, n float64
	for _, e := range t.events {
		sum += e.count * e.impact
		n += e.count
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// judge tells whether the rule fires for player i, and the finding if it
// does: of the measures of the rule the heaviest one that fires.
func (d *data) judge(rl rule, i int, h History) (Finding, bool) {
	var best Finding
	fired := false
	for _, id := range rl.measures {
		m := measureOf[id]
		t := d.tallies[id][i]
		if t.skip || t.sample < rl.MinSample {
			continue
		}
		x := t.x
		if t.hasRuleX {
			x = t.ruleX
		}
		rate := m.rate(d.base(id, func(j int) bool { return j != i }))
		expected := t.n * rate
		spread := m.spread(t.n, rate)
		excess := x - expected
		if spread <= 0 || excess < MinExcess || excess/spread < MinZ {
			continue
		}
		f := Finding{
			Rule: rl.ID, Kind: rl.Kind, Group: rl.Group, Variant: t.variant,
			X: x, N: t.n, Sample: t.sample,
			BaseRate: rate, Expected: expected, Excess: excess, Z: excess / spread,
			Impact: 1, Place: t.place, Side: t.side,
		}
		if rl.Kind == Mistake {
			f.Impact = t.meanImpact()
		}
		f.Weight = f.Z * f.Impact
		if habit(rl, m, id, h, rate) {
			f.Habit = true
			f.Weight *= HabitFactor
		}
		if f.Weight <= 0 || fired && f.Weight <= best.Weight {
			continue
		}
		f.Evidence = evidence(t.events, rl.Kind, d.a.TickRate)
		best, fired = f, true
	}
	return best, fired
}

// habit tells whether the past matches of the player show what the rule
// found in this one: there are MinHistory of them, they make the minimal
// sample of the rule together, and in them too he has more events than the
// rate of the lobby of this match gives, by MinZ spreads of chance at their
// sample or more. Without the spread a rate a hair above the lobby's would
// make a habit of every finding.
func habit(rl rule, m measure, id string, h History, rate float64) bool {
	own := h.Tallies[id]
	if h.Matches < MinHistory || own.N <= 0 {
		return false
	}
	// The minimal sample is in opportunities: it bounds the measures that
	// count events of opportunities, not those that count by the round.
	if m.model == share && own.N < rl.MinSample {
		return false
	}
	spread := m.spread(own.N, rate)
	return spread > 0 && (own.X-own.N*rate)/spread >= MinZ
}

// evidence returns the moments of the events that show a finding best:
// MaxEvidence at most, the heaviest of a mistake and the biggest of a
// strength, in the order of the match.
func evidence(events []event, kind Kind, tickRate float64) []Evidence {
	weight := func(e event) float64 {
		if kind == Mistake {
			return e.count * e.impact
		}
		return e.score
	}
	// An event that weighs nothing, an excused death, shows no mistake.
	sorted := slices.DeleteFunc(slices.Clone(events), func(e event) bool { return kind == Mistake && e.impact <= 0 })
	slices.SortStableFunc(sorted, func(a, b event) int { return cmp.Compare(weight(b), weight(a)) })
	if len(sorted) > MaxEvidence {
		sorted = sorted[:MaxEvidence]
	}
	slices.SortStableFunc(sorted, func(a, b event) int {
		return cmp.Or(cmp.Compare(a.ev.Round, b.ev.Round), cmp.Compare(a.ev.Tick, b.ev.Tick))
	})
	lead := int(math.Round(GotoLead.Seconds() * tickRate))
	out := make([]Evidence, 0, len(sorted))
	for _, e := range sorted {
		ev := e.ev
		ev.Impact = e.impact
		ev.Goto = max(ev.Tick-lead, 0)
		out = append(out, ev)
	}
	return out
}

// compare puts the measure of player i next to the lobby, his teammates,
// his opponents and his own past.
func (d *data) compare(m measure, i int, h History) Comparison {
	ts := d.tallies[m.id]
	team := d.a.Players[i].Team
	c := Comparison{
		Measure:   m.id,
		X:         ts[i].x,
		N:         ts[i].n,
		Rate:      ratio(ts[i].x, ts[i].n),
		Lobby:     d.base(m.id, func(j int) bool { return j != i }),
		Teammates: d.base(m.id, func(j int) bool { return j != i && d.a.Players[j].Team == team }),
		Opponents: d.base(m.id, func(j int) bool { return d.a.Players[j].Team != team }),
	}
	// The place among those who have opportunities: one plus how many of
	// them are strictly better.
	better := 0
	for j, t := range ts {
		if t.n <= 0 {
			continue
		}
		c.Of++
		theirs := ratio(t.x, t.n)
		if j != i && (m.good && theirs > c.Rate || !m.good && theirs < c.Rate) {
			better++
		}
	}
	if ts[i].n > 0 {
		c.Rank = better + 1
	}
	if own, ok := h.Tallies[m.id]; ok && h.Matches >= MinHistory {
		c.Own = &Base{X: own.X, N: own.N, Rate: ratio(own.X, own.N)}
	}
	return c
}

// base returns the measure of the players that in tells of, together: the
// events of them all and the opportunities of them all.
func (d *data) base(id string, in func(j int) bool) Base {
	var b Base
	for j, t := range d.tallies[id] {
		if in(j) {
			b.X += t.x
			b.N += t.n
		}
	}
	b.Rate = ratio(b.X, b.N)
	return b
}

func ratio(x, n float64) float64 {
	if n == 0 {
		return 0
	}
	return x / n
}
