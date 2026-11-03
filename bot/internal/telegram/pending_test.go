package telegram

import (
	"testing"
	"time"
)

func TestPendingLinks(t *testing.T) {
	p := newPendingLinks()
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	if p.take(1, now) {
		t.Error("take() before set() = true")
	}
	p.set(1, now)
	if !p.take(1, now.Add(pendingTTL-time.Second)) {
		t.Error("take() within the TTL = false")
	}
	if p.take(1, now) {
		t.Error("second take() = true, want the wait over")
	}

	p.set(2, now)
	if p.take(2, now.Add(pendingTTL)) {
		t.Error("take() after the TTL = true")
	}

	p.set(3, now)
	p.clear(3)
	if p.take(3, now) {
		t.Error("take() after clear() = true")
	}
}

func TestPendingLinksForgetsExpired(t *testing.T) {
	p := newPendingLinks()
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for id := range int64(100) {
		p.set(id, now)
	}
	p.set(1000, now.Add(pendingTTL))
	if n := len(p.until); n != 1 {
		t.Errorf("%d waits kept, want only the fresh one", n)
	}
}
