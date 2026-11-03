package faceit

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HistoryMatch is a match in a player's history.
type HistoryMatch struct {
	ID         string
	Status     string // "finished" for a played match; lower case in the history
	StartedAt  time.Time
	FinishedAt time.Time
	Winner     string         // the faction that won, "faction1" or "faction2"
	Score      map[string]int // rounds won by each faction
	Factions   map[string]string
}

// FactionOf returns the faction of the player in the match.
func (m HistoryMatch) FactionOf(playerID string) (string, bool) {
	f, ok := m.Factions[playerID]
	return f, ok
}

type historyJSON struct {
	Items []struct {
		MatchID    string `json:"match_id"`
		Status     string `json:"status"`
		StartedAt  int64  `json:"started_at"`
		FinishedAt int64  `json:"finished_at"`
		Results    struct {
			Winner string         `json:"winner"`
			Score  map[string]int `json:"score"`
		} `json:"results"`
		Teams map[string]struct {
			Players []struct {
				PlayerID string `json:"player_id"`
			} `json:"players"`
		} `json:"teams"`
	} `json:"items"`
}

// History returns up to limit (1 to 100) latest CS2 matches of the player,
// newest first. Without the from parameter FACEIT returns matches of any age,
// not just of the last month as its docs say.
func (c *Client) History(ctx context.Context, playerID string, limit int) ([]HistoryMatch, error) {
	if !ValidPlayerID(playerID) {
		return nil, fmt.Errorf("faceit: get history: invalid player ID")
	}
	query := url.Values{"game": {GameCS2}, "limit": {strconv.Itoa(min(max(limit, 1), 100))}}
	var h historyJSON
	if err := c.get(ctx, "get history", "/players/"+playerID+"/history", query, &h); err != nil {
		return nil, err
	}
	out := make([]HistoryMatch, 0, len(h.Items))
	for _, it := range h.Items {
		m := HistoryMatch{
			ID:         it.MatchID,
			Status:     it.Status,
			StartedAt:  unixTime(it.StartedAt),
			FinishedAt: unixTime(it.FinishedAt),
			Winner:     it.Results.Winner,
			Score:      it.Results.Score,
			Factions:   map[string]string{},
		}
		for faction, team := range it.Teams {
			for _, p := range team.Players {
				m.Factions[p.PlayerID] = faction
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// Match is the details of a match.
type Match struct {
	ID         string
	Status     string // "FINISHED" for a played match; upper case in the details
	BestOf     int
	StartedAt  time.Time
	FinishedAt time.Time
	Map        string // the map played, like "de_mirage"; empty if FACEIT has none
	MapName    string // its name to show, like "Mirage"
	Winner     string // the faction that won, "faction1" or "faction2"
	Teams      []Team // faction1, then faction2
	DemoURLs   []string
}

// Team is a side of a match.
type Team struct {
	Faction string // "faction1" or "faction2"
	ID      string // the faction ID; the match stats call it team_id
	Name    string // like "team_<nickname of the leader>"
	Score   int    // rounds won, in a best-of-one match
	Players []MatchPlayer
}

// MatchPlayer is a player of a match.
type MatchPlayer struct {
	ID         string
	Nickname   string
	SteamID64  string
	SkillLevel int // FACEIT level at the time of the match
}

type matchJSON struct {
	MatchID    string   `json:"match_id"`
	Status     string   `json:"status"`
	BestOf     int      `json:"best_of"`
	StartedAt  int64    `json:"started_at"`
	FinishedAt int64    `json:"finished_at"`
	DemoURL    []string `json:"demo_url"`
	Results    struct {
		Winner string         `json:"winner"`
		Score  map[string]int `json:"score"`
	} `json:"results"`
	Teams map[string]struct {
		FactionID string `json:"faction_id"`
		Name      string `json:"name"`
		Roster    []struct {
			PlayerID       string `json:"player_id"`
			Nickname       string `json:"nickname"`
			GamePlayerID   string `json:"game_player_id"`
			GameSkillLevel int    `json:"game_skill_level"`
		} `json:"roster"`
	} `json:"teams"`
	// Voting has no schema in the API docs, so it is read on its own and
	// a surprise in it does not break the rest.
	Voting json.RawMessage `json:"voting"`
}

type votingJSON struct {
	Map struct {
		Pick     []string `json:"pick"`
		Entities []struct {
			Name      string `json:"name"`
			ClassName string `json:"class_name"`
			GameMapID string `json:"game_map_id"`
			GUID      string `json:"guid"`
		} `json:"entities"`
	} `json:"map"`
}

// Match returns the details of a match.
func (c *Client) Match(ctx context.Context, matchID string) (Match, error) {
	if !ValidMatchID(matchID) {
		return Match{}, fmt.Errorf("faceit: get match: invalid match ID")
	}
	var m matchJSON
	if err := c.get(ctx, "get match", "/matches/"+matchID, nil, &m); err != nil {
		return Match{}, err
	}
	out := Match{
		ID:         m.MatchID,
		Status:     m.Status,
		BestOf:     m.BestOf,
		StartedAt:  unixTime(m.StartedAt),
		FinishedAt: unixTime(m.FinishedAt),
		Winner:     m.Results.Winner,
		DemoURLs:   m.DemoURL,
	}
	for _, faction := range slices.Sorted(maps.Keys(m.Teams)) {
		t := m.Teams[faction]
		team := Team{Faction: faction, ID: t.FactionID, Name: t.Name, Score: m.Results.Score[faction]}
		for _, p := range t.Roster {
			team.Players = append(team.Players, MatchPlayer{
				ID:         p.PlayerID,
				Nickname:   p.Nickname,
				SteamID64:  p.GamePlayerID,
				SkillLevel: p.GameSkillLevel,
			})
		}
		out.Teams = append(out.Teams, team)
	}
	var v votingJSON
	if len(m.Voting) > 0 && json.Unmarshal(m.Voting, &v) == nil && len(v.Map.Pick) > 0 {
		out.Map = v.Map.Pick[0]
		for _, e := range v.Map.Entities {
			if out.Map == e.GameMapID || out.Map == e.ClassName || out.Map == e.GUID {
				out.MapName = e.Name
				break
			}
		}
		if out.MapName == "" {
			out.MapName = MapName(out.Map)
		}
	}
	return out, nil
}

// MapName makes a name to show from a map ID such as "de_mirage" or
// "workshop/123/de_cache": "Mirage", "Cache".
func MapName(id string) string {
	name := id[strings.LastIndexByte(id, '/')+1:]
	for _, prefix := range []string{"de_", "cs_", "ar_"} {
		name = strings.TrimPrefix(name, prefix)
	}
	name = strings.ReplaceAll(name, "_", " ")
	if name == "" {
		return id
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// MatchStats is the scoreboard of a match, an entry per map played.
type MatchStats struct {
	Maps []MapStats
}

// MapStats is the scoreboard of one map.
type MapStats struct {
	Map    string // like "de_mirage"
	Rounds int
	Winner string // team ID of the winner
	Teams  []TeamStats
}

// TeamStats is the result of a team on a map.
type TeamStats struct {
	ID         string // equals the faction ID in the match details
	Name       string
	FinalScore int
	FirstHalf  int
	SecondHalf int
	Overtime   int
	Won        bool
	Players    []PlayerStats
}

// PlayerStats is the scoreboard line of a player.
type PlayerStats struct {
	ID        string
	Nickname  string
	Kills     int
	Deaths    int
	Assists   int
	Headshots int
	MVPs      int
	Damage    int
	ADR       float64
	// Raw holds every stat as FACEIT sent it, under FACEIT's names such as
	// "Entry Wins" or "Utility Damage".
	Raw map[string]string
}

// flexString takes a JSON string or number as text. FACEIT sends the stats as
// strings; this keeps working if some turn into numbers.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	if string(b) == "null" {
		*f = ""
		return nil
	}
	*f = flexString(b)
	return nil
}

type statsJSON struct {
	Rounds []struct {
		RoundStats map[string]flexString `json:"round_stats"`
		Teams      []struct {
			TeamID    string                `json:"team_id"`
			TeamStats map[string]flexString `json:"team_stats"`
			Players   []struct {
				PlayerID    string                `json:"player_id"`
				Nickname    string                `json:"nickname"`
				PlayerStats map[string]flexString `json:"player_stats"`
			} `json:"players"`
		} `json:"teams"`
	} `json:"rounds"`
}

// MatchStats returns the scoreboard of a finished match. FACEIT answers 404
// for a match without stats, such as a cancelled one.
func (c *Client) MatchStats(ctx context.Context, matchID string) (MatchStats, error) {
	if !ValidMatchID(matchID) {
		return MatchStats{}, fmt.Errorf("faceit: get match stats: invalid match ID")
	}
	var s statsJSON
	if err := c.get(ctx, "get match stats", "/matches/"+matchID+"/stats", nil, &s); err != nil {
		return MatchStats{}, err
	}
	var out MatchStats
	for _, r := range s.Rounds {
		round := stats(r.RoundStats)
		ms := MapStats{Map: round.text("Map"), Rounds: round.int("Rounds"), Winner: round.text("Winner")}
		for _, t := range r.Teams {
			team := stats(t.TeamStats)
			ts := TeamStats{
				ID:         t.TeamID,
				Name:       team.text("Team"),
				FinalScore: team.int("Final Score"),
				FirstHalf:  team.int("First Half Score"),
				SecondHalf: team.int("Second Half Score"),
				Overtime:   team.int("Overtime score"),
				Won:        team.text("Team Win") == "1",
			}
			for _, p := range t.Players {
				player := stats(p.PlayerStats)
				ts.Players = append(ts.Players, PlayerStats{
					ID:        p.PlayerID,
					Nickname:  p.Nickname,
					Kills:     player.int("Kills"),
					Deaths:    player.int("Deaths"),
					Assists:   player.int("Assists"),
					Headshots: player.int("Headshots"),
					MVPs:      player.int("MVPs"),
					Damage:    player.int("Damage"),
					ADR:       player.float("ADR"),
					Raw:       player.raw(),
				})
				if err := player.err; err != nil {
					return MatchStats{}, fmt.Errorf("faceit: get match stats: %w", err)
				}
			}
			if err := cmp.Or(round.err, team.err); err != nil {
				return MatchStats{}, fmt.Errorf("faceit: get match stats: %w", err)
			}
			ms.Teams = append(ms.Teams, ts)
		}
		out.Maps = append(out.Maps, ms)
	}
	return out, nil
}

// statsReader reads typed values from a map of stats and keeps the first
// error. A missing stat reads as zero, a malformed one is an error: it means
// FACEIT changed the format.
type statsReader struct {
	m   map[string]flexString
	err error
}

func stats(m map[string]flexString) *statsReader { return &statsReader{m: m} }

func (r *statsReader) text(key string) string { return string(r.m[key]) }

func (r *statsReader) int(key string) int {
	s := strings.TrimSpace(r.text(key))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil && r.err == nil {
		r.err = fmt.Errorf("stat %q is not an integer", key)
	}
	return n
}

func (r *statsReader) float(key string) float64 {
	s := strings.TrimSpace(r.text(key))
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && r.err == nil {
		r.err = fmt.Errorf("stat %q is not a number", key)
	}
	return f
}

func (r *statsReader) raw() map[string]string {
	out := make(map[string]string, len(r.m))
	for k, v := range r.m {
		out[k] = string(v)
	}
	return out
}

// MatchSummary is a finished match as one of its players saw it.
type MatchSummary struct {
	ID            string
	StartedAt     time.Time
	Map           string // like "de_mirage"; empty if the details were not available
	MapName       string // like "Mirage"
	Score         int    // rounds won by the player's team
	OpponentScore int
	Won           bool
}

// historyExtra is how many more matches RecentMatches takes from the history
// than it returns, in case some of them are not finished.
const historyExtra = 5

// RecentMatches returns the player's last n (1 to 95) finished CS2 matches,
// newest first. The score and the result come from the history, the map from
// the match details, fetched in parallel; a match whose details fail is
// returned without the map.
func (c *Client) RecentMatches(ctx context.Context, playerID string, n int) ([]MatchSummary, error) {
	history, err := c.History(ctx, playerID, n+historyExtra)
	if err != nil {
		return nil, err
	}
	var out []MatchSummary
	for _, m := range history {
		faction, ok := m.FactionOf(playerID)
		if m.Status != "finished" || !ok {
			continue
		}
		s := MatchSummary{ID: m.ID, StartedAt: m.StartedAt, Score: m.Score[faction], Won: m.Winner == faction}
		for f, score := range m.Score {
			if f != faction {
				s.OpponentScore = score
			}
		}
		out = append(out, s)
		if len(out) == n {
			break
		}
	}

	var wg sync.WaitGroup
	for i := range out {
		wg.Go(func() {
			details, err := c.Match(ctx, out[i].ID)
			if err != nil {
				c.log.Warn("match details unavailable, listing the match without the map", "err", err)
				return
			}
			out[i].Map, out[i].MapName = details.Map, details.MapName
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("faceit: recent matches: %w", err)
	}
	return out, nil
}

// unixTime converts FACEIT unix seconds; 0 means unknown.
func unixTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}
