package storage

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error: %v", err)
		}
	})
	return s
}

// migrationCount returns how many migrations the build has.
func migrationCount(t *testing.T) int {
	t.Helper()
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := loadMigrations(sub)
	if err != nil {
		t.Fatal(err)
	}
	return len(ms)
}

func TestOpenMigratesNewDatabase(t *testing.T) {
	path := filepath.Join(tempDir(t), "nested", "dir", "test.db")
	s := openTestStore(t, path)

	v, err := s.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := migrationCount(t); v != want {
		t.Errorf("SchemaVersion() = %d, want %d", v, want)
	}
	for _, table := range []string{"users", "faceit_accounts"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("%s table: %v", table, err)
		}
	}
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestOpenTwiceAppliesMigrationsOnce(t *testing.T) {
	path := filepath.Join(tempDir(t), "test.db")
	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	s := openTestStore(t, path)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if want := migrationCount(t); n != want {
		t.Errorf("schema_migrations has %d rows, want %d", n, want)
	}
}

// TestOpenUpgradesVersion1 opens a database as the build of step 2 left it,
// with only the users table, as on the server.
func TestOpenUpgradesVersion1(t *testing.T) {
	path := filepath.Join(tempDir(t), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := fs.ReadFile(migrationFiles, "migrations/0001_create_users.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT`,
		string(first),
		`INSERT INTO schema_migrations VALUES (1, 'create_users', 100)`,
		`INSERT INTO users (telegram_id, username, language, joined_waitlist_at, created_at) VALUES (42, 'player', 'ru', 100, 100)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("prepare version 1: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s := openTestStore(t, path)
	v, err := s.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := migrationCount(t); v != want {
		t.Errorf("SchemaVersion() = %d, want %d", v, want)
	}
	got := readUser(t, s, 42)
	if got.language != "ru" || got.joinedAt.Int64 != 100 {
		t.Errorf("user after the upgrade = %+v", got)
	}
	if _, err := s.FaceitAccount(context.Background(), 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("FaceitAccount() error = %v, want ErrNotFound", err)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(tempDir(t), "test.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (99, 'from_the_future', 0)`)
	if closeErr := s.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}

	s, err = Open(context.Background(), path)
	if err == nil {
		s.Close()
		t.Fatal("Open() succeeded on a database from a newer build")
	}
	if !strings.Contains(err.Error(), "newer than this build") {
		t.Errorf("Open() error = %v, want a schema version error", err)
	}
}

func TestLoadMigrations(t *testing.T) {
	file := func(sql string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(sql)} }

	ms, err := loadMigrations(fstest.MapFS{
		"0002_add_b.sql":    file("B"),
		"0001_create_a.sql": file("A"),
	})
	if err != nil {
		t.Fatalf("loadMigrations() error: %v", err)
	}
	if len(ms) != 2 || ms[0].version != 1 || ms[0].name != "create_a" || ms[0].sql != "A" || ms[1].version != 2 {
		t.Errorf("loadMigrations() = %+v", ms)
	}

	bad := map[string]fstest.MapFS{
		"bad name":  {"1_create_a.sql": file("A")},
		"not sql":   {"0001_create_a.txt": file("A")},
		"gap":       {"0001_a.sql": file("A"), "0003_c.sql": file("C")},
		"repeat":    {"0001_a.sql": file("A"), "0001_b.sql": file("B")},
		"from zero": {"0000_a.sql": file("A")},
	}
	for name, fsys := range bad {
		if _, err := loadMigrations(fsys); err == nil {
			t.Errorf("%s: loadMigrations() succeeded, want error", name)
		}
	}
}

type userRow struct {
	username  sql.NullString
	language  string
	joinedAt  sql.NullInt64
	createdAt int64
}

func readUser(t *testing.T, s *Store, id int64) userRow {
	t.Helper()
	var r userRow
	err := s.db.QueryRow(`SELECT username, language, joined_waitlist_at, created_at FROM users WHERE telegram_id = ?`, id).
		Scan(&r.username, &r.language, &r.joinedAt, &r.createdAt)
	if err != nil {
		t.Fatalf("read user %d: %v", id, err)
	}
	return r
}

func countUsers(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestJoinWaitlistRepeatedDoesNotDuplicate(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	t0 := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(48 * time.Hour)

	joined, err := s.JoinWaitlist(ctx, User{TelegramID: 42, Username: "player", Language: "ru"}, t0)
	if err != nil {
		t.Fatalf("first JoinWaitlist() error: %v", err)
	}
	if !joined {
		t.Error("first JoinWaitlist() = false, want true")
	}
	got := readUser(t, s, 42)
	want := userRow{
		username:  sql.NullString{String: "player", Valid: true},
		language:  "ru",
		joinedAt:  sql.NullInt64{Int64: t0.Unix(), Valid: true},
		createdAt: t0.Unix(),
	}
	if got != want {
		t.Errorf("after first join: %+v, want %+v", got, want)
	}

	// The user dropped their username and switched Telegram to English.
	joined, err = s.JoinWaitlist(ctx, User{TelegramID: 42, Language: "en"}, t1)
	if err != nil {
		t.Fatalf("second JoinWaitlist() error: %v", err)
	}
	if joined {
		t.Error("second JoinWaitlist() = true, want false")
	}
	if n := countUsers(t, s); n != 1 {
		t.Fatalf("users has %d rows after a repeated join, want 1", n)
	}
	got = readUser(t, s, 42)
	want = userRow{language: "en", joinedAt: want.joinedAt, createdAt: want.createdAt}
	if got != want {
		t.Errorf("after second join: %+v, want %+v", got, want)
	}
}

func TestJoinWaitlistSeparateUsers(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	now := time.Now()
	for _, id := range []int64{1, 2, 1} {
		if _, err := s.JoinWaitlist(ctx, User{TelegramID: id, Language: "ru"}, now); err != nil {
			t.Fatal(err)
		}
	}
	if n := countUsers(t, s); n != 2 {
		t.Errorf("users has %d rows, want 2", n)
	}
}

func TestJoinWaitlistKnownUserNotOnWaitlist(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	_, err := s.db.Exec(`INSERT INTO users (telegram_id, language, created_at) VALUES (7, 'en', 100)`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(500, 0)

	joined, err := s.JoinWaitlist(ctx, User{TelegramID: 7, Language: "en"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !joined {
		t.Error("JoinWaitlist() = false for a user who was not on the waitlist")
	}
	got := readUser(t, s, 7)
	if got.joinedAt != (sql.NullInt64{Int64: 500, Valid: true}) || got.createdAt != 100 {
		t.Errorf("user = %+v, want joined at 500 and created at 100", got)
	}
}
