package faceit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPlayerByNickname(t *testing.T) {
	srv := newFixtureServer(t)
	c, _ := newTestClient(t, srv)

	p, err := c.PlayerByNickname(context.Background(), testNickname)
	if err != nil {
		t.Fatalf("PlayerByNickname() error: %v", err)
	}
	want := Player{
		ID:         testPlayerID,
		Nickname:   testNickname,
		Country:    "kp",
		Avatar:     "https://distribution.faceit-cdn.net/images/avatar-0001.jpg",
		HasCS2:     true,
		SteamID64:  testSteamID64,
		SkillLevel: 10,
		Elo:        2413,
	}
	if p != want {
		t.Errorf("PlayerByNickname() = %+v, want %+v", p, want)
	}
	if got, want := srv.requestList(), []string{"/players?nickname=" + testNickname}; !slices.Equal(got, want) {
		t.Errorf("requests %q, want %q", got, want)
	}
}

func TestPlayerByNicknameIgnoresCase(t *testing.T) {
	srv := newFixtureServer(t)
	c, _ := newTestClient(t, srv)

	p, err := c.PlayerByNickname(context.Background(), "PLAYER01")
	if err != nil {
		t.Fatalf("PlayerByNickname() error: %v", err)
	}
	if p.ID != testPlayerID || p.Nickname != testNickname {
		t.Errorf("PlayerByNickname() = %+v, want the recorded player", p)
	}
	want := []string{
		"/players?nickname=PLAYER01",
		"/search/players?game=cs2&limit=20&nickname=PLAYER01",
		"/players/" + testPlayerID,
	}
	if got := srv.requestList(); !slices.Equal(got, want) {
		t.Errorf("requests %q, want %q", got, want)
	}
}

func TestPlayerByNicknameNotFound(t *testing.T) {
	srv := newFixtureServer(t)
	c, _ := newTestClient(t, srv)

	// The search finds player01 and player05 for this one, neither matches.
	_, err := c.PlayerByNickname(context.Background(), "player0")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("PlayerByNickname() error = %v, want ErrNotFound", err)
	}
	if n := len(srv.requestList()); n != 2 {
		t.Errorf("%d requests, want the lookup and the search", n)
	}
}

func TestPlayerByNicknameEscapesQuery(t *testing.T) {
	srv := newFixtureServer(t)
	var got string
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/players" {
			got = r.URL.Query().Get("nickname")
		}
		return false
	})
	c, _ := newTestClient(t, srv)

	_, _ = c.PlayerByNickname(context.Background(), "a&game=csgo#x")
	if got != "a&game=csgo#x" {
		t.Errorf("server got nickname %q, want it whole", got)
	}
}

func TestPlayerWithoutCS2(t *testing.T) {
	srv := newFixtureServer(t)
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"player_id":"` + testPlayerID + `","nickname":"oldtimer","country":"RU",
			"games":{"csgo":{"faceit_elo":1500,"game_player_id":"76561190000000009","skill_level":6}}}`))
		return true
	})
	c, _ := newTestClient(t, srv)

	p, err := c.Player(context.Background(), testPlayerID)
	if err != nil {
		t.Fatalf("Player() error: %v", err)
	}
	want := Player{ID: testPlayerID, Nickname: "oldtimer", Country: "ru"}
	if p != want {
		t.Errorf("Player() = %+v, want %+v", p, want)
	}
}

func TestInvalidIDsMakeNoRequest(t *testing.T) {
	srv := newFixtureServer(t)
	c, _ := newTestClient(t, srv)
	ctx := context.Background()

	for _, id := range []string{"", "../players", testPlayerID + "/history", "1-" + testPlayerID} {
		if _, err := c.Player(ctx, id); err == nil {
			t.Errorf("Player(%q) succeeded", id)
		}
		if _, err := c.History(ctx, id, 5); err == nil {
			t.Errorf("History(%q) succeeded", id)
		}
		if _, err := c.LatestMatchStats(ctx, id, 5); err == nil {
			t.Errorf("LatestMatchStats(%q) succeeded", id)
		}
	}
	for _, id := range []string{"", "1-../../players", testMatchID + "/stats", "x-" + testPlayerID} {
		if _, err := c.Match(ctx, id); err == nil {
			t.Errorf("Match(%q) succeeded", id)
		}
		if _, err := c.MatchStats(ctx, id); err == nil {
			t.Errorf("MatchStats(%q) succeeded", id)
		}
	}
	if r := srv.requestList(); len(r) != 0 {
		t.Errorf("requests made: %q", r)
	}
}

func TestHistory(t *testing.T) {
	srv := newFixtureServer(t)
	c, _ := newTestClient(t, srv)

	h, err := c.History(context.Background(), testPlayerID, 10)
	if err != nil {
		t.Fatalf("History() error: %v", err)
	}
	if len(h) != 10 {
		t.Fatalf("History() returned %d matches, want 10", len(h))
	}
	m := h[0]
	if m.ID != testMatchID || m.Status != "finished" || m.Winner != "faction2" {
		t.Errorf("first match = %+v", m)
	}
	if want := time.Date(2026, 10, 31, 17, 3, 25, 0, time.UTC); !m.StartedAt.Equal(want) {
		t.Errorf("StartedAt = %v, want %v", m.StartedAt, want)
	}
	if !m.FinishedAt.After(m.StartedAt) {
		t.Errorf("FinishedAt %v is not after StartedAt %v", m.FinishedAt, m.StartedAt)
	}
	if m.Score["faction1"] != 16 || m.Score["faction2"] != 19 {
		t.Errorf("Score = %v, want faction1 16, faction2 19", m.Score)
	}
	if f, ok := m.FactionOf(testPlayerID); !ok || f != "faction1" {
		t.Errorf("FactionOf() = %q, %t, want faction1", f, ok)
	}
	if len(m.Factions) != 10 {
		t.Errorf("Factions has %d players, want 10", len(m.Factions))
	}
	if got, want := srv.requestList(), []string{"/players/" + testPlayerID + "/history?game=cs2&limit=10"}; !slices.Equal(got, want) {
		t.Errorf("requests %q, want %q", got, want)
	}
}

func TestMatch(t *testing.T) {
	srv := newFixtureServer(t)
	c, _ := newTestClient(t, srv)

	m, err := c.Match(context.Background(), testMatchID)
	if err != nil {
		t.Fatalf("Match() error: %v", err)
	}
	if m.ID != testMatchID || m.Status != "FINISHED" || m.BestOf != 1 || m.Winner != "faction2" {
		t.Errorf("Match() = %+v", m)
	}
	if m.Map != "de_cache" || m.MapName != "Cache" {
		t.Errorf("map = %q %q, want de_cache Cache", m.Map, m.MapName)
	}
	if len(m.Teams) != 2 || m.Teams[0].Faction != "faction1" || m.Teams[1].Faction != "faction2" {
		t.Fatalf("Teams = %+v, want faction1 and faction2", m.Teams)
	}
	if m.Teams[0].Score != 16 || m.Teams[1].Score != 19 {
		t.Errorf("scores %d:%d, want 16:19", m.Teams[0].Score, m.Teams[1].Score)
	}
	if m.Teams[0].ID != "00000000-0000-4000-8000-000000000005" || m.Teams[0].Name != "team_player06" {
		t.Errorf("faction1 = %q %q", m.Teams[0].ID, m.Teams[0].Name)
	}
	var me *MatchPlayer
	for _, team := range m.Teams {
		if len(team.Players) != 5 {
			t.Errorf("%s has %d players, want 5", team.Faction, len(team.Players))
		}
		for i, p := range team.Players {
			if len(p.SteamID64) != 17 || p.SkillLevel < 1 || p.SkillLevel > 10 || p.Nickname == "" {
				t.Errorf("player %+v: want a SteamID64, a level and a nickname", p)
			}
			if p.ID == testPlayerID {
				me = &team.Players[i]
			}
		}
	}
	if me == nil || me.SteamID64 != testSteamID64 || me.SkillLevel != 10 || me.Nickname != testNickname {
		t.Errorf("recorded player in the roster = %+v", me)
	}
	if len(m.DemoURLs) != 1 || !strings.HasSuffix(m.DemoURLs[0], "/cs2/"+testMatchID+"-1-1.dem.zst") {
		t.Errorf("DemoURLs = %q", m.DemoURLs)
	}
}

func TestMatchWithoutVoting(t *testing.T) {
	srv := newFixtureServer(t)
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		_, _ = w.Write([]byte(`{"match_id":"` + testMatchID + `","status":"CANCELLED","voting":["unexpected"]}`))
		return true
	})
	c, _ := newTestClient(t, srv)

	m, err := c.Match(context.Background(), testMatchID)
	if err != nil {
		t.Fatalf("Match() error: %v", err)
	}
	if m.Status != "CANCELLED" || m.Map != "" || m.MapName != "" {
		t.Errorf("Match() = %+v, want no map", m)
	}
}

func TestMapName(t *testing.T) {
	for id, want := range map[string]string{
		"de_mirage":               "Mirage",
		"de_dust2":                "Dust2",
		"cs_office":               "Office",
		"workshop/123/de_cache":   "Cache",
		"de_overpass_2v2":         "Overpass 2v2",
		"mirage":                  "Mirage",
		"de_":                     "de_",
		"workshop/3070284539/aim": "Aim",
	} {
		if got := MapName(id); got != want {
			t.Errorf("MapName(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestMatchStats(t *testing.T) {
	srv := newFixtureServer(t)
	c, _ := newTestClient(t, srv)
	ctx := context.Background()

	s, err := c.MatchStats(ctx, testMatchID)
	if err != nil {
		t.Fatalf("MatchStats() error: %v", err)
	}
	if len(s.Maps) != 1 {
		t.Fatalf("MatchStats() has %d maps, want 1", len(s.Maps))
	}
	m := s.Maps[0]
	if m.Map != "de_cache" || m.Rounds != 35 || len(m.Teams) != 2 {
		t.Fatalf("map = %+v", m)
	}
	a, b := m.Teams[0], m.Teams[1]
	if a.FinalScore != 16 || a.FirstHalf != 6 || a.SecondHalf != 6 || a.Overtime != 4 || a.Won {
		t.Errorf("first team = %+v, want 16 (6+6+4), lost", a)
	}
	if b.FinalScore != 19 || !b.Won || m.Winner != b.ID {
		t.Errorf("second team = %+v, winner %q, want 19, won", b, m.Winner)
	}
	if a.Name != "team_player06" || len(a.Players) != 5 || len(b.Players) != 5 {
		t.Errorf("teams %q with %d and %d players", a.Name, len(a.Players), len(b.Players))
	}

	var me PlayerStats
	for _, p := range a.Players {
		if p.ID == testPlayerID {
			me = p
		}
	}
	want := PlayerStats{ID: testPlayerID, Nickname: testNickname, Kills: 21, Deaths: 27, Assists: 4, Headshots: 9, MVPs: 3, Damage: 2234, ADR: 63.8}
	got := me
	got.Raw = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recorded player = %+v, want %+v", got, want)
	}
	if me.Raw["Entry Wins"] != "2" || me.Raw["Utility Damage"] != "178" || me.Raw["Kills"] != "21" {
		t.Errorf("Raw misses stats: %v", me.Raw)
	}

	// The stats name the teams by the faction IDs of the match details.
	details, err := c.Match(ctx, testMatchID)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != details.Teams[0].ID || b.ID != details.Teams[1].ID {
		t.Errorf("stats team IDs %q %q differ from the faction IDs %q %q", a.ID, b.ID, details.Teams[0].ID, details.Teams[1].ID)
	}
}

func TestMatchStatsMalformed(t *testing.T) {
	tests := map[string]string{
		"kills not a number": `{"rounds":[{"round_stats":{"Map":"de_mirage"},"teams":[{"team_id":"t","team_stats":{},"players":[{"player_id":"p","player_stats":{"Kills":"many"}}]}]}]}`,
		"score not a number": `{"rounds":[{"round_stats":{},"teams":[{"team_id":"t","team_stats":{"Final Score":"13.5"},"players":[]}]}]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newFixtureServer(t)
			srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
				_, _ = w.Write([]byte(body))
				return true
			})
			c, _ := newTestClient(t, srv)
			if _, err := c.MatchStats(context.Background(), testMatchID); err == nil {
				t.Error("MatchStats() succeeded on a malformed stat")
			}
		})
	}
}

func TestMatchStatsNumbersAsJSONNumbers(t *testing.T) {
	srv := newFixtureServer(t)
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		_, _ = w.Write([]byte(`{"rounds":[{"round_stats":{"Rounds":24},"teams":[{"team_id":"t","team_stats":{"Final Score":13,"Team Win":1},
			"players":[{"player_id":"p","player_stats":{"Kills":20,"ADR":91.5,"Assists":null}}]}]}]}`))
		return true
	})
	c, _ := newTestClient(t, srv)

	s, err := c.MatchStats(context.Background(), testMatchID)
	if err != nil {
		t.Fatalf("MatchStats() error: %v", err)
	}
	m := s.Maps[0]
	p := m.Teams[0].Players[0]
	if m.Rounds != 24 || m.Teams[0].FinalScore != 13 || !m.Teams[0].Won || p.Kills != 20 || p.ADR != 91.5 || p.Assists != 0 {
		t.Errorf("MatchStats() = %+v", s)
	}
}

func TestAvatarURL(t *testing.T) {
	for link, want := range map[string]string{
		"https://distribution.faceit-cdn.net/images/abc.jpg":          "https://distribution.faceit-cdn.net/images/abc.jpg",
		"https://assets.faceit-cdn.net/avatars/abc_1550573467862.jpg": "https://assets.faceit-cdn.net/avatars/abc_1550573467862.jpg",
		"https://DISTRIBUTION.FACEIT-CDN.NET/images/abc.jpg":          "https://DISTRIBUTION.FACEIT-CDN.NET/images/abc.jpg",
		"": "",
		"http://distribution.faceit-cdn.net/images/abc.jpg":       "",
		"https://avatars.steamstatic.com/abc_full.jpg":            "",
		"https://faceit-cdn.net.evil.example/abc.jpg":             "",
		"https://evilfaceit-cdn.net/abc.jpg":                      "",
		"https://user@distribution.faceit-cdn.net/images/abc.jpg": "",
		"javascript:alert(1)":                                     "",
	} {
		if got := avatarURL(link); got != want {
			t.Errorf("avatarURL(%q) = %q, want %q", link, got, want)
		}
	}
}

func TestLatestMatchStats(t *testing.T) {
	srv := newFixtureServer(t)
	c, _ := newTestClient(t, srv)

	got, err := c.LatestMatchStats(context.Background(), testPlayerID, 10)
	if err != nil {
		t.Fatalf("LatestMatchStats() error: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("LatestMatchStats() returned %d matches, want 10", len(got))
	}
	want := PlayerMatchStats{MatchID: testMatchID, Map: "de_cache", Kills: 21, Deaths: 27, Assists: 4,
		Headshots: 9, HeadshotsPct: 43, MVPs: 3, ADR: 63.8}
	if got[0] != want {
		t.Errorf("first match = %+v, want %+v", got[0], want)
	}
	if !got[4].Won || got[4].Map != "de_mirage" {
		t.Errorf("fifth match = %+v, want a won match on de_mirage", got[4])
	}
	if r, want := srv.requestList(), []string{"/players/" + testPlayerID + "/games/cs2/stats?limit=10"}; !slices.Equal(r, want) {
		t.Errorf("requests %q, want %q", r, want)
	}
}

// fixtureMatch returns the fake ID of a recorded match by its number.
func fixtureMatch(n int) string { return fmt.Sprintf("1-00000000-0000-4000-8000-%012d", n) }

func TestRecentMatches(t *testing.T) {
	srv := newFixtureServer(t)
	c, _ := newTestClient(t, srv)

	got, err := c.RecentMatches(context.Background(), testPlayerID, 5)
	if err != nil {
		t.Fatalf("RecentMatches() error: %v", err)
	}
	requests := srv.requestList()
	slices.Sort(requests)
	wantRequests := []string{
		"/players/" + testPlayerID + "/games/cs2/stats?limit=10",
		"/players/" + testPlayerID + "/history?game=cs2&limit=10",
	}
	if !slices.Equal(requests, wantRequests) {
		t.Errorf("requests %q, want %q", requests, wantRequests)
	}

	at := func(month, day, hour, min, sec int) time.Time {
		return time.Date(2026, time.Month(month), day, hour, min, sec, 0, time.UTC)
	}
	stats := func(n int, mapID string, k, d, a, hs, hsPct, mvps int, adr float64, won bool) PlayerMatchStats {
		return PlayerMatchStats{MatchID: fixtureMatch(n), Map: mapID, Kills: k, Deaths: d, Assists: a,
			Headshots: hs, HeadshotsPct: hsPct, MVPs: mvps, ADR: adr, Won: won}
	}
	// The numbers are those of the match scoreboards in testdata/matches.
	want := []MatchSummary{
		{ID: fixtureMatch(4), StartedAt: at(10, 31, 17, 3, 25), Score: 16, OpponentScore: 19,
			HasStats: true, Stats: stats(4, "de_cache", 21, 27, 4, 9, 43, 3, 63.8, false)},
		{ID: fixtureMatch(14), StartedAt: at(10, 24, 16, 43, 4), Score: 12, OpponentScore: 16,
			HasStats: true, Stats: stats(14, "de_ancient", 19, 20, 5, 9, 47, 3, 73.5, false)},
		{ID: fixtureMatch(24), StartedAt: at(10, 24, 16, 6, 3), Score: 6, OpponentScore: 13,
			HasStats: true, Stats: stats(24, "de_ancient", 12, 17, 8, 7, 58, 2, 80.6, false)},
		{ID: fixtureMatch(34), StartedAt: at(10, 22, 17, 5, 28), Score: 9, OpponentScore: 13,
			HasStats: true, Stats: stats(34, "de_anubis", 15, 17, 4, 8, 53, 3, 70.4, false)},
		{ID: fixtureMatch(44), StartedAt: at(10, 19, 16, 18, 32), Score: 13, OpponentScore: 10, Won: true,
			HasStats: true, Stats: stats(44, "de_mirage", 13, 18, 4, 7, 54, 3, 65, true)},
	}
	if !slices.Equal(got, want) {
		t.Errorf("RecentMatches() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestRecentMatchesJoinsByMatchID(t *testing.T) {
	srv := newFixtureServer(t)
	line := func(id, mapID string, kills int) string {
		return fmt.Sprintf(`{"stats":{"Match Id":%q,"Map":%q,"Kills":"%d"}}`, id, mapID, kills)
	}
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/games/cs2/stats") {
			return false
		}
		// Out of order, with a match the history does not have, and nothing
		// for the second match of the history.
		_, _ = w.Write([]byte(`{"items":[` +
			line("1-00000000-0000-4000-8000-000000000777", "de_nuke", 30) + `,` +
			line(fixtureMatch(24), "de_ancient", 12) + `,` +
			line(fixtureMatch(4), "de_cache", 21) + `]}`))
		return true
	})
	c, _ := newTestClient(t, srv)

	got, err := c.RecentMatches(context.Background(), testPlayerID, 3)
	if err != nil {
		t.Fatalf("RecentMatches() error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("RecentMatches() returned %d matches, want 3", len(got))
	}
	for i, want := range []struct {
		id       string
		hasStats bool
		mapID    string
		kills    int
	}{
		{fixtureMatch(4), true, "de_cache", 21},
		{fixtureMatch(14), false, "", 0},
		{fixtureMatch(24), true, "de_ancient", 12},
	} {
		m := got[i]
		if m.ID != want.id || m.HasStats != want.hasStats || m.Stats.Map != want.mapID || m.Stats.Kills != want.kills {
			t.Errorf("match %d = %+v, want %+v", i, m, want)
		}
		if m.HasStats && m.Stats.MatchID != m.ID {
			t.Errorf("match %d got the stats of %s", i, m.Stats.MatchID)
		}
	}
	if got[1].Score != 12 || got[1].OpponentScore != 16 {
		t.Errorf("a match without stats lost its score: %+v", got[1])
	}
}

func TestRecentMatchesWithoutStats(t *testing.T) {
	srv := newFixtureServer(t)
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/games/cs2/stats") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}
		return false
	})
	c, _ := newTestClient(t, srv)

	got, err := c.RecentMatches(context.Background(), testPlayerID, 5)
	if err != nil {
		t.Fatalf("RecentMatches() error: %v", err)
	}
	if len(got) != 5 || got[0].Score != 16 || !got[4].Won {
		t.Fatalf("RecentMatches() = %+v, want the 5 matches of the history", got)
	}
	for _, m := range got {
		if m.HasStats {
			t.Errorf("match %s has stats while FACEIT gave none", m.ID)
		}
	}
	n := 0
	for _, r := range srv.requestList() {
		if strings.Contains(r, "/games/cs2/stats") {
			n++
		}
	}
	if n != maxAttempts {
		t.Errorf("stats asked %d times, want %d", n, maxAttempts)
	}
}

func TestRecentMatchesHistoryFails(t *testing.T) {
	srv := newFixtureServer(t)
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/history") {
			w.WriteHeader(http.StatusForbidden)
			return true
		}
		return false
	})
	c, _ := newTestClient(t, srv)

	_, err := c.RecentMatches(context.Background(), testPlayerID, 5)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusForbidden || se.Op != "get history" {
		t.Errorf("RecentMatches() error = %v, want the history error", err)
	}
}

func TestRecentMatchesSkipsUnfinished(t *testing.T) {
	srv := newFixtureServer(t)
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/history") {
			return false
		}
		team := func(id string) string { return `{"players":[{"player_id":"` + id + `"}]}` }
		_, _ = w.Write([]byte(`{"items":[
			{"match_id":"1-00000000-0000-4000-8000-000000000101","status":"cancelled","teams":{"faction1":` + team(testPlayerID) + `}},
			{"match_id":"1-00000000-0000-4000-8000-000000000102","status":"finished","teams":{"faction1":` + team("00000000-0000-4000-8000-000000000999") + `}},
			{"match_id":"` + testMatchID + `","status":"finished","started_at":1793466205,
			 "results":{"winner":"faction2","score":{"faction1":16,"faction2":19}},
			 "teams":{"faction1":` + team(testPlayerID) + `,"faction2":` + team("00000000-0000-4000-8000-000000000998") + `}}]}`))
		return true
	})
	c, _ := newTestClient(t, srv)

	got, err := c.RecentMatches(context.Background(), testPlayerID, 5)
	if err != nil {
		t.Fatalf("RecentMatches() error: %v", err)
	}
	if len(got) != 1 || got[0].ID != testMatchID || got[0].Score != 16 || got[0].OpponentScore != 19 || got[0].Won || !got[0].HasStats {
		t.Errorf("RecentMatches() = %+v, want only the finished match the player played, with stats", got)
	}
}

func TestRetriesTooManyRequests(t *testing.T) {
	srv := newFixtureServer(t)
	calls := 0
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		}
		return false
	})
	c, sleep := newTestClient(t, srv)

	if _, err := c.Player(context.Background(), testPlayerID); err != nil {
		t.Fatalf("Player() error: %v", err)
	}
	if calls != 2 {
		t.Errorf("%d calls, want 2", calls)
	}
	if got := sleep.list(); !slices.Equal(got, []time.Duration{2 * time.Second}) {
		t.Errorf("pauses %v, want the 2 s FACEIT asked for", got)
	}
}

func TestRetryWaitIsCapped(t *testing.T) {
	srv := newFixtureServer(t)
	calls := 0
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		calls++
		if calls == 1 {
			w.Header().Set("RateLimit-Reset", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		}
		return false
	})
	c, sleep := newTestClient(t, srv)

	if _, err := c.Player(context.Background(), testPlayerID); err != nil {
		t.Fatalf("Player() error: %v", err)
	}
	if got := sleep.list(); !slices.Equal(got, []time.Duration{maxRetryWait}) {
		t.Errorf("pauses %v, want %v", got, maxRetryWait)
	}
}

func TestRetriesServerErrorsThenGivesUp(t *testing.T) {
	srv := newFixtureServer(t)
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	})
	c, sleep := newTestClient(t, srv)

	_, err := c.Player(context.Background(), testPlayerID)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusServiceUnavailable || se.Op != "get player" {
		t.Fatalf("Player() error = %v, want status 503", err)
	}
	if n := len(srv.requestList()); n != maxAttempts {
		t.Errorf("%d requests, want %d", n, maxAttempts)
	}
	if got, want := sleep.list(), []time.Duration{retryWait, 2 * retryWait}; !slices.Equal(got, want) {
		t.Errorf("pauses %v, want %v", got, want)
	}
}

func TestNoRetryOnClientErrors(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		srv := newFixtureServer(t)
		c, _ := newTestClient(t, srv)
		_, err := c.Match(context.Background(), "1-00000000-0000-4000-8000-999999999999")
		var se *StatusError
		if !errors.Is(err, ErrNotFound) || !errors.As(err, &se) || se.Code != "err_nf0" {
			t.Errorf("Match() error = %v, want ErrNotFound with code err_nf0", err)
		}
		if n := len(srv.requestList()); n != 1 {
			t.Errorf("%d requests, want 1", n)
		}
	})
	t.Run("wrong key", func(t *testing.T) {
		srv := newFixtureServer(t)
		c := New("wrong-key", WithBaseURL(srv.URL))
		_, err := c.Player(context.Background(), testPlayerID)
		var se *StatusError
		if !errors.As(err, &se) || se.Status != http.StatusBadRequest || se.Code != "invalid_token" {
			t.Fatalf("Player() error = %v, want status 400 invalid_token", err)
		}
		if errors.Is(err, ErrNotFound) {
			t.Error("a rejected key reads as not found")
		}
		if n := len(srv.requestList()); n != 1 {
			t.Errorf("%d requests, want 1", n)
		}
	})
	t.Run("not JSON", func(t *testing.T) {
		srv := newFixtureServer(t)
		srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
			_, _ = w.Write([]byte("<html>maintenance</html>"))
			return true
		})
		c, _ := newTestClient(t, srv)
		if _, err := c.Player(context.Background(), testPlayerID); err == nil || !strings.Contains(err.Error(), "decode") {
			t.Errorf("Player() error = %v, want a decode error", err)
		}
		if n := len(srv.requestList()); n != 1 {
			t.Errorf("%d requests, want 1", n)
		}
	})
}

func TestResponseSizeLimit(t *testing.T) {
	srv := newFixtureServer(t)
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		_, _ = w.Write([]byte(`{"nickname":"` + strings.Repeat("x", maxBodySize) + `"}`))
		return true
	})
	c, _ := newTestClient(t, srv)

	if _, err := c.Player(context.Background(), testPlayerID); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("Player() error = %v, want the size limit", err)
	}
}

func TestNetworkErrorsAreRetriedAndHideTheURL(t *testing.T) {
	// The server drops every connection, so the error comes from the
	// transport, which puts the whole URL into it.
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	sleep := &fakeSleep{}
	c := New(testKey, WithBaseURL(srv.URL))
	c.sleep = sleep.sleep

	_, err := c.PlayerByNickname(context.Background(), "secretnick")
	if err == nil {
		t.Fatal("PlayerByNickname() succeeded")
	}
	if requests != maxAttempts {
		t.Errorf("%d requests, want %d", requests, maxAttempts)
	}
	for _, leak := range []string{"secretnick", "nickname=", srv.URL, testKey} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q contains %q", err, leak)
		}
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		t.Errorf("error wraps *url.Error with the URL: %v", ue)
	}
}

func TestContextEndsRetries(t *testing.T) {
	srv := newFixtureServer(t)
	srv.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusBadGateway)
		return true
	})

	t.Run("cancelled", func(t *testing.T) {
		c, _ := newTestClient(t, srv)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := c.Player(ctx, testPlayerID); !errors.Is(err, context.Canceled) {
			t.Errorf("Player() error = %v, want context.Canceled", err)
		}
	})
	t.Run("deadline before the retry", func(t *testing.T) {
		c, sleep := newTestClient(t, srv)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := c.Player(ctx, testPlayerID)
		var se *StatusError
		if !errors.As(err, &se) || se.Status != http.StatusBadGateway {
			t.Errorf("Player() error = %v, want the 502 at once", err)
		}
		if p := sleep.list(); len(p) != 0 {
			t.Errorf("paused %v past the deadline", p)
		}
	})
}

func TestStatusErrorText(t *testing.T) {
	err := &StatusError{Op: "get match", Status: 404, Code: "err_nf0"}
	if got := err.Error(); got != "faceit: get match: status 404 (err_nf0)" {
		t.Errorf("Error() = %q", got)
	}
	err = &StatusError{Op: "get match", Status: 503}
	if got := err.Error(); got != "faceit: get match: status 503" {
		t.Errorf("Error() = %q", got)
	}
}
