package analysis

import (
	"testing"
	"time"
)

func TestStartIn(t *testing.T) {
	const d = 60 * time.Second
	tests := []struct {
		name    string
		running []time.Duration // how long ago each running job started
		ahead   int
		want    time.Duration
	}{
		{"idle", nil, 0, 0},
		{"one worker busy", []time.Duration{0}, 0, 0},
		{"both busy", []time.Duration{0, 20 * time.Second}, 0, 40 * time.Second},
		{"both busy, one ahead", []time.Duration{0, 20 * time.Second}, 1, 60 * time.Second},
		{"both busy, two ahead", []time.Duration{0, 20 * time.Second}, 2, 100 * time.Second},
		{"both busy, three ahead", []time.Duration{0, 20 * time.Second}, 3, 120 * time.Second},
		{"idle, three ahead", nil, 3, 60 * time.Second},
		// A job running past the estimate is taken to need minLeft yet.
		{"overdue", []time.Duration{5 * time.Minute, 5 * time.Minute}, 0, minLeft},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var running []time.Time
			for _, ago := range tt.running {
				running = append(running, t0.Add(-ago))
			}
			if got := startIn(t0, running, tt.ahead, Workers, d); got != tt.want {
				t.Errorf("startIn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDurations(t *testing.T) {
	var d durations
	if got := d.estimate(); got != defaultDuration {
		t.Errorf("estimate() without jobs = %v, want %v", got, defaultDuration)
	}
	d.add(10 * time.Second)
	d.add(30 * time.Second)
	if got := d.estimate(); got != 20*time.Second {
		t.Errorf("estimate() of two = %v, want their mean", got)
	}
	d.add(time.Hour)
	if got := d.estimate(); got != 30*time.Second {
		t.Errorf("estimate() of three = %v, want the median", got)
	}
	// Only the latest keepDurations count.
	for range keepDurations {
		d.add(5 * time.Second)
	}
	if got := d.estimate(); got != 5*time.Second {
		t.Errorf("estimate() = %v, want the latest jobs alone", got)
	}
}
