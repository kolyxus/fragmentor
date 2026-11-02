// Command bot runs the Fragmentor Telegram bot.
//
// Settings come from environment variables and, for local runs, from .env in
// the working directory (see .env.example). SIGINT or SIGTERM stops the bot
// gracefully: polling stops, the updates in progress are handled, and the
// database is closed. A second signal kills the process at once.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/kolyxus/fragmentor/bot/internal/config"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
	"github.com/kolyxus/fragmentor/bot/internal/telegram"
)

func main() {
	if err := run(); err != nil {
		slog.Error("bot failed", "err", err)
		os.Exit(1)
	}
}

func run() (err error) {
	cfg, err := config.Load(".env")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)
	log.Info("starting", "version", version(), "dev_bot", cfg.DevBot)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, func() {
		log.Info("shutting down, press Ctrl+C again to kill")
		stop() // restore default handling: the next signal kills the process
	})

	store, err := storage.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close storage: %w", closeErr))
		}
	}()
	version, err := store.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	log.Info("database ready", "path", cfg.DatabasePath, "schema_version", version)

	tg, err := telegram.New(cfg.TelegramToken, store, log)
	if err != nil {
		return err
	}
	if err := tg.Run(ctx); err != nil {
		return err
	}
	log.Info("bot stopped")
	return nil
}

// version tells which build is running, so that after a deploy the log shows
// the new version is up.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return versionFrom(info.Settings)
}

// versionFrom returns the commit that go build stamped into the binary,
// shortened to 12 characters, with "+dirty" if the working tree had
// uncommitted changes. Builds without the stamp, such as go run, are "devel".
func versionFrom(settings []debug.BuildSetting) string {
	var revision string
	var dirty bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision == "" {
		return "devel"
	}
	revision = revision[:min(len(revision), 12)]
	if dirty {
		revision += "+dirty"
	}
	return revision
}
