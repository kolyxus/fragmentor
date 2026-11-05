package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

func TestMatchID(t *testing.T) {
	const id = "1-00000000-0000-4000-8000-000000000004"
	for path, want := range map[string]string{
		filepath.Join("testdata", id+"-1-1.dem.zst"): id,
		strings.ToUpper(id) + "-1-2.dem.zst":         id,
		id + "-1-1.dem":                              "",
		"demo.dem.zst":                               "",
		"1-not-a-uuid-1-1.dem.zst":                   "",
	} {
		got, ok := matchID(path)
		if got != want || ok != (want != "") {
			t.Errorf("matchID(%q) = %q, %t; want %q", path, got, ok, want)
		}
	}
}

func TestDemoFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.dem.zst", "a.dem.zst", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	single := filepath.Join(dir, "notes.txt")
	got, err := demoFiles([]string{dir, single})
	want := []string{filepath.Join(dir, "a.dem.zst"), filepath.Join(dir, "b.dem.zst"), single}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("demoFiles = %v, %v; want %v", got, err, want)
	}
	if _, err := demoFiles([]string{filepath.Join(dir, "missing.dem.zst")}); err == nil {
		t.Error("a missing file is not an error")
	}
}

// fixture is a match of two players a side: the demo, the metrics of its
// players and what FACEIT says.
func fixture() (*demo.Match, []metrics.Player, faceit.Match, faceit.MatchStats) {
	m := &demo.Match{
		Map: "de_ancient",
		Teams: [2]demo.Team{
			{Name: "team_a", StartSide: demo.CT, Score: 13, FirstHalf: 8, SecondHalf: 5},
			{Name: "team_b", StartSide: demo.T, Score: 6, FirstHalf: 4, SecondHalf: 2},
		},
		Players: []demo.Player{
			{SteamID64: 76561190000000001, Name: "a1", Team: 0, Kills: 20, Deaths: 15, Assists: 8, Headshots: 9, Damage: 1884},
			{SteamID64: 76561190000000002, Name: "a2", Team: 0, Kills: 17, Deaths: 12, Assists: 5, Headshots: 8, Damage: 1658},
			{SteamID64: 76561190000000003, Name: "b1", Team: 1, Kills: 12, Deaths: 17, Assists: 8, Headshots: 7, Damage: 1532},
			{SteamID64: 76561190000000004, Name: "b2", Team: 1, Kills: 12, Deaths: 15, Assists: 0, Headshots: 6, Damage: 1315},
		},
	}
	details := faceit.Match{Teams: []faceit.Team{
		{Faction: "faction1", ID: "fb", Players: []faceit.MatchPlayer{
			{ID: "id-b1", Nickname: "b1", SteamID64: "76561190000000003"},
			{ID: "id-b2", Nickname: "b2", SteamID64: "76561190000000004"},
		}},
		{Faction: "faction2", ID: "fa", Players: []faceit.MatchPlayer{
			{ID: "id-a1", Nickname: "a1", SteamID64: "76561190000000001"},
			{ID: "id-a2", Nickname: "a2", SteamID64: "76561190000000002"},
		}},
	}}
	stats := faceit.MatchStats{Maps: []faceit.MapStats{{Map: "de_ancient", Teams: []faceit.TeamStats{
		{ID: "fb", Name: "team_b", FinalScore: 6, FirstHalf: 4, SecondHalf: 2, Players: []faceit.PlayerStats{
			{ID: "id-b1", Nickname: "b1", Kills: 12, Deaths: 17, Assists: 8, Headshots: 7, Damage: 1532, ADR: 80.6},
			{ID: "id-b2", Nickname: "b2", Kills: 12, Deaths: 15, Assists: 0, Headshots: 6, Damage: 1315, ADR: 69.2},
		}},
		{ID: "fa", Name: "team_a", FinalScore: 13, FirstHalf: 8, SecondHalf: 5, Players: []faceit.PlayerStats{
			{ID: "id-a1", Nickname: "a1", Kills: 20, Deaths: 15, Assists: 8, Headshots: 9, Damage: 1884, ADR: 99.2},
			{ID: "id-a2", Nickname: "a2", Kills: 17, Deaths: 12, Assists: 5, Headshots: 8, Damage: 1658, ADR: 87.3},
		}},
	}}}}
	// The ADR of 19 rounds, as metrics counts it.
	var players []metrics.Player
	for _, p := range m.Players {
		players = append(players, metrics.Player{SteamID64: p.SteamID64, Team: p.Team, Match: metrics.Stats{ADR: float64(p.Damage) / 19}})
	}
	return m, players, details, stats
}

func TestCompareAgrees(t *testing.T) {
	m, players, details, stats := fixture()
	lines, mismatches := compare(m, players, details, stats)
	if mismatches != 0 || lines[len(lines)-1] != "  result: ok" {
		t.Errorf("%d mismatches:\n%s", mismatches, strings.Join(lines, "\n"))
	}
	if len(lines) != 7 {
		t.Errorf("want a line per team, per player and the result:\n%s", strings.Join(lines, "\n"))
	}
}

func TestCompareFindsDifferences(t *testing.T) {
	m, players, details, stats := fixture()
	m.Players[2].Deaths = 18   // K/D/A: a mismatch
	m.Players[1].Damage = 1612 // damage: only marked
	players[1].Match.ADR = 1612.0 / 19
	players[0].Match.ADR = 99.24 // the same to a tenth
	m.Teams[0].FirstHalf = 7     // the halves: one mismatch for the line
	m.Teams[0].SecondHalf = 6
	stats.Maps[0].Teams[0].Players = stats.Maps[0].Teams[0].Players[:1] // b2 is not in FACEIT
	lines, mismatches := compare(m, players, details, stats)
	text := strings.Join(lines, "\n")
	if mismatches != 3 {
		t.Errorf("%d mismatches, want 3: the score of team_a, b1's deaths and b2:\n%s", mismatches, text)
	}
	for _, want := range []string{
		"team_a               score 13 (7+6+0)  FACEIT 13 (8+5+0)  MISMATCH",
		"b1               K/D/A 12/18/ 8  FACEIT 12/17/ 8  HS 7/7  damage 1532/1532  ADR 80.6/80.6  MISMATCH",
		"damage 1612/1658*  ADR 84.8/87.3*  ok",
		"a1               K/D/A 20/15/ 8  FACEIT 20/15/ 8  HS 9/9  damage 1884/1884  ADR 99.2/99.2  ok",
		"b2               in the demo, not in the FACEIT stats  MISMATCH",
		"result: 3 mismatches",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in:\n%s", want, text)
		}
	}
}

func TestCompareOtherMap(t *testing.T) {
	m, players, details, stats := fixture()
	stats.Maps = append(stats.Maps, faceit.MapStats{Map: "de_mirage"})
	m.Map = "de_nuke"
	if lines, mismatches := compare(m, players, details, stats); mismatches != 1 || !strings.Contains(lines[0], "no stats of de_nuke") {
		t.Errorf("%d mismatches: %q", mismatches, lines)
	}
}

func TestWithoutEvents(t *testing.T) {
	m := &demo.Match{
		Rounds: []demo.Round{{Number: 1, Alive: []demo.Ref{{ID: 1}}}},
		Kills:  []demo.Kill{{Tick: 1}},
		Hits:   []demo.Hit{{Tick: 1}},
	}
	out := withoutEvents(m)
	if out.Kills != nil || out.Hits != nil || out.Rounds[0].Alive != nil || out.Rounds[0].Number != 1 {
		t.Errorf("withoutEvents = %+v", out)
	}
	if len(m.Kills) != 1 || len(m.Hits) != 1 || len(m.Rounds[0].Alive) != 1 {
		t.Errorf("the match itself lost its events: %+v", m)
	}
}

func TestPairTenths(t *testing.T) {
	for _, tc := range []struct {
		ours, theirs float64
		want         string
	}{
		{1614.0 / 24, 67.2, "67.2/67.2"}, // 67.25: a half goes to the even tenth, as on FACEIT
		{1805.0 / 28, 64.5, "64.5/64.5"}, // 64.46
		{80.64, 80.6, "80.6/80.6"},
		{80.66, 80.6, "80.7/80.6*"},
	} {
		if got := pairTenths(tc.ours, tc.theirs); got != tc.want {
			t.Errorf("pairTenths(%v, %v) = %q, want %q", tc.ours, tc.theirs, got, tc.want)
		}
	}
}
