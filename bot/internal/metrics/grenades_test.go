package metrics

import (
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

func TestFlashes(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.flash(r.p(1), r.p(4), 2)
	r.flash(r.p(1), r.p(5), 0.9) // too short
	r.flash(r.p(1), r.p(5), 1)   // a second exactly counts
	r.flash(r.p(1), r.p(2), 3)   // a teammate
	r.flash(r.p(1), r.p(1), 4)   // himself
	r.flash(nil, r.p(6), 3)      // the thrower is unknown
	r.flash(r.p(4), r.bot(1, 0, 0), 2.5)
	// Player 2 in a bot blinds the bot, himself, and an enemy.
	bot := r.bot(2, 0, 2)
	r.flash(bot, bot, 2)
	r.flash(bot, r.p(6), 1.5)
	g.play(r)
	ps := g.compute(t)

	for _, tc := range []struct {
		id               uint64
		side             demo.Side
		enemies, mates   int
		seconds, average float64
	}{
		{1, demo.CT, 2, 1, 3, 1.5},
		{2, demo.CT, 1, 0, 1.5, 1.5},
		{4, demo.T, 1, 0, 2.5, 2.5}, // an enemy bot is an enemy
		{6, demo.T, 0, 0, 0, 0},
	} {
		s := stats(ps, tc.id, tc.side)
		if s.EnemiesFlashed != tc.enemies || s.TeammatesFlashed != tc.mates || !near(s.EnemyBlindSeconds, tc.seconds) || !near(s.EnemyBlindTime, tc.average) {
			t.Errorf("player %d: blinded %d enemies for %v s (%v on average) and %d teammates; want %d, %v, %v, %d", tc.id,
				s.EnemiesFlashed, s.EnemyBlindSeconds, s.EnemyBlindTime, s.TeammatesFlashed, tc.enemies, tc.seconds, tc.average, tc.mates)
		}
	}
}

func TestGrenadeDamage(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.hitAt(10, r.p(1), r.p(4), 40, demo.ClassGrenade).Weapon = demo.HEGrenade
	r.hitAt(20, r.p(1), r.p(5), 8, demo.ClassFire).Weapon = demo.Molotov
	r.hitAt(30, r.p(1), r.bot(1, 1, 0), 7, demo.ClassFire).Weapon = demo.Incendiary // an enemy bot: damage
	r.hitAt(40, r.p(1), r.p(2), 20, demo.ClassGrenade).Weapon = demo.HEGrenade      // a teammate
	r.hitAt(50, r.p(1), r.p(1), 10, demo.ClassGrenade).Weapon = demo.HEGrenade      // himself
	r.hitAt(60, r.p(1), r.p(4), 1, demo.ClassGrenade).Weapon = demo.Flashbang       // it hits, but does no grenade damage
	r.hitAt(70, r.p(1), r.p(4), 30, demo.ClassRifle).Weapon = "AK-47"
	g.play(r)
	p := g.compute(t)[1].CT

	if p.GrenadeDamage != 55 || p.Damage != 86 {
		t.Errorf("grenade damage %d of %d, want 55 of 86", p.GrenadeDamage, p.Damage)
	}
}

func carries(grenades ...string) killOpt { return func(k *demo.Kill) { k.Grenades = grenades } }

func TestUnusedGrenades(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.T)
	r.kill(10, r.p(4), r.p(1), carries(demo.Flashbang, demo.Flashbang, demo.HEGrenade))
	r.kill(20, nil, r.p(2), class(demo.ClassBomb), carries(demo.SmokeGrenade)) // the world: no part of it
	r.kill(30, r.p(3), r.p(3), leave, carries(demo.Molotov))                   // leaving the server
	r.kill(40, r.p(5), r.bot(2, 0, 2), carries(demo.Incendiary))               // player 2 in a bot: his
	r.kill(50, r.p(6), r.p(5), carries(demo.Decoy))                            // a teamkill
	r.kill(60, r.bot(1, 0, 0), r.p(4), carries(demo.SmokeGrenade))             // a bot is an enemy
	r.kill(70, r.p(4), r.p(3))                                                 // nothing left
	g.play(r)
	ps := g.compute(t)

	for _, tc := range []struct {
		id            uint64
		side          demo.Side
		deaths, value int
	}{
		{1, demo.CT, 1, 700},
		{2, demo.CT, 1, 500},
		{3, demo.CT, 0, 0},
		{4, demo.T, 1, 300},
		{5, demo.T, 0, 0},
	} {
		if s := stats(ps, tc.id, tc.side); s.DeathsWithGrenades != tc.deaths || s.UnusedGrenadeValue != tc.value {
			t.Errorf("player %d: %d deaths with grenades worth $%d, want %d and $%d", tc.id, s.DeathsWithGrenades, s.UnusedGrenadeValue, tc.deaths, tc.value)
		}
	}
}
