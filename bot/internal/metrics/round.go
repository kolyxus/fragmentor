package metrics

import "github.com/kolyxus/fragmentor/bot/internal/demo"

// roundFacts is what a round was for its players, for KAST and multikills.
// Who died or left counts from the end of the freeze time on, when the
// round knows who is alive: a player who leaves in the freeze time and is
// back before its end plays the round.
type roundFacts struct {
	freezeEnd int
	kills     map[*player]int  // kills as FACEIT counts them
	assisted  map[*player]bool // an assist, as FACEIT counts them
	died      map[*player]bool // the player's own body died, but not by leaving
	left      map[*player]bool // left the server alive
	traded    map[*player]bool // a death of theirs was traded
}

// round counts the round r with its events.
func (c *counter) round(r demo.Round, e events) {
	kills, hits := e.kills, e.hits
	side := func(p *player) *Stats { return p.on(r.Side(p.team)) }
	f := roundFacts{
		freezeEnd: r.FreezeEndTick,
		kills:     make(map[*player]int),
		assisted:  make(map[*player]bool),
		died:      make(map[*player]bool),
		left:      make(map[*player]bool),
		traded:    make(map[*player]bool),
	}
	for _, p := range c.players {
		side(p).Rounds++
	}
	c.asFACEIT(kills, hits, side, f)
	c.opening(kills, side)
	c.trades(kills, side, f)
	c.clutches(r, kills, side)
	c.shooting(r, e, side)
	c.killTimes(hits, side)
	c.grenades(r, e, side)

	started := make(map[uint64]bool)
	for _, a := range r.Alive {
		if !a.Bot {
			started[a.ID] = true
		}
	}
	for _, p := range c.players {
		// KAST as HLTV counts it, but a player who left the server alive
		// neither died nor survived: the round does not count.
		if !started[p.id] || f.left[p] {
			continue
		}
		s := side(p)
		s.RoundsPlayed++
		if f.kills[p] > 0 || f.assisted[p] || !f.died[p] || f.traded[p] {
			s.KASTRounds++
		}
	}
	byID := make(map[uint64]int, len(f.kills))
	for p, n := range f.kills {
		byID[p.id] = n
		if n >= 2 {
			side(p).MultiKills[min(n, 5)-2]++
		}
	}
	c.roundKills[r.Number] = byID
}

// asFACEIT counts the kills, deaths, assists, headshots and damage as
// FACEIT does, and the headshot rate by hits. A death is any death of the
// player's own body; a bot that a player controls dies for itself, and its
// kills and damage are the player's.
func (c *counter) asFACEIT(kills []demo.Kill, hits []demo.Hit, side func(*player) *Stats, f roundFacts) {
	for _, k := range kills {
		if v := c.player(&k.Victim); v != nil {
			side(v).Deaths++
			switch {
			case k.Tick < f.freezeEnd:
			case k.Disconnect:
				f.left[v] = true
			default:
				f.died[v] = true
			}
		}
		if a := c.player(k.Killer); a != nil && enemies(k.Killer, &k.Victim) {
			s := side(a)
			s.Kills++
			if k.Headshot {
				s.HeadshotKills++
			}
			f.kills[a]++
		}
		if a := c.player(k.Assister); a != nil && enemies(k.Assister, &k.Victim) {
			side(a).Assists++
			f.assisted[a] = true
		}
	}
	for _, h := range hits {
		a := c.player(h.Attacker)
		if a == nil || !enemies(h.Attacker, &h.Victim) {
			continue
		}
		s := side(a)
		s.Damage += h.Damage
		if !bullet(h.Class) || computerBot(h.Victim) {
			continue
		}
		head := 0
		if h.HitGroup == demo.HitHead {
			head = 1
		}
		s.BulletHits++
		s.HeadHits += head
		switch h.Class {
		case demo.ClassRifle:
			s.RifleHits++
			s.RifleHeadHits += head
		case demo.ClassPistol:
			s.PistolHits++
			s.PistolHeadHits += head
		}
	}
}

// opening counts the opening duel: the first kill of the round between
// enemies. The coaching rules skip kills of bots no player controls, but a
// death by such a bot is a death.
func (c *counter) opening(kills []demo.Kill, side func(*player) *Stats) {
	for _, k := range kills {
		if !duel(k) || computerBot(k.Victim) {
			continue
		}
		if p := c.coached(coach(*k.Killer)); p != nil {
			s := side(p)
			s.OpeningAttempts++
			s.OpeningWins++
		}
		if p := c.coached(coach(k.Victim)); p != nil {
			side(p).OpeningAttempts++
		}
		return
	}
}

// trades counts trade kills and traded and untraded deaths. A trade kill
// is a kill of an enemy who killed a teammate of the killer no more than
// TradeWindow before; a death is traded when a teammate kills its killer
// within TradeWindow. Kills in the same tick trade each other whatever their
// order. Only deaths by enemies are traded or not.
func (c *counter) trades(kills []demo.Kill, side func(*player) *Stats, f roundFacts) {
	for j, kj := range kills {
		if !duel(kj) || computerBot(kj.Victim) {
			continue
		}
		if p := c.coached(coach(*kj.Killer)); p != nil && c.tradeKill(kills, j) {
			side(p).TradeKills++
		}
	}
	for i, ki := range kills {
		if !duel(ki) {
			continue
		}
		p := c.coached(coach(ki.Victim))
		if p == nil {
			continue
		}
		if c.tradedDeath(kills, i) {
			side(p).TradedDeaths++
			f.traded[p] = true
		} else {
			side(p).UntradedDeaths++
		}
	}
}

// tradeKill reports whether kills[j], a kill of an enemy, killed an enemy
// who had killed a teammate of the killer no more than TradeWindow before.
func (c *counter) tradeKill(kills []demo.Kill, j int) bool {
	kj := kills[j]
	killer, victim := coach(*kj.Killer), coach(kj.Victim)
	for i, ki := range kills {
		if i != j && duel(ki) && coach(*ki.Killer) == victim && coach(ki.Victim) != killer &&
			ki.Victim.Side == kj.Killer.Side && c.within(ki, kj) {
			return true
		}
	}
	return false
}

// tradedDeath reports whether kills[i], a kill of an enemy, was traded: a
// teammate of the victim killed the killer within TradeWindow.
func (c *counter) tradedDeath(kills []demo.Kill, i int) bool {
	ki := kills[i]
	victim, killer := coach(ki.Victim), coach(*ki.Killer)
	for j, kj := range kills {
		if j != i && duel(kj) && coach(kj.Victim) == killer && coach(*kj.Killer) != victim &&
			kj.Killer.Side == ki.Victim.Side && c.within(ki, kj) {
			return true
		}
	}
	return false
}

// within reports whether revenge came no more than TradeWindow after death,
// the limit included; kills of one tick are within each other's window.
func (c *counter) within(death, revenge demo.Kill) bool {
	return death.Tick <= revenge.Tick && revenge.Tick <= death.Tick+c.window
}

// clutches counts the clutches of the round. A side is in a clutch the first
// time it is down to one alive while an enemy lives; X is how many enemies
// live then. A bot never clutches, whoever controls it. Who is alive counts
// down from the end of the freeze time; deaths after the end of the round
// make no clutches: the round is decided.
func (c *counter) clutches(r demo.Round, kills []demo.Kill, side func(*player) *Stats) {
	alive := map[demo.Side]map[body]bool{demo.T: {}, demo.CT: {}}
	for _, a := range r.Alive {
		if m, ok := alive[a.Side]; ok {
			m[bodyOf(a)] = true
		}
	}
	done := make(map[demo.Side]bool)
	for _, k := range kills {
		if k.Tick < r.FreezeEndTick || k.AfterRoundEnd {
			continue
		}
		b := bodyOf(k.Victim)
		s := demo.T
		if alive[demo.CT][b] {
			s = demo.CT
		}
		if !alive[s][b] {
			continue // not alive at the start: nothing changes
		}
		delete(alive[s], b)
		enemies := len(alive[opposite(s)])
		if done[s] || len(alive[s]) != 1 || enemies == 0 {
			continue
		}
		done[s] = true
		for last := range alive[s] {
			if p := c.byID[last.id]; p != nil && !last.bot {
				cl := &side(p).Clutches[min(enemies, 5)-1]
				cl.Attempts++
				if r.WinnerSide == s {
					cl.Wins++
				}
				play := ClutchPlay{Round: r.Number, Tick: k.Tick, Player: p.id, Enemies: enemies, Won: r.WinnerSide == s}
				_, play.Clock, play.Planted = c.clock.of(r, k.Tick)
				c.clutchLog = append(c.clutchLog, play)
			}
		}
	}
}

// body is whose body a ref is, a bot's for a bot that a player controls.
type body struct {
	id  uint64
	bot bool
}

func bodyOf(r demo.Ref) body { return body{id: r.ID, bot: r.Bot} }

// who is someone for the coaching rules: a bot that a player controls is
// that player.
type who struct {
	id  uint64
	bot bool // a bot that no player controls
}

func coach(r demo.Ref) who {
	if r.Controller != 0 {
		return who{id: r.Controller}
	}
	return who{id: r.ID, bot: r.Bot}
}

// coached returns the player of the match w is, nil for a bot or a
// stranger.
func (c *counter) coached(w who) *player {
	if w.bot {
		return nil
	}
	return c.byID[w.id]
}

// duel reports whether k is a kill of an enemy: not by the world, not a
// suicide or a leave, not of a teammate.
func duel(k demo.Kill) bool { return enemies(k.Killer, &k.Victim) }

// enemies reports whether a and b were on different sides.
func enemies(a, b *demo.Ref) bool {
	return a != nil && b != nil && a.Side != "" && b.Side != "" && a.Side != b.Side
}

// computerBot reports whether r is a bot that no player controls.
func computerBot(r demo.Ref) bool { return r.Bot && r.Controller == 0 }

// bullet reports whether hits of the weapon class count for the headshot
// rate by hits: guns, but not shotguns, whose pellets would swamp it.
func bullet(c demo.WeaponClass) bool {
	switch c {
	case demo.ClassPistol, demo.ClassSMG, demo.ClassMachineGun, demo.ClassRifle, demo.ClassSniper:
		return true
	}
	return false
}

func opposite(s demo.Side) demo.Side {
	if s == demo.CT {
		return demo.T
	}
	return demo.CT
}
