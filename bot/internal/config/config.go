// Package config loads the bot settings from environment variables, with an
// optional .env file for local runs.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// DefaultDatabasePath is used when DATABASE_PATH is not set.
const DefaultDatabasePath = "./data/fragmentor.db"

// Config holds the settings the bot needs.
type Config struct {
	TelegramToken string
	// DevBot reports that TelegramToken is the dev bot token from
	// TELEGRAM_DEV_BOT_TOKEN.
	DevBot bool
	// FaceitAPIKey is the server-side key of the FACEIT Data API.
	FaceitAPIKey string
	DatabasePath string
	LogLevel     slog.Level
	// AdminIDs are the Telegram user IDs of the admins, from
	// ADMIN_TELEGRAM_IDS. Until the beta only they may send demos.
	AdminIDs []int64
}

// Load reads the .env file at dotenvPath, if there is one, and builds the
// config. Environment variables take precedence over values from the file.
func Load(dotenvPath string) (Config, error) {
	return load(dotenvPath, os.LookupEnv)
}

func load(dotenvPath string, lookupEnv func(string) (string, bool)) (Config, error) {
	fileVars, err := readDotEnv(dotenvPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("read %s: %w", dotenvPath, err)
	}
	return Parse(func(key string) (string, bool) {
		if v, ok := lookupEnv(key); ok {
			return v, true
		}
		v, ok := fileVars[key]
		return v, ok
	})
}

// Parse builds the config from lookup. Errors name the variables but never
// include their values, which may be secrets.
//
// TELEGRAM_DEV_BOT_TOKEN, if set, wins over TELEGRAM_BOT_TOKEN, which then is
// not required. It is set only in the local .env, so that local runs use a
// separate dev bot: Telegram lets only one process poll a bot, and a local run
// with the production token would fight the server for updates.
func Parse(lookup func(string) (string, bool)) (Config, error) {
	get := func(key string) string {
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}

	cfg := Config{
		TelegramToken: get("TELEGRAM_BOT_TOKEN"),
		FaceitAPIKey:  get("FACEIT_API_KEY"),
		DatabasePath:  get("DATABASE_PATH"),
		LogLevel:      slog.LevelInfo,
	}
	if dev := get("TELEGRAM_DEV_BOT_TOKEN"); dev != "" {
		cfg.TelegramToken = dev
		cfg.DevBot = true
	}

	var errs []error
	if cfg.TelegramToken == "" {
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN is not set (or TELEGRAM_DEV_BOT_TOKEN for local runs)"))
	}
	if cfg.FaceitAPIKey == "" {
		errs = append(errs, errors.New("FACEIT_API_KEY is not set"))
	}
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = DefaultDatabasePath
	}
	if level := get("LOG_LEVEL"); level != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
			errs = append(errs, fmt.Errorf("invalid LOG_LEVEL %q: use debug, info, warn or error", level))
		}
	}
	if ids, err := parseIDs(get("ADMIN_TELEGRAM_IDS")); err != nil {
		errs = append(errs, err)
	} else {
		cfg.AdminIDs = ids
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// parseIDs reads Telegram user IDs separated by commas: "1, 2". An empty
// list is no IDs. The error does not repeat the value: these are personal
// data.
func parseIDs(s string) ([]int64, error) {
	var ids []int64
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, errors.New("invalid ADMIN_TELEGRAM_IDS: use Telegram user IDs separated by commas")
		}
		ids = append(ids, id)
	}
	return ids, nil
}
