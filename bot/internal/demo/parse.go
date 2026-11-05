package demo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime/debug"
	"strconv"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs/msg"
	"github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs/option"
)

// msgQueueSize is the parser's queue of net messages between the goroutine
// that reads the demo and the one that handles it. The library sizes it by
// the ticks of the demo, up to 500 000; on FACEIT demos 1000 parses as fast
// and lowers the peak memory from 103 to 60 MiB. 0, no queue, is 3 times
// slower.
const msgQueueSize = 1000

// defaultMaxRounds is mp_maxrounds, the regulation length of a match, when
// the demo does not tell it: 24, as on FACEIT.
const defaultMaxRounds = 24

// ParseFile parses the .dem.zst file at path, see Parse.
func ParseFile(ctx context.Context, path string, opts Options) (*Match, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("demo: %w", err)
	}
	defer f.Close()
	return Parse(ctx, f, opts)
}

// Parse decompresses the .dem.zst stream r on the fly and returns the summary
// of the match in it. ctx bounds the time it takes. The errors wrap
// ErrNotZstd, ErrTooLarge, ErrNotDemo, ErrTruncated or ErrNoRounds where they
// apply and the error of ctx when it is done; a panic of the parser comes
// back as a *PanicError.
func Parse(ctx context.Context, r io.Reader, opts Options) (*Match, error) {
	s, err := decompress(r, opts.maxSize())
	if err != nil {
		return nil, err
	}
	defer s.close()
	var m *Match
	err = catch(func() (err error) {
		m, err = parse(ctx, s)
		return err
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// catch runs f and turns its panic into a *PanicError. demoinfocs returns
// its own failures as errors, but a panic of an event handler it raises
// again on this goroutine as *demoinfocs.PanicError.
func catch(f func() error) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		pe := &PanicError{Value: r}
		if lib, ok := r.(*demoinfocs.PanicError); ok {
			pe.Value, pe.Stack = lib.Value, lib.Stack
		} else {
			pe.Stack = debug.Stack()
		}
		err = pe
	}()
	return f()
}

func parse(ctx context.Context, r io.Reader) (m *Match, err error) {
	p := demoinfocs.NewParser(r, option.MsgQueueBufferSize(msgQueueSize))
	defer func() {
		if cerr := p.Close(); cerr != nil && err == nil {
			m, err = nil, fmt.Errorf("demo: close parser: %w", cerr)
		}
	}()
	c := newCollector(p)
	if err := p.ParseToEnd(ctx); err != nil {
		return nil, parseError(err)
	}
	return c.match()
}

func parseError(err error) error {
	switch {
	case errors.Is(err, demoinfocs.ErrInvalidFileType):
		return fmt.Errorf("%w: %w", ErrNotDemo, err)
	case errors.Is(err, demoinfocs.ErrUnexpectedEndOfDemo):
		return fmt.Errorf("%w: %w", ErrTruncated, err)
	}
	return fmt.Errorf("demo: parse: %w", err)
}

// collector turns the events of demoinfocs into the builder's events. All
// its handlers run on the parser's goroutine that handles messages, and it is
// read only after ParseToEnd has returned.
type collector struct {
	p       *demoinfocs.Parser
	gs      *demoinfocs.GameState
	b       *builder
	mapName string

	// The library dispatches its Kill and PlayerHurt right before the raw
	// game event they are made of, in the same tick. The raw event names
	// the victim's pawn, which the library ignores; see victim.
	kill     *events.Kill
	killTick int
	hurt     *events.PlayerHurt
	hurtTick int
	unpaired int // Kill or PlayerHurt without its raw event, or the reverse

	// lastHealth is the health a victim was left with by their last hit,
	// with the tick of the hit.
	lastHealth map[*common.Player]tickHealth

	// A player who leaves the server alive dies by their own hand with the
	// weapon World, and the raw player_disconnect of the same user ID comes
	// in the same tick: before the death on FACEIT demos. left holds the
	// user ID and the tick of the last such event, selfKill the last death
	// by one's own hand in the log, for the other order.
	left     userTick
	selfKill struct {
		userTick
		index int // in the builder's log
	}
}

type tickHealth struct{ tick, health int }

type userTick struct{ user, tick int }

func newCollector(p *demoinfocs.Parser) *collector {
	c := &collector{p: p, gs: p.GameState(), b: newBuilder(), lastHealth: make(map[*common.Player]tickHealth)}
	c.left.tick, c.selfKill.tick = -1, -1
	p.RegisterNetMessageHandler(func(m *msg.CDemoFileHeader) { c.mapName = m.GetMapName() })
	// FACEIT demos start with a round before the warmup, then the warmup,
	// then the match starts: begin_new_match, the MatchStart event. The end
	// of a warmup resets too, in case a demo has no MatchStart.
	p.RegisterEventHandler(func(events.MatchStart) { c.reset() })
	p.RegisterEventHandler(func(e events.IsWarmupPeriodChanged) {
		if e.OldIsWarmupPeriod && !e.NewIsWarmupPeriod {
			c.reset()
		}
	})
	p.RegisterEventHandler(func(events.RoundStart) {
		if c.live() {
			c.b.roundStart()
		}
	})
	p.RegisterEventHandler(c.onFreezeTimeEnd)
	p.RegisterEventHandler(c.onRoundEnd)
	p.RegisterEventHandler(func(e events.Kill) { c.kill, c.killTick = &e, c.gs.IngameTick() })
	p.RegisterEventHandler(func(e events.PlayerHurt) { c.hurt, c.hurtTick = &e, c.gs.IngameTick() })
	p.RegisterEventHandler(c.onGameEvent)
	return c
}

func (c *collector) reset() {
	c.b.reset()
	c.unpaired = 0
	c.left.tick, c.selfKill.tick = -1, -1
}

// live reports whether the match is on: started and not in a warmup. What
// came before the last start the reset drops; after the end of the match
// the game no longer counts it as started.
func (c *collector) live() bool {
	return c.gs.IsMatchStarted() && !c.gs.IsWarmupPeriod()
}

func (c *collector) onGameEvent(e events.GenericGameEvent) {
	tick := c.gs.IngameTick()
	switch e.Name {
	case "player_death":
		k := c.kill
		c.kill = nil
		if k == nil || c.killTick != tick {
			c.noteUnpaired()
			return
		}
		if !c.live() {
			return
		}
		ev := killEvent{
			Tick:        tick,
			Killer:      ref(k.Killer),
			Victim:      c.body(k.Victim, e.Data),
			Assister:    ref(k.Assister),
			FlashAssist: k.AssistedFlash,
			Headshot:    k.IsHeadshot,
			Weapon:      weaponName(k.Weapon),
			Class:       weaponClass(k.Weapon),
		}
		self := k.Victim != nil && k.Killer == k.Victim && ev.Class == ClassWorld
		if self {
			ev.Disconnect = c.left == userTick{k.Victim.UserID, tick}
		}
		c.b.kill(ev)
		if self && ev.Victim != nil {
			c.selfKill.userTick, c.selfKill.index = userTick{k.Victim.UserID, tick}, len(c.b.kills)-1
		}
	case "player_hurt":
		h := c.hurt
		c.hurt = nil
		if h == nil || c.hurtTick != tick {
			c.noteUnpaired()
			return
		}
		victim := c.victim(h.Player, e.Data)
		// Measured for every hit, live or not, to follow the hits of a tick.
		damage := c.healthTaken(victim, h.Health, h.HealthDamage)
		if c.live() {
			c.b.hurt(hurtEvent{
				Tick:     tick,
				Attacker: ref(h.Attacker),
				Victim:   c.bodyRef(victim, h.Player),
				Damage:   damage,
				HitGroup: hitGroup(h.HitGroup),
				Weapon:   weaponName(h.Weapon),
				Class:    weaponClass(h.Weapon),
			})
		}
	case "player_disconnect":
		if c.live() {
			c.onDisconnect(tick, e.Data)
		}
	}
}

// onDisconnect notes a player or a bot leaving the server, for the death by
// their own hand the game makes of it when they leave alive.
func (c *collector) onDisconnect(tick int, data map[string]*msg.CMsgSource1LegacyGameEventKeyT) {
	v := data["userid"]
	if v == nil {
		return
	}
	// As the library reads user IDs: the slot in the low byte.
	user := int(v.GetValShort())
	if user <= math.MaxUint16 {
		user &= 0xff
	}
	c.left = userTick{user, tick}
	if c.selfKill.userTick == c.left {
		c.b.leftServer(c.selfKill.index)
	}
}

// body returns the ref of whose body an event happened to: the player
// whose pawn the raw event names, see victim. A bot that a player controls
// gets that player as its Controller.
func (c *collector) body(lib *common.Player, data map[string]*msg.CMsgSource1LegacyGameEventKeyT) *playerRef {
	return c.bodyRef(c.victim(lib, data), lib)
}

// bodyRef returns the ref of the body p; lib is the player the library
// named, the controller of a bot it differs from.
func (c *collector) bodyRef(p, lib *common.Player) *playerRef {
	r := ref(p)
	if r != nil && p != lib && p.IsBot && lib != nil && !lib.IsBot {
		r.Controller = lib.SteamID64
	}
	return r
}

func (c *collector) noteUnpaired() {
	if c.live() {
		c.unpaired++
	}
}

// victim returns the player whose pawn the raw event names, or lib, the
// library's victim, if it names none. The library takes the victim from the
// userid of the event, the controller: when a dead player takes over a bot,
// the death of the bot and the hits on it name that player. The game counts
// them for the bot, and so does FACEIT; seen on a FACEIT demo with a bot
// that played for a player who left.
func (c *collector) victim(lib *common.Player, data map[string]*msg.CMsgSource1LegacyGameEventKeyT) *common.Player {
	if v := data["userid_pawn"]; v != nil {
		if p := c.gs.Participants().FindByPawnHandle(uint64(v.GetValLong())); p != nil {
			return p
		}
	}
	return lib
}

// healthTaken is the health a hit took: the victim's health before it minus
// after it. The library's HealthDamageTaken is the same, but measured on its
// victim, the wrong one after a bot takeover. Entities update after the
// events of a tick, so Health is the health at the start of the tick, and a
// second hit in the same tick starts from what the first one left.
func (c *collector) healthTaken(victim *common.Player, after, dmg int) int {
	if victim == nil {
		return min(dmg, 100)
	}
	tick := c.gs.IngameTick()
	before := victim.Health()
	if last, ok := c.lastHealth[victim]; ok && last.tick == tick {
		before = last.health
	}
	c.lastHealth[victim] = tickHealth{tick: tick, health: after}
	return max(before-after, 0)
}

// onFreezeTimeEnd notes who is alive as the round gets going: the players
// and the bots on T and CT.
func (c *collector) onFreezeTimeEnd(events.RoundFreezetimeEnd) {
	if !c.live() {
		return
	}
	var alive []playerRef
	for _, p := range c.gs.Participants().Playing() {
		if r := ref(p); r.Side != "" && p.IsAlive() {
			alive = append(alive, *r)
		}
	}
	c.b.freezeTimeEnd(c.gs.IngameTick(), alive)
}

func (c *collector) onRoundEnd(e events.RoundEnd) {
	if !c.live() {
		return
	}
	ev := roundEndEvent{
		Tick:   c.gs.IngameTick(),
		Winner: side(e.Winner),
		Reason: reason(e.Reason),
		Names: map[Side]string{
			CT: c.gs.TeamCounterTerrorists().ClanName(),
			T:  c.gs.TeamTerrorists().ClanName(),
		},
	}
	for _, p := range c.gs.Participants().Playing() {
		if r := ref(p); r.human() && r.Side != "" {
			ev.Players = append(ev.Players, *r)
		}
	}
	c.b.roundEnd(ev)
}

func (c *collector) match() (*Match, error) {
	maxRounds := defaultMaxRounds
	if n, err := strconv.Atoi(c.gs.Rules().ConVars()["mp_maxrounds"]); err == nil && n > 0 {
		maxRounds = n
	}
	if c.unpaired > 0 {
		c.b.warnf("%d kills or hits came without their raw game event and were skipped", c.unpaired)
	}
	m := c.b.build(maxRounds, gameScore{
		CT:     c.gs.TeamCounterTerrorists().Score(),
		T:      c.gs.TeamTerrorists().Score(),
		Rounds: c.gs.TotalRoundsPlayed(),
	})
	if len(m.Rounds) == 0 {
		return nil, ErrNoRounds
	}
	m.Map = c.mapName
	m.TickRate = c.p.TickRate()
	return &m, nil
}

func ref(p *common.Player) *playerRef {
	if p == nil {
		return nil
	}
	id := p.SteamID64
	if p.IsBot {
		// Bots have no SteamID; the user ID tells them apart, plus one so
		// that no bot is 0.
		id = uint64(p.UserID) + 1
	}
	return &playerRef{ID: id, Name: p.Name, Side: side(p.Team), Bot: p.IsBot}
}

func weaponName(w *common.Equipment) string {
	if w == nil {
		return ""
	}
	return w.Type.String()
}

// weaponClass tells the kind of the weapon. The library's classes put
// shotguns and machine guns together, snipers among rifles, the knife and
// the zeus among the equipment and fire among the grenades.
func weaponClass(w *common.Equipment) WeaponClass {
	if w == nil {
		return ClassOther
	}
	switch w.Type {
	case common.EqSawedOff, common.EqNova, common.EqSwag7, common.EqXM1014:
		return ClassShotgun
	case common.EqM249, common.EqNegev:
		return ClassMachineGun
	case common.EqSSG08, common.EqAWP, common.EqScar20, common.EqG3SG1:
		return ClassSniper
	case common.EqKnife, common.EqFists, common.EqAxe, common.EqHammer, common.EqWrench:
		return ClassKnife
	case common.EqZeus:
		return ClassZeus
	case common.EqMolotov, common.EqIncendiary:
		return ClassFire
	case common.EqBomb:
		return ClassBomb
	case common.EqWorld:
		return ClassWorld
	}
	switch w.Type.Class() {
	case common.EqClassPistols:
		return ClassPistol
	case common.EqClassSMG:
		return ClassSMG
	case common.EqClassRifle:
		return ClassRifle
	case common.EqClassGrenade:
		return ClassGrenade
	}
	return ClassOther
}

func hitGroup(g events.HitGroup) HitGroup {
	switch g {
	case events.HitGroupHead:
		return HitHead
	case events.HitGroupChest:
		return HitChest
	case events.HitGroupStomach:
		return HitStomach
	case events.HitGroupLeftArm:
		return HitLeftArm
	case events.HitGroupRightArm:
		return HitRightArm
	case events.HitGroupLeftLeg:
		return HitLeftLeg
	case events.HitGroupRightLeg:
		return HitRightLeg
	case events.HitGroupNeck:
		return HitNeck
	case events.HitGroupGear:
		return HitGear
	}
	return HitGeneric
}

func side(t common.Team) Side {
	switch t {
	case common.TeamTerrorists:
		return T
	case common.TeamCounterTerrorists:
		return CT
	}
	return ""
}

func reason(r events.RoundEndReason) string {
	switch r {
	case events.RoundEndReasonTargetBombed:
		return "bomb_exploded"
	case events.RoundEndReasonBombDefused:
		return "bomb_defused"
	case events.RoundEndReasonCTWin:
		return "t_eliminated"
	case events.RoundEndReasonTerroristsWin:
		return "ct_eliminated"
	case events.RoundEndReasonTargetSaved:
		return "time_ran_out"
	case events.RoundEndReasonTerroristsSurrender:
		return "t_surrendered"
	case events.RoundEndReasonCTSurrender:
		return "ct_surrendered"
	}
	return "reason_" + strconv.Itoa(int(r))
}
