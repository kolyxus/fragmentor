package metrics

import (
	"math"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// The metrics are tested on made-up matches. Team 0 is players 1 to 3,
// team 1 players 4 to 6: teams of three are enough.

const testTickRate = 64

func teamOf(id uint64) int {
	if id <= 3 {
		return 0
	}
	return 1
}

// game is a made-up match.
type game struct{ m demo.Match }

func newGame() *game {
	g := &game{m: demo.Match{TickRate: testTickRate}}
	for id := uint64(1); id <= 6; id++ {
		g.m.Players = append(g.m.Players, demo.Player{SteamID64: id, Team: teamOf(id)})
	}
	return g
}

// round is a made-up round in which team 0 plays on side0 and winner wins.
// Unless alive is set, the six players start it alive.
type round struct {
	side0, winner demo.Side
	freezeEnd     int // the tick the freeze time ended at, 0 by default
	alive         []demo.Ref
	kills         []demo.Kill
	hits          []demo.Hit
	shots         []demo.Shot
	sightings     []demo.Sighting
	flashes       []demo.Flash
	grenades      []demo.Grenade
	equipment     []demo.Loadout
	plant         int // the tick the bomb was planted at, 0 if it was not
}

func newRound(side0, winner demo.Side) *round { return &round{side0: side0, winner: winner} }

// p returns player id with their side in the round.
func (r *round) p(id uint64) *demo.Ref {
	return &demo.Ref{ID: id, Side: r.sideOf(teamOf(id))}
}

// bot returns bot n of team, controlled by the player controller if not 0.
func (r *round) bot(n uint64, team int, controller uint64) *demo.Ref {
	return &demo.Ref{ID: n, Bot: true, Side: r.sideOf(team), Controller: controller}
}

func (r *round) sideOf(team int) demo.Side {
	if team == 0 {
		return r.side0
	}
	return opposite(r.side0)
}

type killOpt func(*demo.Kill)

func headshot(k *demo.Kill)            { k.Headshot = true }
func afterEnd(k *demo.Kill)            { k.AfterRoundEnd = true }
func class(c demo.WeaponClass) killOpt { return func(k *demo.Kill) { k.Class = c } }

// leave makes the kill the victim leaving the server.
func leave(k *demo.Kill) { k.Disconnect, k.Class = true, demo.ClassWorld }

func assist(a *demo.Ref, flash bool) killOpt {
	return func(k *demo.Kill) { k.Assister, k.FlashAssist = a, flash }
}

// kill adds a kill at tick; killer nil is the world.
func (r *round) kill(tick int, killer, victim *demo.Ref, opts ...killOpt) {
	k := demo.Kill{Tick: tick, Killer: killer, Victim: *victim, Class: demo.ClassRifle}
	for _, o := range opts {
		o(&k)
	}
	r.kills = append(r.kills, k)
}

func (r *round) hit(attacker, victim *demo.Ref, damage int, group demo.HitGroup, c demo.WeaponClass) {
	r.hits = append(r.hits, demo.Hit{Attacker: attacker, Victim: *victim, Damage: damage, HitGroup: group, Class: c})
}

// hitAt adds a hit in the chest at tick and returns it to change.
func (r *round) hitAt(tick int, attacker, victim *demo.Ref, damage int, c demo.WeaponClass) *demo.Hit {
	r.hits = append(r.hits, demo.Hit{Tick: tick, Attacker: attacker, Victim: *victim, Damage: damage, HitGroup: demo.HitChest, Class: c})
	return &r.hits[len(r.hits)-1]
}

// shot adds a shot at tick, standing still, and returns it to change.
func (r *round) shot(tick int, shooter *demo.Ref, c demo.WeaponClass) *demo.Shot {
	r.shots = append(r.shots, demo.Shot{Tick: tick, Shooter: *shooter, Class: c, MaxSpeed: 215})
	return &r.shots[len(r.shots)-1]
}

// sees adds a sighting of target by spotter from tick from to tick to.
func (r *round) sees(from, to int, spotter, target *demo.Ref) {
	r.sightings = append(r.sightings, demo.Sighting{Spotter: *spotter, Target: *target, From: from, To: to})
}

// flash adds a flash; thrower nil is unknown.
func (r *round) flash(thrower, victim *demo.Ref, seconds float64) {
	r.flashes = append(r.flashes, demo.Flash{Thrower: thrower, Victim: *victim, Duration: seconds})
}

// play adds the round to the match.
func (g *game) play(r *round) {
	n := len(g.m.Rounds) + 1
	winner := 0
	if r.winner != r.side0 {
		winner = 1
	}
	dr := demo.Round{Number: n, Winner: winner, WinnerSide: r.winner, FreezeEndTick: r.freezeEnd, Alive: r.alive,
		Equipment: r.equipment, BombPlantTick: r.plant}
	if dr.Alive == nil {
		for id := uint64(1); id <= 6; id++ {
			dr.Alive = append(dr.Alive, *r.p(id))
		}
	}
	g.m.Rounds = append(g.m.Rounds, dr)
	for _, k := range r.kills {
		k.Round = n
		g.m.Kills = append(g.m.Kills, k)
	}
	for _, h := range r.hits {
		h.Round = n
		g.m.Hits = append(g.m.Hits, h)
	}
	for _, s := range r.shots {
		s.Round = n
		g.m.Shots = append(g.m.Shots, s)
	}
	for _, s := range r.sightings {
		s.Round = n
		g.m.Sightings = append(g.m.Sightings, s)
	}
	for _, f := range r.flashes {
		f.Round = n
		g.m.Flashes = append(g.m.Flashes, f)
	}
	for _, gr := range r.grenades {
		gr.Round = n
		g.m.Grenades = append(g.m.Grenades, gr)
	}
}

// stats returns the stats of the player id on side.
func stats(ps map[uint64]Player, id uint64, side demo.Side) Stats {
	if side == demo.T {
		return ps[id].T
	}
	return ps[id].CT
}

// compute computes the metrics and checks that the sides add up to the
// match.
func (g *game) compute(t *testing.T) map[uint64]Player {
	t.Helper()
	out := make(map[uint64]Player)
	for _, p := range Compute(&g.m) {
		sum := p.T.plus(p.CT)
		sum.rates()
		if sum != p.Match {
			t.Errorf("player %d: T + CT = %+v, the match %+v", p.SteamID64, sum, p.Match)
		}
		out[p.SteamID64] = p
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestFACEITCounts(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.kill(10, r.p(1), r.p(4), headshot, assist(r.p(2), true))
	r.kill(20, r.p(5), r.p(2))
	r.kill(30, r.p(1), r.p(3))                                // a teamkill: a death, no kill
	r.kill(40, r.p(6), r.p(6), class(demo.ClassGrenade))      // a suicide
	r.kill(50, nil, r.p(5), class(demo.ClassWorld))           // a fall
	r.kill(60, r.p(1), r.bot(1, 1, 0), assist(r.p(4), false)) // an enemy bot is a kill; an assist on the own side is none
	r.hit(r.p(1), r.p(4), 50, demo.HitChest, demo.ClassRifle)
	r.hit(r.p(1), r.p(2), 30, demo.HitChest, demo.ClassRifle) // a teammate
	r.hit(r.p(1), r.bot(1, 1, 0), 40, demo.HitChest, demo.ClassRifle)
	r.hit(r.p(4), r.p(4), 10, demo.HitGeneric, demo.ClassGrenade) // own grenade
	r.hit(nil, r.p(1), 5, demo.HitGeneric, demo.ClassWorld)
	g.play(r)

	r = newRound(demo.T, demo.T)
	r.kill(10, r.p(2), r.p(4))
	// Player 4 took over a bot: its kills and damage are his, its death the bot's.
	r.kill(30, r.p(4), r.p(3))
	r.hit(r.p(1), r.bot(2, 1, 4), 100, demo.HitHead, demo.ClassRifle)
	r.kill(50, r.p(1), r.bot(2, 1, 4))
	r.kill(60, r.p(6), r.p(6), leave)
	g.play(r)
	ps := g.compute(t)

	for _, tc := range []struct {
		id                         uint64
		side                       demo.Side
		kills, deaths, assists, hs int
		damage                     int
	}{
		{1, demo.CT, 2, 0, 0, 1, 90},
		{1, demo.T, 1, 0, 0, 0, 100},
		{2, demo.CT, 0, 1, 1, 0, 0},
		{2, demo.T, 1, 0, 0, 0, 0},
		{3, demo.CT, 0, 1, 0, 0, 0},
		{3, demo.T, 0, 1, 0, 0, 0},
		{4, demo.T, 0, 1, 0, 0, 0},
		{4, demo.CT, 1, 1, 0, 0, 0},
		{5, demo.T, 1, 1, 0, 0, 0},
		{6, demo.T, 0, 1, 0, 0, 0},
		{6, demo.CT, 0, 1, 0, 0, 0},
	} {
		s := ps[tc.id].CT
		if tc.side == demo.T {
			s = ps[tc.id].T
		}
		if s.Kills != tc.kills || s.Deaths != tc.deaths || s.Assists != tc.assists || s.HeadshotKills != tc.hs || s.Damage != tc.damage {
			t.Errorf("player %d on %s: K/D/A %d/%d/%d HS %d damage %d, want %d/%d/%d HS %d damage %d", tc.id, tc.side,
				s.Kills, s.Deaths, s.Assists, s.HeadshotKills, s.Damage, tc.kills, tc.deaths, tc.assists, tc.hs, tc.damage)
		}
	}
	m := ps[1].Match
	if m.Rounds != 2 || !near(m.KD, 3) || !near(m.ADR, 95) || !near(m.HeadshotKillRate, 1.0/3) {
		t.Errorf("player 1: rounds %d, K/D %v, ADR %v, HS%% %v; want 2, 3 (no deaths), 95, 1/3", m.Rounds, m.KD, m.ADR, m.HeadshotKillRate)
	}
	if ct := ps[1].CT; ct.Rounds != 1 || !near(ct.KD, 2) || !near(ct.ADR, 90) || !near(ct.HeadshotKillRate, 0.5) {
		t.Errorf("player 1 on CT: %+v", ct)
	}
	if m := ps[4].Match; !near(m.KD, 0.5) {
		t.Errorf("player 4: K/D %v, want 1/2", m.KD)
	}
}

func TestKAST(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.T)
	r.kill(64, r.p(1), r.p(4), assist(r.p(2), true)) // 1: K; 2: A, the flash assist
	r.kill(192, r.p(5), r.p(1))                      // 5: K, trades 4: T
	r.kill(1920, r.p(5), r.p(2))
	// 3 survives: S. The bomb kills 5, who has a kill anyway, and 6, who
	// has nothing: dying after the end is dying.
	r.kill(2560, nil, r.p(5), afterEnd, class(demo.ClassBomb))
	r.kill(2560, nil, r.p(6), afterEnd, class(demo.ClassBomb))
	g.play(r)

	r = newRound(demo.T, demo.CT)
	r.alive = []demo.Ref{*r.p(1), *r.p(3), *r.p(4), *r.p(5), *r.p(6)} // 2 is away
	r.kill(320, r.p(1), r.p(1), leave)                                // 1 leaves alive: the round is not his
	r.kill(512, r.p(3), r.p(3), class(demo.ClassGrenade))             // 3 kills himself: dead
	r.kill(640, r.p(6), r.p(5))                                       // 5 killed by a teammate; 6 survives
	g.play(r)
	ps := g.compute(t)

	for _, tc := range []struct {
		id           uint64
		played, kast int
	}{
		{1, 1, 1}, {2, 1, 1}, {3, 2, 1}, {4, 2, 2}, {5, 2, 1}, {6, 2, 1},
	} {
		m := ps[tc.id].Match
		if m.RoundsPlayed != tc.played || m.KASTRounds != tc.kast || !near(m.KAST, float64(tc.kast)/float64(tc.played)) {
			t.Errorf("player %d: KAST %d of %d (%v), want %d of %d", tc.id, m.KASTRounds, m.RoundsPlayed, m.KAST, tc.kast, tc.played)
		}
	}
	if ct, tt := ps[1].CT, ps[1].T; ct.RoundsPlayed != 1 || ct.KAST != 1 || tt.RoundsPlayed != 0 || tt.KAST != 0 {
		t.Errorf("player 1: CT %d/%d, T %d/%d", ct.KASTRounds, ct.RoundsPlayed, tt.KASTRounds, tt.RoundsPlayed)
	}
}

func TestHeadshotHitRate(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.hit(r.p(1), r.p(4), 120, demo.HitHead, demo.ClassRifle)
	r.hit(r.p(1), r.p(4), 30, demo.HitNeck, demo.ClassRifle) // the neck is not the head
	r.hit(r.p(1), r.p(4), 25, demo.HitStomach, demo.ClassMachineGun)
	r.hit(r.p(1), r.p(5), 20, demo.HitChest, demo.ClassPistol)
	r.hit(r.p(1), r.p(6), 100, demo.HitHead, demo.ClassSniper)
	r.hit(r.p(1), r.bot(2, 1, 4), 60, demo.HitHead, demo.ClassSMG) // a bot under player 4: player 4
	// Not counted: a shotgun, a grenade, a knife, a teammate, a bot no player controls.
	r.hit(r.p(1), r.p(5), 40, demo.HitHead, demo.ClassShotgun)
	r.hit(r.p(1), r.p(6), 50, demo.HitGeneric, demo.ClassGrenade)
	r.hit(r.p(1), r.p(6), 40, demo.HitChest, demo.ClassKnife)
	r.hit(r.p(1), r.p(2), 100, demo.HitHead, demo.ClassRifle)
	r.hit(r.p(1), r.bot(1, 1, 0), 100, demo.HitHead, demo.ClassRifle)
	g.play(r)
	p := g.compute(t)[1].Match

	if p.BulletHits != 6 || p.HeadHits != 3 || !near(p.HeadshotHitRate, 0.5) {
		t.Errorf("bullet hits %d, head %d, rate %v; want 6, 3, 0.5", p.BulletHits, p.HeadHits, p.HeadshotHitRate)
	}
	if p.Damage != 120+30+25+20+100+60+40+50+40+100 {
		t.Errorf("damage %d: every hit on an enemy counts, bots and shotguns too", p.Damage)
	}
}

func TestOpening(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.T)
	// Not duels: a leave, a fall, a suicide, a teamkill, a kill of a bot no
	// player controls. A death by such a bot is.
	r.kill(10, r.p(6), r.p(6), leave)
	r.kill(20, nil, r.p(5), class(demo.ClassWorld))
	r.kill(30, r.p(4), r.p(4), class(demo.ClassGrenade))
	r.kill(40, r.p(1), r.p(2))
	r.kill(50, r.p(3), r.bot(1, 1, 0))
	r.kill(60, r.bot(2, 1, 0), r.p(1))
	r.kill(70, r.p(3), r.bot(2, 1, 0))
	g.play(r)

	r = newRound(demo.T, demo.CT)
	r.kill(10, r.p(4), r.p(1))
	r.kill(20, r.p(2), r.p(4))
	g.play(r)

	r = newRound(demo.T, demo.T)
	r.kill(10, r.p(1), r.bot(3, 1, 6)) // the bot under player 6 is player 6
	g.play(r)
	ps := g.compute(t)

	for _, tc := range []struct {
		id             uint64
		side           demo.Side
		attempts, wins int
	}{
		{1, demo.CT, 1, 0}, {1, demo.T, 2, 1},
		{4, demo.CT, 1, 1},
		{6, demo.CT, 1, 0},
		{2, demo.T, 0, 0}, {3, demo.CT, 0, 0},
	} {
		s := ps[tc.id].CT
		if tc.side == demo.T {
			s = ps[tc.id].T
		}
		if s.OpeningAttempts != tc.attempts || s.OpeningWins != tc.wins {
			t.Errorf("player %d on %s: opening duels %d, won %d; want %d, %d", tc.id, tc.side, s.OpeningAttempts, s.OpeningWins, tc.attempts, tc.wins)
		}
	}
}

func TestTrades(t *testing.T) {
	window := tradeTicks(testTickRate)
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.kill(0, r.p(4), r.p(1))
	r.kill(window, r.p(2), r.p(4))      // 5 s exactly: a trade
	r.kill(window+80, r.p(5), r.p(2))   // trades 4
	r.kill(2*window+81, r.p(3), r.p(5)) // 5 s and a tick: no trade
	g.play(r)

	r = newRound(demo.T, demo.T)
	// In the same tick a trade comes first or last alike.
	r.kill(100, r.p(2), r.p(4))
	r.kill(100, r.p(4), r.p(1))
	r.kill(200, r.p(5), r.p(6)) // a teamkill: neither traded nor not
	r.kill(300, r.p(5), r.p(3))
	r.kill(310, nil, r.p(5), class(demo.ClassWorld)) // the killer falls: no trade
	g.play(r)

	r = newRound(demo.T, demo.T)
	r.kill(100, r.p(4), r.p(1))
	r.kill(150, r.p(1), r.p(4)) // 1 in a bot avenges himself: no trade
	r.kill(1000, r.p(5), r.p(2))
	r.kill(1100, r.p(3), r.p(5), afterEnd) // after the end, still a trade
	g.play(r)
	ps := g.compute(t)

	for _, tc := range []struct {
		id                        uint64
		side                      demo.Side
		tradeKills, traded, untrd int
	}{
		{1, demo.CT, 0, 1, 0}, {1, demo.T, 0, 1, 1},
		{2, demo.CT, 1, 0, 1}, {2, demo.T, 1, 1, 0},
		{3, demo.CT, 0, 0, 0}, {3, demo.T, 1, 0, 1},
		{4, demo.T, 0, 1, 0}, {4, demo.CT, 0, 0, 2},
		{5, demo.T, 1, 0, 1}, {5, demo.CT, 0, 0, 1},
		{6, demo.CT, 0, 0, 0},
	} {
		s := ps[tc.id].CT
		if tc.side == demo.T {
			s = ps[tc.id].T
		}
		if s.TradeKills != tc.tradeKills || s.TradedDeaths != tc.traded || s.UntradedDeaths != tc.untrd {
			t.Errorf("player %d on %s: trade kills %d, traded %d, untraded %d; want %d, %d, %d",
				tc.id, tc.side, s.TradeKills, s.TradedDeaths, s.UntradedDeaths, tc.tradeKills, tc.traded, tc.untrd)
		}
	}
}

func TestClutches(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.T)
	r.kill(1, r.p(4), r.p(1))
	r.kill(2, r.p(4), r.p(2)) // 3: 1v3
	r.kill(3, r.p(3), r.p(4))
	r.kill(4, r.p(3), r.p(5)) // 6: 1v1, the second clutch of the round
	r.kill(5, r.p(6), r.p(3))
	g.play(r)

	r = newRound(demo.T, demo.T)
	r.kill(1, r.p(1), r.p(1), leave) // leaving counts down the alive too
	r.kill(2, r.p(4), r.p(2))        // 3: 1v3, won
	r.kill(10, r.p(3), r.p(4))
	r.kill(11, r.p(3), r.p(5)) // 6: 1v1, lost
	r.kill(12, r.p(3), r.p(6))
	g.play(r)

	r = newRound(demo.T, demo.CT)
	r.alive = []demo.Ref{*r.p(1), *r.p(2), *r.p(3), *r.bot(1, 0, 0), *r.p(4), *r.p(5), *r.p(6)}
	r.kill(1, r.p(4), r.p(1))
	r.kill(1, r.p(4), r.p(2))
	r.kill(2, r.p(5), r.p(3)) // the last alive is a bot: no clutch
	r.kill(3, r.bot(1, 0, 0), r.p(4))
	r.kill(4, r.p(5), r.bot(1, 0, 0))
	r.kill(5, r.p(5), r.p(6), afterEnd) // after the end: no clutch for 5
	g.play(r)

	r = newRound(demo.T, demo.CT)
	r.kill(1, r.p(4), r.p(1))
	r.kill(1, r.p(5), r.p(2)) // in one tick down to 3: 1v3, lost
	g.play(r)

	r = newRound(demo.T, demo.T)
	r.kill(1, r.p(1), r.p(4))
	// The bomb explodes after the end: 6 stays alone against three, but
	// the round is decided.
	r.kill(9, nil, r.p(5), afterEnd, class(demo.ClassBomb))
	g.play(r)
	ps := g.compute(t)

	none := [5]Clutch{}
	for _, tc := range []struct {
		id   uint64
		side demo.Side
		want [5]Clutch
	}{
		{3, demo.CT, [5]Clutch{2: {1, 0}}},
		{3, demo.T, [5]Clutch{2: {2, 1}}},
		{6, demo.T, [5]Clutch{0: {1, 1}}},
		{6, demo.CT, [5]Clutch{0: {1, 0}}},
		{5, demo.CT, none}, {1, demo.T, none}, {2, demo.T, none},
	} {
		s := ps[tc.id].CT
		if tc.side == demo.T {
			s = ps[tc.id].T
		}
		if s.Clutches != tc.want {
			t.Errorf("player %d on %s: clutches %+v, want %+v", tc.id, tc.side, s.Clutches, tc.want)
		}
	}
}

func TestMultiKills(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.kill(1, r.p(1), r.p(4))
	r.kill(2, r.p(1), r.p(5))
	r.kill(3, r.p(1), r.p(6))
	g.play(r)
	r = newRound(demo.T, demo.T)
	r.kill(1, r.p(1), r.p(4))
	r.kill(2, r.p(1), r.bot(1, 1, 0)) // a bot is a kill, as for K
	r.kill(3, r.p(1), r.p(2))         // a teamkill is not
	g.play(r)
	r = newRound(demo.T, demo.T)
	r.kill(1, r.p(1), r.p(4))
	r.kill(2, r.p(1), r.p(5))
	r.kill(3, r.p(1), r.p(6))
	r.kill(4, r.p(1), r.bot(1, 1, 0), afterEnd)
	g.play(r)
	r = newRound(demo.T, demo.T)
	r.kill(1, r.p(1), r.p(4))
	g.play(r)
	p := g.compute(t)[1]

	if p.CT.MultiKills != [4]int{0, 1, 0, 0} || p.T.MultiKills != [4]int{1, 0, 1, 0} || p.Match.MultiKills != [4]int{1, 1, 1, 0} {
		t.Errorf("multikills CT %v, T %v, match %v", p.CT.MultiKills, p.T.MultiKills, p.Match.MultiKills)
	}
}

func TestComputeKeepsToTheMatch(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.kill(1, r.p(1), &demo.Ref{ID: 99, Side: demo.T}) // a stranger is no player
	g.play(r)
	// An event outside the rounds of the match.
	g.m.Kills = append(g.m.Kills, demo.Kill{Round: 7, Killer: r.p(1), Victim: *r.p(4)})
	ps := Compute(&g.m)

	if len(ps) != 6 {
		t.Fatalf("%d players, want 6", len(ps))
	}
	for i, p := range ps {
		if p.SteamID64 != uint64(i+1) || p.Team != teamOf(p.SteamID64) {
			t.Errorf("player %d = %d of team %d, want the order of the match", i, p.SteamID64, p.Team)
		}
	}
	if p := ps[0].Match; p.Kills != 1 || p.Rounds != 1 {
		t.Errorf("player 1: %d kills in %d rounds, want 1 in 1", p.Kills, p.Rounds)
	}
	if p := ps[3].Match; p.Deaths != 0 {
		t.Errorf("player 4 died %d times outside the rounds", p.Deaths)
	}
}

func TestTradeTicks(t *testing.T) {
	for rate, want := range map[float64]int{64: 320, 128: 640, 0: 320, -1: 320} {
		if got := tradeTicks(rate); got != want {
			t.Errorf("tradeTicks(%v) = %d, want %d", rate, got, want)
		}
	}
}

// A player who leaves in the freeze time and is back before its end plays
// the round: the leave is a death for FACEIT, but neither leaves the round
// for KAST nor counts down who is alive.
func TestLeaveInTheFreezeTime(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.T)
	r.freezeEnd = 1000
	r.kill(500, r.p(2), r.p(2), leave)
	r.kill(1200, r.p(4), r.p(1))
	r.kill(1300, r.p(4), r.p(3)) // 2 is the last alive: 1v3
	r.kill(1500, r.p(5), r.p(2))
	g.play(r)
	p := g.compute(t)[2].Match

	if p.Deaths != 2 || p.RoundsPlayed != 1 || p.KASTRounds != 0 {
		t.Errorf("deaths %d, KAST %d of %d; want 2 deaths and the round played without KAST", p.Deaths, p.KASTRounds, p.RoundsPlayed)
	}
	if p.Clutches != [5]Clutch{2: {1, 0}} {
		t.Errorf("clutches %+v, want a lost 1v3", p.Clutches)
	}
}
