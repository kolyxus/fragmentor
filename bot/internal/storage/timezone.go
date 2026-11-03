package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// UTCOffset returns the time zone the user chose, as minutes east of UTC.
// ok is false if they have not chosen one.
func (s *Store) UTCOffset(ctx context.Context, telegramID int64) (minutes int, ok bool, err error) {
	var v sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT utc_offset FROM users WHERE telegram_id = ?`, telegramID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("select utc offset: %w", err)
	}
	return int(v.Int64), v.Valid, nil
}

// SetUTCOffset saves the time zone the user chose, as minutes east of UTC.
// Like LinkFaceitAccount, it creates a user the bot has not seen off the
// waitlist and refreshes the username and language of a known one.
func (s *Store) SetUTCOffset(ctx context.Context, u User, minutes int, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (telegram_id, username, language, created_at, utc_offset) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (telegram_id) DO UPDATE SET
			username = excluded.username, language = excluded.language, utc_offset = excluded.utc_offset`,
		u.TelegramID, nullString(u.Username), u.Language, now.Unix(), minutes)
	if err != nil {
		return fmt.Errorf("upsert utc offset: %w", err)
	}
	return nil
}
