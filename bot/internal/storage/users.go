package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// User is a Telegram user of the bot.
type User struct {
	TelegramID int64
	Username   string // without @, empty if the user has none
	Language   string // bot language, "ru" or "en"
}

// JoinWaitlist puts the user on the beta waitlist, creating them on first
// contact. For a known user it refreshes the username and language and keeps
// the time they were created and first joined. It reports whether the user
// joined the waitlist just now.
func (s *Store) JoinWaitlist(ctx context.Context, u User, now time.Time) (joined bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer rollback(tx, &err)

	var joinedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT joined_waitlist_at FROM users WHERE telegram_id = ?`, u.TelegramID).Scan(&joinedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx,
			`INSERT INTO users (telegram_id, username, language, joined_waitlist_at, created_at) VALUES (?, ?, ?, ?, ?)`,
			u.TelegramID, nullString(u.Username), u.Language, now.Unix(), now.Unix())
		if err != nil {
			return false, fmt.Errorf("insert user: %w", err)
		}
		joined = true
	case err != nil:
		return false, fmt.Errorf("select user: %w", err)
	default:
		_, err = tx.ExecContext(ctx,
			`UPDATE users SET username = ?, language = ?, joined_waitlist_at = COALESCE(joined_waitlist_at, ?) WHERE telegram_id = ?`,
			nullString(u.Username), u.Language, now.Unix(), u.TelegramID)
		if err != nil {
			return false, fmt.Errorf("update user: %w", err)
		}
		joined = !joinedAt.Valid
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return joined, nil
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
