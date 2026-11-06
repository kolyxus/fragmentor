package analysis

import (
	"slices"
	"sync"
	"time"
)

const (
	// defaultDuration is how long a job is taken to last before any has
	// run. The first live run, a demo of 180 MiB to the dev machine, took
	// 71 s, the download being the bottleneck; the parse alone takes 4–8 s.
	// The estimate follows the jobs that run after.
	defaultDuration = 60 * time.Second
	// minLeft is how long a running job is taken to need yet when it runs
	// longer than the estimate.
	minLeft = 10 * time.Second
	// keepDurations is how many of the latest jobs the estimate follows.
	keepDurations = 10
)

// startIn estimates how long a job waits for a worker with ahead jobs
// queued before it and jobs running since running: every job takes d, and
// each of the workers takes the next job in the queue as soon as it is
// free.
func startIn(now time.Time, running []time.Time, ahead, workers int, d time.Duration) time.Duration {
	free := make([]time.Duration, workers) // when each worker is free
	for i, started := range running {
		if i == workers {
			break
		}
		free[i] = max(d-now.Sub(started), minLeft)
	}
	for range ahead {
		i := slices.Index(free, slices.Min(free))
		free[i] += d
	}
	return slices.Min(free)
}

// durations keeps how long the latest jobs that downloaded and parsed a
// demo took, for the estimate of the next one. It lives in memory: after a
// restart the estimate starts from defaultDuration again.
type durations struct {
	mu   sync.Mutex
	last []time.Duration
}

func (d *durations) add(x time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.last = append(d.last, x)
	if len(d.last) > keepDurations {
		d.last = d.last[len(d.last)-keepDurations:]
	}
}

// estimate returns the median of the latest durations, defaultDuration if
// there are none.
func (d *durations) estimate() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.last) == 0 {
		return defaultDuration
	}
	sorted := slices.Sorted(slices.Values(d.last))
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
