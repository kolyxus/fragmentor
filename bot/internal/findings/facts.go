package findings

import (
	"fmt"
	"math"
	"slices"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// FactsVersion is the version of the contract of Facts and of the rules
// they are made by, docs/FINDINGS.md. Stored facts carry it: bump it
// whenever a field changes its meaning or a rule, a threshold or a weight of
// the findings changes, and the facts of every match are made again from its
// journal.
const FactsVersion = 1

// Facts is what a review of a match is made of for one player: the contract
// that the template of internal/review and the text of step 8в read. It holds
// no names and no IDs, only the numbers of the match and of the player, the
// comparisons, the findings with their evidence, what a review tells, and
// the player's events in their context. Coordinates of other players are no
// part of it: only what is made of them, like the distance to a teammate.
type Facts struct {
	Version     int          `json:"version"`
	Match       MatchFacts   `json:"match"`
	Player      PlayerFacts  `json:"player"`
	Comparisons []Comparison `json:"comparisons"`
	// Findings is the findings of the player, the heaviest first, with IDs
	// f1, f2… in this order: the text of a review refers to them.
	Findings []FactFinding `json:"findings"`
	Review   ReviewFacts   `json:"review"`
	Events   EventFacts    `json:"events"`
}

// MatchFacts is the match as the player saw it.
type MatchFacts struct {
	Map    string `json:"map"` // like "de_mirage"
	Rounds int    `json:"rounds"`
	// Score is the rounds won by the player's team, then by the other one;
	// Result is ResultWin, ResultLoss or ResultDraw for the player.
	Score  [2]int `json:"score"`
	Result string `json:"result"`
	// StartSide is the side the player's team started on.
	StartSide demo.Side `json:"start_side"`
	TickRate  float64   `json:"tick_rate"`
	// Suspicious means the summary of the demo may be wrong, see demo.Match.
	Suspicious bool `json:"suspicious,omitempty"`
}

// The results of a match.
const (
	ResultWin  = "win"
	ResultLoss = "loss"
	ResultDraw = "draw"
)

// PlayerFacts is the scoreboard of the player in the match.
type PlayerFacts struct {
	// Level is the FACEIT level of the player in the match, 0 if unknown.
	Level   int `json:"level,omitempty"`
	Kills   int `json:"kills"`
	Deaths  int `json:"deaths"`
	Assists int `json:"assists"`
	// ADR is the damage a round; KAST and HeadshotKills are rates from 0 to
	// 1, as in metrics.Stats.
	ADR           float64 `json:"adr"`
	KAST          float64 `json:"kast"`
	HeadshotKills float64 `json:"headshot_kill_rate"`
	RoundsPlayed  int     `json:"rounds_played"`
}

// FactFinding is a finding of the facts: its ID, and the drills against it
// for a mistake, the IDs of the drills of bot/content that fit the match.
type FactFinding struct {
	ID string `json:"id"`
	Finding
	Drills []string `json:"drills,omitempty"`
}

// ReviewFacts is what a review tells: the mistakes, the strength and the
// task of the week. They refer to the findings by ID.
type ReviewFacts struct {
	Mistakes []PickFacts `json:"mistakes"`
	Strength *PickFacts  `json:"strength,omitempty"`
	Task     *TaskFacts  `json:"task,omitempty"`
}

// PickFacts is a finding picked for the review, and the findings nested with
// it whose numbers its text names, see Pick.
type PickFacts struct {
	Finding string   `json:"finding"`
	With    []string `json:"with,omitempty"`
}

// TaskFacts is the task of the week: a drill against a mistake of the review.
type TaskFacts struct {
	Drill   string `json:"drill"`
	Finding string `json:"finding"`
}

// EventFacts is the events of the player in their context.
type EventFacts struct {
	Kills         []KillFact    `json:"kills"`
	Deaths        []DeathFact   `json:"deaths"`
	Grenades      []GrenadeFact `json:"grenades"`
	GrenadeDamage []DamageFact  `json:"grenade_damage"`
}

// Moment is when something happened: the round, the round clock as in
// Evidence, the tick of the journal, and how the round went for the player's
// team.
type Moment struct {
	Round   int         `json:"round"`
	Clock   float64     `json:"clock"`
	Planted bool        `json:"planted,omitempty"`
	Tick    int         `json:"tick"`
	Won     bool        `json:"won"`
	Buy     metrics.Buy `json:"buy,omitempty"`
}

// KillFact is a kill of an enemy by the player.
type KillFact struct {
	Moment
	// Place is where the player stood, VictimPlace where the enemy did, the
	// place names of the game.
	Place       string `json:"place,omitempty"`
	VictimPlace string `json:"victim_place,omitempty"`
	Weapon      string `json:"weapon"`
	// EnemyBuy is how the enemy team bought.
	EnemyBuy     metrics.Buy `json:"enemy_buy,omitempty"`
	Headshot     bool        `json:"headshot,omitempty"`
	Opening      bool        `json:"opening,omitempty"`
	TradeKill    bool        `json:"trade_kill,omitempty"`
	ThroughSmoke bool        `json:"through_smoke,omitempty"`
	Wallbang     bool        `json:"wallbang,omitempty"`
	NoScope      bool        `json:"no_scope,omitempty"`
	Blind        bool        `json:"blind,omitempty"` // the player was blinded
}

// DeathFact is a death of the player by an enemy, see metrics.Death.
type DeathFact struct {
	Moment
	Place       string `json:"place,omitempty"`
	KillerPlace string `json:"killer_place,omitempty"`
	// Weapon is what killed him, VictimWeapon what he held.
	Weapon       string      `json:"weapon"`
	VictimWeapon string      `json:"victim_weapon,omitempty"`
	EnemyBuy     metrics.Buy `json:"enemy_buy,omitempty"`
	Opening      bool        `json:"opening,omitempty"`
	Traded       bool        `json:"traded"`
	Lone         bool        `json:"lone,omitempty"`
	Dry          bool        `json:"dry,omitempty"`
	Advantage    bool        `json:"advantage,omitempty"`
	Excused      bool        `json:"excused,omitempty"`
	// TeammateMeters is how far the nearest live teammate stood, 0 if none
	// was alive; Alive and AliveEnemy are how many were alive on the two
	// sides right before, the player among them; EnemiesNear how many enemies
	// stood within metrics.NearDistance.
	TeammateMeters float64 `json:"teammate_m,omitempty"`
	Alive          int     `json:"alive"`
	AliveEnemy     int     `json:"alive_enemy"`
	EnemiesNear    int     `json:"enemies_near"`
	DamageBefore   int     `json:"damage_before"`
	// Grenades is what he carried, GrenadeValue its worth in dollars.
	Grenades      []string `json:"grenades,omitempty"`
	GrenadeValue  int      `json:"grenade_value,omitempty"`
	Headshot      bool     `json:"headshot,omitempty"`
	ThroughSmoke  bool     `json:"through_smoke,omitempty"`
	Wallbang      bool     `json:"wallbang,omitempty"`
	NoScope       bool     `json:"no_scope,omitempty"`
	KillerBlind   bool     `json:"killer_blind,omitempty"`
	AfterRoundEnd bool     `json:"after_round_end,omitempty"`
}

// GrenadeFact is a grenade the player threw; a flashbang tells whom it
// blinded for metrics.MinFlash or longer.
type GrenadeFact struct {
	Moment
	Weapon    string `json:"weapon"` // demo.Flashbang, demo.SmokeGrenade and the like
	Enemies   int    `json:"enemies,omitempty"`
	Teammates int    `json:"teammates,omitempty"`
}

// DamageFact is the health that the HE grenades or the fire of the player
// took from enemies in a round, see metrics.GrenadeHit.
type DamageFact struct {
	Moment
	Fire    bool `json:"fire,omitempty"`
	Damage  int  `json:"damage"`
	Enemies int  `json:"enemies"`
}

// FactsInput is what the facts of a player are made of.
type FactsInput struct {
	Match    *demo.Match
	Analysis *metrics.Analysis
	// Player is the index of the player in Analysis.Players, and Report his
	// report, from Analyze.
	Player int
	Report Report
	// Level is the FACEIT level of the player in the match, 0 if unknown.
	Level int
	// Drills returns the IDs of the drills against a mistake that fit the
	// match, in the order a review gives them in; nil means none.
	Drills func(RuleID) []string
}

// BuildFacts makes the facts of a player of the match.
func BuildFacts(in FactsInput) Facts {
	a, m := in.Analysis, in.Match
	p := a.Players[in.Player]
	team := p.Team
	st := p.Match
	f := Facts{
		Version: FactsVersion,
		Match: MatchFacts{
			Map: m.Map, Rounds: len(m.Rounds), Score: [2]int{m.Teams[team].Score, m.Teams[1-team].Score},
			StartSide: m.Teams[team].StartSide, TickRate: a.TickRate, Suspicious: m.Suspicious,
		},
		Player: PlayerFacts{
			Level: in.Level, Kills: st.Kills, Deaths: st.Deaths, Assists: st.Assists,
			ADR: st.ADR, KAST: st.KAST, HeadshotKills: st.HeadshotKillRate, RoundsPlayed: st.RoundsPlayed,
		},
		Comparisons: in.Report.Comparisons,
		Findings:    make([]FactFinding, 0, len(in.Report.Findings)),
		Review:      ReviewFacts{Mistakes: []PickFacts{}},
		Events:      playerEvents(a, p),
	}
	switch s := f.Match.Score; {
	case s[0] > s[1]:
		f.Match.Result = ResultWin
	case s[0] < s[1]:
		f.Match.Result = ResultLoss
	default:
		f.Match.Result = ResultDraw
	}
	id := func(i int) string { return fmt.Sprintf("f%d", i+1) }
	for i, fd := range in.Report.Findings {
		ff := FactFinding{ID: id(i), Finding: fd}
		if fd.Kind == Mistake && in.Drills != nil {
			ff.Drills = in.Drills(fd.Rule)
		}
		f.Findings = append(f.Findings, ff)
	}
	pickOf := func(pk Pick) PickFacts {
		out := PickFacts{Finding: id(pk.Finding)}
		for _, j := range pk.With {
			out.With = append(out.With, id(j))
		}
		return out
	}
	for _, pk := range in.Report.Mistakes {
		f.Review.Mistakes = append(f.Review.Mistakes, pickOf(pk))
	}
	if s := in.Report.Strength; s != nil {
		pk := pickOf(*s)
		f.Review.Strength = &pk
	}
	// The task is against the heaviest mistake that has a drill: the drill
	// of the finding the review tells, or of one told with it.
	for _, pk := range in.Report.Mistakes {
		for _, j := range append([]int{pk.Finding}, pk.With...) {
			if drills := f.Findings[j].Drills; len(drills) > 0 {
				f.Review.Task = &TaskFacts{Drill: drills[0], Finding: id(pk.Finding)}
				break
			}
		}
		if f.Review.Task != nil {
			break
		}
	}
	return f
}

// playerEvents returns the kills, the deaths and the grenades of the player p of
// the match of a.
func playerEvents(a *metrics.Analysis, p metrics.Player) EventFacts {
	out := EventFacts{Kills: []KillFact{}, Deaths: []DeathFact{}, Grenades: []GrenadeFact{}, GrenadeDamage: []DamageFact{}}
	rounds := make(map[int]metrics.RoundInfo, len(a.Rounds))
	for _, r := range a.Rounds {
		rounds[r.Number] = r
	}
	// moment is a moment of the round for the team of p.
	moment := func(round int, clock float64, planted bool, tick int) Moment {
		r := rounds[round]
		return Moment{Round: round, Clock: clock, Planted: planted, Tick: tick, Won: r.Winner == p.Team, Buy: r.Buy[p.Team]}
	}
	for _, d := range a.Deaths {
		switch p.SteamID64 {
		case d.Killer:
			out.Kills = append(out.Kills, KillFact{
				Moment: Moment{Round: d.Round, Clock: d.Clock, Planted: d.Planted, Tick: d.Tick, Won: !d.Won, Buy: d.EnemyBuy},
				Place:  d.KillerPlace, VictimPlace: d.VictimPlace, Weapon: d.Weapon, EnemyBuy: d.Buy,
				Headshot: d.Headshot, Opening: d.Opening, TradeKill: d.TradeKill,
				ThroughSmoke: d.ThroughSmoke, Wallbang: d.Wallbang, NoScope: d.NoScope, Blind: d.KillerBlind,
			})
		case d.Victim:
			out.Deaths = append(out.Deaths, DeathFact{
				Moment: Moment{Round: d.Round, Clock: d.Clock, Planted: d.Planted, Tick: d.Tick, Won: d.Won, Buy: d.Buy},
				Place:  d.VictimPlace, KillerPlace: d.KillerPlace, Weapon: d.Weapon, VictimWeapon: d.VictimWeapon,
				EnemyBuy: d.EnemyBuy, Opening: d.Opening, Traded: d.Traded, Lone: d.Lone, Dry: d.Dry,
				Advantage: d.Advantage, Excused: d.Excused, TeammateMeters: round1(meters(d.TeammateDistance)),
				Alive: d.AliveOwn, AliveEnemy: d.AliveEnemy, EnemiesNear: d.EnemiesNear, DamageBefore: d.DamageBefore,
				Grenades: slices.Clone(d.Grenades), GrenadeValue: d.GrenadeValue, Headshot: d.Headshot,
				ThroughSmoke: d.ThroughSmoke, Wallbang: d.Wallbang, NoScope: d.NoScope, KillerBlind: d.KillerBlind,
				AfterRoundEnd: d.AfterRoundEnd,
			})
		}
	}
	blinded := make(map[int]metrics.Flashbang, len(a.Flashbangs))
	for _, fb := range a.Flashbangs {
		blinded[fb.Grenade] = fb
	}
	for _, th := range a.Throws {
		if th.Thrower != p.SteamID64 {
			continue
		}
		g := GrenadeFact{Moment: moment(th.Round, th.Clock, th.Planted, th.Tick), Weapon: th.Weapon}
		if fb, ok := blinded[th.Grenade]; ok {
			g.Enemies, g.Teammates = fb.Enemies, fb.Teammates
		}
		out.Grenades = append(out.Grenades, g)
	}
	for _, h := range a.GrenadeHits {
		if h.Player == p.SteamID64 {
			out.GrenadeDamage = append(out.GrenadeDamage, DamageFact{
				Moment: moment(h.Round, h.Clock, h.Planted, h.Tick), Fire: h.Fire, Damage: h.Damage, Enemies: h.Enemies,
			})
		}
	}
	return out
}

// GotoTick returns the tick for demo_gototick that shows a moment of the
// tick: GotoLead before it, or the start of the demo.
func GotoTick(tick int, tickRate float64) int {
	return max(tick-int(math.Round(GotoLead.Seconds()*tickRate)), 0)
}

// round1 rounds to a tenth: a distance in meters needs no more.
func round1(v float64) float64 { return math.Round(v*10) / 10 }

// AddFacts adds a past match of the player, his facts of it, to the history.
func (h *History) AddFacts(f Facts) { h.Add(Report{Comparisons: f.Comparisons}) }
