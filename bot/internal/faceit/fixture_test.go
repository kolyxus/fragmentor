package faceit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fixtures in testdata are real FACEIT responses recorded by
// cmd/faceitrecord, with the nicknames, IDs and SteamIDs replaced by fake
// ones. These are the fake values of the recorded player.
const (
	testKey       = "test-key-not-real"
	testPlayerID  = "00000000-0000-4000-8000-000000000001"
	testNickname  = "player01"
	testSteamID64 = "76561190000000001"
	testMatchID   = "1-00000000-0000-4000-8000-000000000004"
)

// fixtureServer answers like the Data API with the recorded responses.
type fixtureServer struct {
	*httptest.Server
	t *testing.T

	mu       sync.Mutex
	requests []string // "path?query" of every request, in order
	// override, if set, may answer a request itself and return true.
	override func(w http.ResponseWriter, r *http.Request) bool
}

func newFixtureServer(t *testing.T) *fixtureServer {
	t.Helper()
	f := &fixtureServer{t: t}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fixtureServer) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.URL.RequestURI())
	override := f.override
	f.mu.Unlock()

	if r.Method != http.MethodGet {
		f.t.Errorf("%s %s, want GET", r.Method, r.URL.Path)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
		// What FACEIT answers to a wrong key.
		f.file(w, http.StatusBadRequest, "error_unauthorized.json")
		return
	}
	if override != nil && override(w, r) {
		return
	}

	path := r.URL.Path
	switch {
	case path == "/players":
		if r.URL.Query().Get("nickname") == testNickname {
			f.file(w, http.StatusOK, "player.json")
			return
		}
	case path == "/search/players":
		f.file(w, http.StatusOK, "search_players.json")
		return
	case path == "/players/"+testPlayerID:
		f.file(w, http.StatusOK, "player.json")
		return
	case path == "/players/"+testPlayerID+"/history":
		f.file(w, http.StatusOK, "history.json")
		return
	case strings.HasPrefix(path, "/matches/"):
		id, isStats := strings.CutSuffix(strings.TrimPrefix(path, "/matches/"), "/stats")
		name := filepath.Join("matches", id+".json")
		if isStats {
			name = filepath.Join("matches", id+"_stats.json")
		}
		if _, err := os.Stat(filepath.Join("testdata", name)); err == nil && !strings.Contains(id, "..") {
			f.file(w, http.StatusOK, name)
			return
		}
	}
	f.file(w, http.StatusNotFound, "error_not_found.json")
}

func (f *fixtureServer) file(w http.ResponseWriter, status int, name string) {
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		f.t.Errorf("read fixture: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func (f *fixtureServer) setOverride(fn func(w http.ResponseWriter, r *http.Request) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.override = fn
}

func (f *fixtureServer) requestList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// fakeSleep records the pauses instead of making them.
type fakeSleep struct {
	mu     sync.Mutex
	pauses []time.Duration
}

func (s *fakeSleep) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.pauses = append(s.pauses, d)
	s.mu.Unlock()
	return ctx.Err()
}

func (s *fakeSleep) list() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.pauses...)
}

// newTestClient returns a client of srv that does not really pause.
func newTestClient(t *testing.T, srv *fixtureServer, opts ...Option) (*Client, *fakeSleep) {
	t.Helper()
	sleep := &fakeSleep{}
	c := New(testKey, append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	c.sleep = sleep.sleep
	return c, sleep
}
