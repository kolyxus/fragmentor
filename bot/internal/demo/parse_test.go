package demo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs"
)

func TestParseNotZstd(t *testing.T) {
	_, err := Parse(context.Background(), strings.NewReader("a page of HTML instead of a demo"), Options{})
	if !errors.Is(err, ErrNotZstd) {
		t.Errorf("err = %v, want ErrNotZstd", err)
	}
}

func TestParseNotDemo(t *testing.T) {
	zst := compress(t, []byte("HL2DEMO\x00 a CS:GO demo, or anything but a CS2 one"))
	_, err := Parse(context.Background(), bytes.NewReader(zst), Options{})
	if !errors.Is(err, ErrNotDemo) {
		t.Errorf("err = %v, want ErrNotDemo", err)
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := Parse(context.Background(), bytes.NewReader(compress(t, nil)), Options{})
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("err = %v, want ErrTruncated", err)
	}
}

func TestParseCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Parse(ctx, bytes.NewReader(compress(t, []byte("PBDEMS2\x00"))), Options{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestCatch(t *testing.T) {
	err := catch(func() error { panic("boom") })
	var pe *PanicError
	if !errors.As(err, &pe) || pe.Value != "boom" || len(pe.Stack) == 0 {
		t.Errorf("err = %#v, want a *PanicError with the value and a stack", err)
	}
	// demoinfocs raises a handler's panic again wrapped in its own type.
	err = catch(func() error { panic(&demoinfocs.PanicError{Value: "handler", Stack: []byte("stack")}) })
	if !errors.As(err, &pe) || pe.Value != "handler" || string(pe.Stack) != "stack" {
		t.Errorf("err = %#v, want the handler's value and stack", err)
	}
	if err := catch(func() error { return io.EOF }); err != io.EOF {
		t.Errorf("err = %v, want the error of f", err)
	}
}

// testDemos returns the demos in bot/testdata. They are not in git: the test
// skips without them, and with -short, as each takes seconds.
func testDemos(t *testing.T) []string {
	t.Helper()
	if testing.Short() {
		t.Skip("parsing real demos takes seconds")
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "*.dem.zst"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no demos in bot/testdata")
	}
	return files
}

// TestParseTestdata checks what holds for any FACEIT match. That the numbers
// are FACEIT's is checked by go run ./cmd/parsedemo -faceit testdata.
func TestParseTestdata(t *testing.T) {
	for _, file := range testDemos(t) {
		t.Run(filepath.Base(file)[:10], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			m, err := ParseFile(ctx, file, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if m.Suspicious || len(m.Warnings) != 0 {
				t.Errorf("suspicious %t, warnings: %q", m.Suspicious, m.Warnings)
			}
			if !strings.HasPrefix(m.Map, "de_") || m.TickRate != 64 {
				t.Errorf("map %q, tick rate %v; want de_*, 64", m.Map, m.TickRate)
			}
			a, b := m.Teams[0], m.Teams[1]
			if a.Score+b.Score != len(m.Rounds) || max(a.Score, b.Score) < 13 {
				t.Errorf("score %d:%d in %d rounds", a.Score, b.Score, len(m.Rounds))
			}
			for _, team := range m.Teams {
				if team.FirstHalf+team.SecondHalf+team.Overtime != team.Score {
					t.Errorf("halves of %+v do not add up", team)
				}
			}
			if a.FirstHalf+b.FirstHalf != 12 {
				t.Errorf("first half %d:%d, want 12 rounds", a.FirstHalf, b.FirstHalf)
			}
			for i, r := range m.Rounds {
				if r.Number != i+1 || r.Score[r.Winner] < 1 || (i > 0 && r.EndTick <= m.Rounds[i-1].EndTick) {
					t.Errorf("round %d = %+v", i+1, r)
				}
			}
			if last := m.Rounds[len(m.Rounds)-1].Score; last != [2]int{a.Score, b.Score} {
				t.Errorf("score after the last round %v, teams %d:%d", last, a.Score, b.Score)
			}
			var perTeam [2]int
			for _, p := range m.Players {
				perTeam[p.Team]++
				if p.SteamID64 == 0 || p.Name == "" || p.Headshots > p.Kills || p.Damage <= 0 || p.StartSide != m.Teams[p.Team].StartSide {
					t.Errorf("player %+v", p)
				}
			}
			if perTeam != [2]int{5, 5} {
				t.Errorf("players per team %v, want 5 and 5", perTeam)
			}
		})
	}
}

func TestParseTestdataFailures(t *testing.T) {
	file := testDemos(t)[0]
	open := func(t *testing.T) *os.File {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}

	t.Run("truncated", func(t *testing.T) {
		_, err := Parse(context.Background(), io.LimitReader(open(t), 8<<20), Options{})
		if !errors.Is(err, ErrTruncated) {
			t.Errorf("err = %v, want ErrTruncated", err)
		}
	})
	t.Run("too large", func(t *testing.T) {
		_, err := Parse(context.Background(), open(t), Options{MaxSize: 16 << 20})
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := Parse(ctx, open(t), Options{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want context.DeadlineExceeded", err)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("stopped after %v", d)
		}
	})
}
