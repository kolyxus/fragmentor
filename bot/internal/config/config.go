// Package config loads the bot settings from environment variables, with an
// optional .env file for local runs.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
)

// DefaultDatabasePath is used when DATABASE_PATH is not set.
const DefaultDatabasePath = "./data/fragmentor.db"

// Config holds the settings the bot needs.
type Config struct {
	TelegramToken string
	DatabasePath  string
	LogLevel      slog.Level
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
func Parse(lookup func(string) (string, bool)) (Config, error) {
	get := func(key string) string {
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}

	cfg := Config{
		TelegramToken: get("TELEGRAM_BOT_TOKEN"),
		DatabasePath:  get("DATABASE_PATH"),
		LogLevel:      slog.LevelInfo,
	}

	var errs []error
	if cfg.TelegramToken == "" {
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN is not set"))
	}
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = DefaultDatabasePath
	}
	if level := get("LOG_LEVEL"); level != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
			errs = append(errs, fmt.Errorf("invalid LOG_LEVEL %q: use debug, info, warn or error", level))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
