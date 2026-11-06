package storage

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// accountX is in the matches, but no user linked it.
var accountX = FaceitAccount{PlayerID: "00000000-0000-4000-8000-000000000009", SteamID64: "76561190000000009"}

func testMatch(version int) ParsedMatch {
	return ParsedMatch{
		MatchID: matchM, Map: "de_mirage", StartedAt: jobsT0.Add(-time.Hour),
		Score: [2]int{13, 9}, Rounds: 22, MetricsVersion: version,
	}
}

// testPlayers are a linked player, one nobody linked and one the roster
// lacks.
func testPlayers() []PlayerMetrics {
	return []PlayerMetrics{
		{PlayerID: accountA.PlayerID, SteamID64: accountA.SteamID64, SkillLevel: 7, Team: 0, Metrics: []byte(`{"match":{"kills":20}}`)},
		{PlayerID: accountX.PlayerID, SteamID64: accountX.SteamID64, SkillLevel: 5, Team: 1, Metrics: []byte(`{"match":{"kills":11}}`)},
		{SteamID64: "76561190000000010", Team: 1, Metrics: []byte(`{"match":{"kills":3}}`)},
	}
}

type samplesRow struct {
	level   int
	mapName string
	version int
	metrics string
}

func readSamples(t *testing.T, s *Store) ([]int64, []samplesRow) {
	t.Helper()
	rows, err := s.db.Query(`SELECT id, skill_level, map, metrics_version, metrics FROM reference_samples ORDER BY metrics`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	var out []samplesRow
	for rows.Next() {
		var id int64
		var r samplesRow
		if err := rows.Scan(&id, &r.level, &r.mapName, &r.version, &r.metrics); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		out = append(out, r)
	}
	return ids, out
}

func matchPlayerIDs(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT player_id FROM match_players ORDER BY player_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

func TestSaveMatch(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t) // accountA linked by 42, accountB by 43

	if err := s.SaveMatch(ctx, testMatch(1), testPlayers(), jobsT0); err != nil {
		t.Fatalf("SaveMatch() error: %v", err)
	}
	// Only the player who uses the bot is kept with the IDs.
	if got := matchPlayerIDs(t, s); !slices.Equal(got, []string{accountA.PlayerID}) {
		t.Errorf("match_players = %v, want only the linked account", got)
	}
	var steam, metrics string
	var level, team int
	err := s.db.QueryRow(`SELECT steam_id64, skill_level, team, metrics FROM match_players`).Scan(&steam, &level, &team, &metrics)
	if err != nil {
		t.Fatal(err)
	}
	if steam != accountA.SteamID64 || level != 7 || team != 0 || metrics != `{"match":{"kills":20}}` {
		t.Errorf("match player = %s, level %d, team %d, %s", steam, level, team, metrics)
	}
	// Every player of a known level gives an anonymous sample.
	ids, samples := readSamples(t, s)
	want := []samplesRow{
		{7, "de_mirage", 1, `{"match":{"kills":20}}`},
		{5, "de_mirage", 1, `{"match":{"kills":11}}`},
	}
	slices.SortFunc(want, func(a, b samplesRow) int { return cmp.Compare(a.metrics, b.metrics) })
	if !slices.Equal(samples, want) {
		t.Errorf("reference samples = %+v, want %+v", samples, want)
	}
	for _, id := range ids {
		if id < 1<<20 {
			t.Errorf("reference sample ID %d, want a random one", id)
		}
	}

	r, err := s.MatchResult(ctx, matchM, accountA.PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	if want := (MatchResult{Map: "de_mirage", Score: [2]int{13, 9}, MetricsVersion: 1, Team: 0}); r != want {
		t.Errorf("MatchResult() = %+v, want %+v", r, want)
	}
	if r, err := s.MatchResult(ctx, matchM, accountX.PlayerID); err != nil || r.Team != -1 {
		t.Errorf("MatchResult() for a player not kept = %+v, %v; want team -1", r, err)
	}
	if _, err := s.MatchResult(ctx, matchN, accountA.PlayerID); !errors.Is(err, ErrNotFound) {
		t.Errorf("MatchResult() for a match not parsed: error %v, want ErrNotFound", err)
	}
}

func TestSaveMatchAgain(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	if err := s.SaveMatch(ctx, testMatch(1), testPlayers(), jobsT0); err != nil {
		t.Fatal(err)
	}

	// The player nobody linked starts using the bot and sends the match.
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 44, Language: "en"}, accountX, jobsT0); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMatch(ctx, testMatch(1), testPlayers(), jobsT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := matchPlayerIDs(t, s); !slices.Equal(got, []string{accountA.PlayerID, accountX.PlayerID}) {
		t.Errorf("match_players = %v, want both linked accounts", got)
	}
	// The same version: no second set of samples.
	if _, samples := readSamples(t, s); len(samples) != 2 {
		t.Errorf("%d reference samples after a second parse, want 2", len(samples))
	}
	var parsedAt int64
	if err := s.db.QueryRow(`SELECT parsed_at FROM matches WHERE match_id = ?`, matchM).Scan(&parsedAt); err != nil || parsedAt != jobsT0.Add(time.Hour).Unix() {
		t.Errorf("parsed_at = %d, %v; want the second parse", parsedAt, err)
	}

	// New formulas: the match gives samples of the new version.
	if err := s.SaveMatch(ctx, testMatch(2), testPlayers(), jobsT0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, samples := readSamples(t, s)
	versions := map[int]int{}
	for _, r := range samples {
		versions[r.version]++
	}
	if versions[1] != 2 || versions[2] != 2 {
		t.Errorf("reference samples by version = %v, want 2 of each", versions)
	}
	if r, err := s.MatchResult(ctx, matchM, accountX.PlayerID); err != nil || r.MetricsVersion != 2 || r.Team != 1 {
		t.Errorf("MatchResult() = %+v, %v; want version 2, team 1", r, err)
	}
}

func TestSaveSuspiciousMatch(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	m := testMatch(1)
	m.Suspicious, m.Warnings = true, []string{"round 3 has no winner"}
	if err := s.SaveMatch(ctx, m, testPlayers(), jobsT0); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "reference_samples"); n != 0 {
		t.Errorf("%d reference samples of a suspicious match, want none", n)
	}
	if got := matchPlayerIDs(t, s); len(got) != 1 {
		t.Errorf("match_players = %v, want the linked player kept", got)
	}
	var suspicious int
	var warnings string
	if err := s.db.QueryRow(`SELECT suspicious, warnings FROM matches`).Scan(&suspicious, &warnings); err != nil {
		t.Fatal(err)
	}
	if suspicious != 1 || warnings != `["round 3 has no winner"]` {
		t.Errorf("suspicious = %d, warnings = %s", suspicious, warnings)
	}
}

// TestReferenceSamplesHaveNoIDs pins the columns of the anonymous table:
// a new column there is a decision, not a detail.
func TestReferenceSamplesHaveNoIDs(t *testing.T) {
	s := newJobStore(t)
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info('reference_samples') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	if want := []string{"id", "skill_level", "map", "metrics_version", "metrics"}; !slices.Equal(cols, want) {
		t.Errorf("reference_samples columns = %v, want %v", cols, want)
	}
}

func TestRelinkDropsMatchRows(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	if err := s.SaveMatch(ctx, testMatch(1), testPlayers(), jobsT0); err != nil {
		t.Fatal(err)
	}
	// 45 links accountA too: when 42 moves to another account, the rows of
	// accountA stay.
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 45, Language: "ru"}, accountA, jobsT0); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 42, Language: "ru"}, accountX, jobsT0); err != nil {
		t.Fatal(err)
	}
	if got := matchPlayerIDs(t, s); !slices.Equal(got, []string{accountA.PlayerID}) {
		t.Errorf("match_players = %v, want accountA kept while 45 has it", got)
	}
	// 45 moves too: nobody uses accountA, its rows go, its samples stay.
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 45, Language: "ru"}, accountB, jobsT0); err != nil {
		t.Fatal(err)
	}
	if got := matchPlayerIDs(t, s); len(got) != 0 {
		t.Errorf("match_players = %v, want none", got)
	}
	if n := countRows(t, s, "reference_samples"); n != 2 {
		t.Errorf("%d reference samples, want 2 kept", n)
	}
	// Linking the same account again changes nothing.
	if err := s.SaveMatch(ctx, testMatch(1), testPlayers(), jobsT0); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 42, Language: "ru"}, accountX, jobsT0); err != nil {
		t.Fatal(err)
	}
	if got := matchPlayerIDs(t, s); !slices.Equal(got, []string{accountX.PlayerID}) {
		t.Errorf("match_players = %v, want accountX kept", got)
	}
}
