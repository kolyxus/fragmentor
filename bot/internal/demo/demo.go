// Package demo turns a CS2 demo of a FACEIT match into a summary: the map,
// the rounds with the score after each, and every player's kills, deaths,
// assists, headshots and damage. With the summary come the events the
// metrics are counted from: every kill, hit, shot and flash, who saw whom
// and when, and who was alive when each round got going.
//
// It is the only package that imports demoinfocs-golang, so a change of the
// library version stays here; see docs/notes/demoinfocs.md. It knows nothing
// of Telegram or the database.
package demo

import (
	"errors"
	"fmt"
)

// Errors a caller may want to tell the user about. Errors of Parse wrap them.
var (
	// ErrNotZstd means the input is not a zstd stream, so not a .dem.zst.
	ErrNotZstd = errors.New("demo: not a zstd stream")
	// ErrTooLarge means the decompressed demo is larger than the limit.
	ErrTooLarge = errors.New("demo: decompressed demo is too large")
	// ErrNotDemo means the decompressed data is not a CS2 demo.
	ErrNotDemo = errors.New("demo: not a CS2 demo")
	// ErrTruncated means the demo ends before its end, as a partly
	// downloaded one does.
	ErrTruncated = errors.New("demo: demo is truncated")
	// ErrNoRounds means the demo has no round of a match, only a warmup
	// for example.
	ErrNoRounds = errors.New("demo: no rounds of a match in the demo")
)

// PanicError is a panic of the parser or of our event handlers, recovered by
// Parse. Error does not include the stack, which is in Stack.
type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("demo: parser panicked: %v", e.Value)
}

// Options tune Parse. The zero value is the defaults.
type Options struct {
	// MaxSize bounds the decompressed demo in bytes, DefaultMaxSize if 0.
	MaxSize int64
}

// DefaultMaxSize is the default bound of a decompressed demo. FACEIT demos
// of 19 and 28 rounds take 233 and 339 MiB, about 12 MiB per round, so even
// a match with several overtimes stays below.
const DefaultMaxSize int64 = 1 << 30

func (o Options) maxSize() int64 {
	if o.MaxSize > 0 {
		return o.MaxSize
	}
	return DefaultMaxSize
}

// Side is a side of the game.
type Side string

// The two sides.
const (
	T  Side = "T"
	CT Side = "CT"
)

// Match is the summary of a demo. Only the match itself counts: what happens
// before its last start, during a warmup and after its end is left out.
type Match struct {
	Map      string  `json:"map"`       // like "de_ancient"
	TickRate float64 `json:"tick_rate"` // server ticks per second, 64 on FACEIT
	// Teams keep their index when they switch sides: Teams[0] is the team
	// that started the match as CT.
	Teams   [2]Team  `json:"teams"`
	Rounds  []Round  `json:"rounds"`
	Players []Player `json:"players"` // by team, then by kills
	// Suspicious means the summary may be wrong: the rounds played since
	// the start do not add up to the score, or the score or the number of
	// rounds differs from the game's own, or the like. Rounds are never
	// lost silently. Warnings tell why.
	Suspicious bool `json:"suspicious"`
	// Warnings tell what in the demo looked wrong. They never hold names
	// or IDs.
	Warnings []string `json:"warnings,omitempty"`
	// Kills, Hits, Shots, Sightings and Flashes are the events of the
	// match, see events.go.
	Kills     []Kill     `json:"kills,omitempty"`
	Hits      []Hit      `json:"hits,omitempty"`
	Shots     []Shot     `json:"shots,omitempty"`
	Sightings []Sighting `json:"sightings,omitempty"`
	Flashes   []Flash    `json:"flashes,omitempty"`
}

// Team is one of the two teams of a match.
type Team struct {
	// Name is the clan name the server set, on FACEIT "team_<captain>".
	// It may be empty.
	Name       string `json:"name"`
	StartSide  Side   `json:"start_side"`
	Score      int    `json:"score"`
	FirstHalf  int    `json:"first_half"`
	SecondHalf int    `json:"second_half"`
	Overtime   int    `json:"overtime"` // rounds won in all overtimes
}

// Round is a played round.
type Round struct {
	Number     int    `json:"number"` // from 1
	Winner     int    `json:"winner"` // index in Match.Teams
	WinnerSide Side   `json:"winner_side"`
	Reason     string `json:"reason"` // how it was won, like "bomb_defused"
	Score      [2]int `json:"score"`  // of Match.Teams after the round
	EndTick    int    `json:"end_tick"`
	// FreezeEndTick is when the freeze time ended and the round got going.
	FreezeEndTick int `json:"freeze_end_tick"`
	// Alive is who was alive then, the players and the bots of both sides.
	Alive []Ref `json:"alive,omitempty"`
}

// Side returns the side that team, an index in Match.Teams, played the
// round on.
func (r Round) Side(team int) Side {
	if (team == r.Winner) == (r.WinnerSide == CT) {
		return CT
	}
	return T
}

// Player is a player of the match and their scoreboard. Bots are not
// players.
type Player struct {
	SteamID64 uint64 `json:"steam_id64,string"`
	Name      string `json:"name"` // in-game name; on FACEIT the FACEIT nickname
	Team      int    `json:"team"` // index in Match.Teams
	StartSide Side   `json:"start_side"`
	Kills     int    `json:"kills"`
	Deaths    int    `json:"deaths"`
	Assists   int    `json:"assists"`
	Headshots int    `json:"headshots"` // kills with a headshot
	Damage    int    `json:"damage"`    // health taken from enemies
}
