package faceit

import (
	"context"
	"errors"
	"sync"
	"time"
)

// errRateWait means the context ends before the rate limit lets the request
// go.
var errRateWait = errors.New("rate limit wait exceeds the context deadline")

// limiter is a token bucket: up to burst requests go at once, then one every
// interval. It keeps tat, the time the bucket would be full again if no more
// requests came (the "theoretical arrival time" of GCRA).
type limiter struct {
	interval time.Duration
	burst    int
	now      func() time.Time

	mu  sync.Mutex
	tat time.Time
}

func newLimiter(interval time.Duration, burst int, now func() time.Time) *limiter {
	return &limiter{interval: interval, burst: burst, now: now}
}

// reserve books the next slot and returns how long to wait for it. It books
// nothing and returns false if the wait would end after deadline; a zero
// deadline means none.
func (l *limiter) reserve(deadline time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	tat := l.tat
	if tat.Before(now) {
		tat = now
	}
	at := tat.Add(-time.Duration(l.burst-1) * l.interval)
	wait := max(at.Sub(now), 0)
	if !deadline.IsZero() && now.Add(wait).After(deadline) {
		return 0, false
	}
	l.tat = tat.Add(l.interval)
	return wait, true
}

// wait blocks until the request may go, failing at once if ctx would end
// first.
func (l *limiter) wait(ctx context.Context, sleep func(context.Context, time.Duration) error) error {
	deadline, _ := ctx.Deadline()
	d, ok := l.reserve(deadline)
	if !ok {
		return errRateWait
	}
	if d == 0 {
		return nil
	}
	return sleep(ctx, d)
}
