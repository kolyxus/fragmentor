package demo

import (
	"cmp"
	"fmt"
	"slices"
)

// The builder turns the events of a match into a Match. It knows nothing of
// demoinfocs: parse.go converts the library's events into the types below,
// so the counting rules are tested on made-up events.

// playerRef is a player as an event saw them.
type playerRef struct {
	ID   uint64 // SteamID64; for a bot its user ID plus one, see Ref.ID
	Name string
	Side Side // "" if neither T nor CT
	Bot  bool
	// Controller is the SteamID64 of the player who controls the bot, see
	// Ref.Controller.
	Controller uint64
}

// human reports whether p is a player and not a bot.
func (p *playerRef) human() bool { return p != nil && !p.Bot && p.ID != 0 }

// export makes the Ref of an event out of p, nil out of nil.
func (p *playerRef) export() *Ref {
	if p == nil {
		return nil
	}
	return &Ref{ID: p.ID, Bot: p.Bot, Side: p.Side, Controller: p.Controller}
}

// enemies reports whether a and b were on different sides. A suicide is not
// a kill of an enemy: the killer and the victim are on the same side.
func enemies(a, b *playerRef) bool {
	return a != nil && b != nil && a.Side != "" && b.Side != "" && a.Side != b.Side
}

type killEvent struct {
	Tick        int
	Killer      *playerRef // nil for the world: the bomb, a fall
	Victim      *playerRef // whose body died
	Assister    *playerRef // flash assists included
	FlashAssist bool
	Headshot    bool
	Disconnect  bool // the victim left the server
	Weapon      string
	Class       WeaponClass
	Grenades    []string // what the victim's body carried

	// The context, see Kill.
	VictimPlace, KillerPlace string
	VictimWeapon             string
	VictimClass              WeaponClass
	ThroughSmoke, Wallbang   bool
	NoScope, KillerBlind     bool
	// Positions is every body the game had alive at the moment; the builder
	// drops those that died earlier in the round.
	Positions []positionEvent
}

type positionEvent struct {
	Body    playerRef
	X, Y, Z float64
}

type grenadeEvent struct {
	Tick    int
	Thrower *playerRef // whose body threw it
	Weapon  string
}

type loadoutEvent struct {
	Body  playerRef
	Value int
}

type shotEvent struct {
	Tick     int
	Shooter  *playerRef // whose body fired
	Weapon   string
	Class    WeaponClass
	Speed    float64 // over the ground, units per second
	MaxSpeed float64 // with the gun at the moment
}

type flashEvent struct {
	Tick     int
	Thrower  *playerRef // nil if unknown
	Victim   *playerRef // whose body was blinded
	Duration float64    // seconds
	Grenade  int        // the ID of the flashbang, 0 if unknown
}

// body tells the bodies of the events apart: the ID of a bot is unique
// only among bots.
type body struct {
	id  uint64
	bot bool
}

func bodyOf(p *playerRef) body { return body{id: p.ID, bot: p.Bot} }

// sightKey is who sees whom.
type sightKey struct{ spotter, target body }

type hurtEvent struct {
	Tick     int
	Attacker *playerRef // nil for the world
	Victim   *playerRef // whose body was hit
	Damage   int        // health taken, no more than the victim had
	HitGroup HitGroup
	Weapon   string
	Class    WeaponClass
}

type roundEndEvent struct {
	Tick    int
	Winner  Side // "" if nobody won
	Reason  string
	Players []playerRef     // the players on T and CT at the end of the round
	Names   map[Side]string // the clan names of the sides
}

// gameScore is the score as the game itself keeps it, to check ours.
type gameScore struct {
	CT, T, Rounds int
}

type playerStats struct {
	Player
	// firstRound and firstSide tell where the player was first seen: the
	// index of the round then in progress and the side. The team follows.
	firstRound int
	firstSide  Side
}

type roundRec struct {
	Round
	ctTeam int // the team that played the round as CT
}

type builder struct {
	rounds   []roundRec
	played   int // rounds that ended, with a winner or without
	players  map[uint64]*playerStats
	names    [2]string
	score    [2]int
	warnings []string

	// The events of the match. They fall in round cur: the round in
	// progress, or the one that has just ended (ended) until the next one
	// starts. A round without a winner gets no Number, so its events fall
	// in the round after it; such a summary is suspicious anyway.
	kills     []Kill
	hits      []Hit
	shots     []Shot
	grenades  []Grenade
	flashes   []Flash
	sightings []Sighting
	cur       int
	ended     bool
	// alive is who was alive when the freeze time of the round in progress
	// ended, at tick freezeEnd; nil until then. loadouts is what they
	// carried, which the game tells a tick later.
	alive     []Ref
	freezeEnd int
	loadouts  []Loadout
	// plantTick and plantSite are when and where the bomb was planted in
	// the round in progress, 0 and "" until it is.
	plantTick int
	plantSite string
	// seeing holds the sightings in progress, by their index in sightings.
	// dead is the bodies that died in the round, which neither see nor are
	// seen any more.
	seeing map[sightKey]int
	dead   map[body]bool
	// tick is the last tick of an event, where the sightings still in
	// progress end when the demo does.
	tick int
}

func newBuilder() *builder {
	return &builder{
		players: make(map[uint64]*playerStats),
		cur:     1,
		seeing:  make(map[sightKey]int),
		dead:    make(map[body]bool),
	}
}

// reset forgets everything counted: the match starts, or starts over.
func (b *builder) reset() { *b = *newBuilder() }

func (b *builder) warnf(format string, args ...any) {
	b.warnings = append(b.warnings, fmt.Sprintf(format, args...))
}

// see returns the stats of a human player, made at the first sight. It is
// nil for a bot or nobody.
func (b *builder) see(p *playerRef) *playerStats {
	if !p.human() {
		return nil
	}
	s := b.players[p.ID]
	if s == nil {
		s = &playerStats{Player: Player{SteamID64: p.ID}, firstRound: -1}
		b.players[p.ID] = s
	}
	if p.Name != "" {
		s.Name = p.Name
	}
	if s.firstRound < 0 && (p.Side == T || p.Side == CT) {
		s.firstRound, s.firstSide = len(b.rounds), p.Side
	}
	return s
}

// kill counts a death of the victim, whatever killed them, and a kill and
// an assist only against an enemy: a teamkill or a suicide gives no kill.
// It logs the kill as an event of the current round.
func (b *builder) kill(k killEvent) {
	b.tick = max(b.tick, k.Tick)
	if v := b.see(k.Victim); v != nil {
		v.Deaths++
	}
	if killer := b.see(k.Killer); killer != nil && enemies(k.Killer, k.Victim) {
		killer.Kills++
		if k.Headshot {
			killer.Headshots++
		}
	}
	if a := b.see(k.Assister); a != nil && enemies(k.Assister, k.Victim) {
		a.Assists++
	}
	if k.Victim == nil {
		return // a broken demo: a death of nobody tells nothing
	}
	var positions []Position
	for _, p := range k.Positions {
		// The game removes the dead after the events of the tick: a body
		// that died earlier in the round, the same tick among them, is
		// gone.
		if !b.dead[bodyOf(&p.Body)] {
			positions = append(positions, Position{Body: *p.Body.export(), X: p.X, Y: p.Y, Z: p.Z})
		}
	}
	b.kills = append(b.kills, Kill{
		Tick:          k.Tick,
		Round:         b.cur,
		AfterRoundEnd: b.ended,
		Killer:        k.Killer.export(),
		Victim:        *k.Victim.export(),
		Assister:      k.Assister.export(),
		FlashAssist:   k.FlashAssist && k.Assister != nil,
		Headshot:      k.Headshot,
		Disconnect:    k.Disconnect,
		Weapon:        k.Weapon,
		Class:         k.Class,
		Grenades:      k.Grenades,
		VictimPlace:   k.VictimPlace,
		KillerPlace:   k.KillerPlace,
		VictimWeapon:  k.VictimWeapon,
		VictimClass:   k.VictimClass,
		ThroughSmoke:  k.ThroughSmoke,
		Wallbang:      k.Wallbang,
		NoScope:       k.NoScope,
		KillerBlind:   k.KillerBlind,
		Positions:     positions,
	})
	b.gone(k.Tick, bodyOf(k.Victim), !k.Disconnect)
}

// grenade logs a grenade thrown in the current round and returns its ID, 0
// for a grenade of nobody, which is not logged.
func (b *builder) grenade(g grenadeEvent) int {
	b.tick = max(b.tick, g.Tick)
	if g.Thrower == nil {
		return 0
	}
	id := len(b.grenades) + 1
	b.grenades = append(b.grenades, Grenade{
		ID:            id,
		Tick:          g.Tick,
		Round:         b.cur,
		AfterRoundEnd: b.ended,
		Thrower:       *g.Thrower.export(),
		Weapon:        g.Weapon,
	})
	return id
}

// bombPlanted notes the bomb planted at tick on site, "A", "B" or "".
func (b *builder) bombPlanted(tick int, site string) {
	b.tick = max(b.tick, tick)
	if !b.ended {
		b.plantTick, b.plantSite = tick, site
	}
}

// equipment notes what the bodies alive at the end of the freeze time of
// the round in progress carried then.
func (b *builder) equipment(loadouts []loadoutEvent) {
	if b.ended || b.alive == nil {
		return
	}
	b.loadouts = make([]Loadout, 0, len(loadouts))
	for _, l := range loadouts {
		b.loadouts = append(b.loadouts, Loadout{Body: *l.Body.export(), Value: l.Value})
	}
}

// gone ends the sightings of the body who, as the spotter and as the
// target, at tick: it died or left the server. A body that died stays dead
// until the next round; one that left may come back and play on.
func (b *builder) gone(tick int, who body, died bool) {
	if died {
		b.dead[who] = true
	}
	for k, i := range b.seeing {
		if k.spotter == who || k.target == who {
			b.sightings[i].To = tick
			delete(b.seeing, k)
		}
	}
}

// leftServer marks the kill i of the log as a disconnect: its victim left
// the server. It is for the case the game tells of the leaving after the
// death.
func (b *builder) leftServer(i int) {
	if i >= 0 && i < len(b.kills) {
		b.kills[i].Disconnect = true
		v := b.kills[i].Victim
		delete(b.dead, body{id: v.ID, bot: v.Bot})
	}
}

// shot logs a shot of a gun in the current round.
func (b *builder) shot(s shotEvent) {
	b.tick = max(b.tick, s.Tick)
	if s.Shooter == nil {
		return
	}
	b.shots = append(b.shots, Shot{
		Tick:          s.Tick,
		Round:         b.cur,
		AfterRoundEnd: b.ended,
		Shooter:       *s.Shooter.export(),
		Weapon:        s.Weapon,
		Class:         s.Class,
		Speed:         s.Speed,
		MaxSpeed:      s.MaxSpeed,
	})
}

// flash logs a body blinded by a flashbang in the current round.
func (b *builder) flash(f flashEvent) {
	b.tick = max(b.tick, f.Tick)
	if f.Victim == nil {
		return
	}
	b.flashes = append(b.flashes, Flash{
		Tick:          f.Tick,
		Round:         b.cur,
		AfterRoundEnd: b.ended,
		Thrower:       f.Thrower.export(),
		Victim:        *f.Victim.export(),
		Duration:      f.Duration,
		Grenade:       f.Grenade,
	})
}

// spotted notes who sees target from tick on: the game tells it whenever
// the spotters of a body change. Only enemies count, and only the living:
// the game keeps the flag of a dead body for a while.
func (b *builder) spotted(tick int, target *playerRef, spotters []playerRef) {
	b.tick = max(b.tick, tick)
	if target == nil {
		return
	}
	t := bodyOf(target)
	sees := func(s *playerRef) bool { return !b.dead[t] && !b.dead[bodyOf(s)] && enemies(s, target) }
	now := make(map[body]bool, len(spotters))
	for i := range spotters {
		if sees(&spotters[i]) {
			now[bodyOf(&spotters[i])] = true
		}
	}
	for k, i := range b.seeing {
		if k.target == t && !now[k.spotter] {
			b.sightings[i].To = tick
			delete(b.seeing, k)
		}
	}
	// In the order of spotters, for the order of the log.
	for i := range spotters {
		s := &spotters[i]
		k := sightKey{spotter: bodyOf(s), target: t}
		if _, ok := b.seeing[k]; ok || !now[k.spotter] {
			continue
		}
		b.seeing[k] = len(b.sightings)
		b.sightings = append(b.sightings, Sighting{Round: b.cur, Spotter: *s.export(), Target: *target.export(), From: tick, To: tick})
	}
}

// endSightings ends every sighting in progress at tick.
func (b *builder) endSightings(tick int) {
	for k, i := range b.seeing {
		b.sightings[i].To = tick
		delete(b.seeing, k)
	}
}

// hurt counts damage to enemies only, bots among them, and logs the hit.
func (b *builder) hurt(h hurtEvent) {
	b.tick = max(b.tick, h.Tick)
	b.see(h.Victim)
	if a := b.see(h.Attacker); a != nil && enemies(h.Attacker, h.Victim) {
		a.Damage += h.Damage
	}
	if h.Victim == nil {
		return
	}
	b.hits = append(b.hits, Hit{
		Tick:          h.Tick,
		Round:         b.cur,
		AfterRoundEnd: b.ended,
		Attacker:      h.Attacker.export(),
		Victim:        *h.Victim.export(),
		Damage:        h.Damage,
		HitGroup:      h.HitGroup,
		Weapon:        h.Weapon,
		Class:         h.Class,
	})
}

// roundStart starts the next round at tick once the last one has ended:
// the events from now on are the next round's. The sightings of the last
// round end, and the dead are alive again.
func (b *builder) roundStart(tick int) {
	b.tick = max(b.tick, tick)
	if b.ended {
		b.cur, b.ended = len(b.rounds)+1, false
		b.endSightings(tick)
		clear(b.dead)
	}
}

// freezeTimeEnd notes who was alive when the freeze time of the round ended
// at tick. It starts the round too, should the demo lack its start.
func (b *builder) freezeTimeEnd(tick int, alive []playerRef) {
	b.roundStart(tick)
	b.freezeEnd = tick
	b.alive = make([]Ref, 0, len(alive))
	for i := range alive {
		b.alive = append(b.alive, *alive[i].export())
	}
	b.loadouts = nil
}

// roundEnd counts a round. A round without a winner, a draw, gets no number
// and no score, but it counts as played, so check finds the gap.
func (b *builder) roundEnd(e roundEndEvent) {
	b.tick = max(b.tick, e.Tick)
	b.played++
	b.ended = true
	alive, freezeEnd, loadouts := b.alive, b.freezeEnd, b.loadouts
	plantTick, plantSite := b.plantTick, b.plantSite
	b.alive, b.freezeEnd, b.loadouts = nil, 0, nil
	b.plantTick, b.plantSite = 0, ""
	if e.Winner != T && e.Winner != CT {
		b.warnf("a round ended without a winner at tick %d", e.Tick)
		return
	}
	if alive == nil {
		b.warnf("round %d has no end of its freeze time: who was alive in it is unknown", len(b.rounds)+1)
	}
	ct := b.ctTeam(e.Players)
	for i := range e.Players {
		b.see(&e.Players[i])
	}
	for side, name := range e.Names {
		if name != "" {
			b.names[teamOn(side, ct)] = name
		}
	}
	winner := teamOn(e.Winner, ct)
	b.score[winner]++
	b.rounds = append(b.rounds, roundRec{
		Round: Round{
			Number:        len(b.rounds) + 1,
			Winner:        winner,
			WinnerSide:    e.Winner,
			Reason:        e.Reason,
			Score:         b.score,
			EndTick:       e.Tick,
			FreezeEndTick: freezeEnd,
			Alive:         alive,
			Equipment:     loadouts,
			BombPlantTick: plantTick,
			BombSite:      plantSite,
		},
		ctTeam: ct,
	})
}

// teamOn returns the team on side when the team ct plays CT.
func teamOn(side Side, ct int) int {
	if side == CT {
		return ct
	}
	return 1 - ct
}

// ctTeam tells which team played the round that just ended as CT. Teams are
// told apart by their players: everyone seen in an earlier round votes with
// their side for their team. In the first round team 0 is CT by definition.
func (b *builder) ctTeam(players []playerRef) int {
	if len(b.rounds) == 0 {
		return 0
	}
	var votes [2]int
	for _, p := range players {
		s := b.players[p.ID]
		if s == nil || s.firstRound < 0 || s.firstRound >= len(b.rounds) {
			continue // first seen in this round: their team is what is decided
		}
		team := b.teamAt(s.firstRound, s.firstSide)
		if p.Side == CT {
			votes[team]++
		} else {
			votes[1-team]++
		}
	}
	switch {
	case votes[0] > votes[1]:
		return 0
	case votes[1] > votes[0]:
		return 1
	}
	b.warnf("could not tell the teams apart in round %d, kept the sides of the round before", len(b.rounds)+1)
	return b.rounds[len(b.rounds)-1].ctTeam
}

// teamAt returns the team that played side in round i, counted from 0. A
// round after the last one ended has the sides of the last.
func (b *builder) teamAt(i int, side Side) int {
	ct := 0
	if len(b.rounds) > 0 {
		ct = b.rounds[min(i, len(b.rounds)-1)].ctTeam
	}
	return teamOn(side, ct)
}

// build makes the summary. maxRounds is the regulation length of the
// match, mp_maxrounds: its first half is the first half of the match, the
// rounds after it are overtime. game is the game's own score to check ours.
func (b *builder) build(maxRounds int, game gameScore) Match {
	m := Match{MaxRounds: maxRounds, Rounds: make([]Round, 0, len(b.rounds))}
	m.Teams[0] = Team{Name: b.names[0], StartSide: CT, Score: b.score[0]}
	m.Teams[1] = Team{Name: b.names[1], StartSide: T, Score: b.score[1]}
	for _, r := range b.rounds {
		m.Rounds = append(m.Rounds, r.Round)
		t := &m.Teams[r.Winner]
		switch {
		case r.Number <= maxRounds/2:
			t.FirstHalf++
		case r.Number <= maxRounds:
			t.SecondHalf++
		default:
			t.Overtime++
		}
	}
	for _, s := range b.players {
		if s.firstRound < 0 {
			continue // never on T or CT: not a player of the match
		}
		p := s.Player
		p.Team = b.teamAt(s.firstRound, s.firstSide)
		p.StartSide = m.Teams[p.Team].StartSide
		m.Players = append(m.Players, p)
	}
	slices.SortFunc(m.Players, func(x, y Player) int {
		return cmp.Or(cmp.Compare(x.Team, y.Team), cmp.Compare(y.Kills, x.Kills), cmp.Compare(x.SteamID64, y.SteamID64))
	})
	b.check(game)
	m.Warnings = b.warnings
	m.Suspicious = len(m.Warnings) > 0
	b.endSightings(b.tick)
	m.Kills, m.Hits, m.Shots, m.Sightings, m.Flashes = b.kills, b.hits, b.shots, b.sightings, b.flashes
	m.Grenades = b.grenades
	return m
}

// check makes sure no round is lost: the rounds played since the start must
// add up to the score, and the score and the number of rounds must be the
// game's own. The game keeps the score by side, so the team on CT in the
// last round has the game's CT score.
func (b *builder) check(game gameScore) {
	if total := b.score[0] + b.score[1]; b.played != total {
		b.warnf("%d rounds were played since the start, the score adds up to %d", b.played, total)
	}
	if b.played != game.Rounds {
		b.warnf("%d rounds were played since the start, the game counted %d", b.played, game.Rounds)
	}
	if len(b.rounds) == 0 {
		return
	}
	ct := b.rounds[len(b.rounds)-1].ctTeam
	if b.score[ct] != game.CT || b.score[1-ct] != game.T {
		b.warnf("the score %d:%d (CT:T) differs from the game's %d:%d", b.score[ct], b.score[1-ct], game.CT, game.T)
	}
}
