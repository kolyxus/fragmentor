package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound means there is no such record.
var ErrNotFound = errors.New("storage: not found")

// FaceitAccount is the FACEIT account a user linked.
type FaceitAccount struct {
	PlayerID  string // FACEIT player ID
	Nickname  string // at the time of linking
	SteamID64 string
	LinkedAt  time.Time // set by LinkFaceitAccount
}

// LinkFaceitAccount links the FACEIT account to the user, replacing the one
// linked before. A user the bot has not seen is created off the waitlist; a
// known one gets the username and language refreshed, as /start does.
//
// The parsed matches keep with an ID only the players who use the bot (see
// SaveMatch), so the account replaced loses its rows in them, unless
// another user has it linked too. Its reference samples stay: they are
// anonymous.
func (s *Store) LinkFaceitAccount(ctx context.Context, u User, acc FaceitAccount, now time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer rollback(tx, &err)

	var prev sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT player_id FROM faceit_accounts WHERE telegram_id = ?`, u.TelegramID).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("select faceit account: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO users (telegram_id, username, language, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (telegram_id) DO UPDATE SET username = excluded.username, language = excluded.language`,
		u.TelegramID, nullString(u.Username), u.Language, now.Unix())
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO faceit_accounts (telegram_id, player_id, nickname, steam_id64, linked_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (telegram_id) DO UPDATE SET
			player_id = excluded.player_id, nickname = excluded.nickname,
			steam_id64 = excluded.steam_id64, linked_at = excluded.linked_at`,
		u.TelegramID, acc.PlayerID, acc.Nickname, acc.SteamID64, now.Unix())
	if err != nil {
		return fmt.Errorf("upsert faceit account: %w", err)
	}
	if prev.Valid && prev.String != acc.PlayerID {
		_, err = tx.ExecContext(ctx, `DELETE FROM match_players WHERE player_id = ?
			AND NOT EXISTS (SELECT 1 FROM faceit_accounts WHERE player_id = ?)`, prev.String, prev.String)
		if err != nil {
			return fmt.Errorf("delete match players of the account replaced: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// FaceitAccount returns the FACEIT account linked by the user, ErrNotFound if
// there is none.
func (s *Store) FaceitAccount(ctx context.Context, telegramID int64) (FaceitAccount, error) {
	var acc FaceitAccount
	var linkedAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT player_id, nickname, steam_id64, linked_at FROM faceit_accounts WHERE telegram_id = ?`, telegramID).
		Scan(&acc.PlayerID, &acc.Nickname, &acc.SteamID64, &linkedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FaceitAccount{}, ErrNotFound
	}
	if err != nil {
		return FaceitAccount{}, fmt.Errorf("select faceit account: %w", err)
	}
	acc.LinkedAt = time.Unix(linkedAt, 0).UTC()
	return acc, nil
}
