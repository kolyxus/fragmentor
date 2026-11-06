package config

import (
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"plain", "A=1\nB=two", map[string]string{"A": "1", "B": "two"}},
		{"comments and blank lines", "# comment\n\n  # indented\nA=1\n", map[string]string{"A": "1"}},
		{"spaces around", "  A = 1  \n", map[string]string{"A": "1"}},
		{"empty value", "A=\nB=  ", map[string]string{"A": "", "B": ""}},
		{"export prefix", "export A=1\nexport\tB=2", map[string]string{"A": "1", "B": "2"}},
		{"export as a name", "export=1\nexported=2", map[string]string{"export": "1", "exported": "2"}},
		{"double quotes", `A="x # y = z"`, map[string]string{"A": "x # y = z"}},
		{"single quotes", `A='x "y"'`, map[string]string{"A": `x "y"`}},
		{"no escapes inside quotes", `A='it''s'`, nil},
		{"quoted with comment", `A="x" # note`, map[string]string{"A": "x"}},
		{"inline comment", "A=x # note\nB=y\t# note", map[string]string{"A": "x", "B": "y"}},
		{"hash inside value", "A=x#y", map[string]string{"A": "x#y"}},
		{"token-like value", "A=123456:AA-bb_CC=dd", map[string]string{"A": "123456:AA-bb_CC=dd"}},
		{"crlf and bom", "\xef\xbb\xbfA=1\r\nB=2\r\n", map[string]string{"A": "1", "B": "2"}},
		{"last wins", "A=1\nA=2", map[string]string{"A": "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDotEnv([]byte(tt.in))
			if tt.want == nil {
				if err == nil {
					t.Fatalf("parseDotEnv() = %v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDotEnv() error: %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("parseDotEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseDotEnvErrorsHideLineContent(t *testing.T) {
	const secret = "123456:SECRET-TOKEN"
	tests := []struct {
		name, in, wantErr string
	}{
		{"no equals sign", "A=1\n" + secret, "line 2: expected KEY=VALUE"},
		{"bad name", secret + "=1", "line 1: invalid variable name"},
		{"unterminated quote", `A="` + secret, "line 1: unterminated quoted value"},
		{"text after quotes", `A="x" ` + secret, "line 1: unexpected text after quoted value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDotEnv([]byte(tt.in))
			if err == nil {
				t.Fatal("parseDotEnv() succeeded, want error")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Errorf("error %q leaks the line content", err)
			}
		})
	}
}

func TestParse(t *testing.T) {
	// required holds the variables every valid config needs.
	required := map[string]string{"TELEGRAM_BOT_TOKEN": "tok", "FACEIT_API_KEY": "fkey"}
	with := func(extra map[string]string) map[string]string {
		env := maps.Clone(required)
		maps.Copy(env, extra)
		return env
	}
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string
	}{
		{
			name: "defaults",
			env:  required,
			want: Config{TelegramToken: "tok", FaceitAPIKey: "fkey", DatabasePath: DefaultDatabasePath, LogLevel: slog.LevelInfo},
		},
		{
			name: "all set",
			env:  map[string]string{"TELEGRAM_BOT_TOKEN": " tok ", "FACEIT_API_KEY": " fkey ", "DATABASE_PATH": "x.db", "LOG_LEVEL": "debug"},
			want: Config{TelegramToken: "tok", FaceitAPIKey: "fkey", DatabasePath: "x.db", LogLevel: slog.LevelDebug},
		},
		{
			name: "level is case-insensitive",
			env:  with(map[string]string{"LOG_LEVEL": "WARN"}),
			want: Config{TelegramToken: "tok", FaceitAPIKey: "fkey", DatabasePath: DefaultDatabasePath, LogLevel: slog.LevelWarn},
		},
		{
			name: "dev token wins",
			env:  with(map[string]string{"TELEGRAM_BOT_TOKEN": "prod", "TELEGRAM_DEV_BOT_TOKEN": " dev "}),
			want: Config{TelegramToken: "dev", DevBot: true, FaceitAPIKey: "fkey", DatabasePath: DefaultDatabasePath, LogLevel: slog.LevelInfo},
		},
		{
			name: "dev token alone is enough",
			env:  map[string]string{"TELEGRAM_DEV_BOT_TOKEN": "dev", "FACEIT_API_KEY": "fkey"},
			want: Config{TelegramToken: "dev", DevBot: true, FaceitAPIKey: "fkey", DatabasePath: DefaultDatabasePath, LogLevel: slog.LevelInfo},
		},
		{
			name: "blank dev token is ignored",
			env:  with(map[string]string{"TELEGRAM_BOT_TOKEN": "prod", "TELEGRAM_DEV_BOT_TOKEN": "  "}),
			want: Config{TelegramToken: "prod", FaceitAPIKey: "fkey", DatabasePath: DefaultDatabasePath, LogLevel: slog.LevelInfo},
		},
		{
			name:    "missing token",
			env:     with(map[string]string{"TELEGRAM_BOT_TOKEN": "  "}),
			wantErr: "TELEGRAM_BOT_TOKEN is not set (or TELEGRAM_DEV_BOT_TOKEN for local runs)",
		},
		{
			name:    "both tokens blank",
			env:     with(map[string]string{"TELEGRAM_BOT_TOKEN": "", "TELEGRAM_DEV_BOT_TOKEN": " "}),
			wantErr: "TELEGRAM_BOT_TOKEN is not set (or TELEGRAM_DEV_BOT_TOKEN for local runs)",
		},
		{
			name:    "missing faceit key",
			env:     map[string]string{"TELEGRAM_BOT_TOKEN": "tok", "FACEIT_API_KEY": " "},
			wantErr: "FACEIT_API_KEY is not set",
		},
		{
			name:    "nothing set",
			env:     nil,
			wantErr: "TELEGRAM_BOT_TOKEN is not set (or TELEGRAM_DEV_BOT_TOKEN for local runs)\nFACEIT_API_KEY is not set",
		},
		{
			name:    "bad level",
			env:     with(map[string]string{"LOG_LEVEL": "verbose"}),
			wantErr: `invalid LOG_LEVEL "verbose": use debug, info, warn or error`,
		},
		{
			name: "admins",
			env:  with(map[string]string{"ADMIN_TELEGRAM_IDS": " 1001, 2002 ,,3003"}),
			want: Config{TelegramToken: "tok", FaceitAPIKey: "fkey", DatabasePath: DefaultDatabasePath, LogLevel: slog.LevelInfo,
				AdminIDs: []int64{1001, 2002, 3003}},
		},
		{
			name: "no admins",
			env:  with(map[string]string{"ADMIN_TELEGRAM_IDS": "  "}),
			want: Config{TelegramToken: "tok", FaceitAPIKey: "fkey", DatabasePath: DefaultDatabasePath, LogLevel: slog.LevelInfo},
		},
		{
			name:    "bad admin ID",
			env:     with(map[string]string{"ADMIN_TELEGRAM_IDS": "1001, kolyx"}),
			wantErr: "invalid ADMIN_TELEGRAM_IDS: use Telegram user IDs separated by commas",
		},
		{
			name:    "negative admin ID",
			env:     with(map[string]string{"ADMIN_TELEGRAM_IDS": "-1001"}),
			wantErr: "invalid ADMIN_TELEGRAM_IDS: use Telegram user IDs separated by commas",
		},
		{
			name:    "semicolons",
			env:     with(map[string]string{"ADMIN_TELEGRAM_IDS": "1001;2002"}),
			wantErr: "invalid ADMIN_TELEGRAM_IDS: use Telegram user IDs separated by commas",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(mapLookup(tt.env))
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("Parse() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	data := "TELEGRAM_BOT_TOKEN=file-token\nFACEIT_API_KEY=file-key\nDATABASE_PATH=file.db\nLOG_LEVEL=error\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("file values", func(t *testing.T) {
		cfg, err := load(path, mapLookup(nil))
		if err != nil {
			t.Fatalf("load() error: %v", err)
		}
		want := Config{TelegramToken: "file-token", FaceitAPIKey: "file-key", DatabasePath: "file.db", LogLevel: slog.LevelError}
		if !reflect.DeepEqual(cfg, want) {
			t.Errorf("load() = %+v, want %+v", cfg, want)
		}
	})

	t.Run("environment wins", func(t *testing.T) {
		cfg, err := load(path, mapLookup(map[string]string{"DATABASE_PATH": "env.db"}))
		if err != nil {
			t.Fatalf("load() error: %v", err)
		}
		if cfg.DatabasePath != "env.db" || cfg.TelegramToken != "file-token" {
			t.Errorf("load() = %+v, want DATABASE_PATH from env and token from file", cfg)
		}
	})

	t.Run("dev token only in file", func(t *testing.T) {
		devPath := filepath.Join(dir, "dev.env")
		if err := os.WriteFile(devPath, []byte("TELEGRAM_DEV_BOT_TOKEN=dev-token\nFACEIT_API_KEY=k\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := load(devPath, mapLookup(nil))
		if err != nil {
			t.Fatalf("load() error: %v", err)
		}
		if cfg.TelegramToken != "dev-token" || !cfg.DevBot {
			t.Errorf("load() = %+v, want the dev token", cfg)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		cfg, err := load(filepath.Join(dir, "absent.env"), mapLookup(map[string]string{"TELEGRAM_BOT_TOKEN": "env-token", "FACEIT_API_KEY": "env-key"}))
		if err != nil {
			t.Fatalf("load() error: %v", err)
		}
		if cfg.TelegramToken != "env-token" || cfg.FaceitAPIKey != "env-key" {
			t.Errorf("load() = %+v, want the values from env", cfg)
		}
	})
}

func mapLookup(m map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}
