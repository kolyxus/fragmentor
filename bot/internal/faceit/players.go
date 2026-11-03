package faceit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// GameCS2 is the FACEIT game ID of Counter-Strike 2.
const GameCS2 = "cs2"

// Player is a FACEIT account with its CS2 profile.
type Player struct {
	ID       string // FACEIT player ID, a UUID
	Nickname string
	Country  string // ISO 3166-1 code as FACEIT has it, lower case

	// HasCS2 is false for an account that never played CS2; the fields
	// below are empty then.
	HasCS2     bool
	SteamID64  string
	SkillLevel int // FACEIT level, 1 to 10
	Elo        int
}

type playerJSON struct {
	PlayerID  string `json:"player_id"`
	Nickname  string `json:"nickname"`
	Country   string `json:"country"`
	SteamID64 string `json:"steam_id_64"`
	Games     map[string]struct {
		Elo          int    `json:"faceit_elo"`
		GamePlayerID string `json:"game_player_id"`
		SkillLevel   int    `json:"skill_level"`
	} `json:"games"`
}

func (p playerJSON) player() Player {
	out := Player{ID: p.PlayerID, Nickname: p.Nickname, Country: strings.ToLower(p.Country)}
	if g, ok := p.Games[GameCS2]; ok {
		out.HasCS2 = true
		out.SkillLevel = g.SkillLevel
		out.Elo = g.Elo
		out.SteamID64 = g.GamePlayerID
		if out.SteamID64 == "" {
			out.SteamID64 = p.SteamID64
		}
	}
	return out
}

// PlayerByNickname finds a player by nickname. FACEIT looks nicknames up
// with the letter case as is, so when that finds nothing, the player search,
// which ignores case, is asked for the same nickname in another case.
func (c *Client) PlayerByNickname(ctx context.Context, nickname string) (Player, error) {
	var p playerJSON
	err := c.get(ctx, "find player", "/players", url.Values{"nickname": {nickname}}, &p)
	if err == nil {
		return p.player(), nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Player{}, err
	}

	var found struct {
		Items []struct {
			PlayerID string `json:"player_id"`
			Nickname string `json:"nickname"`
		} `json:"items"`
	}
	query := url.Values{"nickname": {nickname}, "game": {GameCS2}, "limit": {"20"}}
	if err := c.get(ctx, "search players", "/search/players", query, &found); err != nil {
		return Player{}, err
	}
	for _, it := range found.Items {
		if strings.EqualFold(it.Nickname, nickname) {
			return c.Player(ctx, it.PlayerID)
		}
	}
	return Player{}, fmt.Errorf("faceit: find player: %w", ErrNotFound)
}

// Player returns the player with the FACEIT player ID.
func (c *Client) Player(ctx context.Context, playerID string) (Player, error) {
	if !ValidPlayerID(playerID) {
		return Player{}, fmt.Errorf("faceit: get player: invalid player ID")
	}
	var p playerJSON
	if err := c.get(ctx, "get player", "/players/"+playerID, nil, &p); err != nil {
		return Player{}, err
	}
	return p.player(), nil
}

var (
	playerIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	matchIDRe  = regexp.MustCompile(`^(?:[0-9]+-)?[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// ValidPlayerID reports whether id looks like a FACEIT player ID, a UUID.
// IDs go into URL paths, so the client checks every one it gets.
func ValidPlayerID(id string) bool { return playerIDRe.MatchString(id) }

// ValidMatchID reports whether id looks like a FACEIT match ID: a UUID,
// for CS2 matches with a "1-" prefix.
func ValidMatchID(id string) bool { return matchIDRe.MatchString(id) }
