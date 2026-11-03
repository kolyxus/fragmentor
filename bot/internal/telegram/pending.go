package telegram

import (
	"sync"
	"time"
)

// pendingTTL is how long the bot takes the next message after /link as the
// nickname.
const pendingTTL = 10 * time.Minute

// pendingLinks remembers the users the bot asked for a FACEIT nickname, so
// that their next message is taken as one. It lives in memory: after a
// restart the user just sends /link again.
type pendingLinks struct {
	mu    sync.Mutex
	until map[int64]time.Time
}

func newPendingLinks() *pendingLinks {
	return &pendingLinks{until: map[int64]time.Time{}}
}

// set starts waiting for a nickname from the user. It also forgets the waits
// that have run out, so the map does not grow.
func (p *pendingLinks) set(userID int64, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, until := range p.until {
		if !now.Before(until) {
			delete(p.until, id)
		}
	}
	p.until[userID] = now.Add(pendingTTL)
}

// take reports whether the bot waits for a nickname from the user, and stops
// waiting.
func (p *pendingLinks) take(userID int64, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	until, ok := p.until[userID]
	delete(p.until, userID)
	return ok && now.Before(until)
}

// clear stops waiting for a nickname from the user.
func (p *pendingLinks) clear(userID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.until, userID)
}
