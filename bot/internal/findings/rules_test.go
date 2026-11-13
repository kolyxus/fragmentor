package findings

import (
	"slices"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// The rules are tested on made-up matches of ten players, 1 to 5 of team 0
// and 6 to 10 of team 1. Team 0 wins every round on a force buy of both
// teams unless a test tells otherwise, so an event of a mistake of player 1
// weighs one.

type game struct {
	a    metrics.Analysis
	tick int
}

func newGame(rounds int) *game {
	g := &game{a: metrics.Analysis{TickRate: testTickRate}}
	for id := uint64(1); id <= 10; id++ {
		g.a.Players = append(g.a.Players, metrics.Player{
			SteamID64: id, Team: teamOf(id), Match: metrics.Stats{Rounds: rounds, RoundsPlayed: rounds},
		})
	}
	for n := 1; n <= rounds; n++ {
		g.a.Rounds = append(g.a.Rounds, metrics.RoundInfo{
			Number: n, Winner: 0, Buy: [2]metrics.Buy{metrics.BuyForce, metrics.BuyForce}, Kills: map[uint64]int{},
		})
	}
	return g
}

// teamOf returns the team of a player; a stranger, an ID above 10, is put
// on team 1.
func teamOf(id uint64) int { return min(int((id-1)/5), 1) }

func (g *game) round(n int) *metrics.RoundInfo { return &g.a.Rounds[n-1] }

// stats returns the metrics of the player id to set.
func (g *game) stats(id uint64) *metrics.Stats { return &g.a.Players[id-1].Match }

type deathOpt func(*metrics.Death)

func untraded(d *metrics.Death)  { d.Traded = false }
func lone(d *metrics.Death)      { d.Traded, d.Lone, d.TeammateDistance = false, true, 2000 }
func dry(d *metrics.Death)       { d.Traded, d.Dry, d.DamageBefore = false, true, 0 }
func advantage(d *metrics.Death) { d.Advantage, d.AliveOwn, d.AliveEnemy = true, 4, 3 }
func opening(d *metrics.Death)   { d.Opening = true }
func excused(d *metrics.Death)   { d.Excused = true }
func tradeKill(d *metrics.Death) { d.TradeKill = true }

func carrying(grenades ...string) deathOpt {
	return func(d *metrics.Death) {
		d.Grenades = grenades
		for _, g := range grenades {
			d.GrenadeValue += demo.GrenadePrice(g)
		}
	}
}

func at(place string, side demo.Side) deathOpt {
	return func(d *metrics.Death) { d.VictimPlace, d.Side = place, side }
}

func crowd(enemies int) deathOpt { return func(d *metrics.Death) { d.EnemiesNear = enemies } }

func flashAssist(by uint64) deathOpt {
	return func(d *metrics.Death) { d.Assister, d.FlashAssist = by, true }
}

// death adds a death of the victim by the killer in round n and returns its
// index: traded, with teammates near, damage dealt and no grenades left,
// unless the options tell otherwise.
func (g *game) death(n int, victim, killer uint64, opts ...deathOpt) int {
	g.tick += 100
	r := g.round(n)
	team := teamOf(victim)
	d := metrics.Death{
		Round: n, Tick: g.tick, Clock: 60, Victim: victim, Team: team, Side: demo.CT, Killer: killer,
		Won: r.Winner == team, Buy: r.Buy[team], EnemyBuy: r.Buy[1-team],
		Traded: true, AliveOwn: 3, AliveEnemy: 3, TeammateDistance: 300, EnemiesNear: 1, DamageBefore: 50,
		VictimPlace: "Mid", KillerPlace: "Window", Weapon: "AK-47",
	}
	for _, o := range opts {
		o(&d)
	}
	g.a.Deaths = append(g.a.Deaths, d)
	return len(g.a.Deaths) - 1
}

// deaths adds count deaths of the victim, one a round from round 1 on, by
// an enemy.
func (g *game) deaths(victim uint64, count int, opts ...deathOpt) {
	killer := uint64(6)
	if teamOf(victim) == 1 {
		killer = 1
	}
	for n := 1; n <= count; n++ {
		g.death(n, victim, killer, opts...)
	}
}

// lobby gives each of the players 2 to 10 total deaths, with of them marked
// by the options.
func (g *game) lobby(total, with int, opts ...deathOpt) {
	for id := uint64(2); id <= 10; id++ {
		g.deaths(id, with, opts...)
		g.deaths(id, total-with)
	}
}

// finding returns the finding of the rule in the report, nil if the rule
// did not fire.
func finding(r Report, id RuleID) *Finding {
	for i := range r.Findings {
		if r.Findings[i].Rule == id {
			return &r.Findings[i]
		}
	}
	return nil
}

func comparison(t *testing.T, r Report, measure string) Comparison {
	t.Helper()
	for _, c := range r.Comparisons {
		if c.Measure == measure {
			return c
		}
	}
	t.Fatalf("no comparison of %q", measure)
	return Comparison{}
}

// The rules that count deaths of a kind of the deaths by enemies.
func TestDeathRules(t *testing.T) {
	for _, tc := range []struct {
		rule RuleID
		opt  deathOpt
	}{
		{UntradedDeath, untraded},
		{LoneDeath, lone},
		{AdvantageDeath, advantage},
		{DryDeath, dry},
		{UnusedGrenades, carrying(demo.Flashbang, demo.SmokeGrenade)},
	} {
		t.Run(string(tc.rule), func(t *testing.T) {
			g := newGame(16)
			g.lobby(16, 4, tc.opt)
			g.deaths(1, 9, tc.opt)
			g.deaths(1, 7)
			// A death of a stranger is nobody's.
			g.death(1, 99, 6, tc.opt)
			reports := Analyze(&g.a, nil)

			f := finding(reports[0], tc.rule)
			if f == nil {
				t.Fatalf("9 of 16 against 4 of 16 in the lobby is no finding: %+v", reports[0].Findings)
			}
			if f.X != 9 || f.N != 16 || f.Sample != 16 || f.Kind != Mistake || !near(f.BaseRate, 37.0/146) || len(f.Evidence) != MaxEvidence {
				t.Errorf("finding = %+v", f)
			}
			for i := 1; i < 10; i++ {
				if f := finding(reports[i], tc.rule); f != nil {
					t.Errorf("player %d has 4 of 16 as the lobby and the finding %+v", i+1, f)
				}
			}
			if c := comparison(t, reports[1], string(tc.rule)); c.X != 4 || c.N != 16 || c.Lobby.X != 9+8*4 || c.Lobby.N != 144 {
				t.Errorf("player 2: %v of %v, the lobby %+v; want 4 of 16, 41 of 144", c.X, c.N, c.Lobby)
			}
		})
	}
}

func TestDeathEvidence(t *testing.T) {
	g := newGame(16)
	g.lobby(16, 4, untraded)
	g.deaths(1, 8, untraded)
	g.deaths(1, 7)
	// Round 16 is lost on a full buy: the death weighs more, and it is the
	// one shown in full.
	g.round(16).Winner = 1
	g.round(16).Buy[0] = metrics.BuyFull
	i := g.death(16, 1, 7, lone, at("Banana", demo.T))
	d := &g.a.Deaths[i]
	d.Clock, d.Planted, d.Tick, d.AliveOwn, d.AliveEnemy, d.KillerPlace, d.Weapon = 25.5, true, 50000, 2, 4, "BombsiteB", "AWP"
	f := finding(Analyze(&g.a, nil)[0], UntradedDeath)
	if f == nil {
		t.Fatal("no finding")
	}
	want := Evidence{
		Round: 16, Clock: 25.5, Planted: true, Tick: 50000, Goto: 50000 - 5*testTickRate, Won: false, Buy: metrics.BuyFull,
		Impact: 1.875, Place: "Banana", EnemyPlace: "BombsiteB", TeammateMeters: 2000 * 0.0254, Alive: 2, AliveEnemy: 4, Weapon: "AWP",
	}
	got := f.Evidence[len(f.Evidence)-1]
	if !near(got.TeammateMeters, want.TeammateMeters) {
		t.Errorf("the teammate stood %v m away, want %v", got.TeammateMeters, want.TeammateMeters)
	}
	got.TeammateMeters = want.TeammateMeters
	if !evidenceEqual(got, want) {
		t.Errorf("the moment =\n%+v\nwant\n%+v", got, want)
	}
	// 8 deaths weigh one and one 1.875.
	if want := (8 + 1.875) / 9; !near(f.Impact, want) {
		t.Errorf("impact %v, want %v", f.Impact, want)
	}
	// No teammate alive: no distance.
	g.a.Deaths[i].TeammateDistance = -1
	f = finding(Analyze(&g.a, nil)[0], UntradedDeath)
	if got := f.Evidence[len(f.Evidence)-1].TeammateMeters; got != 0 {
		t.Errorf("the distance to no teammate = %v, want 0", got)
	}
}

func evidenceEqual(a, b Evidence) bool {
	ga, gb := a.Grenades, b.Grenades
	a.Grenades, b.Grenades = nil, nil
	return slices.Equal(ga, gb) && a.Round == b.Round && a.Clock == b.Clock && a.Planted == b.Planted && a.Tick == b.Tick &&
		a.Goto == b.Goto && a.Won == b.Won && a.Buy == b.Buy && near(a.Impact, b.Impact) && a.Place == b.Place &&
		a.EnemyPlace == b.EnemyPlace && near(a.TeammateMeters, b.TeammateMeters) && a.Alive == b.Alive && a.AliveEnemy == b.AliveEnemy &&
		a.Weapon == b.Weapon && a.Count == b.Count && a.Of == b.Of && a.Enemies == b.Enemies && a.Teammates == b.Teammates &&
		a.GrenadeValue == b.GrenadeValue && a.Detail == b.Detail
}

func TestUnusedGrenades(t *testing.T) {
	g := newGame(16)
	g.lobby(16, 4, carrying(demo.Flashbang))
	// Six deaths with $500 of grenades weigh one each, three with $1300 2.6.
	g.deaths(1, 6, carrying(demo.Flashbang, demo.SmokeGrenade))
	for n := 7; n <= 9; n++ {
		g.death(n, 1, 6, carrying(demo.Flashbang, demo.Flashbang, demo.HEGrenade, demo.SmokeGrenade, demo.Molotov))
	}
	for n := 10; n <= 16; n++ {
		g.death(n, 1, 6)
	}
	f := finding(Analyze(&g.a, nil)[0], UnusedGrenades)
	if f == nil {
		t.Fatal("no finding")
	}
	if want := (6*1 + 3*1400.0/GrenadeUnit) / 9; f.X != 9 || !near(f.Impact, want) || !near(f.Weight, f.Z*want) {
		t.Errorf("x %v, impact %v, weight %v; want 9, %v and z by it", f.X, f.Impact, f.Weight, want)
	}
	// The dearest grenades are shown first, with what they were.
	if e := f.Evidence[len(f.Evidence)-1]; e.Round != 9 || e.GrenadeValue != 1400 || len(e.Grenades) != 5 || !near(e.Impact, 2.8) {
		t.Errorf("the last moment = %+v, want round 9 with $1400 of five grenades", e)
	}
	if e := f.Evidence[0]; e.GrenadeValue != 500 {
		t.Errorf("the first moment = %+v, want one of the $500", e)
	}
	// The other rules of deaths show no grenades.
	g.lobby(16, 0)
	for i := range g.a.Deaths {
		if g.a.Deaths[i].Victim == 1 {
			g.a.Deaths[i].Traded = false
		}
	}
	if f := finding(Analyze(&g.a, nil)[0], UntradedDeath); f == nil || f.Evidence[0].GrenadeValue != 0 || f.Evidence[0].Grenades != nil {
		t.Errorf("an untraded death shows grenades: %+v", f)
	}
}

func TestOpeningDuels(t *testing.T) {
	g := newGame(16)
	// Players 2 to 5 and 7 to 10 win one opening duel each and lose one.
	for i := uint64(0); i < 4; i++ {
		g.death(int(1+2*i), 2+i, 7+i, opening)
		g.death(int(2+2*i), 7+i, 2+i, opening)
	}
	// Player 1 loses six to player 6, and one more that is excused: a
	// pistol against a rifle in an eco.
	for n := 9; n <= 14; n++ {
		g.death(n, 1, 6, opening)
	}
	g.death(15, 1, 6, opening, excused)
	// A death that opened nothing.
	g.death(16, 1, 6)
	reports := Analyze(&g.a, nil)

	f := finding(reports[0], OpeningLoss)
	if f == nil {
		t.Fatalf("six opening duels lost of six is no finding: %+v", reports[0].Findings)
	}
	// The excused duel is no duel for the mistake: 6 of 6, and the lobby is
	// 8 lost of 22, the 6 of player 6 won among them.
	if f.X != 6 || f.N != 6 || !near(f.BaseRate, 9.0/24) || f.Group != Duels || len(f.Evidence) != MaxEvidence {
		t.Errorf("finding = %+v", f)
	}
	for _, e := range f.Evidence {
		if e.Round == 15 {
			t.Errorf("the excused duel is shown: %+v", e)
		}
	}
	if c := comparison(t, reports[5], string(OpeningLoss)); c.X != 0 || c.N != 6 {
		t.Errorf("player 6 lost %v of %v opening duels, want 0 of 6: the excused one is none", c.X, c.N)
	}
	// For the strength every duel counts: player 6 won seven of seven.
	s := finding(reports[5], OpeningWins)
	if s == nil || s.X != 7 || s.N != 7 || s.Kind != Strength || s.Group != Duels || !near(s.BaseRate, 9.0/25) {
		t.Fatalf("the opening wins of player 6 = %+v", s)
	}
	// The kill as the killer saw it: his place, the victim's, his round.
	if e := s.Evidence[0]; e.Round != 9 || e.Place != "Window" || e.EnemyPlace != "Mid" || e.Weapon != "AK-47" || e.Won || e.Buy != metrics.BuyForce {
		t.Errorf("the first opening kill = %+v", e)
	}
	if c := comparison(t, reports[0], string(OpeningWins)); c.X != 0 || c.N != 7 {
		t.Errorf("player 1 won %v of %v opening duels, want 0 of 7", c.X, c.N)
	}
	if finding(reports[0], OpeningWins) != nil || finding(reports[5], OpeningLoss) != nil {
		t.Error("the loser has the strength or the winner the mistake")
	}
}

func (g *game) flashbang(n int, thrower uint64, enemies, teammates int) {
	g.tick += 100
	g.a.Flashbangs = append(g.a.Flashbangs, metrics.Flashbang{
		Grenade: len(g.a.Flashbangs) + 1, Round: n, Tick: g.tick, Clock: 80, Thrower: thrower,
		Enemies: enemies, Teammates: teammates, Empty: enemies == 0, TeamFlash: teammates > 0,
	})
}

func TestBadFlashes(t *testing.T) {
	g := newGame(16)
	// The others: one flashbang of ten is bad.
	for id := uint64(2); id <= 10; id++ {
		for n := 1; n <= 10; n++ {
			enemies := 1
			if n == 1 {
				enemies = 0
			}
			g.flashbang(n, id, enemies, 0)
		}
	}
	// Player 1: of eight, three blind nobody, two blind a teammate and an
	// enemy, one a teammate and nobody else; two are good.
	g.round(3).Winner = 1 // a lost round
	for n := 1; n <= 3; n++ {
		g.flashbang(n, 1, 0, 0)
	}
	g.flashbang(4, 1, 2, 1)
	g.flashbang(5, 1, 1, 2)
	g.flashbang(6, 1, 0, 1)
	g.flashbang(7, 1, 1, 0)
	g.flashbang(8, 1, 3, 0)
	g.flashbang(9, 99, 0, 0) // of a stranger
	reports := Analyze(&g.a, nil)

	f := finding(reports[0], BadFlashes)
	if f == nil {
		t.Fatalf("six bad flashbangs of eight is no finding: %+v", reports[0].Findings)
	}
	if f.X != 6 || f.N != 8 || f.Group != Grenades || f.Variant != VariantEmpty || !near(f.BaseRate, 10.0/92) {
		t.Errorf("finding = %+v", f)
	}
	// Five weigh one and the one of the lost round 1.5.
	if want := (5 + 1.5) / 6; !near(f.Impact, want) {
		t.Errorf("impact %v, want %v", f.Impact, want)
	}
	if e := f.Evidence[2]; e.Round != 3 || e.Won || !near(e.Impact, 1.5) || e.Enemies != 0 || e.Teammates != 0 || e.Clock != 80 {
		t.Errorf("the flashbang of round 3 = %+v", e)
	}
	// Mostly teammates blinded: the other variant.
	g.a.Flashbangs[len(g.a.Flashbangs)-9].Teammates = 1 // the flashbang of round 1
	g.a.Flashbangs[len(g.a.Flashbangs)-9].TeamFlash = true
	g.a.Flashbangs[len(g.a.Flashbangs)-8].Teammates = 1
	g.a.Flashbangs[len(g.a.Flashbangs)-8].TeamFlash = true
	if f := finding(Analyze(&g.a, nil)[0], BadFlashes); f == nil || f.Variant != VariantTeam {
		t.Errorf("five of six bad flashbangs blinded teammates: %+v", f)
	}
	// Below five flashbangs the rule is silent.
	g.a.Flashbangs = slices.DeleteFunc(g.a.Flashbangs, func(fb metrics.Flashbang) bool { return fb.Thrower == 1 && fb.Round > 4 })
	if f := finding(Analyze(&g.a, nil)[0], BadFlashes); f != nil {
		t.Errorf("four flashbangs made a finding: %+v", f)
	}
}

func TestMovingShots(t *testing.T) {
	g := newGame(16)
	shots := func(n int, id uint64, all, moving int, weapon string) {
		g.tick += 100
		g.a.RoundShots = append(g.a.RoundShots, metrics.RoundShots{
			Round: n, Player: id, Shots: all, Moving: moving, Tick: g.tick, Clock: 70, Weapon: weapon,
		})
	}
	for id := uint64(2); id <= 10; id++ {
		for n := 1; n <= 5; n++ {
			shots(n, id, 10, 1, "AK-47")
		}
	}
	// Player 1: 40 shots in four rounds, 15 of them on the move; round 2 is
	// lost on a full buy.
	g.round(2).Winner = 1
	g.round(2).Buy[0] = metrics.BuyFull
	shots(1, 1, 10, 3, "AK-47")
	shots(2, 1, 10, 8, "M4A4")
	shots(3, 1, 10, 0, "AK-47")
	shots(4, 1, 10, 4, "AWP")
	reports := Analyze(&g.a, nil)

	f := finding(reports[0], MovingShots)
	if f == nil {
		t.Fatalf("15 shots on the move of 40 is no finding: %+v", reports[0].Findings)
	}
	if f.X != 15 || f.N != 40 || f.Group != Shooting || !near(f.BaseRate, 46.0/452) {
		t.Errorf("finding = %+v", f)
	}
	// Every shot weighs what its round does: eight of them 1.875.
	if want := (3 + 8*1.875 + 4) / 15; !near(f.Impact, want) {
		t.Errorf("impact %v, want %v", f.Impact, want)
	}
	// A round is one moment, with its shots; a round without shots on the
	// move is none.
	if len(f.Evidence) != 3 {
		t.Fatalf("%d moments, want the three rounds with shots on the move: %+v", len(f.Evidence), f.Evidence)
	}
	if e := f.Evidence[1]; e.Round != 2 || e.Count != 8 || e.Of != 10 || e.Weapon != "M4A4" || e.Clock != 70 || !near(e.Impact, 1.875) {
		t.Errorf("the moment of round 2 = %+v", e)
	}
	// Below 30 shots the rule is silent.
	g.a.RoundShots = g.a.RoundShots[:len(g.a.RoundShots)-2]
	if f := finding(Analyze(&g.a, nil)[0], MovingShots); f != nil {
		t.Errorf("20 shots made a finding: %+v", f)
	}
}

func TestFirstShot(t *testing.T) {
	g := newGame(16)
	fight := func(n int, id uint64, hit bool) {
		g.tick += 100
		g.a.Fights = append(g.a.Fights, metrics.Fight{Round: n, Tick: g.tick, Clock: 65, Player: id, Weapon: "AK-47", Hit: hit})
	}
	for id := uint64(2); id <= 10; id++ {
		for n := 1; n <= 12; n++ {
			fight(n, id, n > 6)
		}
	}
	for n := 1; n <= 12; n++ {
		fight(n, 1, n > 10)
	}
	reports := Analyze(&g.a, nil)
	f := finding(reports[0], FirstShot)
	if f == nil {
		t.Fatalf("ten fights of twelve opened with a miss is no finding: %+v", reports[0].Findings)
	}
	if f.X != 10 || f.N != 12 || f.Group != Shooting || !near(f.BaseRate, 55.0/110) || len(f.Evidence) != MaxEvidence {
		t.Errorf("finding = %+v", f)
	}
	if e := f.Evidence[0]; e.Round != 1 || e.Weapon != "AK-47" || e.Clock != 65 || !e.Won {
		t.Errorf("the first moment = %+v", e)
	}
	// Below ten fights the rule is silent.
	g.a.Fights = g.a.Fights[:len(g.a.Fights)-3]
	if f := finding(Analyze(&g.a, nil)[0], FirstShot); f != nil {
		t.Errorf("nine fights made a finding: %+v", f)
	}
}

func TestMissedTrades(t *testing.T) {
	g := newGame(16)
	chance := func(death int, id uint64, missed, shot, hit bool) {
		g.a.Chances = append(g.a.Chances, metrics.TradeChance{Death: death, Player: id, Missed: missed, Shot: shot, Hit: hit, Traded: !missed})
	}
	// The others: one chance of four missed.
	for id := uint64(2); id <= 10; id++ {
		mate := uint64(1)
		if teamOf(id) == 1 {
			mate = 6
		}
		for n := 1; n <= 4; n++ {
			d := g.death(n, mate, 11-mate)
			chance(d, id, n == 1, true, false)
		}
	}
	// Player 1 misses five of six: twice he does not shoot, twice he
	// shoots and misses, once he hits and does not kill.
	g.round(12).Winner = 1
	var last int
	for n := 11; n <= 16; n++ {
		last = g.death(n, 2, 7, at("Banana", demo.T))
		chance(last, 1, n != 16, n >= 13, n == 15)
	}
	reports := Analyze(&g.a, nil)

	f := finding(reports[0], MissedTrade)
	if f == nil {
		t.Fatalf("five chances of six missed is no finding: %+v", reports[0].Findings)
	}
	if f.X != 5 || f.N != 6 || f.Group != Trades || f.Variant != VariantNoKill || !near(f.BaseRate, 10.0/38) {
		t.Errorf("finding = %+v", f)
	}
	var details []string
	for _, e := range f.Evidence {
		details = append(details, e.Detail)
		// The moment is the death of the teammate, where he died.
		if e.Place != "Banana" || e.EnemyPlace != "Window" || e.Clock != 60 {
			t.Errorf("the moment = %+v", e)
		}
	}
	if want := []string{DetailNoShot, DetailNoShot, DetailMissed, DetailMissed, DetailHit}; !slices.Equal(details, want) {
		t.Errorf("details %v, want %v", details, want)
	}
	if want := (4 + 1.5) / 5; !near(f.Impact, want) || f.Evidence[1].Won {
		t.Errorf("impact %v, want %v: round 12 is lost", f.Impact, want)
	}
	// Mostly without a shot: the other variant.
	g.a.Chances[len(g.a.Chances)-4].Shot = false
	if f := finding(Analyze(&g.a, nil)[0], MissedTrade); f == nil || f.Variant != VariantNoShot {
		t.Errorf("three of five without a shot: %+v", f)
	}
	// A chance of a death the match does not have is skipped.
	chance(len(g.a.Deaths)+5, 1, true, false, false)
	if f := finding(Analyze(&g.a, nil)[0], MissedTrade); f == nil || f.N != 6 {
		t.Errorf("a chance of no death counted: %+v", f)
	}
	_ = last
}

func TestDeathSpot(t *testing.T) {
	g := newGame(16)
	// The others die untraded twice at their worst place.
	for id := uint64(2); id <= 10; id++ {
		g.deaths(id, 2, untraded, at("Short", demo.CT))
		g.deaths(id, 1, untraded, at("Mid", demo.CT))
		g.deaths(id, 5, at("Long", demo.CT))
	}
	// Player 1 on Banana as T: five deaths, four untraded. Deaths there on
	// the other side, in a crowd of enemies and at no place are no part of
	// it.
	for n := 1; n <= 4; n++ {
		g.death(n, 1, 6, untraded, at("Banana", demo.T))
	}
	g.death(5, 1, 6, at("Banana", demo.T))
	g.death(6, 1, 6, untraded, at("Banana", demo.CT))
	g.death(7, 1, 6, untraded, at("Banana", demo.T), crowd(3))
	for n := 8; n <= 14; n++ {
		g.death(n, 1, 6, untraded, at("", demo.T))
	}
	// And six untraded deaths of ten in Apartments: the worst place, but
	// no spot, as less than two thirds are untraded.
	for n := 1; n <= 10; n++ {
		if n <= 6 {
			g.death(n, 1, 6, untraded, at("Apartments", demo.T))
		} else {
			g.death(n, 1, 6, at("Apartments", demo.T))
		}
	}
	reports := Analyze(&g.a, nil)

	f := finding(reports[0], DeathSpot)
	if f == nil {
		t.Fatalf("four untraded deaths of five on Banana is no finding: %+v", reports[0].Findings)
	}
	if f.X != 4 || f.N != 1 || f.Sample != 5 || f.Place != "Banana" || f.Side != demo.T || f.Group != Trades || !near(f.BaseRate, 2) || !near(f.Excess, 2) {
		t.Errorf("finding = %+v", f)
	}
	if len(f.Evidence) != 4 || f.Evidence[0].Place != "Banana" || f.Evidence[3].Round != 4 {
		t.Errorf("the moments = %+v, want the four untraded deaths on Banana", f.Evidence)
	}
	// The measure is the worst place of all, a spot or not.
	if c := comparison(t, reports[0], string(DeathSpot)); c.X != 6 || c.N != 1 || !near(c.Lobby.Rate, 2) {
		t.Errorf("the worst place: %v of %v, the lobby %v; want 6 of 1, 2", c.X, c.N, c.Lobby.Rate)
	}
	if c := comparison(t, reports[1], string(DeathSpot)); c.X != 2 || c.N != 1 {
		t.Errorf("the worst place of player 2: %v of %v, want 2 of 1", c.X, c.N)
	}
	for i := 1; i < 10; i++ {
		if f := finding(reports[i], DeathSpot); f != nil {
			t.Errorf("player %d has the finding %+v", i+1, f)
		}
	}

	// Two untraded deaths of three make a spot, of four they do not; a
	// spot of two deaths is none.
	for _, tc := range []struct {
		deaths, untraded int
		want             bool
	}{{3, 2, true}, {4, 2, false}, {3, 3, true}, {2, 2, false}, {6, 4, true}, {7, 4, false}} {
		cells := map[spot]*cell{{"A", demo.T}: {deaths: tc.deaths, untraded: tc.untraded}}
		var tl tally
		deathSpot(&tl, cells)
		if tl.skip == tc.want || tl.x != float64(tc.untraded) || tl.n != 1 {
			t.Errorf("%d deaths, %d untraded: a spot %t, the measure %v of %v; want %t", tc.deaths, tc.untraded, !tl.skip, tl.x, tl.n, tc.want)
		}
	}
	var none tally
	deathSpot(&none, nil)
	if !none.skip || none.x != 0 || none.n != 1 {
		t.Errorf("a player without deaths: %+v", none)
	}
}

func TestMultiKills(t *testing.T) {
	g := newGame(20)
	// The others: four of them have a round of three kills, all a round of
	// two.
	for id := uint64(2); id <= 10; id++ {
		if id <= 5 {
			g.round(1).Kills[id] = 3
		}
		g.round(2).Kills[id] = 2
	}
	// Player 1: rounds of three, four and five kills, and one of two.
	g.round(3).Kills[1] = 3
	g.round(4).Kills[1] = 4
	g.round(5).Kills[1] = 5
	g.round(6).Kills[1] = 2
	g.round(7).Kills[99] = 5 // a stranger
	first := g.death(4, 6, 1)
	g.a.Deaths[first].Clock, g.a.Deaths[first].Tick, g.a.Deaths[first].Weapon = 100, 40000, "AWP"
	g.death(4, 7, 1)
	reports := Analyze(&g.a, nil)

	f := finding(reports[0], MultiKills)
	if f == nil {
		t.Fatalf("three rounds of three kills and more is no finding: %+v", reports[0].Findings)
	}
	if f.X != 3 || f.N != 20 || f.Kind != Strength || f.Group != Duels || !near(f.BaseRate, 5.0/182) {
		t.Errorf("finding = %+v", f)
	}
	if len(f.Evidence) != 3 {
		t.Fatalf("moments = %+v", f.Evidence)
	}
	// The moment of a round is its first kill, if the match tells it.
	if e := f.Evidence[1]; e.Round != 4 || e.Count != 4 || e.Tick != 40000 || e.Clock != 100 || e.Weapon != "AWP" || !e.Won {
		t.Errorf("the round of four kills = %+v", e)
	}
	if e := f.Evidence[2]; e.Round != 5 || e.Count != 5 || e.Tick != 0 {
		t.Errorf("the round of five kills = %+v", e)
	}
	// The rounds count are those the player played.
	g.stats(1).RoundsPlayed = 10
	if f := finding(Analyze(&g.a, nil)[0], MultiKills); f == nil || f.N != 10 {
		t.Errorf("after ten rounds played: %+v", f)
	}
}

func TestClutches(t *testing.T) {
	g := newGame(20)
	clutch := func(n int, id uint64, enemies int, won bool) {
		g.tick += 100
		g.a.Clutches = append(g.a.Clutches, metrics.ClutchPlay{Round: n, Tick: g.tick, Clock: 30, Planted: true, Player: id, Enemies: enemies, Won: won})
	}
	clutch(1, 2, 1, true)
	clutch(2, 7, 2, false)
	// Player 1 wins a 1v1 and a 1v3, and loses a 1v4: a 1vX won weighs X.
	clutch(3, 1, 1, true)
	clutch(4, 1, 3, true)
	clutch(5, 1, 4, false)
	reports := Analyze(&g.a, nil)

	f := finding(reports[0], Clutches)
	if f == nil {
		t.Fatalf("a 1v1 and a 1v3 won is no finding: %+v", reports[0].Findings)
	}
	if f.X != 4 || f.N != 20 || f.Kind != Strength || f.Group != Duels || !near(f.BaseRate, 2.0/182) {
		t.Errorf("finding = %+v", f)
	}
	if len(f.Evidence) != 2 || f.Evidence[1].Round != 4 || f.Evidence[1].Count != 3 || !f.Evidence[1].Planted || f.Evidence[1].Clock != 30 {
		t.Errorf("moments = %+v", f.Evidence)
	}
	// A clutch lost is none: player 7 has nothing.
	if c := comparison(t, reports[6], string(Clutches)); c.X != 0 || c.N != 20 {
		t.Errorf("player 7: %v of %v, want 0 of 20", c.X, c.N)
	}
	// A 1v2 alone is less than two events more than expected.
	g.a.Clutches = g.a.Clutches[:2]
	clutch(3, 1, 2, true)
	if f := finding(Analyze(&g.a, nil)[0], Clutches); f != nil {
		t.Errorf("a single 1v2 made a finding: %+v", f)
	}
}

func TestUsefulGrenades(t *testing.T) {
	newLobby := func() *game {
		g := newGame(20)
		for id := uint64(1); id <= 10; id++ {
			st := g.stats(id)
			st.FlashesThrown, st.HEGrenadesThrown, st.SmokesThrown = 6, 3, 3
			st.EnemiesFlashed, st.GrenadeDamage = 4, 60
		}
		return g
	}
	// Flashes: 12 enemies blinded and two flash assists.
	g := newLobby()
	g.stats(1).EnemiesFlashed = 12
	g.flashbang(1, 1, 3, 0)
	g.flashbang(2, 1, 1, 0)
	g.flashbang(3, 1, 0, 0)
	g.death(4, 6, 2, flashAssist(1))
	g.death(5, 7, 2, flashAssist(1))
	g.death(6, 8, 2, func(d *metrics.Death) { d.Assister = 1 }) // an assist without a flash
	reports := Analyze(&g.a, nil)
	f := finding(reports[0], UsefulGrenades)
	if f == nil {
		t.Fatalf("14 blinded against 4 is no finding: %+v", reports[0].Findings)
	}
	if f.X != 14 || f.N != 20 || f.Sample != 12 || f.Variant != VariantFlashes || f.Kind != Strength || f.Group != Grenades || !near(f.BaseRate, 37.0/182) {
		t.Errorf("finding = %+v", f)
	}
	// The flashbangs that blinded enemies, the biggest first to pick.
	if len(f.Evidence) != 2 || f.Evidence[0].Enemies != 3 || f.Evidence[1].Enemies != 1 {
		t.Errorf("moments = %+v", f.Evidence)
	}
	if c := comparison(t, reports[0], MeasureGrenadeDamage); !near(c.X, 0.6) || c.N != 20 {
		t.Errorf("grenade damage %v of %v, want 0.6 of 20", c.X, c.N)
	}

	// Damage: 330 against 60, in units of 100.
	g = newLobby()
	g.stats(1).GrenadeDamage = 330
	g.a.GrenadeHits = []metrics.GrenadeHit{
		{Round: 2, Tick: 2000, Clock: 90, Player: 1, Damage: 180, Enemies: 3},
		{Round: 5, Tick: 5000, Clock: 20, Planted: true, Player: 1, Fire: true, Damage: 150, Enemies: 2},
	}
	f = finding(Analyze(&g.a, nil)[0], UsefulGrenades)
	if f == nil {
		t.Fatal("330 grenade damage against 60 is no finding")
	}
	if !near(f.X, 3.3) || f.N != 20 || f.Variant != VariantDamage || !near(f.BaseRate, (9*0.6+1)/182) {
		t.Errorf("finding = %+v", f)
	}
	if len(f.Evidence) != 2 || f.Evidence[0].Count != 180 || f.Evidence[0].Detail != DetailHE || f.Evidence[0].Enemies != 3 ||
		f.Evidence[1].Detail != DetailFire || !f.Evidence[1].Planted || f.Evidence[1].Count != 150 {
		t.Errorf("moments = %+v", f.Evidence)
	}

	// Both fire: the heavier one tells, whichever it is. Nine blinded
	// against four is two and a half spreads, the damage three.
	g.stats(1).EnemiesFlashed = 9
	if f := finding(Analyze(&g.a, nil)[0], UsefulGrenades); f == nil || f.Variant != VariantDamage {
		t.Errorf("9 blinded and 330 damage: %+v", f)
	}
	g.stats(1).EnemiesFlashed = 30
	if f := finding(Analyze(&g.a, nil)[0], UsefulGrenades); f == nil || f.Variant != VariantFlashes {
		t.Errorf("30 blinded and 330 damage: %+v", f)
	}
	// Below five grenades thrown the rule is silent.
	st := g.stats(1)
	st.FlashesThrown, st.HEGrenadesThrown, st.SmokesThrown, st.FireGrenadesThrown, st.DecoysThrown = 1, 1, 1, 1, 0
	if f := finding(Analyze(&g.a, nil)[0], UsefulGrenades); f != nil {
		t.Errorf("four grenades thrown made a finding: %+v", f)
	}
	st.DecoysThrown = 1
	if f := finding(Analyze(&g.a, nil)[0], UsefulGrenades); f == nil || f.Sample != 5 {
		t.Errorf("five grenades thrown: %+v", f)
	}
}

func TestTradeKills(t *testing.T) {
	g := newGame(20)
	for id := uint64(1); id <= 10; id++ {
		g.stats(id).TeammateDeaths = 30
	}
	// The others trade three of thirty.
	for id := uint64(2); id <= 10; id++ {
		victim := uint64(6)
		if teamOf(id) == 1 {
			victim = 1
		}
		for n := 1; n <= 3; n++ {
			g.death(n, victim, id, tradeKill)
		}
	}
	// Player 1 trades ten, and kills twice more without a trade.
	for n := 1; n <= 10; n++ {
		g.death(n, 7, 1, tradeKill)
	}
	g.death(11, 7, 1)
	g.death(12, 7, 1)
	reports := Analyze(&g.a, nil)
	f := finding(reports[0], TradeKills)
	if f == nil {
		t.Fatalf("ten trades of thirty against three is no finding: %+v", reports[0].Findings)
	}
	if f.X != 10 || f.N != 30 || f.Kind != Strength || f.Group != Trades || !near(f.BaseRate, 28.0/272) || len(f.Evidence) != MaxEvidence {
		t.Errorf("finding = %+v", f)
	}
	if e := f.Evidence[0]; e.Place != "Window" || e.EnemyPlace != "Mid" || !e.Won {
		t.Errorf("the first trade = %+v", e)
	}
	// Below five deaths of teammates the rule is silent.
	g.stats(1).TeammateDeaths = 4
	if f := finding(Analyze(&g.a, nil)[0], TradeKills); f != nil {
		t.Errorf("four deaths of teammates made a finding: %+v", f)
	}
}

func TestScoreboard(t *testing.T) {
	g := newGame(20)
	for id := uint64(1); id <= 10; id++ {
		st := g.stats(id)
		st.Kills, st.Deaths, st.Damage, st.KASTRounds, st.HeadshotKills, st.AccuracyHits, st.AccuracyShots = 10, 10, 1500, 14, 4, 20, 100
	}
	st := g.stats(1)
	st.Kills, st.Deaths, st.Damage, st.KASTRounds, st.HeadshotKills, st.AccuracyHits, st.AccuracyShots = 20, 8, 2000, 16, 10, 30, 90
	r := Analyze(&g.a, nil)[0]
	for _, tc := range []struct {
		measure      string
		x, n, lobby  float64
		rank, of     int
		rate, others float64
	}{
		{MeasureKD, 20, 8, 90, 1, 10, 2.5, 1},
		{MeasureADR, 2000, 20, 13500, 1, 10, 100, 75},
		{MeasureKAST, 16, 20, 126, 1, 10, 0.8, 0.7},
		{MeasureHeadshots, 10, 20, 36, 1, 10, 0.5, 0.4},
		{MeasureAccuracy, 30, 90, 180, 1, 10, 1.0 / 3, 0.2},
	} {
		c := comparison(t, r, tc.measure)
		if c.X != tc.x || c.N != tc.n || c.Lobby.X != tc.lobby || c.Rank != tc.rank || c.Of != tc.of || !near(c.Rate, tc.rate) || !near(c.Lobby.Rate, tc.others) {
			t.Errorf("%s = %+v", tc.measure, c)
		}
	}
	if len(r.Comparisons) != len(Measures()) {
		t.Errorf("%d comparisons, want one a measure, %d", len(r.Comparisons), len(Measures()))
	}
	for i, c := range r.Comparisons {
		if c.Measure != Measures()[i] {
			t.Errorf("comparison %d is of %q, want %q", i, c.Measure, Measures()[i])
		}
	}
}
