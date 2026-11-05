package demo

import (
	"context"
	"errors"
	"fmt"
	"io"
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
}

type tickHealth struct{ tick, health int }

func newCollector(p *demoinfocs.Parser) *collector {
	c := &collector{p: p, gs: p.GameState(), b: newBuilder(), lastHealth: make(map[*common.Player]tickHealth)}
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
	p.RegisterEventHandler(c.onRoundEnd)
	p.RegisterEventHandler(func(e events.Kill) { c.kill, c.killTick = &e, c.gs.IngameTick() })
	p.RegisterEventHandler(func(e events.PlayerHurt) { c.hurt, c.hurtTick = &e, c.gs.IngameTick() })
	p.RegisterEventHandler(c.onGameEvent)
	return c
}

func (c *collector) reset() {
	c.b.reset()
	c.unpaired = 0
}

// live reports whether the match is on: started and not in a warmup. What
// came before the last start the reset drops; after the end of the match
// the game no longer counts it as started.
func (c *collector) live() bool {
	return c.gs.IsMatchStarted() && !c.gs.IsWarmupPeriod()
}

func (c *collector) onGameEvent(e events.GenericGameEvent) {
	switch e.Name {
	case "player_death":
		k := c.kill
		c.kill = nil
		if k == nil || c.killTick != c.gs.IngameTick() {
			c.noteUnpaired()
			return
		}
		if c.live() {
			c.b.kill(killEvent{
				Killer:   ref(k.Killer),
				Victim:   ref(c.victim(k.Victim, e.Data)),
				Assister: ref(k.Assister),
				Headshot: k.IsHeadshot,
			})
		}
	case "player_hurt":
		h := c.hurt
		c.hurt = nil
		if h == nil || c.hurtTick != c.gs.IngameTick() {
			c.noteUnpaired()
			return
		}
		victim := c.victim(h.Player, e.Data)
		// Measured for every hit, live or not, to follow the hits of a tick.
		damage := c.healthTaken(victim, h.Health, h.HealthDamage)
		if c.live() {
			c.b.hurt(hurtEvent{Attacker: ref(h.Attacker), Victim: ref(victim), Damage: damage})
		}
	}
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
	return &playerRef{ID: p.SteamID64, Name: p.Name, Side: side(p.Team), Bot: p.IsBot}
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
