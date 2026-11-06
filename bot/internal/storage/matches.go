package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
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

// PlayerMetrics is a player of a parsed match and their metrics.
type PlayerMetrics struct {
	PlayerID   string // FACEIT player ID, "" if the roster of the match lacks the player
	SteamID64  string
	SkillLevel int // FACEIT level in the match, 0 if unknown
	Team       int // 0 or 1, as in ParsedMatch.Score
	// Metrics is JSON of the metrics for the match and each side. It must
	// not hold IDs: it goes into the anonymous reference samples.
	Metrics []byte
}

// MatchResult is a parsed match as one of its players saw it.
type MatchResult struct {
	Map            string
	Score          [2]int // as in ParsedMatch
	MetricsVersion int
	// Team is the team of the player, -1 if the player is not kept with
	// the match.
	Team int
}

// SaveMatch keeps the parsed match m and its players, in place of what an
// earlier parse of the match left:
//   - the match itself;
//   - the players who use the bot, those whose FACEIT account some user has
//     linked: their IDs, level, team and metrics. Nobody else is kept with
//     an ID, whatever players holds;
//   - a reference sample of every player of a known level, with the level,
//     the map and the metrics alone, if the match has not added its samples
//     for this metrics version yet and its summary is not suspicious. A
//     second parse of the match adds no samples: they cannot be told apart
//     to be replaced.
func (s *Store) SaveMatch(ctx context.Context, m ParsedMatch, players []PlayerMetrics, now time.Time) (err error) {
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

	var prev sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT metrics_version FROM matches WHERE match_id = ?`, m.MatchID).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("select match: %w", err)
	}
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

	if _, err := tx.ExecContext(ctx, `DELETE FROM match_players WHERE match_id = ?`, m.MatchID); err != nil {
		return fmt.Errorf("delete match players: %w", err)
	}
	for _, p := range players {
		if p.PlayerID == "" {
			continue
		}
		// The row goes in only if the account is linked: the rule lives
		// here, in the same transaction as the links.
		_, err := tx.ExecContext(ctx, `INSERT INTO match_players (match_id, player_id, steam_id64, skill_level, team, metrics)
			SELECT ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM faceit_accounts WHERE player_id = ?)`,
			m.MatchID, p.PlayerID, p.SteamID64, nullInt(p.SkillLevel), p.Team, string(p.Metrics), p.PlayerID)
		if err != nil {
			return fmt.Errorf("insert match player: %w", err)
		}
	}

	if !m.Suspicious && (!prev.Valid || int(prev.Int64) != m.MetricsVersion) {
		for _, p := range players {
			if p.SkillLevel < 1 || p.SkillLevel > 10 {
				continue
			}
			if err := insertSample(ctx, tx, p.SkillLevel, m.Map, m.MetricsVersion, p.Metrics); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// insertSample adds a reference sample under a random ID, so that the order
// of the rows does not follow the order the matches came in. A taken ID,
// one in 2^63, is drawn again.
func insertSample(ctx context.Context, tx *sql.Tx, level int, mapName string, version int, metrics []byte) error {
	for range 3 {
		res, err := tx.ExecContext(ctx, `INSERT INTO reference_samples (id, skill_level, map, metrics_version, metrics)
			VALUES (?, ?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`,
			rand.Int64N(1<<63-1)+1, level, mapName, version, string(metrics))
		if err != nil {
			return fmt.Errorf("insert reference sample: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("insert reference sample: %w", err)
		} else if n == 1 {
			return nil
		}
	}
	return errors.New("insert reference sample: no free ID")
}

// MatchResult returns the parsed match and the team of the FACEIT player in
// it, ErrNotFound if the match was not parsed.
func (s *Store) MatchResult(ctx context.Context, matchID, playerID string) (MatchResult, error) {
	var r MatchResult
	err := s.db.QueryRowContext(ctx, `SELECT m.map, m.team0_score, m.team1_score, m.metrics_version,
			COALESCE((SELECT p.team FROM match_players p WHERE p.match_id = m.match_id AND p.player_id = ?), -1)
		FROM matches m WHERE m.match_id = ?`, playerID, matchID).
		Scan(&r.Map, &r.Score[0], &r.Score[1], &r.MetricsVersion, &r.Team)
	if errors.Is(err, sql.ErrNoRows) {
		return MatchResult{}, ErrNotFound
	}
	if err != nil {
		return MatchResult{}, fmt.Errorf("select match: %w", err)
	}
	return r, nil
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
