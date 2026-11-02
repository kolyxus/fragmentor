package main

import (
	"context"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestVersionFrom(t *testing.T) {
	const rev = "26ab92f95c27e6a2d9326e76f4ecf0b9cf5c02b4"
	tests := []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{"go run", []debug.BuildSetting{{Key: "GOOS", Value: "linux"}}, "devel"},
		{"clean tree", []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: "false"}}, "26ab92f95c27"},
		{"dirty tree", []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}, {Key: "vcs.revision", Value: rev}}, "26ab92f95c27+dirty"},
		{"short revision", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionFrom(tt.settings); got != tt.want {
				t.Errorf("versionFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ctxWriter collects log lines and notes whether ctx was already cancelled
// when one was written.
type ctxWriter struct {
	ctx   context.Context
	lines []string
	late  bool
}

func (w *ctxWriter) Write(p []byte) (int, error) {
	if w.ctx.Err() != nil {
		w.late = true
	}
	w.lines = append(w.lines, string(p))
	return len(p), nil
}

func TestCancelOnSignalLogsBeforeCancel(t *testing.T) {
	w := &ctxWriter{}
	sigs := make(chan os.Signal, 1)
	ctx, cancel := cancelOnSignal(slog.New(slog.NewTextHandler(w, nil)), sigs)
	defer cancel()
	w.ctx = ctx

	sigs <- os.Interrupt
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the signal did not cancel the context")
	}
	if len(w.lines) != 1 || !strings.Contains(w.lines[0], "shutting down") {
		t.Fatalf("log = %q, want one shutting down line", w.lines)
	}
	if w.late {
		t.Error("shutting down was logged after the context was cancelled")
	}
}
