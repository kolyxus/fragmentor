package metrics

import (
	"slices"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// The events of the Analysis that the findings take their evidence from:
// who assisted a death, when a clutch began, the shots in sight by round,
// the fights of the first shot and the damage of the grenades.

func TestDeathAssists(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.kill(10, r.p(1), r.p(4), assist(r.p(2), true))
	r.kill(20, r.p(1), r.p(5), assist(r.p(3), false))
	r.kill(30, r.p(1), r.p(6), assist(r.p(4), true)) // an assist on the death of a teammate is none
	r.kill(40, r.p(5), r.p(1))
	g.play(r)
	a := Analyze(&g.m)

	type assisted struct {
		by    uint64
		flash bool
	}
	want := []assisted{{2, true}, {3, false}, {0, false}, {0, false}}
	if len(a.Deaths) != len(want) {
		t.Fatalf("deaths = %+v, want %d", a.Deaths, len(want))
	}
	for i, w := range want {
		if d := a.Deaths[i]; (assisted{d.Assister, d.FlashAssist}) != w {
			t.Errorf("death %d: the assist of %d, by a flash %t; want %+v", i, d.Assister, d.FlashAssist, w)
		}
	}
}

func TestClutchPlays(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.T)
	r.plant = 100
	r.kill(64, r.p(4), r.p(1))
	r.kill(128, r.p(4), r.p(2)) // 3 is the last alive, after the plant: 1v3
	r.kill(192, r.p(3), r.p(4))
	r.kill(256, r.p(3), r.p(5)) // 6 is: 1v1, and wins it
	r.kill(320, r.p(6), r.p(3))
	g.play(r)
	r = newRound(demo.T, demo.T)
	r.kill(640, r.p(4), r.p(1))
	r.kill(704, r.p(4), r.p(2)) // 1v3 again, with no bomb planted, and the round is won
	g.play(r)
	a := Analyze(&g.m)

	want := []ClutchPlay{
		{Round: 1, Tick: 128, Clock: bombAt(100, 128), Planted: true, Player: 3, Enemies: 3},
		{Round: 1, Tick: 256, Clock: bombAt(100, 256), Planted: true, Player: 6, Enemies: 1, Won: true},
		{Round: 2, Tick: 704, Clock: clockAt(704), Player: 3, Enemies: 3, Won: true},
	}
	if !slices.Equal(a.Clutches, want) {
		t.Errorf("clutches =\n%+v\nwant\n%+v", a.Clutches, want)
	}
	if a.TickRate != testTickRate {
		t.Errorf("tick rate %v, want %v", a.TickRate, testTickRate)
	}
}

func TestShootingEvents(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.plant = 600
	shoot := func(r *round, tick int, id uint64, c demo.WeaponClass, weapon string, speed float64) {
		s := r.shot(tick, r.p(id), c)
		s.Weapon, s.Speed = weapon, speed
	}
	r.sees(0, 1000, r.p(1), r.p(4))
	shoot(r, 100, 1, demo.ClassRifle, "AK-47", 0)       // standing: the moment until a shot on the move
	shoot(r, 110, 1, demo.ClassRifle, "Galil AR", 200)  // the first on the move
	shoot(r, 120, 1, demo.ClassSniper, "AWP", 200)      // a sniper rifle counts, on the move too
	shoot(r, 130, 1, demo.ClassPistol, "Glock-18", 200) // a pistol is no part of it
	shoot(r, 2000, 1, demo.ClassRifle, "AK-47", 200)    // out of sight
	r.hitAt(110, r.p(1), r.p(4), 30, demo.ClassRifle)
	// Player 5 fires once after the plant, standing, and hits.
	r.sees(0, 1000, r.p(5), r.p(1))
	shoot(r, 700, 5, demo.ClassRifle, "M4A4", 0)
	r.hitAt(700, r.p(5), r.p(1), 30, demo.ClassRifle)
	// Player 2 sees an enemy and never fires: nothing of his.
	r.sees(0, 1000, r.p(2), r.p(6))
	g.play(r)

	r = newRound(demo.T, demo.T)
	r.sees(0, 1000, r.p(1), r.p(4))
	shoot(r, 50, 1, demo.ClassRifle, "AK-47", 0)
	shoot(r, 60, 1, demo.ClassRifle, "AK-47", 10)
	g.play(r)
	a := Analyze(&g.m)

	wantShots := []RoundShots{
		{Round: 1, Player: 1, Shots: 3, Moving: 2, Tick: 110, Clock: clockAt(110), Weapon: "Galil AR"},
		{Round: 1, Player: 5, Shots: 1, Tick: 700, Clock: bombAt(600, 700), Planted: true, Weapon: "M4A4"},
		{Round: 2, Player: 1, Shots: 2, Tick: 50, Clock: clockAt(50), Weapon: "AK-47"},
	}
	if !slices.Equal(a.RoundShots, wantShots) {
		t.Errorf("round shots =\n%+v\nwant\n%+v", a.RoundShots, wantShots)
	}
	// Player 1 hit the enemy he saw, but not with the first shot; player 5
	// did with it. The second round has no hit, so no fight.
	wantFights := []Fight{
		{Round: 1, Tick: 100, Clock: clockAt(100), Player: 1, Weapon: "AK-47"},
		{Round: 1, Tick: 700, Clock: bombAt(600, 700), Planted: true, Player: 5, Weapon: "M4A4", Hit: true},
	}
	if !slices.Equal(a.Fights, wantFights) {
		t.Errorf("fights =\n%+v\nwant\n%+v", a.Fights, wantFights)
	}
	ps := g.compute(t)
	if s := ps[1].Match; s.MovingBase != 5 || s.MovingShots != 2 || s.FirstShotFights != 1 || s.FirstShotHits != 0 {
		t.Errorf("player 1: %d of %d shots on the move, the first shot hit in %d of %d fights; want 2 of 5, 0 of 1",
			s.MovingShots, s.MovingBase, s.FirstShotHits, s.FirstShotFights)
	}
}

func TestGrenadeHits(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	he := func(r *round, tick int, attacker, victim *demo.Ref, damage int) {
		r.hitAt(tick, attacker, victim, damage, demo.ClassGrenade).Weapon = demo.HEGrenade
	}
	fire := func(r *round, tick int, attacker, victim *demo.Ref, damage int) {
		r.hitAt(tick, attacker, victim, damage, demo.ClassFire).Weapon = demo.Molotov
	}
	he(r, 100, r.p(1), r.p(4), 40)
	he(r, 100, r.p(1), r.p(5), 25)
	he(r, 100, r.p(1), r.p(2), 20) // a teammate
	fire(r, 200, r.p(1), r.p(4), 8)
	fire(r, 210, r.p(1), r.p(4), 8) // the same enemy burns on
	fire(r, 220, r.p(1), r.p(6), 5)
	he(r, 300, r.p(4), r.p(1), 50)
	he(r, 310, r.p(4), r.p(2), 0)                                              // the armor took it all
	r.hitAt(400, r.p(1), r.p(4), 1, demo.ClassGrenade).Weapon = demo.Flashbang // the hit of a flashbang
	r.hitAt(410, r.p(1), r.p(4), 30, demo.ClassRifle).Weapon = "AK-47"
	g.play(r)
	r = newRound(demo.CT, demo.CT)
	r.plant = 50
	he(r, 100, r.p(1), r.p(4), 10)
	g.play(r)
	a := Analyze(&g.m)

	want := []GrenadeHit{
		{Round: 1, Tick: 100, Clock: clockAt(100), Player: 1, Damage: 65, Enemies: 2},
		{Round: 1, Tick: 200, Clock: clockAt(200), Player: 1, Fire: true, Damage: 21, Enemies: 2},
		{Round: 1, Tick: 300, Clock: clockAt(300), Player: 4, Damage: 50, Enemies: 1},
		{Round: 2, Tick: 100, Clock: bombAt(50, 100), Planted: true, Player: 1, Damage: 10, Enemies: 1},
	}
	if !slices.Equal(a.GrenadeHits, want) {
		t.Errorf("grenade hits =\n%+v\nwant\n%+v", a.GrenadeHits, want)
	}
	if got := g.compute(t)[1].Match.GrenadeDamage; got != 96 {
		t.Errorf("grenade damage of player 1 = %d, want 96", got)
	}
}
