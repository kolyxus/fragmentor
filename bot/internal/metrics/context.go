package metrics

import (
	"math"
	"slices"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// The context of the events of docs/METRICS.md, "Ситуации": the round clock,
// who was alive, how far the nearest teammate stood, how each team bought,
// and what came of the deaths, the flashbangs and the chances to trade. The
// findings of internal/findings judge these; here they are only measured.

// LoneDistance is how far, in units of the game, the nearest live teammate
// of a player must stand for an untraded death to be alone: see
// docs/METRICS.md for how it was chosen.
const LoneDistance = 1100

// NearDistance is how close, in units, a live enemy must stand to a player
// to be near him as he dies.
const NearDistance = 1000

// The buy of a team in a round, by what its players alive at the end of the
// freeze time carried on average, as cs-demo-analyzer counts it: up to
// EcoValue an eco, from FullValueT (FullValueCT on CT) a full buy, a force
// buy in between. The first round of each half of the regulation time is a
// pistol round, whatever the team carried; the rounds of overtime are not.
const (
	EcoValue    = 1000
	FullValueT  = 4000
	FullValueCT = 4500
)

// Buy is how a team bought for a round.
type Buy string

// The buys.
const (
	BuyPistol Buy = "pistol"
	BuyEco    Buy = "eco"
	BuyForce  Buy = "force"
	BuyFull   Buy = "full"
)

// defaultRoundTime and defaultBombTime stand in for the round time and the
// bomb timer of a match that tells none: 1:55 and 40 s, as on FACEIT.
const (
	defaultRoundTime = 115
	defaultBombTime  = 40
)

// Analysis is the metrics of the players of a match, as Compute returns
// them, with the events they are counted from, each in its context. The
// events are of the players of the match: a bot that a player controls is
// that player, bots no player controls are left out.
type Analysis struct {
	Players []Player
	// Rounds is the context of each round, in the order of the match.
	Rounds []RoundInfo
	// Deaths is the deaths of players by enemies, in the order of the
	// demo: a teamkill, the world, a suicide and leaving the server are no
	// such deaths.
	Deaths []Death
	// Flashbangs is the flashbangs the players threw, those after the end
	// of the round aside.
	Flashbangs []Flashbang
	// Chances is every chance a player had to trade a teammate.
	Chances []TradeChance
	// Clutches is every clutch, see Stats.Clutches.
	Clutches []ClutchPlay
}

// RoundInfo is the context of a round.
type RoundInfo struct {
	Number int
	Winner int // the team that won, index in demo.Match.Teams
	// Side, Buy and Equipment are of each team: the side it played, how it
	// bought, and what its players alive at the end of the freeze time
	// carried together. Buy is "" if the demo does not tell.
	Side      [2]demo.Side
	Buy       [2]Buy
	Equipment [2]int
	// Kills is the kills of each player in the round, as FACEIT counts
	// them, by SteamID64.
	Kills map[uint64]int
}

// Death is a death of a player by an enemy, in its context.
type Death struct {
	Kill          int // index in demo.Match.Kills
	Round         int
	Tick          int
	AfterRoundEnd bool
	Victim        uint64 // SteamID64 of the player
	Team          int    // of the victim
	Side          demo.Side
	// Killer is the SteamID64 of the player who killed, 0 for a bot no
	// player controls.
	Killer uint64
	// Elapsed is the time since the freeze time ended, in seconds. Clock is
	// what the round clock showed: the time left of the round, or of the
	// bomb once it was planted (Planted). 0 after the time is out.
	Elapsed float64
	Clock   float64
	Planted bool
	// Won means the victim's team won the round. Buy and EnemyBuy are the
	// buys of the victim's team and of the killer's.
	Won      bool
	Buy      Buy
	EnemyBuy Buy
	// Opening means the death was the first duel of the round. Traded
	// means a teammate killed the killer within TradeWindow; TradeKill that
	// the kill itself traded a teammate of the killer.
	Opening   bool
	Traded    bool
	TradeKill bool
	// Excused means the victim held a pistol in an eco of the team and the
	// killer a rifle or a sniper rifle.
	Excused bool
	// AliveOwn and AliveEnemy are how many bodies were alive on the
	// victim's side, the victim among them, and on the enemy's right before
	// the death; Advantage means AliveOwn was more.
	AliveOwn   int
	AliveEnemy int
	Advantage  bool
	// TeammateDistance is how far the nearest live teammate stood, in
	// units, -1 if none was alive or the demo does not tell. Lone means the
	// death was untraded and every live teammate farther than LoneDistance;
	// a player last alive is never alone.
	TeammateDistance float64
	Lone             bool
	// EnemiesNear is how many live enemies stood within NearDistance of the
	// victim, the killer among them.
	EnemiesNear int
	// DamageBefore is the damage the victim dealt to enemies in the round
	// up to the death; Dry means none and an untraded death.
	DamageBefore int
	Dry          bool
	// Where the two stood, and their weapons.
	VictimPlace  string
	KillerPlace  string
	VictimWeapon string
	VictimClass  demo.WeaponClass
	Weapon       string // of the kill
	Class        demo.WeaponClass
	// Grenades is what the victim carried, GrenadeValue its worth.
	Grenades     []string
	GrenadeValue int
	Headshot     bool
	ThroughSmoke bool
	Wallbang     bool
	NoScope      bool
	KillerBlind  bool
}

// Flashbang is a flashbang a player threw and whom it blinded for MinFlash
// or longer: Empty means no enemy, TeamFlash at least one teammate.
type Flashbang struct {
	Grenade   int // ID in demo.Match.Grenades
	Round     int
	Tick      int
	Thrower   uint64
	Enemies   int
	Teammates int
	Empty     bool
	TeamFlash bool
}

// TradeChance is a chance of a player to trade a teammate: the teammate
// died by an enemy before the end of the round, the player was alive, saw
// the killer within TradeWindow after the death and did not exchange damage
// with another enemy in it.
type TradeChance struct {
	Death  int    // index in Analysis.Deaths of the teammate's death
	Player uint64 // SteamID64 of the player who had the chance
	// Missed means nobody traded the death; Traded that the player killed
	// the killer within the window. Shot means the player fired at the
	// killer while seeing him in the window, Hit that he hit him in it.
	Missed bool
	Traded bool
	Shot   bool
	Hit    bool
}

// ClutchPlay is a clutch of a player, see Stats.Clutches.
type ClutchPlay struct {
	Round   int
	Player  uint64
	Enemies int // the X of 1vX
	Won     bool
}

// Analyze returns the metrics of the players of m, as Compute does, with
// the events they are counted from in their context.
func Analyze(m *demo.Match) *Analysis {
	c := newCounter(m)
	var e roundEvents
	for _, r := range m.Rounds {
		c.round(r, e.of(m, r.Number))
	}
	a := c.annotate(m)
	a.Players = c.result()
	a.Clutches = c.clutchLog
	return a
}

// matchClock is what the clock of a round needs.
type matchClock struct {
	tickRate, roundTime, bombTime float64
}

func (mc matchClock) of(r demo.Round, tick int) (elapsed, clock float64, planted bool) {
	elapsed = float64(tick-r.FreezeEndTick) / mc.tickRate
	clock = mc.roundTime - elapsed
	if r.BombPlantTick > 0 && tick >= r.BombPlantTick {
		planted = true
		clock = mc.bombTime - float64(tick-r.BombPlantTick)/mc.tickRate
	}
	return max(elapsed, 0), max(clock, 0), planted
}

// buys returns how each team bought for the round r of m.
func buys(m *demo.Match, r demo.Round) (buy [2]Buy, equipment [2]int) {
	maxRounds := m.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 24
	}
	var alive [2]int
	for _, l := range r.Equipment {
		for team := range 2 {
			if l.Body.Side == r.Side(team) {
				equipment[team] += l.Value
				alive[team]++
			}
		}
	}
	for team := range 2 {
		switch {
		case r.Number == 1 || r.Number == maxRounds/2+1:
			buy[team] = BuyPistol
		case alive[team] == 0:
		case equipment[team] <= EcoValue*alive[team]:
			buy[team] = BuyEco
		case r.Side(team) == demo.T && equipment[team] >= FullValueT*alive[team],
			r.Side(team) == demo.CT && equipment[team] >= FullValueCT*alive[team]:
			buy[team] = BuyFull
		default:
			buy[team] = BuyForce
		}
	}
	return buy, equipment
}

// indexByRound returns the indices of the events of list by their round.
func indexByRound[E any](list []E, round func(E) int) map[int][]int {
	out := make(map[int][]int)
	for i, e := range list {
		out[round(e)] = append(out[round(e)], i)
	}
	return out
}

// annotate puts the events of m in their context, and counts the metrics
// of the situations, the grenades thrown and the flashbangs.
func (c *counter) annotate(m *demo.Match) *Analysis {
	a := &Analysis{}
	mc := matchClock{tickRate: c.tickRate, roundTime: m.RoundTime, bombTime: m.BombTime}
	if mc.roundTime <= 0 {
		mc.roundTime = defaultRoundTime
	}
	if mc.bombTime <= 0 {
		mc.bombTime = defaultBombTime
	}
	kills := indexByRound(m.Kills, func(k demo.Kill) int { return k.Round })
	hits := indexByRound(m.Hits, func(h demo.Hit) int { return h.Round })
	shots := indexByRound(m.Shots, func(s demo.Shot) int { return s.Round })
	sightings := indexByRound(m.Sightings, func(s demo.Sighting) int { return s.Round })
	rounds := make(map[int]demo.Round, len(m.Rounds))
	for _, r := range m.Rounds {
		rounds[r.Number] = r
		buy, equipment := buys(m, r)
		a.Rounds = append(a.Rounds, RoundInfo{
			Number: r.Number, Winner: r.Winner,
			Side: [2]demo.Side{r.Side(0), r.Side(1)}, Buy: buy, Equipment: equipment,
			Kills: c.roundKills[r.Number],
		})
		info := a.Rounds[len(a.Rounds)-1]
		re := roundIndex{m: m, kills: kills[r.Number], hits: hits[r.Number], shots: shots[r.Number], sightings: sightings[r.Number]}
		c.deaths(a, r, info, re, mc)
	}
	c.grenadesThrown(a, m, rounds)
	return a
}

// roundIndex is the events of a round, as indices in the lists of m.
type roundIndex struct {
	m                             *demo.Match
	kills, hits, shots, sightings []int
}

// deaths annotates the deaths of the round r and the chances to trade them.
func (c *counter) deaths(a *Analysis, r demo.Round, info RoundInfo, re roundIndex, mc matchClock) {
	m := re.m
	roundKills := make([]demo.Kill, len(re.kills))
	for i, k := range re.kills {
		roundKills[i] = m.Kills[k]
	}
	side := func(p *player) *Stats { return p.on(r.Side(p.team)) }
	alive := map[demo.Side]map[body]bool{demo.T: {}, demo.CT: {}}
	for _, b := range r.Alive {
		if s, ok := alive[b.Side]; ok {
			s[bodyOf(b)] = true
		}
	}
	// The damage each player dealt to enemies so far in the round.
	damage := make(map[uint64]int)
	hit := 0
	opening := true
	for i, k := range roundKills {
		for ; hit < len(re.hits) && m.Hits[re.hits[hit]].Tick <= k.Tick; hit++ {
			h := m.Hits[re.hits[hit]]
			if p := c.player(h.Attacker); p != nil && enemies(h.Attacker, &h.Victim) {
				damage[p.id] += h.Damage
			}
		}
		started := k.Tick >= r.FreezeEndTick
		var own, enemy int
		if s, ok := alive[k.Victim.Side]; ok && started {
			own, enemy = len(s), len(alive[opposite(k.Victim.Side)])
		}
		victimBody := bodyOf(k.Victim)
		if started {
			delete(alive[k.Victim.Side], victimBody)
		}
		if !duel(k) || computerBot(k.Victim) {
			continue
		}
		first := opening
		opening = false
		v := c.coached(coach(k.Victim))
		if v == nil {
			continue
		}
		d := Death{
			Kill: re.kills[i], Round: r.Number, Tick: k.Tick, AfterRoundEnd: k.AfterRoundEnd,
			Victim: v.id, Team: v.team, Side: k.Victim.Side,
			Won:      r.Winner == v.team,
			Buy:      info.Buy[v.team],
			EnemyBuy: info.Buy[1-v.team],
			Opening:  first, Traded: c.tradedDeath(roundKills, i), TradeKill: c.tradeKill(roundKills, i),
			AliveOwn: own, AliveEnemy: enemy, Advantage: own > enemy,
			DamageBefore: damage[v.id],
			VictimPlace:  k.VictimPlace, KillerPlace: k.KillerPlace,
			VictimWeapon: k.VictimWeapon, VictimClass: k.VictimClass, Weapon: k.Weapon, Class: k.Class,
			Grenades: k.Grenades, Headshot: k.Headshot,
			ThroughSmoke: k.ThroughSmoke, Wallbang: k.Wallbang, NoScope: k.NoScope, KillerBlind: k.KillerBlind,
		}
		if killer := c.coached(coach(*k.Killer)); killer != nil {
			d.Killer = killer.id
		}
		d.Elapsed, d.Clock, d.Planted = mc.of(r, k.Tick)
		for _, g := range k.Grenades {
			d.GrenadeValue += demo.GrenadePrice(g)
		}
		d.Excused = d.Buy == BuyEco && d.VictimClass == demo.ClassPistol && (d.Class == demo.ClassRifle || d.Class == demo.ClassSniper)
		d.TeammateDistance, d.EnemiesNear = surroundings(k)
		d.Lone = !d.Traded && d.TeammateDistance > LoneDistance
		d.Dry = !d.Traded && d.DamageBefore == 0

		st := side(v)
		if d.Lone {
			st.LoneDeaths++
		}
		if d.Advantage {
			st.AdvantageDeaths++
		}
		if d.Dry {
			st.DryDeaths++
		}
		a.Deaths = append(a.Deaths, d)
		// The teammates alive as he died: their chances to trade him, in the
		// order of the players.
		if started {
			for _, p := range c.players {
				if p == v || !alive[k.Victim.Side][body{id: p.id}] {
					continue
				}
				side(p).TeammateDeaths++
				if !k.AfterRoundEnd {
					c.tradeChance(a, len(a.Deaths)-1, p, k, re, side)
				}
			}
		}
	}
}

// surroundings returns how far the nearest live teammate of the victim of
// k stood, -1 if none was alive or the demo does not tell, and how many
// live enemies stood within NearDistance of him.
func surroundings(k demo.Kill) (teammate float64, near int) {
	i := slices.IndexFunc(k.Positions, func(p demo.Position) bool { return bodyOf(p.Body) == bodyOf(k.Victim) })
	if i < 0 {
		return -1, 0
	}
	at := k.Positions[i]
	teammate = -1
	for _, p := range k.Positions {
		if bodyOf(p.Body) == bodyOf(k.Victim) {
			continue
		}
		d := math.Sqrt((p.X-at.X)*(p.X-at.X) + (p.Y-at.Y)*(p.Y-at.Y) + (p.Z-at.Z)*(p.Z-at.Z))
		switch {
		case p.Body.Side == k.Victim.Side:
			if teammate < 0 || d < teammate {
				teammate = d
			}
		case d <= NearDistance:
			near++
		}
	}
	return teammate, near
}

// tradeChance notes the chance of p to trade the death a.Deaths[death], the
// kill k, if he had one: he saw the killer within TradeWindow and exchanged
// no damage with another enemy in it.
func (c *counter) tradeChance(a *Analysis, death int, p *player, k demo.Kill, re roundIndex, side func(*player) *Stats) {
	m := re.m
	me := who{id: p.id}
	killer := coach(*k.Killer)
	from, to := k.Tick, k.Tick+c.window
	var seen []span
	for _, i := range re.sightings {
		s := m.Sightings[i]
		if coach(s.Spotter) == me && coach(s.Target) == killer && s.From <= to && s.To >= from {
			seen = append(seen, span{from: max(s.From, from), to: min(s.To, to)})
		}
	}
	if len(seen) == 0 {
		return
	}
	ch := TradeChance{Death: death, Player: p.id, Missed: !a.Deaths[death].Traded}
	for _, i := range re.hits {
		h := m.Hits[i]
		if h.Tick < from || h.Tick > to || h.Attacker == nil || !enemies(h.Attacker, &h.Victim) {
			continue
		}
		attacker, victim := who{id: h.Attacker.ID, bot: h.Attacker.Bot}, coach(h.Victim)
		switch {
		case attacker == me && victim == killer:
			ch.Hit = true
		case victim == me && attacker == killer:
			// The killer shoots at him: the same fight.
		case attacker == me || victim == me:
			return // in a fight with another enemy
		}
	}
	for _, i := range re.shots {
		s := m.Shots[i]
		if coach(s.Shooter) == me && slices.ContainsFunc(seen, func(sp span) bool { return sp.from <= s.Tick && s.Tick <= sp.to }) {
			ch.Shot = true
			break
		}
	}
	for _, i := range re.kills {
		r := m.Kills[i]
		if r.Killer != nil && duel(r) && coach(*r.Killer) == me && coach(r.Victim) == killer && r.Tick >= from && r.Tick <= to {
			ch.Traded = true
			break
		}
	}
	a.Chances = append(a.Chances, ch)
	st := side(p)
	st.TradeChances++
	if ch.Missed {
		st.MissedTrades++
	}
}

// grenadesThrown counts the grenades the players threw, those after the
// end of the round aside, and whom each flashbang blinded.
func (c *counter) grenadesThrown(a *Analysis, m *demo.Match, rounds map[int]demo.Round) {
	blinded := make(map[int][]demo.Flash)
	for _, f := range m.Flashes {
		if f.Grenade > 0 && f.Duration >= MinFlash.Seconds() {
			blinded[f.Grenade] = append(blinded[f.Grenade], f)
		}
	}
	for _, g := range m.Grenades {
		r, ok := rounds[g.Round]
		thrower := coach(g.Thrower)
		p := c.coached(thrower)
		if !ok || p == nil || g.AfterRoundEnd {
			continue
		}
		st := p.on(r.Side(p.team))
		switch g.Weapon {
		case demo.SmokeGrenade:
			st.SmokesThrown++
		case demo.HEGrenade:
			st.HEGrenadesThrown++
		case demo.Molotov, demo.Incendiary:
			st.FireGrenadesThrown++
		case demo.Decoy:
			st.DecoysThrown++
		case demo.Flashbang:
			st.FlashesThrown++
			fb := Flashbang{Grenade: g.ID, Round: g.Round, Tick: g.Tick, Thrower: p.id}
			for _, f := range blinded[g.ID] {
				switch {
				case coach(f.Victim) == thrower:
				case enemies(&g.Thrower, &f.Victim):
					fb.Enemies++
				case f.Victim.Side == g.Thrower.Side:
					fb.Teammates++
				}
			}
			fb.Empty, fb.TeamFlash = fb.Enemies == 0, fb.Teammates > 0
			if fb.Empty {
				st.EmptyFlashes++
			}
			if fb.TeamFlash {
				st.TeamFlashes++
			}
			if fb.Empty || fb.TeamFlash {
				st.BadFlashes++
			}
			a.Flashbangs = append(a.Flashbangs, fb)
		}
	}
}
