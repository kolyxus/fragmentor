package metrics

import (
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

func TestAccuracy(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.sees(100, 200, r.p(1), r.p(4))
	r.shot(110, r.p(1), demo.ClassRifle)
	r.hitAt(110, r.p(1), r.p(4), 30, demo.ClassRifle) // a hit
	r.shot(120, r.p(1), demo.ClassRifle)              // a miss
	// Pistols and sniper rifles are no part of it.
	r.shot(130, r.p(1), demo.ClassPistol)
	r.hitAt(130, r.p(1), r.p(4), 20, demo.ClassPistol)
	r.shot(140, r.p(1), demo.ClassSniper)
	// The last tick in sight; the bullet hits a teammate: a miss.
	r.shot(200, r.p(1), demo.ClassRifle)
	r.hitAt(200, r.p(1), r.p(2), 25, demo.ClassRifle)
	// Out of sight: no part of it, though it hits.
	r.shot(250, r.p(1), demo.ClassRifle)
	r.hitAt(250, r.p(1), r.p(4), 30, demo.ClassRifle)
	// Player 2 sees only a bot no player controls: no enemy in sight.
	r.sees(100, 200, r.p(2), r.bot(1, 1, 0))
	r.shot(150, r.p(2), demo.ClassRifle)
	r.hitAt(150, r.p(2), r.bot(1, 1, 0), 30, demo.ClassRifle)
	// Player 3 fires after the end of the round.
	r.sees(100, 400, r.p(3), r.p(5))
	r.shot(300, r.p(3), demo.ClassRifle).AfterRoundEnd = true
	g.play(r)

	r = newRound(demo.T, demo.T)
	// Player 3 in a bot: the bot sees and fires, the hit is his.
	bot := r.bot(2, 0, 3)
	r.sees(100, 200, bot, r.p(5))
	r.shot(150, bot, demo.ClassRifle)
	r.hitAt(150, r.p(3), r.p(5), 27, demo.ClassRifle)
	g.play(r)
	ps := g.compute(t)

	if s := ps[1].CT; s.AccuracyShots != 3 || s.AccuracyHits != 1 || !near(s.Accuracy, 1.0/3) {
		t.Errorf("player 1: %d of %d shots hit (%v), want 1 of 3", s.AccuracyHits, s.AccuracyShots, s.Accuracy)
	}
	if s := ps[2].Match; s.AccuracyShots != 0 {
		t.Errorf("player 2: %d shots, want none: a bot no player controls is no enemy in sight", s.AccuracyShots)
	}
	if s := ps[3]; s.CT.AccuracyShots != 0 || s.T.AccuracyShots != 1 || s.T.AccuracyHits != 1 {
		t.Errorf("player 3: CT %d shots, T %d of %d; want none after the end, 1 of 1 in the bot", s.CT.AccuracyShots, s.T.AccuracyHits, s.T.AccuracyShots)
	}
}

func TestFirstShot(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	// Player 1 sees 4: the first shot misses, the second hits 4. He sees 5
	// at the same time and never hits him: that fight is no part of it.
	r.sees(100, 200, r.p(1), r.p(4))
	r.sees(100, 200, r.p(1), r.p(5))
	r.shot(110, r.p(1), demo.ClassRifle)
	r.shot(120, r.p(1), demo.ClassRifle)
	r.hitAt(120, r.p(1), r.p(4), 30, demo.ClassRifle)
	// He sees 4 again: the first shot hits.
	r.sees(300, 400, r.p(1), r.p(4))
	r.shot(310, r.p(1), demo.ClassRifle)
	r.hitAt(310, r.p(1), r.p(4), 30, demo.ClassRifle)
	// A fight opened with a pistol: no part of it.
	r.sees(500, 600, r.p(1), r.p(6))
	r.shot(510, r.p(1), demo.ClassPistol)
	r.hitAt(510, r.p(1), r.p(6), 20, demo.ClassPistol)
	r.shot(520, r.p(1), demo.ClassRifle)
	r.hitAt(520, r.p(1), r.p(6), 30, demo.ClassRifle)
	// A shotgun is no first shot: the rifle after it is.
	r.sees(700, 800, r.p(1), r.p(6))
	r.shot(705, r.p(1), demo.ClassShotgun)
	r.hitAt(705, r.p(1), r.p(6), 10, demo.ClassShotgun)
	r.shot(710, r.p(1), demo.ClassRifle)
	r.hitAt(710, r.p(1), r.p(6), 10, demo.ClassRifle)
	// A fight without shots.
	r.sees(900, 950, r.p(1), r.p(6))
	// Player 2 hits a bot no player controls.
	r.sees(100, 200, r.p(2), r.bot(1, 1, 0))
	r.shot(110, r.p(2), demo.ClassRifle)
	r.hitAt(110, r.p(2), r.bot(1, 1, 0), 30, demo.ClassRifle)
	g.play(r)
	ps := g.compute(t)

	if s := ps[1].CT; s.FirstShotFights != 3 || s.FirstShotHits != 2 || !near(s.FirstShotAccuracy, 2.0/3) {
		t.Errorf("player 1: first shot hit in %d of %d fights (%v), want 2 of 3", s.FirstShotHits, s.FirstShotFights, s.FirstShotAccuracy)
	}
	if s := ps[2].Match; s.FirstShotFights != 0 {
		t.Errorf("player 2: %d fights, want none", s.FirstShotFights)
	}
}

func TestMovingShots(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.sees(0, 1000, r.p(1), r.p(4))
	shoot := func(tick int, c demo.WeaponClass, speed, maxSpeed float64) {
		s := r.shot(tick, r.p(1), c)
		s.Speed, s.MaxSpeed = speed, maxSpeed
	}
	shoot(10, demo.ClassRifle, 73, 215)  // 33.95% of the top speed: accurate
	shoot(20, demo.ClassRifle, 74, 215)  // 34.4%: on the move
	shoot(30, demo.ClassRifle, 68, 200)  // 34% exactly: accurate
	shoot(40, demo.ClassSniper, 35, 100) // scoped in, 35%: on the move
	shoot(50, demo.ClassSniper, 30, 100)
	shoot(60, demo.ClassPistol, 200, 240) // pistols and SMGs are no part of it
	shoot(70, demo.ClassSMG, 200, 240)
	shoot(80, demo.ClassRifle, 200, 0) // a gun of unknown speed
	shoot(2000, demo.ClassRifle, 200, 215)
	g.play(r)
	p := g.compute(t)[1].CT

	if p.MovingBase != 5 || p.MovingShots != 2 || !near(p.MovingShotRate, 0.4) {
		t.Errorf("%d of %d shots on the move (%v), want 2 of 5", p.MovingShots, p.MovingBase, p.MovingShotRate)
	}
}

func TestKillTime(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.hitAt(100, r.p(1), r.p(4), 100, demo.ClassRifle) // one shot: 0 s
	r.hitAt(200, r.p(1), r.p(5), 30, demo.ClassRifle)
	r.hitAt(232, r.p(1), r.p(5), 30, demo.ClassRifle)
	r.hitAt(264, r.p(1), r.p(5), 40, demo.ClassRifle) // 64 ticks: 1 s
	r.hitAt(300, r.p(2), r.p(6), 50, demo.ClassRifle)
	r.hitAt(400, r.p(2), r.p(6), 50, demo.ClassRifle) // 100 ticks later: a new duel with a hurt enemy
	g.play(r)

	r = newRound(demo.T, demo.T)
	r.hitAt(50, nil, r.p(5), 10, demo.ClassWorld) // a fall
	r.hitAt(100, r.p(3), r.p(4), 30, demo.ClassRifle)
	r.hitAt(110, r.p(2), r.p(4), 30, demo.ClassRifle) // a teammate helps: no duel
	r.hitAt(120, r.p(3), r.p(4), 40, demo.ClassRifle)
	r.hitAt(150, r.p(1), r.p(5), 90, demo.ClassRifle)   // the enemy was hurt
	r.hitAt(200, r.p(1), r.p(6), 100, demo.ClassSniper) // a sniper rifle: 0 s
	g.play(r)

	r = newRound(demo.CT, demo.CT)
	r.hitAt(100, r.p(1), r.p(4), 40, demo.ClassRifle)
	r.hitAt(150, r.p(1), r.p(4), 60, demo.ClassPistol) // two guns: no duel
	r.hitAt(100, r.p(2), r.p(5), 80, demo.ClassSniper)
	r.hitAt(140, r.p(2), r.p(5), 20, demo.ClassRifle)
	r.hitAt(100, r.p(3), r.bot(1, 1, 0), 100, demo.ClassRifle) // a bot no player controls
	r.hitAt(200, r.p(6), r.p(3), 25, demo.ClassRifle)
	r.hitAt(296, r.p(6), r.p(3), 25, demo.ClassRifle)
	r.hitAt(392, r.p(6), r.p(3), 25, demo.ClassRifle)
	r.hitAt(488, r.p(6), r.p(3), 25, demo.ClassRifle) // the longest pause, 1.5 s, each time: 4.5 s
	r.hitAt(400, r.p(6), r.p(1), 99, demo.ClassRifle) // 1 survives
	g.play(r)
	ps := g.compute(t)

	for _, tc := range []struct {
		id                    uint64
		side                  demo.Side
		rifles, snipers       int
		rifleTime, sniperTime float64
	}{
		{1, demo.CT, 2, 0, 0.5, 0},
		{1, demo.T, 0, 1, 0, 0},
		{2, demo.CT, 0, 0, 0, 0},
		{2, demo.T, 0, 0, 0, 0},
		{3, demo.T, 0, 0, 0, 0},
		{3, demo.CT, 0, 0, 0, 0},
		{6, demo.T, 1, 0, 4.5, 0},
		{6, demo.CT, 0, 0, 0, 0},
	} {
		s := stats(ps, tc.id, tc.side)
		if s.RifleKillDuels != tc.rifles || s.SniperKillDuels != tc.snipers || !near(s.RifleKillTime, tc.rifleTime) || !near(s.SniperKillTime, tc.sniperTime) {
			t.Errorf("player %d on %s: rifles %d duels, %v s; sniper rifles %d, %v s; want %d, %v; %d, %v", tc.id, tc.side,
				s.RifleKillDuels, s.RifleKillTime, s.SniperKillDuels, s.SniperKillTime, tc.rifles, tc.rifleTime, tc.snipers, tc.sniperTime)
		}
	}
	if s := ps[1].Match; s.RifleKillDuels != 2 || !near(s.RifleKillSeconds, 1) || s.SniperKillDuels != 1 {
		t.Errorf("player 1: %d rifle duels in %v s, %d of sniper rifles", s.RifleKillDuels, s.RifleKillSeconds, s.SniperKillDuels)
	}
}

func TestHeadshotRatesByWeapon(t *testing.T) {
	g := newGame()
	r := newRound(demo.CT, demo.CT)
	r.hit(r.p(1), r.p(4), 100, demo.HitHead, demo.ClassRifle)
	r.hit(r.p(1), r.p(5), 27, demo.HitChest, demo.ClassRifle)
	r.hit(r.p(1), r.p(5), 60, demo.HitHead, demo.ClassPistol)
	r.hit(r.p(1), r.p(6), 40, demo.HitHead, demo.ClassPistol)
	r.hit(r.p(1), r.p(6), 15, demo.HitLeftLeg, demo.ClassPistol)
	// Neither rifles nor pistols: an SMG, a sniper rifle, a shotgun.
	r.hit(r.p(1), r.p(6), 30, demo.HitHead, demo.ClassSMG)
	r.hit(r.p(1), r.p(4), 100, demo.HitHead, demo.ClassSniper)
	r.hit(r.p(1), r.p(4), 20, demo.HitHead, demo.ClassShotgun)
	// Not on enemies: a teammate, a bot no player controls.
	r.hit(r.p(1), r.p(2), 30, demo.HitHead, demo.ClassRifle)
	r.hit(r.p(1), r.bot(1, 1, 0), 30, demo.HitHead, demo.ClassPistol)
	g.play(r)
	p := g.compute(t)[1].Match

	if p.RifleHits != 2 || p.RifleHeadHits != 1 || !near(p.RifleHeadshotRate, 0.5) {
		t.Errorf("rifles: %d of %d hits in the head (%v), want 1 of 2", p.RifleHeadHits, p.RifleHits, p.RifleHeadshotRate)
	}
	if p.PistolHits != 3 || p.PistolHeadHits != 2 || !near(p.PistolHeadshotRate, 2.0/3) {
		t.Errorf("pistols: %d of %d hits in the head (%v), want 2 of 3", p.PistolHeadHits, p.PistolHits, p.PistolHeadshotRate)
	}
	if p.BulletHits != 7 || p.HeadHits != 5 {
		t.Errorf("all bullets: %d of %d hits in the head, want 5 of 7", p.HeadHits, p.BulletHits)
	}
}
