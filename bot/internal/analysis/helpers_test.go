package analysis

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

const (
	testMatchID   = "1-00000000-0000-4000-8000-0000000000a1"
	otherMatchID  = "1-00000000-0000-4000-8000-0000000000a2"
	thirdMatchID  = "1-00000000-0000-4000-8000-0000000000a3"
	fourthMatchID = "1-00000000-0000-4000-8000-0000000000a4"
	// signature stands for the signature of a link: it must never reach
	// the log.
	signature = "secret-signature"
)

// steamOf is the SteamID64 of player i of the test demo, 1 to 10.
func steamOf(i int) uint64 { return 76561190000000000 + uint64(i) }

// playerIDOf is the FACEIT player ID of player i of the test demo.
func playerIDOf(i int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", i) }

// levelOf is the FACEIT level of player i of the test demo.
func levelOf(i int) int { return (i-1)%10 + 1 }

// testDemo is a match of two rounds on Mirage: players 1–5 start as CT,
// 6–10 as T. Player 1 kills player 6 with a headshot in round 1, which CT
// win; player 7 kills player 2 in round 2, which T win.
func testDemo() *demo.Match {
	m := &demo.Match{Map: "de_mirage", TickRate: 64}
	m.Teams = [2]demo.Team{
		{Name: "team_p1", StartSide: demo.CT, Score: 1, FirstHalf: 1},
		{Name: "team_p6", StartSide: demo.T, Score: 1, FirstHalf: 1},
	}
	var alive []demo.Ref
	for i := 1; i <= 10; i++ {
		team, side := 0, demo.CT
		if i > 5 {
			team, side = 1, demo.T
		}
		m.Players = append(m.Players, demo.Player{SteamID64: steamOf(i), Name: fmt.Sprintf("p%d", i), Team: team, StartSide: side})
		alive = append(alive, demo.Ref{ID: steamOf(i), Side: side})
	}
	m.Rounds = []demo.Round{
		{Number: 1, Winner: 0, WinnerSide: demo.CT, Reason: "ct_win", Score: [2]int{1, 0}, EndTick: 2000, FreezeEndTick: 1000, Alive: alive},
		{Number: 2, Winner: 1, WinnerSide: demo.T, Reason: "t_win", Score: [2]int{1, 1}, EndTick: 4000, FreezeEndTick: 3000, Alive: alive},
	}
	m.Kills = []demo.Kill{
		{Tick: 1500, Round: 1, Killer: &demo.Ref{ID: steamOf(1), Side: demo.CT}, Victim: demo.Ref{ID: steamOf(6), Side: demo.T},
			Headshot: true, Weapon: "AK-47", Class: demo.ClassRifle},
		{Tick: 3500, Round: 2, Killer: &demo.Ref{ID: steamOf(7), Side: demo.T}, Victim: demo.Ref{ID: steamOf(2), Side: demo.CT},
			Weapon: "AK-47", Class: demo.ClassRifle},
	}
	return m
}

// testDetails is the FACEIT match of testDemo: players 1–5 in faction1,
// 6–10 in faction2.
func testDetails() faceit.Match {
	m := faceit.Match{
		ID: testMatchID, Status: "FINISHED", BestOf: 1, StartedAt: t0.Add(-time.Hour),
		Map: "de_mirage", MapName: "Mirage",
	}
	for f := range 2 {
		team := faceit.Team{Faction: fmt.Sprintf("faction%d", f+1)}
		for i := f*5 + 1; i <= f*5+5; i++ {
			team.Players = append(team.Players, faceit.MatchPlayer{
				ID: playerIDOf(i), Nickname: fmt.Sprintf("p%d", i),
				SteamID64: strconv.FormatUint(steamOf(i), 10), SkillLevel: levelOf(i),
			})
		}
		m.Teams = append(m.Teams, team)
	}
	return m
}

// stubFaceit stands in for FACEIT: every match is testDetails unless set.
type stubFaceit struct {
	mu      sync.Mutex
	matches map[string]faceit.Match
	err     error
	calls   int
}

func (s *stubFaceit) Match(_ context.Context, matchID string) (faceit.Match, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return faceit.Match{}, s.err
	}
	if m, ok := s.matches[matchID]; ok {
		return m, nil
	}
	m := testDetails()
	m.ID = matchID
	return m, nil
}

func (s *stubFaceit) set(matchID string, m faceit.Match) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.matches == nil {
		s.matches = map[string]faceit.Match{}
	}
	s.matches[matchID] = m
}

// demoServer stands in for the storage of the demos. By default it serves
// demoBytes for any path; handler, if set, answers instead.
type demoServer struct {
	*httptest.Server
	requests atomic.Int32
	mu       sync.Mutex
	handlers []http.HandlerFunc // answers of the next requests, in order
}

var demoBytes = bytes.Repeat([]byte("demo"), 1000)

func newDemoServer(t *testing.T) *demoServer {
	s := &demoServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		s.mu.Lock()
		var h http.HandlerFunc
		if len(s.handlers) > 0 {
			h, s.handlers = s.handlers[0], s.handlers[1:]
		}
		s.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(demoBytes)))
		_, _ = w.Write(demoBytes)
	}))
	t.Cleanup(s.Close)
	return s
}

// then makes the next requests get these answers.
func (s *demoServer) then(hs ...http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, hs...)
}

// status answers with the code and an S3 error body.
func status(code int, s3Code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(code)
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Error>
    <Code>%s</Code>
    <Message>Request has expired given timestamp: '20261106T120000Z' and expiration: 299</Message>
</Error>`, s3Code)
	}
}

// cutShort promises the whole demo but breaks the connection after 100
// bytes.
func cutShort(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Length", strconv.Itoa(len(demoBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(demoBytes[:100])
	w.(http.Flusher).Flush()
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		conn.Close()
	}
}

// notes records the jobs a Notifier heard of.
type notes struct {
	mu  sync.Mutex
	ids []int64
}

func (n *notes) JobsChanged(ids ...int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ids = append(n.ids, ids...)
}

func (n *notes) heard(id int64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Contains(n.ids, id)
}

// fixture is a queue on a real database, with FACEIT stubbed, the demos
// served by a test server and a clock that moves only when told.
type fixture struct {
	t      *testing.T
	ctx    context.Context
	path   string
	store  *storage.Store
	faceit *stubFaceit
	srv    *demoServer
	q      *Queue
	notes  *notes
	log    *bytes.Buffer
	parsed atomic.Int32 // demos the parse stub read

	mu  sync.Mutex
	now time.Time
}

// Users of the fixture: 42 plays as player 1, 43 as player 6, and 44
// linked an account that is not in the match.
const (
	userP1    = 42
	userP6    = 43
	userOther = 44
)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, ctx: context.Background(), faceit: &stubFaceit{}, notes: &notes{}, log: &bytes.Buffer{}, now: t0}
	path := filepath.Join(tempDir(t), "test.db")
	store, err := storage.Open(f.ctx, path)
	if err != nil {
		t.Fatalf("storage.Open() error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close() error: %v", err)
		}
	})
	f.store, f.path = store, path
	f.srv = newDemoServer(t)
	f.link(userP1, 1)
	f.link(userP6, 6)
	f.linkAccount(userOther, storage.FaceitAccount{PlayerID: playerIDOf(99), SteamID64: strconv.FormatUint(steamOf(99), 10)})

	f.q = New(store, f.faceit, slog.New(slog.NewTextHandler(syncWriter{w: f.log}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	f.q.now = f.clock
	f.q.sleep = func(context.Context, time.Duration) error { return nil }
	// The parse stub reads the whole body, as the parser would, and
	// returns the test demo.
	f.q.parse = func(_ context.Context, r io.Reader) (*demo.Match, error) {
		if _, err := io.Copy(io.Discard, r); err != nil {
			return nil, err
		}
		f.parsed.Add(1)
		return testDemo(), nil
	}
	return f
}

// syncWriter lets the workers log to one buffer.
type syncWriter struct{ w *bytes.Buffer }

var logMu sync.Mutex

func (s syncWriter) Write(p []byte) (int, error) {
	logMu.Lock()
	defer logMu.Unlock()
	return s.w.Write(p)
}

func (f *fixture) logText() string {
	logMu.Lock()
	defer logMu.Unlock()
	return f.log.String()
}

func (f *fixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fixture) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// link links the account of player i of the test demo to the user.
func (f *fixture) link(telegramID int64, i int) {
	f.linkAccount(telegramID, storage.FaceitAccount{
		PlayerID: playerIDOf(i), Nickname: fmt.Sprintf("p%d", i), SteamID64: strconv.FormatUint(steamOf(i), 10),
	})
}

func (f *fixture) linkAccount(telegramID int64, acc storage.FaceitAccount) {
	f.t.Helper()
	if err := f.store.LinkFaceitAccount(f.ctx, storage.User{TelegramID: telegramID, Language: "ru"}, acc, t0); err != nil {
		f.t.Fatal(err)
	}
}

// demoLink is a link to the match on the test server, signed now for 299 s.
func (f *fixture) demoLink(matchID string) faceit.DemoLink {
	return faceit.DemoLink{
		URL:      f.srv.URL + "/cs2/" + matchID + "-1-1.dem.zst?X-Amz-Expires=299&X-Amz-Signature=" + signature,
		MatchID:  matchID,
		Expires:  f.clock().Add(299 * time.Second),
		Lifetime: 299 * time.Second,
	}
}

func (f *fixture) submit(telegramID int64, matchID string) Outcome {
	f.t.Helper()
	out, err := f.q.Submit(f.ctx, Request{TelegramID: telegramID, Language: "ru", Link: f.demoLink(matchID)})
	if err != nil {
		f.t.Fatalf("Submit() error: %v", err)
	}
	return out
}

// accept submits the link and checks it is accepted.
func (f *fixture) accept(telegramID int64, matchID string) storage.Job {
	f.t.Helper()
	out := f.submit(telegramID, matchID)
	if out.Verdict != Accepted {
		f.t.Fatalf("Submit() verdict %v, want Accepted", out.Verdict)
	}
	return out.Job
}

// step does what a worker does with the next job in the queue, on this
// goroutine, and returns the job as it ended.
func (f *fixture) step() storage.Job {
	f.t.Helper()
	job, ok, err := f.store.ClaimJob(f.ctx, f.clock())
	if err != nil || !ok {
		f.t.Fatalf("ClaimJob() = %v, %v; want a job", ok, err)
	}
	f.q.run(f.ctx, f.notes, job)
	return f.job(job.ID)
}

func (f *fixture) job(id int64) storage.Job {
	f.t.Helper()
	j, err := f.store.Job(f.ctx, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return j
}

// checkLog checks that the log holds no link, signature or match ID.
func (f *fixture) checkLog() {
	f.t.Helper()
	text := f.logText()
	for _, secret := range []string{signature, f.srv.URL, testMatchID, otherMatchID, "/cs2/"} {
		if strings.Contains(text, secret) {
			f.t.Errorf("the log holds %q:\n%s", secret, text)
		}
	}
}

// tempDir stands in for t.TempDir in tests with a database: on Windows the
// files of a closed SQLite database may stay for a moment, and t.TempDir
// then fails the test. The removal is retried for 5 s here.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "fragmentor-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Logf("temporary directory left behind: %v", err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	return dir
}
