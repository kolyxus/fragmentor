package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestUTCOffset(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	t0 := time.Unix(1000, 0).UTC()

	check := func(id int64, wantMinutes int, wantOK bool) {
		t.Helper()
		minutes, ok, err := s.UTCOffset(ctx, id)
		if err != nil {
			t.Fatalf("UTCOffset(%d) error: %v", id, err)
		}
		if minutes != wantMinutes || ok != wantOK {
			t.Errorf("UTCOffset(%d) = %d, %t, want %d, %t", id, minutes, ok, wantMinutes, wantOK)
		}
	}

	check(42, 0, false) // unknown user
	if _, err := s.JoinWaitlist(ctx, User{TelegramID: 42, Username: "player", Language: "ru"}, t0); err != nil {
		t.Fatal(err)
	}
	check(42, 0, false) // known user who has not chosen

	if err := s.SetUTCOffset(ctx, User{TelegramID: 42, Username: "player", Language: "ru"}, 300, t0.Add(time.Hour)); err != nil {
		t.Fatalf("SetUTCOffset() error: %v", err)
	}
	check(42, 300, true)
	// UTC is a choice too, not the absence of one.
	if err := s.SetUTCOffset(ctx, User{TelegramID: 42, Language: "en"}, 0, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	check(42, 0, true)

	got := readUser(t, s, 42)
	want := userRow{language: "en", joinedAt: sql.NullInt64{Int64: t0.Unix(), Valid: true}, createdAt: t0.Unix()}
	if got != want {
		t.Errorf("user = %+v, want %+v: the language refreshed, the rest kept", got, want)
	}
}

func TestSetUTCOffsetCreatesUserOffWaitlist(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))

	if err := s.SetUTCOffset(ctx, User{TelegramID: 7, Language: "ru"}, 720, time.Unix(5000, 0)); err != nil {
		t.Fatalf("SetUTCOffset() error: %v", err)
	}

	if u := readUser(t, s, 7); u != (userRow{language: "ru", createdAt: 5000}) {
		t.Errorf("user = %+v, want created at 5000 off the waitlist", u)
	}
	if minutes, ok, err := s.UTCOffset(ctx, 7); err != nil || !ok || minutes != 720 {
		t.Errorf("UTCOffset() = %d, %t, %v, want 720", minutes, ok, err)
	}
}
