package metrics

import "github.com/kolyxus/fragmentor/bot/internal/demo"

// grenades counts the grenade metrics of docs/METRICS.md in the round of e:
// the flashes, the grenade damage and the grenades left at death. A bot
// that a player controls is that player.
func (c *counter) grenades(e events, side func(*player) *Stats) {
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
	for _, h := range e.hits {
		if h.Weapon != demo.HEGrenade && h.Class != demo.ClassFire {
			continue
		}
		if p := c.player(h.Attacker); p != nil && enemies(h.Attacker, &h.Victim) {
			side(p).GrenadeDamage += h.Damage
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
