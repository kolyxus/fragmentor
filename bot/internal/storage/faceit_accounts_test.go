package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

var (
	accountA = FaceitAccount{PlayerID: "00000000-0000-4000-8000-000000000001", Nickname: "player01", SteamID64: "76561190000000001"}
	accountB = FaceitAccount{PlayerID: "00000000-0000-4000-8000-000000000002", Nickname: "player02", SteamID64: "76561190000000002"}
)

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLinkFaceitAccount(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	if _, err := s.JoinWaitlist(ctx, User{TelegramID: 42, Username: "old", Language: "ru"}, t0); err != nil {
		t.Fatal(err)
	}

	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 42, Username: "new", Language: "en"}, accountA, t1); err != nil {
		t.Fatalf("LinkFaceitAccount() error: %v", err)
	}

	got, err := s.FaceitAccount(ctx, 42)
	if err != nil {
		t.Fatalf("FaceitAccount() error: %v", err)
	}
	want := accountA
	want.LinkedAt = t1
	if got != want {
		t.Errorf("FaceitAccount() = %+v, want %+v", got, want)
	}
	// The user is refreshed as on /start and stays on the waitlist.
	u := readUser(t, s, 42)
	wantUser := userRow{
		username:  sql.NullString{String: "new", Valid: true},
		language:  "en",
		joinedAt:  sql.NullInt64{Int64: t0.Unix(), Valid: true},
		createdAt: t0.Unix(),
	}
	if u != wantUser {
		t.Errorf("user = %+v, want %+v", u, wantUser)
	}
}

func TestLinkFaceitAccountReplaces(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	t0 := time.Unix(1000, 0).UTC()

	for i, acc := range []FaceitAccount{accountA, accountB} {
		if err := s.LinkFaceitAccount(ctx, User{TelegramID: 42, Language: "ru"}, acc, t0.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.FaceitAccount(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	want := accountB
	want.LinkedAt = t0.Add(time.Minute)
	if got != want {
		t.Errorf("FaceitAccount() = %+v, want %+v", got, want)
	}
	if n := countRows(t, s, "faceit_accounts"); n != 1 {
		t.Errorf("faceit_accounts has %d rows, want 1", n)
	}
}

func TestLinkFaceitAccountCreatesUserOffWaitlist(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	now := time.Unix(5000, 0).UTC()

	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 7, Language: "en"}, accountA, now); err != nil {
		t.Fatalf("LinkFaceitAccount() error: %v", err)
	}

	u := readUser(t, s, 7)
	if u != (userRow{language: "en", createdAt: 5000}) {
		t.Errorf("user = %+v, want created at 5000 without a username and off the waitlist", u)
	}
	// /start afterwards puts them on the waitlist as usual.
	joined, err := s.JoinWaitlist(ctx, User{TelegramID: 7, Language: "en"}, now.Add(time.Hour))
	if err != nil || !joined {
		t.Errorf("JoinWaitlist() = %t, %v, want joined", joined, err)
	}
}

func TestFaceitAccountNotFound(t *testing.T) {
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	if _, err := s.JoinWaitlist(context.Background(), User{TelegramID: 1, Language: "ru"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2} {
		if _, err := s.FaceitAccount(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Errorf("FaceitAccount(%d) error = %v, want ErrNotFound", id, err)
		}
	}
}

func TestSameFaceitAccountForTwoUsers(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	for _, id := range []int64{1, 2} {
		if err := s.LinkFaceitAccount(ctx, User{TelegramID: id, Language: "ru"}, accountA, time.Now()); err != nil {
			t.Fatalf("LinkFaceitAccount(%d) error: %v", id, err)
		}
	}
	if n := countRows(t, s, "faceit_accounts"); n != 2 {
		t.Errorf("faceit_accounts has %d rows, want 2", n)
	}
}

func TestDeletingUserDeletesLink(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	if err := s.LinkFaceitAccount(ctx, User{TelegramID: 42, Language: "ru"}, accountA, time.Now()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.Exec(`DELETE FROM users WHERE telegram_id = 42`); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "faceit_accounts"); n != 0 {
		t.Errorf("faceit_accounts has %d rows after the user was deleted, want 0", n)
	}
}
