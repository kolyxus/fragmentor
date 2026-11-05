package metrics

import (
	"slices"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// The shooting metrics of docs/METRICS.md. "In sight" means while the
// shooter saw a live enemy, the game's spotted flag that a demo.Sighting
// keeps: shots at nobody, sprays into a smoke and prefires count for
// neither accuracy nor shots on the move. Shots after the end of the round
// do not count at all. Bots no player controls are neither shooters nor
// targets; a bot that a player controls is that player.

// fullHealth is the health of a player at the start of a round.
const fullHealth = 100

// span is a stretch of time in which a player saw the enemy target.
type span struct {
	from, to int
	target   who
}

// shooting counts accuracy, the first shot and shots on the move in the
// round of e.
func (c *counter) shooting(e events, side func(*player) *Stats) {
	seen := make(map[who][]span)
	for _, s := range e.sightings {
		if !computerBot(s.Target) {
			w := coach(s.Spotter)
			seen[w] = append(seen[w], span{from: s.From, to: s.To, target: coach(s.Target)})
		}
	}
	// Whom the bullets of each player hit, by tick: a bullet hits in the
	// tick of its shot.
	hitAt := make(map[who]map[int][]who)
	for _, h := range e.hits {
		if h.Attacker == nil || !gunHit(h.Class) || !enemies(h.Attacker, &h.Victim) || computerBot(h.Victim) {
			continue
		}
		a := who{id: h.Attacker.ID, bot: h.Attacker.Bot}
		if hitAt[a] == nil {
			hitAt[a] = make(map[int][]who)
		}
		hitAt[a][h.Tick] = append(hitAt[a][h.Tick], coach(h.Victim))
	}
	inSight := func(w who, tick int) bool {
		return slices.ContainsFunc(seen[w], func(sp span) bool { return sp.from <= tick && tick <= sp.to })
	}

	shots := make(map[who][]demo.Shot) // the shots of the round that count, by shooter
	for _, s := range e.shots {
		w := coach(s.Shooter)
		p := c.coached(w)
		if p == nil || s.AfterRoundEnd {
			continue
		}
		shots[w] = append(shots[w], s)
		if (s.Class != demo.ClassRifle && s.Class != demo.ClassSniper) || !inSight(w, s.Tick) {
			continue
		}
		st := side(p)
		if s.Class == demo.ClassRifle {
			st.AccuracyShots++
			if len(hitAt[w][s.Tick]) > 0 {
				st.AccuracyHits++
			}
		}
		if s.MaxSpeed > 0 {
			st.MovingBase++
			if s.Speed > MovingShare*s.MaxSpeed {
				st.MovingShots++
			}
		}
	}

	// The first shot: in each fight, a stretch of seeing an enemy, in
	// which the player hit that enemy, did the first shot hit him? Fights
	// opened with another gun than a rifle do not count; shotguns are no
	// first shots.
	for w, spans := range seen {
		p := c.coached(w)
		if p == nil {
			continue
		}
		for _, sp := range spans {
			hitHim := func(tick int) bool { return slices.Contains(hitAt[w][tick], sp.target) }
			var first *demo.Shot
			hit := false
			for i := range shots[w] {
				s := &shots[w][i]
				if s.Tick < sp.from || s.Tick > sp.to || s.Class == demo.ClassShotgun {
					continue
				}
				if first == nil {
					first = s
				}
				hit = hit || hitHim(s.Tick)
			}
			if first == nil || first.Class != demo.ClassRifle || !hit {
				continue
			}
			st := side(p)
			st.FirstShotFights++
			if hitHim(first.Tick) {
				st.FirstShotHits++
			}
		}
	}
}

// killTimes counts the duels of the kill time in the round: a player's
// hits on an enemy no more than KillTimeGap apart, from the first one to
// the one that brought the player's damage to fullHealth, that is killed
// him with no help. A hit takes no more health than the enemy has, so the
// damage gets there only if the enemy was at full health at the first hit
// and nobody else hurt him; duels the enemy survived never get there.
// Every hit of a duel is of one class: rifles or sniper rifles.
func (c *counter) killTimes(hits []demo.Hit, side func(*player) *Stats) {
	type key struct {
		attacker who
		victim   body
	}
	type duel struct {
		first, last, damage int
		class               demo.WeaponClass
		mixed               bool
	}
	open := make(map[key]*duel)
	for _, h := range hits {
		if h.Attacker == nil || !enemies(h.Attacker, &h.Victim) || computerBot(h.Victim) {
			continue
		}
		a := who{id: h.Attacker.ID, bot: h.Attacker.Bot}
		p := c.coached(a)
		if p == nil {
			continue
		}
		k := key{a, bodyOf(h.Victim)}
		d := open[k]
		if d != nil && h.Tick-d.last > c.gap {
			d = nil
		}
		if d == nil {
			d = &duel{first: h.Tick, class: h.Class}
			open[k] = d
		}
		d.last = h.Tick
		d.damage += h.Damage
		d.mixed = d.mixed || h.Class != d.class
		if d.damage < fullHealth {
			continue
		}
		delete(open, k)
		if d.mixed {
			continue
		}
		seconds := float64(h.Tick-d.first) / c.tickRate
		st := side(p)
		switch d.class {
		case demo.ClassRifle:
			st.RifleKillDuels++
			st.RifleKillSeconds += seconds
		case demo.ClassSniper:
			st.SniperKillDuels++
			st.SniperKillSeconds += seconds
		}
	}
}

// gunHit reports whether hits of the weapon class are bullets: guns,
// shotguns among them.
func gunHit(c demo.WeaponClass) bool {
	return bullet(c) || c == demo.ClassShotgun
}
