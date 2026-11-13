package metrics

import "github.com/kolyxus/fragmentor/bot/internal/demo"

// grenades counts the grenade metrics of docs/METRICS.md in the round r of
// e: the flashes, the grenade damage and the grenades left at death, and
// logs the damage of the grenades for the Analysis. A bot that a player
// controls is that player.
func (c *counter) grenades(r demo.Round, e events, side func(*player) *Stats) {
	// Blindings of MinFlash or longer: of enemies, with their duration,
	// and of teammates. Blinding oneself counts for neither.
	for _, f := range e.flashes {
		if f.Thrower == nil || f.Duration < MinFlash.Seconds() {
			continue
		}
		thrower := coach(*f.Thrower)
		p := c.coached(thrower)
		if p == nil || coach(f.Victim) == thrower {
			continue
		}
		st := side(p)
		switch {
		case enemies(f.Thrower, &f.Victim):
			st.EnemiesFlashed++
			st.EnemyBlindSeconds += f.Duration
		case f.Thrower.Side != "" && f.Thrower.Side == f.Victim.Side:
			st.TeammatesFlashed++
		}
	}
	// The health HE grenades and fire took from enemies, bots among them,
	// as damage counts. The hit of another grenade is not damage of it.
	type source struct {
		p    *player
		fire bool
	}
	dealt := make(map[source]*GrenadeHit)
	hurt := make(map[source]map[body]bool)
	for _, h := range e.hits {
		if h.Weapon != demo.HEGrenade && h.Class != demo.ClassFire {
			continue
		}
		p := c.player(h.Attacker)
		if p == nil || !enemies(h.Attacker, &h.Victim) {
			continue
		}
		side(p).GrenadeDamage += h.Damage
		if h.Damage <= 0 {
			continue
		}
		src := source{p, h.Class == demo.ClassFire}
		g := dealt[src]
		if g == nil {
			g = &GrenadeHit{Round: r.Number, Tick: h.Tick, Player: p.id, Fire: src.fire}
			_, g.Clock, g.Planted = c.clock.of(r, h.Tick)
			dealt[src] = g
			hurt[src] = make(map[body]bool)
		}
		g.Damage += h.Damage
		if b := bodyOf(h.Victim); !hurt[src][b] {
			hurt[src][b] = true
			g.Enemies++
		}
	}
	for _, p := range c.players {
		for _, fire := range []bool{false, true} {
			if g := dealt[source{p, fire}]; g != nil {
				c.grenadeLog = append(c.grenadeLog, *g)
			}
		}
	}
	// Deaths by enemies with grenades left, and what the grenades cost.
	for _, k := range e.kills {
		if len(k.Grenades) == 0 || !duel(k) {
			continue
		}
		p := c.coached(coach(k.Victim))
		if p == nil {
			continue
		}
		st := side(p)
		st.DeathsWithGrenades++
		for _, g := range k.Grenades {
			st.UnusedGrenadeValue += demo.GrenadePrice(g)
		}
	}
}
