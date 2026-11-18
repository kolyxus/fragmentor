package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ParsedMatch is a FACEIT match whose demo the bot parsed.
type ParsedMatch struct {
	MatchID   string
	Map       string    // like "de_mirage"
	StartedAt time.Time // from FACEIT, zero if it did not tell
	// Score is the rounds won by each team; team 0 started the match as
	// CT, as in demo.Match.
	Score  [2]int
	Rounds int
	// Suspicious means the summary of the demo may be wrong, Warnings tell
	// why; see demo.Match.
	Suspicious bool
	Warnings   []string
	// MetricsVersion is metrics.Version of the metrics of the players.
	MetricsVersion int
}

// Journal is the journal of a parsed match: what the bot keeps of its demo,
// see demo.EncodeJournal. Version is demo.JournalVersion.
type Journal struct {
	Version int
	Data    []byte
}

// MatchPlayer is a player of a parsed match.
type MatchPlayer struct {
	Seat      int // his number in the journal, from 1
	SteamID64 string
	// PlayerID and SkillLevel are his FACEIT player ID and level in the
	// match, "" and 0 if the roster of the match lacks him.
	PlayerID   string
	SkillLevel int
	Team       int // 0 or 1, as in ParsedMatch.Score
	// Metrics is JSON of his metrics for the match and for each side.
	Metrics []byte
	// Facts is JSON of the facts of his review, FactsVersion their version.
	// Only a user of the bot keeps them, see SaveAnalysis: for anyone else
	// they are nil when loaded.
	Facts        []byte
	FactsVersion int
}

// MatchResult is a parsed match as one of its players saw it.
type MatchResult struct {
	Map            string
	Score          [2]int // as in ParsedMatch
	MetricsVersion int
	// JournalVersion is the version of the journal of the match, 0 if it
	// has none.
	JournalVersion int
	// Team and Seat are those of the player, -1 and 0 if he did not play
	// the match; FactsVersion is the version of his facts, 0 if he has none.
	Team         int
	Seat         int
	FactsVersion int
}

// SaveAnalysis keeps the parsed match m, its journal and its players, in
// place of what an earlier parse of the match left. Facts are kept only for
// the users of the bot, players whose account some user has linked by
// SteamID64: the rule lives here, in the same transaction as the links, and
// the facts of anyone else are dropped.
func (s *Store) SaveAnalysis(ctx context.Context, m ParsedMatch, j Journal, players []MatchPlayer, now time.Time) (err error) {
	var warnings sql.NullString
	if len(m.Warnings) > 0 {
		b, err := json.Marshal(m.Warnings)
		if err != nil {
			return fmt.Errorf("encode warnings: %w", err)
		}
		warnings = sql.NullString{String: string(b), Valid: true}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer rollback(tx, &err)

	_, err = tx.ExecContext(ctx, `INSERT INTO matches (match_id, map, started_at, team0_score, team1_score, rounds,
			suspicious, warnings, metrics_version, parsed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (match_id) DO UPDATE SET map = excluded.map, started_at = excluded.started_at,
			team0_score = excluded.team0_score, team1_score = excluded.team1_score, rounds = excluded.rounds,
			suspicious = excluded.suspicious, warnings = excluded.warnings,
			metrics_version = excluded.metrics_version, parsed_at = excluded.parsed_at`,
		m.MatchID, m.Map, nullUnix(m.StartedAt), m.Score[0], m.Score[1], m.Rounds,
		boolInt(m.Suspicious), warnings, m.MetricsVersion, now.Unix())
	if err != nil {
		return fmt.Errorf("upsert match: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO match_journals (match_id, version, journal, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (match_id) DO UPDATE SET version = excluded.version, journal = excluded.journal, created_at = excluded.created_at`,
		m.MatchID, j.Version, j.Data, now.Unix())
	if err != nil {
		return fmt.Errorf("upsert journal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM match_players WHERE match_id = ?`, m.MatchID); err != nil {
		return fmt.Errorf("delete match players: %w", err)
	}
	for _, p := range players {
		_, err := tx.ExecContext(ctx, `INSERT INTO match_players (match_id, seat, steam_id64, player_id, skill_level, team, metrics)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			m.MatchID, p.Seat, p.SteamID64, nullString(p.PlayerID), nullInt(p.SkillLevel), p.Team, string(p.Metrics))
		if err != nil {
			return fmt.Errorf("insert match player: %w", err)
		}
		if err := setFacts(ctx, tx, m.MatchID, p); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// setFacts keeps the facts of the player if he is a user of the bot, and
// drops them if not.
func setFacts(ctx context.Context, tx *sql.Tx, matchID string, p MatchPlayer) error {
	var (
		linked  bool
		facts   sql.NullString
		version sql.NullInt64
	)
	if len(p.Facts) > 0 {
		err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM faceit_accounts WHERE steam_id64 = ?)`, p.SteamID64).Scan(&linked)
		if err != nil {
			return fmt.Errorf("select link: %w", err)
		}
	}
	if linked {
		facts = sql.NullString{String: string(p.Facts), Valid: true}
		version = sql.NullInt64{Int64: int64(p.FactsVersion), Valid: true}
	}
	_, err := tx.ExecContext(ctx, `UPDATE match_players SET facts = ?, facts_version = ? WHERE match_id = ? AND seat = ?`,
		facts, version, matchID, p.Seat)
	if err != nil {
		return fmt.Errorf("set facts: %w", err)
	}
	return nil
}

// SaveRecount keeps what was counted again from the journal of the match:
// the version of the metrics, the metrics of every player and the facts of
// the users of the bot, by seat. The journal and the rest stay.
func (s *Store) SaveRecount(ctx context.Context, matchID string, metricsVersion int, players []MatchPlayer) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer rollback(tx, &err)
	res, err := tx.ExecContext(ctx, `UPDATE matches SET metrics_version = ? WHERE match_id = ?`, metricsVersion, matchID)
	if err != nil {
		return fmt.Errorf("update match: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("update match: %w", err)
	} else if n == 0 {
		return ErrNotFound
	}
	for _, p := range players {
		if _, err := tx.ExecContext(ctx, `UPDATE match_players SET metrics = ? WHERE match_id = ? AND seat = ?`,
			string(p.Metrics), matchID, p.Seat); err != nil {
			return fmt.Errorf("update metrics: %w", err)
		}
		if err := setFacts(ctx, tx, matchID, p); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// LoadAnalysis returns a parsed match, its journal and its players in the
// order of their seats: what counting again needs. A match without a journal
// is ErrNotFound.
func (s *Store) LoadAnalysis(ctx context.Context, matchID string) (ParsedMatch, Journal, []MatchPlayer, error) {
	m := ParsedMatch{MatchID: matchID}
	var (
		j          Journal
		started    sql.NullInt64
		suspicious int
		warnings   sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT m.map, m.started_at, m.team0_score, m.team1_score, m.rounds, m.suspicious,
			m.warnings, m.metrics_version, j.version, j.journal
		FROM matches m JOIN match_journals j ON j.match_id = m.match_id WHERE m.match_id = ?`, matchID).
		Scan(&m.Map, &started, &m.Score[0], &m.Score[1], &m.Rounds, &suspicious, &warnings, &m.MetricsVersion, &j.Version, &j.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return ParsedMatch{}, Journal{}, nil, ErrNotFound
	}
	if err != nil {
		return ParsedMatch{}, Journal{}, nil, fmt.Errorf("select match: %w", err)
	}
	if started.Valid {
		m.StartedAt = time.Unix(started.Int64, 0).UTC()
	}
	m.Suspicious = suspicious == 1
	if warnings.Valid {
		if err := json.Unmarshal([]byte(warnings.String), &m.Warnings); err != nil {
			return ParsedMatch{}, Journal{}, nil, fmt.Errorf("decode warnings: %w", err)
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seat, steam_id64, player_id, skill_level, team, metrics, facts, facts_version
		FROM match_players WHERE match_id = ? ORDER BY seat`, matchID)
	if err != nil {
		return ParsedMatch{}, Journal{}, nil, fmt.Errorf("select match players: %w", err)
	}
	defer rows.Close()
	var players []MatchPlayer
	for rows.Next() {
		var (
			p             MatchPlayer
			playerID      sql.NullString
			level, factsV sql.NullInt64
			metrics       string
			facts         sql.NullString
		)
		if err := rows.Scan(&p.Seat, &p.SteamID64, &playerID, &level, &p.Team, &metrics, &facts, &factsV); err != nil {
			return ParsedMatch{}, Journal{}, nil, fmt.Errorf("scan match player: %w", err)
		}
		p.PlayerID, p.SkillLevel, p.Metrics = playerID.String, int(level.Int64), []byte(metrics)
		if facts.Valid {
			p.Facts, p.FactsVersion = []byte(facts.String), int(factsV.Int64)
		}
		players = append(players, p)
	}
	if err := rows.Err(); err != nil {
		return ParsedMatch{}, Journal{}, nil, fmt.Errorf("read match players: %w", err)
	}
	return m, j, players, nil
}

// MatchResult returns the parsed match and the player of the SteamID64 in
// it, ErrNotFound if the match was not parsed.
func (s *Store) MatchResult(ctx context.Context, matchID, steamID64 string) (MatchResult, error) {
	r := MatchResult{Team: -1}
	var (
		journal, seat, team, facts sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `SELECT m.map, m.team0_score, m.team1_score, m.metrics_version,
			(SELECT j.version FROM match_journals j WHERE j.match_id = m.match_id),
			p.seat, p.team, p.facts_version
		FROM matches m LEFT JOIN match_players p ON p.match_id = m.match_id AND p.steam_id64 = ?
		WHERE m.match_id = ? ORDER BY p.seat LIMIT 1`, steamID64, matchID).
		Scan(&r.Map, &r.Score[0], &r.Score[1], &r.MetricsVersion, &journal, &seat, &team, &facts)
	if errors.Is(err, sql.ErrNoRows) {
		return MatchResult{}, ErrNotFound
	}
	if err != nil {
		return MatchResult{}, fmt.Errorf("select match: %w", err)
	}
	r.JournalVersion, r.FactsVersion = int(journal.Int64), int(facts.Int64)
	if seat.Valid {
		r.Seat, r.Team = int(seat.Int64), int(team.Int64)
	}
	return r, nil
}

// PastFacts returns the facts of the player of the SteamID64 in the matches
// he played before the match that started at before, the match itself
// aside: the last limit of them with facts of factsVersion and metrics of
// metricsVersion, the latest first. That is the past his own average is made
// of. A match of an unknown start has no past, and is no past of another.
func (s *Store) PastFacts(ctx context.Context, steamID64, matchID string, before time.Time, metricsVersion, factsVersion, limit int) ([][]byte, error) {
	if before.IsZero() {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.facts FROM match_players p JOIN matches m ON m.match_id = p.match_id
		WHERE p.steam_id64 = ? AND p.facts_version = ? AND m.metrics_version = ? AND m.match_id <> ? AND m.started_at < ?
		ORDER BY m.started_at DESC, m.match_id LIMIT ?`,
		steamID64, factsVersion, metricsVersion, matchID, before.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("select past facts: %w", err)
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("scan past facts: %w", err)
		}
		out = append(out, []byte(f))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read past facts: %w", err)
	}
	return out, nil
}

// StaleMatches returns up to limit matches to count again from their
// journals, the earliest played first: their journal is of journalVersion,
// and their metrics are not of metricsVersion, or a user of the bot in them
// has no facts of factsVersion, a user who linked his account after the
// match was parsed among them. A match of an older journal needs its demo
// again, and is no such match.
func (s *Store) StaleMatches(ctx context.Context, journalVersion, metricsVersion, factsVersion, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.match_id FROM matches m JOIN match_journals j ON j.match_id = m.match_id
		WHERE j.version = ? AND (m.metrics_version <> ? OR EXISTS (
			SELECT 1 FROM match_players p JOIN faceit_accounts a ON a.steam_id64 = p.steam_id64
			WHERE p.match_id = m.match_id AND (p.facts_version IS NULL OR p.facts_version <> ?)))
		ORDER BY m.started_at IS NULL, m.started_at, m.match_id LIMIT ?`,
		journalVersion, metricsVersion, factsVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("select stale matches: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stale match: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stale matches: %w", err)
	}
	return out, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullInt stores 0 as NULL.
func nullInt(n int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(n), Valid: n != 0}
}
