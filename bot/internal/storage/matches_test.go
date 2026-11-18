package storage

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// accountX is in the matches, but no user linked it.
var accountX = FaceitAccount{PlayerID: "00000000-0000-4000-8000-000000000009", SteamID64: "76561190000000009"}

func testMatch(id string, version int, started time.Time) ParsedMatch {
	return ParsedMatch{
		MatchID: id, Map: "de_mirage", StartedAt: started,
		Score: [2]int{13, 9}, Rounds: 22, Warnings: []string{"a warning"}, MetricsVersion: version,
	}
}

func testJournal(version int) Journal {
	return Journal{Version: version, Data: []byte{0x28, 0xb5, 0x2f, 0xfd, 1, 2, 3}}
}

// testPlayers are a linked player, one nobody linked and one the roster
// lacks, each with facts of version 1.
func testPlayers(facts string) []MatchPlayer {
	return []MatchPlayer{
		{Seat: 1, PlayerID: accountA.PlayerID, SteamID64: accountA.SteamID64, SkillLevel: 7, Team: 0,
			Metrics: []byte(`{"match":{"kills":20}}`), Facts: []byte(facts), FactsVersion: 1},
		{Seat: 2, PlayerID: accountX.PlayerID, SteamID64: accountX.SteamID64, SkillLevel: 5, Team: 1,
			Metrics: []byte(`{"match":{"kills":11}}`), Facts: []byte(facts), FactsVersion: 1},
		{Seat: 3, SteamID64: "76561190000000010", Team: 1, Metrics: []byte(`{"match":{"kills":3}}`), Facts: []byte(facts), FactsVersion: 1},
	}
}

// factsOf returns the facts kept for each seat of the match, "" for none.
func factsOf(t *testing.T, s *Store, matchID string) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT COALESCE(facts, ''), COALESCE(facts_version, 0) FROM match_players WHERE match_id = ? ORDER BY seat`, matchID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		var v int
		if err := rows.Scan(&f, &v); err != nil {
			t.Fatal(err)
		}
		if (f == "") != (v == 0) {
			t.Errorf("facts %q of version %d", f, v)
		}
		out = append(out, f)
	}
	return out
}

func TestSaveAnalysis(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t) // accountA linked by 42, accountB by 43
	m := testMatch(matchM, 2, jobsT0.Add(-time.Hour))
	if err := s.SaveAnalysis(ctx, m, testJournal(1), testPlayers(`{"version":1}`), jobsT0); err != nil {
		t.Fatalf("SaveAnalysis() error: %v", err)
	}
	// Every player is kept, the facts only of the user of the bot.
	if got := factsOf(t, s, matchM); !slices.Equal(got, []string{`{"version":1}`, "", ""}) {
		t.Errorf("facts = %q, want those of accountA alone", got)
	}
	gotM, j, players, err := s.LoadAnalysis(ctx, matchM)
	if err != nil {
		t.Fatal(err)
	}
	if gotM.Map != m.Map || !gotM.StartedAt.Equal(m.StartedAt) || gotM.Score != m.Score || gotM.Rounds != 22 || gotM.MetricsVersion != 2 ||
		!slices.Equal(gotM.Warnings, m.Warnings) || gotM.Suspicious {
		t.Errorf("match = %+v, want %+v", gotM, m)
	}
	if j.Version != 1 || string(j.Data) != string(testJournal(1).Data) {
		t.Errorf("journal = %+v", j)
	}
	want := testPlayers(`{"version":1}`)
	want[1].Facts, want[1].FactsVersion, want[2].Facts, want[2].FactsVersion = nil, 0, nil, 0
	if len(players) != 3 {
		t.Fatalf("players = %+v", players)
	}
	for i, p := range players {
		w := want[i]
		if p.Seat != w.Seat || p.SteamID64 != w.SteamID64 || p.PlayerID != w.PlayerID || p.SkillLevel != w.SkillLevel || p.Team != w.Team ||
			string(p.Metrics) != string(w.Metrics) || string(p.Facts) != string(w.Facts) || p.FactsVersion != w.FactsVersion {
			t.Errorf("player %d = %+v, want %+v", i, p, w)
		}
	}

	// The match as each player saw it.
	for _, tc := range []struct {
		steamID string
		want    MatchResult
	}{
		{accountA.SteamID64, MatchResult{Map: "de_mirage", Score: [2]int{13, 9}, MetricsVersion: 2, JournalVersion: 1, Team: 0, Seat: 1, FactsVersion: 1}},
		{accountX.SteamID64, MatchResult{Map: "de_mirage", Score: [2]int{13, 9}, MetricsVersion: 2, JournalVersion: 1, Team: 1, Seat: 2}},
		{accountB.SteamID64, MatchResult{Map: "de_mirage", Score: [2]int{13, 9}, MetricsVersion: 2, JournalVersion: 1, Team: -1}},
	} {
		if r, err := s.MatchResult(ctx, matchM, tc.steamID); err != nil || r != tc.want {
			t.Errorf("MatchResult(%s) = %+v, %v; want %+v", tc.steamID, r, err, tc.want)
		}
	}
	if _, err := s.MatchResult(ctx, matchN, accountA.SteamID64); !errors.Is(err, ErrNotFound) {
		t.Errorf("MatchResult() of a match not parsed: %v, want ErrNotFound", err)
	}
	if _, _, _, err := s.LoadAnalysis(ctx, matchN); !errors.Is(err, ErrNotFound) {
		t.Errorf("LoadAnalysis() of a match not parsed: %v, want ErrNotFound", err)
	}

	// A second parse replaces the first: the players, the journal, the time.
	m.Map, m.Suspicious = "de_ancient", true
	if err := s.SaveAnalysis(ctx, m, testJournal(2), testPlayers(`{"version":2}`)[:1], jobsT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	gotM, j, players, err = s.LoadAnalysis(ctx, matchM)
	if err != nil || gotM.Map != "de_ancient" || !gotM.Suspicious || j.Version != 2 || len(players) != 1 || string(players[0].Facts) != `{"version":2}` {
		t.Errorf("after the second parse: %+v, %+v, %+v, %v", gotM, j, players, err)
	}
	var parsedAt, createdAt int64
	if err := s.db.QueryRow(`SELECT m.parsed_at, j.created_at FROM matches m JOIN match_journals j USING (match_id)`).Scan(&parsedAt, &createdAt); err != nil ||
		parsedAt != jobsT0.Add(time.Hour).Unix() || createdAt != parsedAt {
		t.Errorf("parsed at %d, journal created at %d, %v", parsedAt, createdAt, err)
	}
}

func TestSaveRecount(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	if err := s.SaveAnalysis(ctx, testMatch(matchM, 1, jobsT0), testJournal(1), testPlayers(""), jobsT0); err != nil {
		t.Fatal(err)
	}
	if got := factsOf(t, s, matchM); !slices.Equal(got, []string{"", "", ""}) {
		t.Errorf("facts without facts = %q", got)
	}
	players := testPlayers(`{"version":3}`)
	for i := range players {
		players[i].Metrics, players[i].FactsVersion = []byte(`{"recounted":true}`), 3
	}
	if err := s.SaveRecount(ctx, matchM, 2, players); err != nil {
		t.Fatal(err)
	}
	m, _, got, err := s.LoadAnalysis(ctx, matchM)
	if err != nil || m.MetricsVersion != 2 || len(got) != 3 {
		t.Fatalf("after the recount: %+v, %+v, %v", m, got, err)
	}
	for _, p := range got {
		if string(p.Metrics) != `{"recounted":true}` || (p.Seat == 1) != (string(p.Facts) == `{"version":3}` && p.FactsVersion == 3) {
			t.Errorf("player %+v", p)
		}
	}
	if err := s.SaveRecount(ctx, matchN, 2, players); !errors.Is(err, ErrNotFound) {
		t.Errorf("SaveRecount() of a match not parsed: %v, want ErrNotFound", err)
	}
}

func TestPastFacts(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	// accountA played five matches an hour apart; the third is of old
	// metrics, the fourth of old facts.
	ids := []string{
		"1-00000000-0000-4000-8000-0000000000b1", "1-00000000-0000-4000-8000-0000000000b2", "1-00000000-0000-4000-8000-0000000000b3",
		"1-00000000-0000-4000-8000-0000000000b4", "1-00000000-0000-4000-8000-0000000000b5",
	}
	for i, id := range ids {
		version, facts := 2, 1
		switch i {
		case 2:
			version = 1
		case 3:
			facts = 0
		}
		players := testPlayers(`{"match":` + string(rune('1'+i)) + `}`)
		players[0].FactsVersion = facts
		if facts == 0 {
			players[0].Facts = nil
		}
		if err := s.SaveAnalysis(ctx, testMatch(id, version, jobsT0.Add(time.Duration(i)*time.Hour)), testJournal(1), players, jobsT0); err != nil {
			t.Fatal(err)
		}
	}
	// One of an unknown start is nobody's past.
	if err := s.SaveAnalysis(ctx, testMatch(matchO, 2, time.Time{}), testJournal(1), testPlayers(`{"match":0}`), jobsT0); err != nil {
		t.Fatal(err)
	}
	past := func(matchID string, before time.Time, limit int) []string {
		t.Helper()
		got, err := s.PastFacts(ctx, accountA.SteamID64, matchID, before, 2, 1, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, f := range got {
			out = append(out, string(f))
		}
		return out
	}
	// Before the last match: the second and the first, the latest first.
	if got := past(ids[4], jobsT0.Add(4*time.Hour), 10); !slices.Equal(got, []string{`{"match":2}`, `{"match":1}`}) {
		t.Errorf("the past of the fifth match = %q", got)
	}
	if got := past(ids[4], jobsT0.Add(4*time.Hour), 1); !slices.Equal(got, []string{`{"match":2}`}) {
		t.Errorf("the last one of the past = %q", got)
	}
	// A match after all of them: the fifth too; the match itself is never
	// its own past.
	if got := past(ids[4], jobsT0.Add(9*time.Hour), 10); !slices.Equal(got, []string{`{"match":2}`, `{"match":1}`}) {
		t.Errorf("the past of the fifth match parsed again = %q", got)
	}
	if got := past(matchN, jobsT0.Add(9*time.Hour), 10); !slices.Equal(got, []string{`{"match":5}`, `{"match":2}`, `{"match":1}`}) {
		t.Errorf("the past of a later match = %q", got)
	}
	// One that started the same second is not before.
	if got := past(matchN, jobsT0.Add(4*time.Hour), 10); !slices.Equal(got, []string{`{"match":2}`, `{"match":1}`}) {
		t.Errorf("the past of a match that started with the fifth = %q", got)
	}
	if got := past(matchN, time.Time{}, 10); got != nil {
		t.Errorf("the past of a match of an unknown start = %q", got)
	}
	if got, err := s.PastFacts(ctx, accountX.SteamID64, matchN, jobsT0.Add(9*time.Hour), 2, 1, 10); err != nil || len(got) != 0 {
		t.Errorf("the past of a player nobody linked = %q, %v", got, err)
	}
}

func TestStaleMatches(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	stale := func() []string {
		t.Helper()
		got, err := s.StaleMatches(ctx, 1, 2, 3, 10)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	fresh := testPlayers(`{}`)
	for i := range fresh {
		fresh[i].FactsVersion = 3
	}
	// matchM is up to date; matchN has old metrics; matchO's user has old
	// facts; the fourth an old journal; the fifth no facts for its user.
	matches := []struct {
		id      string
		version int
		journal int
		facts   int
		started time.Duration
	}{
		{matchM, 2, 1, 3, 3 * time.Hour},
		{matchN, 1, 1, 3, 2 * time.Hour},
		{matchO, 2, 1, 2, time.Hour},
		{"1-00000000-0000-4000-8000-0000000000c1", 1, 0, 3, 4 * time.Hour},
		{"1-00000000-0000-4000-8000-0000000000c2", 2, 1, 0, 0},
	}
	for _, mt := range matches {
		players := testPlayers(`{}`)
		for i := range players {
			players[i].FactsVersion = mt.facts
			if mt.facts == 0 {
				players[i].Facts = nil
			}
		}
		if err := s.SaveAnalysis(ctx, testMatch(mt.id, mt.version, jobsT0.Add(mt.started)), testJournal(mt.journal), players, jobsT0); err != nil {
			t.Fatal(err)
		}
	}
	// The earliest first; the old journal needs its demo, not a recount.
	want := []string{"1-00000000-0000-4000-8000-0000000000c2", matchO, matchN}
	if got := stale(); !slices.Equal(got, want) {
		t.Errorf("StaleMatches() = %v, want %v", got, want)
	}
	if got, err := s.StaleMatches(ctx, 1, 2, 3, 1); err != nil || !slices.Equal(got, want[:1]) {
		t.Errorf("StaleMatches(1) = %v, %v", got, err)
	}
	// A player nobody linked needs no facts: accountX links later, and the
	// match he played with old facts becomes stale.
	if err := s.SaveRecount(ctx, matchO, 2, fresh); err != nil {
		t.Fatal(err)
	}
	if got := stale(); !slices.Equal(got, []string{"1-00000000-0000-4000-8000-0000000000c2", matchN}) {
		t.Errorf("StaleMatches() after a recount = %v", got)
	}
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 44, Language: "ru"}, accountX, jobsT0); err != nil {
		t.Fatal(err)
	}
	if got := stale(); !slices.Equal(got, []string{"1-00000000-0000-4000-8000-0000000000c2", matchO, matchN, matchM}) {
		t.Errorf("StaleMatches() after accountX is linked = %v", got)
	}
}

func TestRelinkDropsFacts(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	if err := s.SaveAnalysis(ctx, testMatch(matchM, 1, jobsT0), testJournal(1), testPlayers(`{"f":1}`), jobsT0); err != nil {
		t.Fatal(err)
	}
	// 45 links accountA too: when 42 moves to another account, the facts of
	// accountA stay.
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 45, Language: "ru"}, accountA, jobsT0); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 42, Language: "ru"}, accountX, jobsT0); err != nil {
		t.Fatal(err)
	}
	if got := factsOf(t, s, matchM); !slices.Equal(got, []string{`{"f":1}`, "", ""}) {
		t.Errorf("facts = %q, want those of accountA kept while 45 has it", got)
	}
	// 45 moves too: nobody uses accountA, its facts go, its row stays.
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 45, Language: "ru"}, accountB, jobsT0); err != nil {
		t.Fatal(err)
	}
	if got := factsOf(t, s, matchM); !slices.Equal(got, []string{"", "", ""}) {
		t.Errorf("facts = %q, want none", got)
	}
	if n := countRows(t, s, "match_players"); n != 3 {
		t.Errorf("%d match players, want all 3 kept", n)
	}
	// Linking the same account again drops nothing.
	if err := s.SaveAnalysis(ctx, testMatch(matchM, 1, jobsT0), testJournal(1), testPlayers(`{"f":2}`), jobsT0); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 42, Language: "ru"}, accountX, jobsT0); err != nil {
		t.Fatal(err)
	}
	if got := factsOf(t, s, matchM); !slices.Equal(got, []string{"", `{"f":2}`, ""}) {
		t.Errorf("facts = %q, want those of accountX", got)
	}
	// The same FACEIT account with another SteamID64: the facts belong to the
	// SteamID64, nobody links the old one, they go.
	moved := accountX
	moved.SteamID64 = "76561190000000099"
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 42, Language: "ru"}, moved, jobsT0); err != nil {
		t.Fatal(err)
	}
	if got := factsOf(t, s, matchM); !slices.Equal(got, []string{"", "", ""}) {
		t.Errorf("facts = %q, want none after the SteamID64 of accountX changed", got)
	}
}

// TestMatchPlayersChecks checks what the schema itself refuses: a seat
// below 1, a team other than 0 and 1, facts without their version.
func TestMatchPlayersChecks(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	if err := s.SaveAnalysis(ctx, testMatch(matchM, 2, jobsT0), testJournal(1), testPlayers(`{}`)[:1], jobsT0); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO match_players (match_id, seat, steam_id64, team, metrics, facts, facts_version) VALUES (?, ?, ?, ?, '{}', ?, ?)`
	for name, args := range map[string][]any{
		"seat 0":                {matchM, 0, "1", 0, nil, nil},
		"team 2":                {matchM, 5, "1", 2, nil, nil},
		"facts without version": {matchM, 5, "1", 0, "{}", nil},
		"a version of no facts": {matchM, 5, "1", 0, nil, 1},
	} {
		if _, err := s.db.ExecContext(ctx, insert, args...); err == nil {
			t.Errorf("%s: inserted", name)
		}
	}
	if _, err := s.db.ExecContext(ctx, insert, matchM, 5, "1", 1, "{}", 1); err != nil {
		t.Errorf("a valid row: %v", err)
	}
}

// TestMigrateStep7 migrates a database of step 7, its matches parsed, to
// the journals of step 8a: the anonymous samples and the matches without a
// journal go, the jobs and the accounts stay.
func TestMigrateStep7(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(tempDir(t), "step7.db")
	db, err := sql.Open("sqlite", path+dsnParams)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	t.Cleanup(func() { _ = db.Close() })
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := loadMigrations(sub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	for _, m := range ms[:4] {
		if err := s.apply(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO users (telegram_id, language, created_at) VALUES (42, 'ru', 1)`,
		`INSERT INTO faceit_accounts (telegram_id, player_id, nickname, steam_id64, linked_at) VALUES (42, 'p', 'n', '76561190000000001', 1)`,
		`INSERT INTO jobs (telegram_id, match_id, language, status, created_at) VALUES (42, '` + matchM + `', 'ru', 'done', 1)`,
		`INSERT INTO matches (match_id, map, team0_score, team1_score, rounds, suspicious, metrics_version, parsed_at)
			VALUES ('` + matchM + `', 'de_mirage', 13, 9, 22, 0, 1, 1)`,
		`INSERT INTO match_players (match_id, player_id, steam_id64, team, metrics) VALUES ('` + matchM + `', 'p', '76561190000000001', 0, '{}')`,
		`INSERT INTO reference_samples (id, skill_level, map, metrics_version, metrics) VALUES (7, 5, 'de_mirage', 1, '{}')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.migrate(ctx); err != nil {
		t.Fatalf("migrate() error: %v", err)
	}
	if v, err := s.SchemaVersion(ctx); err != nil || v != migrationCount(t) {
		t.Errorf("schema version %d, %v", v, err)
	}
	for table, want := range map[string]int{"users": 1, "faceit_accounts": 1, "jobs": 1, "matches": 0, "match_players": 0, "match_journals": 0} {
		if n := countRows(t, s, table); n != want {
			t.Errorf("%d rows in %s, want %d", n, table, want)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE name = 'reference_samples'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("reference_samples is still there: %d, %v", n, err)
	}
	// The new tables work.
	if err := s.SaveAnalysis(ctx, testMatch(matchM, 2, jobsT0), testJournal(1), testPlayers(`{}`), jobsT0); err != nil {
		t.Fatal(err)
	}
}

// TestMatchTablesHoldNoNames pins the columns of the tables of the parsed
// matches: a player is his SteamID64 and his FACEIT ID, no nickname is kept.
func TestMatchTablesHoldNoNames(t *testing.T) {
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	for table, want := range map[string][]string{
		"match_players":  {"match_id", "seat", "steam_id64", "player_id", "skill_level", "team", "metrics", "facts", "facts_version"},
		"match_journals": {"match_id", "version", "journal", "created_at"},
	} {
		rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
		if err != nil {
			t.Fatal(err)
		}
		var cols []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			cols = append(cols, c)
		}
		rows.Close()
		if !slices.Equal(cols, want) {
			t.Errorf("%s columns = %v, want %v", table, cols, want)
		}
	}
}
