// Package metrics counts the metrics of docs/METRICS.md for every player of
// a match, for the whole match and for each side: the standard ones (K/D,
// ADR, KAST, headshot rates, opening duels, trades, clutches, multikills),
// the shooting (accuracy, the first shot, the kill time, shots on the move)
// and the grenades (flashes, grenade damage, grenades unused at death).
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

// KillTimeGap is the longest pause between two hits of one duel for the
// kill time, the limit included.
const KillTimeGap = 1500 * time.Millisecond

// MovingShare is the share of the top speed with the gun above which a
// shot is on the move: the game makes a gun inaccurate from 34% of it.
const MovingShare = 0.34

// MinFlash is how long a flash has to blind a player to count.
const MinFlash = time.Second

// MinKillTimeDuels is how many duels the kill time of a player needs before
// the bot shows it: one match gives a player a handful, see
// docs/METRICS.md. The duels of rifles and of sniper rifles count apart.
const MinKillTimeDuels = 20

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
	// The same with rifles alone, sniper rifles aside, and with pistols.
	RifleHits          int     `json:"rifle_hits"`
	RifleHeadHits      int     `json:"rifle_head_hits"`
	RifleHeadshotRate  float64 `json:"rifle_headshot_rate"`
	PistolHits         int     `json:"pistol_hits"`
	PistolHeadHits     int     `json:"pistol_head_hits"`
	PistolHeadshotRate float64 `json:"pistol_headshot_rate"`

	OpeningAttempts int `json:"opening_attempts"` // the first kill of a round: killed or died in it
	OpeningWins     int `json:"opening_wins"`     // killed in it
	TradeKills      int `json:"trade_kills"`
	TradedDeaths    int `json:"traded_deaths"`
	UntradedDeaths  int `json:"untraded_deaths"`

	// Clutches[x-1] is the 1vX clutches, X from 1 to 5.
	Clutches [5]Clutch `json:"clutches"`
	// MultiKills[n-2] is the rounds with exactly n kills, n from 2 to 5.
	MultiKills [4]int `json:"multi_kills"`

	// Shooting. "In sight" is while the player sees a live enemy, the
	// game's spotted flag.
	//
	// AccuracyShots is the rifle shots in sight, sniper rifles aside;
	// AccuracyHits those of them that hit an enemy.
	AccuracyShots int     `json:"accuracy_shots"`
	AccuracyHits  int     `json:"accuracy_hits"`
	Accuracy      float64 `json:"accuracy"`
	// FirstShotFights is the fights, the stretches of seeing an enemy, in
	// which the player hit that enemy and fired a rifle first;
	// FirstShotHits those in which the first shot hit him.
	FirstShotFights   int     `json:"first_shot_fights"`
	FirstShotHits     int     `json:"first_shot_hits"`
	FirstShotAccuracy float64 `json:"first_shot_accuracy"`
	// The kill time: the duels of rifles and of sniper rifles, the seconds
	// they took together and on average. The average is 0 without duels.
	RifleKillDuels    int     `json:"rifle_kill_duels"`
	RifleKillSeconds  float64 `json:"rifle_kill_seconds"`
	RifleKillTime     float64 `json:"rifle_kill_time"`
	SniperKillDuels   int     `json:"sniper_kill_duels"`
	SniperKillSeconds float64 `json:"sniper_kill_seconds"`
	SniperKillTime    float64 `json:"sniper_kill_time"`
	// MovingBase is the shots of rifles and sniper rifles in sight;
	// MovingShots those of them faster than MovingShare of the top speed.
	MovingBase     int     `json:"moving_base"`
	MovingShots    int     `json:"moving_shots"`
	MovingShotRate float64 `json:"moving_shot_rate"`

	// Grenades. EnemiesFlashed and TeammatesFlashed count the blindings
	// of MinFlash or longer by the player's flashbangs; EnemyBlindSeconds is
	// how long those of the enemies took together, EnemyBlindTime on
	// average.
	EnemiesFlashed    int     `json:"enemies_flashed"`
	TeammatesFlashed  int     `json:"teammates_flashed"`
	EnemyBlindSeconds float64 `json:"enemy_blind_seconds"`
	EnemyBlindTime    float64 `json:"enemy_blind_time"`
	// GrenadeDamage is the health HE grenades and fire took from enemies.
	GrenadeDamage int `json:"grenade_damage"`
	// DeathsWithGrenades is the deaths by enemies with grenades left;
	// UnusedGrenadeValue what those grenades cost, in dollars of the game.
	DeathsWithGrenades int `json:"deaths_with_grenades"`
	UnusedGrenadeValue int `json:"unused_grenade_value"`
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
	rate := m.TickRate
	if rate <= 0 {
		rate = defaultTickRate
	}
	c := &counter{
		tickRate: rate,
		window:   ticks(TradeWindow, rate),
		gap:      ticks(KillTimeGap, rate),
		byID:     make(map[uint64]*player, len(m.Players)),
	}
	for _, p := range m.Players {
		pl := &player{id: p.SteamID64, team: p.Team}
		c.players = append(c.players, pl)
		c.byID[p.SteamID64] = pl
	}
	var e roundEvents
	for _, r := range m.Rounds {
		c.round(r, e.of(m, r.Number))
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
	return ticks(TradeWindow, tickRate)
}

// ticks is d in ticks of the tick rate.
func ticks(d time.Duration, tickRate float64) int {
	return int(math.Round(d.Seconds() * tickRate))
}

// roundEvents is the events of a match by round.
type roundEvents struct {
	kills     map[int][]demo.Kill
	hits      map[int][]demo.Hit
	shots     map[int][]demo.Shot
	sightings map[int][]demo.Sighting
	flashes   map[int][]demo.Flash
}

// events is what happened in one round, in the order of the demo.
type events struct {
	kills     []demo.Kill
	hits      []demo.Hit
	shots     []demo.Shot
	sightings []demo.Sighting
	flashes   []demo.Flash
}

// of returns the events of round n of m, sorting the events of m by round
// the first time.
func (e *roundEvents) of(m *demo.Match, n int) events {
	if e.kills == nil {
		e.kills = byRound(m.Kills, func(k demo.Kill) int { return k.Round })
		e.hits = byRound(m.Hits, func(h demo.Hit) int { return h.Round })
		e.shots = byRound(m.Shots, func(s demo.Shot) int { return s.Round })
		e.sightings = byRound(m.Sightings, func(s demo.Sighting) int { return s.Round })
		e.flashes = byRound(m.Flashes, func(f demo.Flash) int { return f.Round })
	}
	return events{e.kills[n], e.hits[n], e.shots[n], e.sightings[n], e.flashes[n]}
}

func byRound[E any](list []E, round func(E) int) map[int][]E {
	out := make(map[int][]E)
	for _, e := range list {
		out[round(e)] = append(out[round(e)], e)
	}
	return out
}

type counter struct {
	tickRate float64
	window   int // TradeWindow in ticks
	gap      int // KillTimeGap in ticks
	players  []*player
	byID     map[uint64]*player
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
	s.RifleHeadshotRate = rate(s.RifleHeadHits, s.RifleHits)
	s.PistolHeadshotRate = rate(s.PistolHeadHits, s.PistolHits)
	s.Accuracy = rate(s.AccuracyHits, s.AccuracyShots)
	s.FirstShotAccuracy = rate(s.FirstShotHits, s.FirstShotFights)
	s.RifleKillTime = mean(s.RifleKillSeconds, s.RifleKillDuels)
	s.SniperKillTime = mean(s.SniperKillSeconds, s.SniperKillDuels)
	s.MovingShotRate = rate(s.MovingShots, s.MovingBase)
	s.EnemyBlindTime = mean(s.EnemyBlindSeconds, s.EnemiesFlashed)
}

func rate(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

func mean(sum float64, of int) float64 {
	if of == 0 {
		return 0
	}
	return sum / float64(of)
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
		RifleHits:       s.RifleHits + o.RifleHits,
		RifleHeadHits:   s.RifleHeadHits + o.RifleHeadHits,
		PistolHits:      s.PistolHits + o.PistolHits,
		PistolHeadHits:  s.PistolHeadHits + o.PistolHeadHits,
		OpeningAttempts: s.OpeningAttempts + o.OpeningAttempts,
		OpeningWins:     s.OpeningWins + o.OpeningWins,
		TradeKills:      s.TradeKills + o.TradeKills,
		TradedDeaths:    s.TradedDeaths + o.TradedDeaths,
		UntradedDeaths:  s.UntradedDeaths + o.UntradedDeaths,

		AccuracyShots:     s.AccuracyShots + o.AccuracyShots,
		AccuracyHits:      s.AccuracyHits + o.AccuracyHits,
		FirstShotFights:   s.FirstShotFights + o.FirstShotFights,
		FirstShotHits:     s.FirstShotHits + o.FirstShotHits,
		RifleKillDuels:    s.RifleKillDuels + o.RifleKillDuels,
		RifleKillSeconds:  s.RifleKillSeconds + o.RifleKillSeconds,
		SniperKillDuels:   s.SniperKillDuels + o.SniperKillDuels,
		SniperKillSeconds: s.SniperKillSeconds + o.SniperKillSeconds,
		MovingBase:        s.MovingBase + o.MovingBase,
		MovingShots:       s.MovingShots + o.MovingShots,

		EnemiesFlashed:     s.EnemiesFlashed + o.EnemiesFlashed,
		TeammatesFlashed:   s.TeammatesFlashed + o.TeammatesFlashed,
		EnemyBlindSeconds:  s.EnemyBlindSeconds + o.EnemyBlindSeconds,
		GrenadeDamage:      s.GrenadeDamage + o.GrenadeDamage,
		DeathsWithGrenades: s.DeathsWithGrenades + o.DeathsWithGrenades,
		UnusedGrenadeValue: s.UnusedGrenadeValue + o.UnusedGrenadeValue,
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
