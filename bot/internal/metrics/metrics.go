// Package metrics counts the standard metrics of docs/METRICS.md for every
// player of a match: K/D, ADR, KAST, headshot rates, opening duels, trades,
// clutches and multikills, for the whole match and for each side.
//
// It counts from the events of a demo.Match and knows nothing of the demo
// parser, Telegram or the database. The rules are those of METRICS.md: the
// final K/D/A, headshots and damage as FACEIT counts them, KAST as HLTV
// does, and the coaching rules for the rest.
package metrics

import (
	"math"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// TradeWindow is how long after a death its killer may be killed for a
// trade, the limit included.
const TradeWindow = 5 * time.Second

// defaultTickRate stands in for the tick rate of a demo that tells none:
// 64, as on FACEIT.
const defaultTickRate = 64

// Player is the metrics of a player for the whole match and for each side.
type Player struct {
	SteamID64 uint64 `json:"steam_id64,string"`
	Team      int    `json:"team"` // index in demo.Match.Teams
	Match     Stats  `json:"match"`
	T         Stats  `json:"t"`
	CT        Stats  `json:"ct"`
}

// Stats is the metrics of a player over some rounds: the whole match or the
// rounds of one side. Counts come with the rates made of them; rates are
// fractions from 0 to 1 and are not rounded.
type Stats struct {
	// Rounds is the rounds the player's team played: the base of ADR.
	Rounds int `json:"rounds"`

	// As FACEIT counts them.
	Kills            int     `json:"kills"`
	Deaths           int     `json:"deaths"`
	Assists          int     `json:"assists"`
	HeadshotKills    int     `json:"headshot_kills"`
	Damage           int     `json:"damage"`
	KD               float64 `json:"kd"`                 // kills per death, the kills if no deaths
	ADR              float64 `json:"adr"`                // damage per round
	HeadshotKillRate float64 `json:"headshot_kill_rate"` // HS% by kills

	// RoundsPlayed is the rounds the player was alive in when the freeze
	// time ended and did not leave the server alive after: the base of
	// KAST. KASTRounds is those with a kill, an assist, survival or a
	// traded death.
	RoundsPlayed int     `json:"rounds_played"`
	KASTRounds   int     `json:"kast_rounds"`
	KAST         float64 `json:"kast"`

	// BulletHits is the hits on enemies with guns, shotguns aside, and
	// HeadHits those in the head.
	BulletHits      int     `json:"bullet_hits"`
	HeadHits        int     `json:"head_hits"`
	HeadshotHitRate float64 `json:"headshot_hit_rate"` // HS% by hits

	OpeningAttempts int `json:"opening_attempts"` // the first kill of a round: killed or died in it
	OpeningWins     int `json:"opening_wins"`     // killed in it
	TradeKills      int `json:"trade_kills"`
	TradedDeaths    int `json:"traded_deaths"`
	UntradedDeaths  int `json:"untraded_deaths"`

	// Clutches[x-1] is the 1vX clutches, X from 1 to 5.
	Clutches [5]Clutch `json:"clutches"`
	// MultiKills[n-2] is the rounds with exactly n kills, n from 2 to 5.
	MultiKills [4]int `json:"multi_kills"`
}

// Clutch counts clutches: the rounds the player was the last alive of the
// team, and those of them the team won.
type Clutch struct {
	Attempts int `json:"attempts"`
	Wins     int `json:"wins"`
}

// Compute returns the metrics of the players of m, in the order of
// m.Players. Events outside the rounds of m are left out; a demo.Match from
// demo.Parse has none.
func Compute(m *demo.Match) []Player {
	c := &counter{
		window: tradeTicks(m.TickRate),
		byID:   make(map[uint64]*player, len(m.Players)),
	}
	for _, p := range m.Players {
		pl := &player{id: p.SteamID64, team: p.Team}
		c.players = append(c.players, pl)
		c.byID[p.SteamID64] = pl
	}
	kills := make(map[int][]demo.Kill)
	for _, k := range m.Kills {
		kills[k.Round] = append(kills[k.Round], k)
	}
	hits := make(map[int][]demo.Hit)
	for _, h := range m.Hits {
		hits[h.Round] = append(hits[h.Round], h)
	}
	for _, r := range m.Rounds {
		c.round(r, kills[r.Number], hits[r.Number])
	}
	out := make([]Player, 0, len(c.players))
	for _, p := range c.players {
		p.t.rates()
		p.ct.rates()
		all := p.t.plus(p.ct)
		all.rates()
		out = append(out, Player{SteamID64: p.id, Team: p.team, Match: all, T: p.t, CT: p.ct})
	}
	return out
}

// tradeTicks is TradeWindow in ticks.
func tradeTicks(tickRate float64) int {
	if tickRate <= 0 {
		tickRate = defaultTickRate
	}
	return int(math.Round(TradeWindow.Seconds() * tickRate))
}

type counter struct {
	window  int // TradeWindow in ticks
	players []*player
	byID    map[uint64]*player
}

type player struct {
	id    uint64
	team  int
	t, ct Stats
}

// on returns the stats of p for side.
func (p *player) on(side demo.Side) *Stats {
	if side == demo.CT {
		return &p.ct
	}
	return &p.t
}

// player returns the player of the match r refers to, nil for a bot,
// nobody or a stranger. It is who FACEIT counts: the killer, the assister
// and the attacker are the controller of a bot already, and the victim is
// the body, a bot's for a bot a player controls.
func (c *counter) player(r *demo.Ref) *player {
	if r == nil || r.Bot {
		return nil
	}
	return c.byID[r.ID]
}

// rates makes the rates of s out of its counts.
func (s *Stats) rates() {
	s.KD = float64(s.Kills)
	if s.Deaths > 0 {
		s.KD /= float64(s.Deaths)
	}
	s.ADR = rate(s.Damage, s.Rounds)
	s.HeadshotKillRate = rate(s.HeadshotKills, s.Kills)
	s.KAST = rate(s.KASTRounds, s.RoundsPlayed)
	s.HeadshotHitRate = rate(s.HeadHits, s.BulletHits)
}

func rate(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

// plus returns the counts of s and o together; the rates are left to
// rates.
func (s Stats) plus(o Stats) Stats {
	out := Stats{
		Rounds:          s.Rounds + o.Rounds,
		Kills:           s.Kills + o.Kills,
		Deaths:          s.Deaths + o.Deaths,
		Assists:         s.Assists + o.Assists,
		HeadshotKills:   s.HeadshotKills + o.HeadshotKills,
		Damage:          s.Damage + o.Damage,
		RoundsPlayed:    s.RoundsPlayed + o.RoundsPlayed,
		KASTRounds:      s.KASTRounds + o.KASTRounds,
		BulletHits:      s.BulletHits + o.BulletHits,
		HeadHits:        s.HeadHits + o.HeadHits,
		OpeningAttempts: s.OpeningAttempts + o.OpeningAttempts,
		OpeningWins:     s.OpeningWins + o.OpeningWins,
		TradeKills:      s.TradeKills + o.TradeKills,
		TradedDeaths:    s.TradedDeaths + o.TradedDeaths,
		UntradedDeaths:  s.UntradedDeaths + o.UntradedDeaths,
	}
	for i := range out.Clutches {
		out.Clutches[i] = Clutch{
			Attempts: s.Clutches[i].Attempts + o.Clutches[i].Attempts,
			Wins:     s.Clutches[i].Wins + o.Clutches[i].Wins,
		}
	}
	for i := range out.MultiKills {
		out.MultiKills[i] = s.MultiKills[i] + o.MultiKills[i]
	}
	return out
}
