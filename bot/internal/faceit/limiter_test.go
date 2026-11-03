package faceit

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestLimiterBurstThenInterval(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(100*time.Millisecond, 3, clock.now)

	want := []time.Duration{0, 0, 0, 100 * time.Millisecond, 200 * time.Millisecond}
	for i, w := range want {
		got, ok := l.reserve(time.Time{})
		if !ok || got != w {
			t.Errorf("request %d waits %v (%t), want %v", i+1, got, ok, w)
		}
	}

	// After a quiet second the whole burst is available again.
	clock.advance(time.Second)
	for i := range 3 {
		if got, _ := l.reserve(time.Time{}); got != 0 {
			t.Errorf("after a pause, request %d waits %v, want 0", i+1, got)
		}
	}
	if got, _ := l.reserve(time.Time{}); got != 100*time.Millisecond {
		t.Errorf("request past the burst waits %v, want 100ms", got)
	}
}

func TestLimiterSpacesSteadyRequests(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(200*time.Millisecond, 10, clock.now)

	// 60 requests, each made as soon as allowed: over the first second no
	// more than the burst and five more may go.
	start := clock.t
	sent := 0
	for range 60 {
		wait, _ := l.reserve(time.Time{})
		clock.advance(wait)
		if clock.t.Sub(start) < time.Second {
			sent++
		}
	}
	if sent > 15 {
		t.Errorf("%d requests in the first second, want at most 15", sent)
	}
	if took := clock.t.Sub(start); took < 9*time.Second {
		t.Errorf("60 requests took %v, want about (60-10)*200ms = 10s", took)
	}
}

func TestLimiterDeadline(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(100*time.Millisecond, 1, clock.now)

	if got, ok := l.reserve(time.Time{}); !ok || got != 0 {
		t.Fatalf("first request waits %v (%t), want 0", got, ok)
	}
	if _, ok := l.reserve(clock.t.Add(50 * time.Millisecond)); ok {
		t.Error("reserved a slot past the deadline")
	}
	// The refused request booked nothing.
	if got, ok := l.reserve(time.Time{}); !ok || got != 100*time.Millisecond {
		t.Errorf("next request waits %v (%t), want 100ms", got, ok)
	}
}

func TestLimiterWait(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(100*time.Millisecond, 1, clock.now)
	var slept []time.Duration
	sleep := func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}

	for range 2 {
		if err := l.wait(context.Background(), sleep); err != nil {
			t.Fatalf("wait() error: %v", err)
		}
	}
	if len(slept) != 1 || slept[0] != 100*time.Millisecond {
		t.Errorf("slept %v, want one 100ms pause", slept)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := l.wait(ctx, sleep); !errors.Is(err, errRateWait) {
		t.Errorf("wait() with a passed deadline = %v, want errRateWait", err)
	}
}
