package metrics

import (
	"slices"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// loadouts returns the equipment of bodies 1, 2, ... on side, one value
// each.
func loadouts(side demo.Side, values ...int) []demo.Loadout {
	out := make([]demo.Loadout, 0, len(values))
	for i, v := range values {
		out = append(out, demo.Loadout{Body: demo.Ref{ID: uint64(10*len(side) + i), Side: side}, Value: v})
	}
	return out
}

func TestBuys(t *testing.T) {
	// Team 0 plays CT: it wins on CT.
	round := func(n int, ct, tt []int) demo.Round {
		return demo.Round{Number: n, Winner: 0, WinnerSide: demo.CT,
			Equipment: append(loadouts(demo.CT, ct...), loadouts(demo.T, tt...)...)}
	}
	for _, tc := range []struct {
		name      string
		maxRounds int
		r         demo.Round
		want      [2]Buy
	}{
		{"the first round", 24, round(1, []int{850, 850}, []int{5000}), [2]Buy{BuyPistol, BuyPistol}},
		{"the first round of the second half", 24, round(13, []int{4700}, []int{4700}), [2]Buy{BuyPistol, BuyPistol}},
		{"mp_maxrounds unknown", 0, round(13, []int{4700}, []int{4700}), [2]Buy{BuyPistol, BuyPistol}},
		{"the first round of overtime", 24, round(25, []int{5000, 4000}, []int{4000, 4000}), [2]Buy{BuyFull, BuyFull}},
		{"full from 4500 on CT, 4000 on T", 24, round(2, []int{4500, 4500}, []int{4000, 3999}), [2]Buy{BuyFull, BuyForce}},
		{"just short of full", 24, round(3, []int{4499, 4500}, []int{4000, 4000}), [2]Buy{BuyForce, BuyFull}},
		{"an eco up to 1000", 24, round(4, []int{1000, 1000, 1000}, []int{1001}), [2]Buy{BuyEco, BuyForce}},
		{"nothing told", 24, round(5, []int{200, 200}, nil), [2]Buy{BuyEco, ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := buys(&demo.Match{MaxRounds: tc.maxRounds}, tc.r)
			if got != tc.want {
				t.Errorf("buys = %q, want %q", got, tc.want)
			}
		})
	}
	if _, eq := buys(&demo.Match{MaxRounds: 24}, round(2, []int{4500, 700}, []int{300})); eq != [2]int{5200, 300} {
		t.Errorf("equipment = %v, want 5200 and 300", eq)
	}
}

func TestClock(t *testing.T) {
	mc := matchClock{tickRate: 64, roundTime: 115, bombTime: 40}
	r := demo.Round{FreezeEndTick: 1000}
	if e, c, p := mc.of(r, 1000+64*15); e != 15 || c != 100 || p {
		t.Errorf("15 s in: elapsed %v, clock %v, planted %t; want 15, 100, false", e, c, p)
	}
	r.BombPlantTick = 1000 + 64*60
	if e, c, p := mc.of(r, r.BombPlantTick+64*10); e != 70 || c != 30 || !p {
		t.Errorf("10 s after the plant: elapsed %v, clock %v, planted %t; want 70, 30, true", e, c, p)
	}
	if _, c, _ := mc.of(r, r.BombPlantTick+64*50); c != 0 {
		t.Errorf("after the bomb went off the clock shows %v, want 0", c)
	}
}

// at puts player id of the round at x.
func at(r *round, id uint64, x float64) demo.Position { return demo.Position{Body: *r.p(id), X: x} }

func positions(ps ...demo.Position) killOpt { return func(k *demo.Kill) { k.Positions = ps } }

func TestDeathSituations(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.T)
	r.hitAt(90, r.p(4), r.p(1), 100, demo.ClassRifle)
	r.kill(100, r.p(4), r.p(1), positions(at(r, 1, 0), at(r, 2, 500), at(r, 3, 2000), at(r, 4, 300), at(r, 5, 800), at(r, 6, 3000)))
	// Player 2 trades him; his victim stood 1200 from his nearest teammate.
	r.kill(292, r.p(2), r.p(4), positions(at(r, 4, 0), at(r, 5, 1200), at(r, 6, 3000), at(r, 2, 500), at(r, 3, 2000)))
	r.kill(1000, r.p(5), r.p(2), positions(at(r, 2, 0), at(r, 3, 100), at(r, 5, 400), at(r, 6, 3000)))
	r.kill(1100, r.p(5), r.p(3), positions(at(r, 3, 0), at(r, 5, 200), at(r, 6, 3000)))
	g.play(r)

	// An eco of team 0 on T: a pistol against a rifle is excused.
	r = newRound(demo.T, demo.CT)
	r.equipment = append(loadouts(demo.T, 200, 200, 200), loadouts(demo.CT, 5000, 5000, 5000)...)
	r.plant = 600
	r.kill(100, r.p(4), r.p(1), func(k *demo.Kill) { k.VictimClass = demo.ClassPistol })
	r.kill(700, r.p(5), r.p(2), class(demo.ClassPistol), func(k *demo.Kill) { k.VictimClass = demo.ClassPistol })
	g.play(r)
	g.m.RoundTime, g.m.BombTime = 115, 40
	a := Analyze(&g.m)

	if len(a.Deaths) != 6 {
		t.Fatalf("deaths = %+v, want 6", a.Deaths)
	}
	type want struct {
		victim, killer      uint64
		opening, traded, tk bool
		own, enemy          int
		advantage           bool
		teammate            float64
		lone                bool
		near, damage        int
		dry, excused, won   bool
		buy, enemyBuy       Buy
	}
	for i, w := range []want{
		{victim: 1, killer: 4, opening: true, traded: true, own: 3, enemy: 3, teammate: 500, near: 2, buy: BuyPistol, enemyBuy: BuyPistol},
		{victim: 4, killer: 2, tk: true, own: 3, enemy: 2, advantage: true, teammate: 1200, lone: true, near: 1, damage: 100, won: true, buy: BuyPistol, enemyBuy: BuyPistol},
		{victim: 2, killer: 5, own: 2, enemy: 2, teammate: 100, near: 1, dry: true, buy: BuyPistol, enemyBuy: BuyPistol},
		{victim: 3, killer: 5, own: 1, enemy: 2, teammate: -1, near: 1, dry: true, buy: BuyPistol, enemyBuy: BuyPistol},
		{victim: 1, killer: 4, opening: true, own: 3, enemy: 3, teammate: -1, dry: true, excused: true, buy: BuyEco, enemyBuy: BuyFull},
		{victim: 2, killer: 5, own: 2, enemy: 3, teammate: -1, dry: true, buy: BuyEco, enemyBuy: BuyFull},
	} {
		d := a.Deaths[i]
		got := want{d.Victim, d.Killer, d.Opening, d.Traded, d.TradeKill, d.AliveOwn, d.AliveEnemy, d.Advantage,
			d.TeammateDistance, d.Lone, d.EnemiesNear, d.DamageBefore, d.Dry, d.Excused, d.Won, d.Buy, d.EnemyBuy}
		if got != w {
			t.Errorf("death %d =\n%+v\nwant\n%+v", i, got, w)
		}
	}
	if d := a.Deaths[0]; d.Kill != 0 || d.Round != 1 || d.Tick != 100 || d.Planted || !near(d.Elapsed, 100.0/64) || !near(d.Clock, 115-100.0/64) {
		t.Errorf("death 0 = %+v", d)
	}
	if d := a.Deaths[5]; !d.Planted || !near(d.Clock, 40-100.0/64) || !near(d.Elapsed, 700.0/64) {
		t.Errorf("death 5 after the plant: planted %t, clock %v, elapsed %v", d.Planted, d.Clock, d.Elapsed)
	}

	ps := g.compute(t)
	for _, tc := range []struct {
		id                                   uint64
		side                                 demo.Side
		lone, advantage, dry, teammateDeaths int
	}{
		{1, demo.CT, 0, 0, 0, 0},
		{2, demo.CT, 0, 0, 1, 1},
		{3, demo.CT, 0, 0, 1, 2},
		{4, demo.T, 1, 1, 0, 0},
		{5, demo.T, 0, 0, 0, 1},
		{1, demo.T, 0, 0, 1, 0},
		{2, demo.T, 0, 0, 1, 1},
		{3, demo.T, 0, 0, 0, 2},
	} {
		s := stats(ps, tc.id, tc.side)
		if s.LoneDeaths != tc.lone || s.AdvantageDeaths != tc.advantage || s.DryDeaths != tc.dry || s.TeammateDeaths != tc.teammateDeaths {
			t.Errorf("player %d on %s: lone %d, advantage %d, dry %d, teammates' deaths %d; want %d, %d, %d, %d", tc.id, tc.side,
				s.LoneDeaths, s.AdvantageDeaths, s.DryDeaths, s.TeammateDeaths, tc.lone, tc.advantage, tc.dry, tc.teammateDeaths)
		}
	}
}

func TestTradeChances(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.T)
	r.kill(100, r.p(4), r.p(1))
	r.sees(120, 200, r.p(2), r.p(4)) // in the window: a chance
	r.shot(150, r.p(2), demo.ClassRifle)
	r.sees(50, 90, r.p(3), r.p(4)) // before the death: none
	r.kill(1000, r.p(5), r.p(2))
	r.sees(1010, 1100, r.p(3), r.p(5))
	r.hitAt(1020, r.p(6), r.p(3), 20, demo.ClassRifle) // in a fight with another enemy: none
	r.kill(2000, r.p(6), r.p(3))                       // nobody left to trade him
	g.play(r)

	r = newRound(demo.CT, demo.CT)
	r.kill(100, r.p(4), r.p(1))
	r.sees(100, 300, r.p(2), r.p(4))
	r.hitAt(150, r.p(4), r.p(2), 30, demo.ClassRifle) // the killer shoots at him: the same fight
	r.hitAt(200, r.p(2), r.p(4), 100, demo.ClassRifle)
	r.kill(210, r.p(2), r.p(4))
	r.sees(100, 200, r.p(3), r.p(4))
	r.kill(5000, r.p(5), r.p(2), afterEnd) // the round is over: no chance
	r.sees(5000, 5100, r.p(3), r.p(5))
	g.play(r)
	a := Analyze(&g.m)

	want := []TradeChance{
		{Death: 0, Player: 2, Missed: true, Shot: true},
		{Death: 3, Player: 2, Traded: true, Hit: true},
		{Death: 3, Player: 3},
	}
	if !slices.Equal(a.Chances, want) {
		t.Errorf("chances =\n%+v\nwant\n%+v", a.Chances, want)
	}
	ps := g.compute(t)
	if s := stats(ps, 2, demo.CT); s.TradeChances != 2 || s.MissedTrades != 1 || s.TeammateDeaths != 2 {
		t.Errorf("player 2: %d chances, %d missed, %d teammates' deaths; want 2, 1, 2", s.TradeChances, s.MissedTrades, s.TeammateDeaths)
	}
	// The death after the end of the round is a teammate's death all the
	// same: the base of the trade kills counts it as they do.
	if s := stats(ps, 3, demo.CT); s.TradeChances != 1 || s.MissedTrades != 0 || s.TeammateDeaths != 4 {
		t.Errorf("player 3: %d chances, %d missed, %d teammates' deaths; want 1, 0, 4", s.TradeChances, s.MissedTrades, s.TeammateDeaths)
	}
}

// throw adds a grenade with the ID to the round.
func (r *round) throw(id int, thrower *demo.Ref, weapon string, after bool) {
	r.grenades = append(r.grenades, demo.Grenade{ID: id, Tick: 10 * id, AfterRoundEnd: after, Thrower: *thrower, Weapon: weapon})
}

// flashOf adds a flash of the flashbang with the ID.
func (r *round) flashOf(id int, thrower, victim *demo.Ref, seconds float64) {
	r.flashes = append(r.flashes, demo.Flash{Thrower: thrower, Victim: *victim, Duration: seconds, Grenade: id})
}

func TestGrenadesThrown(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.throw(1, r.p(1), demo.Flashbang, false)
	r.flashOf(1, r.p(1), r.p(4), 2)
	r.flashOf(1, r.p(1), r.p(5), 0.5) // too short
	r.throw(2, r.p(1), demo.Flashbang, false)
	r.flashOf(2, r.p(1), r.p(2), 1.5)
	r.flashOf(2, r.p(1), r.p(1), 2) // himself
	r.throw(3, r.p(2), demo.Flashbang, false)
	r.flash(r.p(2), r.p(6), 2) // of an unknown flashbang
	r.throw(4, r.p(4), demo.Flashbang, false)
	r.flashOf(4, r.p(4), r.p(1), 3)
	r.flashOf(4, r.p(4), r.p(5), 1.2)
	for i, w := range []string{demo.SmokeGrenade, demo.HEGrenade, demo.Molotov, demo.Incendiary, demo.Decoy} {
		r.throw(5+i, r.p(1), w, false)
	}
	r.throw(10, r.p(1), demo.Flashbang, true)          // after the end of the round
	r.throw(11, r.bot(7, 1, 0), demo.Flashbang, false) // a bot nobody controls
	r.throw(12, r.bot(8, 0, 3), demo.Flashbang, false) // a bot player 3 controls
	g.play(r)
	a := Analyze(&g.m)

	want := []Flashbang{
		{Grenade: 1, Round: 1, Tick: 10, Thrower: 1, Enemies: 1},
		{Grenade: 2, Round: 1, Tick: 20, Thrower: 1, Teammates: 1, Empty: true, TeamFlash: true},
		{Grenade: 3, Round: 1, Tick: 30, Thrower: 2, Empty: true},
		{Grenade: 4, Round: 1, Tick: 40, Thrower: 4, Enemies: 1, Teammates: 1, TeamFlash: true},
		{Grenade: 12, Round: 1, Tick: 120, Thrower: 3, Empty: true},
	}
	if !slices.Equal(a.Flashbangs, want) {
		t.Errorf("flashbangs =\n%+v\nwant\n%+v", a.Flashbangs, want)
	}
	ps := g.compute(t)
	for _, tc := range []struct {
		id                                       uint64
		side                                     demo.Side
		flashes, empty, team, bad                int
		smokes, he, fire, decoys, enemiesFlashed int
	}{
		{1, demo.CT, 2, 1, 1, 1, 1, 1, 2, 1, 1},
		{2, demo.CT, 1, 1, 0, 1, 0, 0, 0, 0, 1},
		{3, demo.CT, 1, 1, 0, 1, 0, 0, 0, 0, 0},
		{4, demo.T, 1, 0, 1, 1, 0, 0, 0, 0, 1},
	} {
		s := stats(ps, tc.id, tc.side)
		got := [9]int{s.FlashesThrown, s.EmptyFlashes, s.TeamFlashes, s.BadFlashes, s.SmokesThrown, s.HEGrenadesThrown, s.FireGrenadesThrown, s.DecoysThrown, s.EnemiesFlashed}
		if want := [9]int{tc.flashes, tc.empty, tc.team, tc.bad, tc.smokes, tc.he, tc.fire, tc.decoys, tc.enemiesFlashed}; got != want {
			t.Errorf("player %d: flashes, empty, team, bad, smokes, HE, fire, decoys, enemies flashed = %v, want %v", tc.id, got, want)
		}
	}
}
