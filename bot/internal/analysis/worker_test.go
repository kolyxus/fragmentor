package analysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// db opens the database of the fixture on its own, to look at the tables.
func (f *fixture) db() *sql.DB {
	f.t.Helper()
	db, err := sql.Open("sqlite", f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { db.Close() })
	return db
}

func (f *fixture) count(table string) int {
	f.t.Helper()
	var n int
	if err := f.db().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// keptPlayers returns the FACEIT player IDs kept with the match, in order.
func (f *fixture) keptPlayers(matchID string) []string {
	f.t.Helper()
	rows, err := f.db().Query(`SELECT player_id FROM match_players WHERE match_id = ? ORDER BY player_id`, matchID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			f.t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestJobDone(t *testing.T) {
	f := newFixture(t)
	job := f.accept(userP1, testMatchID)
	got := f.step()

	if got.Status != storage.JobDone || got.Reason != "" || got.DemoURL != "" || got.Attempts != 1 {
		t.Errorf("job = %+v, want done, without the link", got)
	}
	if !f.notes.heard(job.ID) {
		t.Error("the notifier did not hear of the job")
	}
	if n := f.srv.requests.Load(); n != 1 {
		t.Errorf("%d downloads, want 1", n)
	}

	// The match, as the user saw it.
	res, err := f.store.MatchResult(f.ctx, testMatchID, playerIDOf(1))
	if err != nil {
		t.Fatal(err)
	}
	if want := (storage.MatchResult{Map: "de_mirage", Score: [2]int{1, 1}, MetricsVersion: metrics.Version, Team: 0}); res != want {
		t.Errorf("MatchResult() = %+v, want %+v", res, want)
	}
	var started int64
	if err := f.db().QueryRow(`SELECT started_at FROM matches`).Scan(&started); err != nil || started != t0.Add(-time.Hour).Unix() {
		t.Errorf("started_at = %d, %v; want the start of the match on FACEIT", started, err)
	}

	// With IDs, only the players who use the bot: 1 and 6.
	if got, want := f.keptPlayers(testMatchID), []string{playerIDOf(1), playerIDOf(6)}; !slices.Equal(got, want) {
		t.Errorf("match_players = %v, want %v", got, want)
	}
	var steam, stored string
	var level, team int
	err = f.db().QueryRow(`SELECT steam_id64, skill_level, team, metrics FROM match_players WHERE player_id = ?`, playerIDOf(1)).
		Scan(&steam, &level, &team, &stored)
	if err != nil {
		t.Fatal(err)
	}
	if steam != strconv.FormatUint(steamOf(1), 10) || level != levelOf(1) || team != 0 {
		t.Errorf("player 1 kept as %s, level %d, team %d", steam, level, team)
	}
	// The metrics are those of the formulas, without IDs.
	want := metrics.Compute(testDemo())[0]
	var m storedMetrics
	if err := json.Unmarshal([]byte(stored), &m); err != nil {
		t.Fatal(err)
	}
	if m.Match != want.Match || m.T != want.T || m.CT != want.CT {
		t.Errorf("stored metrics differ from metrics.Compute:\n%s", stored)
	}
	if m.Match.Kills != 1 || m.Match.HeadshotKills != 1 {
		t.Errorf("player 1: %d kills, %d headshots; want 1 and 1", m.Match.Kills, m.Match.HeadshotKills)
	}
	if strings.Contains(stored, "steam_id64") || strings.Contains(stored, strconv.FormatUint(steamOf(1), 10)) {
		t.Errorf("stored metrics hold the SteamID: %s", stored)
	}

	// Every player gives an anonymous sample with the level.
	if n := f.count("reference_samples"); n != 10 {
		t.Errorf("%d reference samples, want 10", n)
	}
	var levels, maps int
	err = f.db().QueryRow(`SELECT COUNT(DISTINCT skill_level), COUNT(DISTINCT map) FROM reference_samples WHERE metrics_version = ?`, metrics.Version).
		Scan(&levels, &maps)
	if err != nil || levels != 10 || maps != 1 {
		t.Errorf("reference samples: %d levels, %d maps, %v; want 10 and 1", levels, maps, err)
	}
	f.checkLog()
}

func TestSecondJobOfMatchNotDownloaded(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	second := f.accept(userP6, testMatchID)
	f.step()
	if got := f.step(); got.ID != second.ID || got.Status != storage.JobDone {
		t.Errorf("second job = %+v, want done", got)
	}
	if n := f.srv.requests.Load(); n != 1 {
		t.Errorf("%d downloads, want 1: the match was parsed by the first job", n)
	}
	if n := f.parsed.Load(); n != 1 {
		t.Errorf("%d parses, want 1", n)
	}
	// The link does not matter for a match parsed already.
	f.accept(userP1, otherMatchID)
	fourth := f.accept(userP6, otherMatchID)
	f.step()
	f.advance(280 * time.Second)
	if got := f.step(); got.ID != fourth.ID || got.Status != storage.JobDone {
		t.Errorf("job %d after its link ran out = %+v, want done: no download needed", fourth.ID, got)
	}
}

func TestNewUserParsesAgain(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	f.step()

	// Player 7 starts using the bot after the match was parsed: the match
	// is parsed again for them, and the samples are not added again.
	const userP7 = 45
	f.link(userP7, 7)
	out := f.submit(userP7, testMatchID)
	if out.Verdict != Accepted {
		t.Fatalf("Submit() of player 7 = %v, want Accepted", out.Verdict)
	}
	if got := f.step(); got.Status != storage.JobDone {
		t.Fatalf("job = %+v, want done", got)
	}
	if n := f.srv.requests.Load(); n != 2 {
		t.Errorf("%d downloads, want 2", n)
	}
	if got, want := f.keptPlayers(testMatchID), []string{playerIDOf(1), playerIDOf(6), playerIDOf(7)}; !slices.Equal(got, want) {
		t.Errorf("match_players = %v, want %v", got, want)
	}
	if n := f.count("reference_samples"); n != 10 {
		t.Errorf("%d reference samples, want the 10 of the first parse", n)
	}
	if out := f.submit(userP7, testMatchID); out.Verdict != Analyzed || out.Result.Team != 1 {
		t.Errorf("Submit() again = %+v, want Analyzed for team 1", out)
	}
}

func TestOldFormulasParsedAgain(t *testing.T) {
	f := newFixture(t)
	// The match was parsed with the formulas before the current ones.
	old := storage.ParsedMatch{MatchID: testMatchID, Map: "de_mirage", Score: [2]int{1, 1}, Rounds: 2, MetricsVersion: metrics.Version - 1}
	players := []storage.PlayerMetrics{{PlayerID: playerIDOf(1), SteamID64: strconv.FormatUint(steamOf(1), 10), SkillLevel: 1, Metrics: []byte(`{}`)}}
	if err := f.store.SaveMatch(f.ctx, old, players, t0); err != nil {
		t.Fatal(err)
	}
	if out := f.submit(userP1, testMatchID); out.Verdict != Accepted {
		t.Fatalf("Submit() of a match of old formulas = %v, want Accepted", out.Verdict)
	}
	if got := f.step(); got.Status != storage.JobDone {
		t.Fatalf("job = %+v, want done", got)
	}
	if n := f.srv.requests.Load(); n != 1 {
		t.Errorf("%d downloads, want 1", n)
	}
	res, err := f.store.MatchResult(f.ctx, testMatchID, playerIDOf(1))
	if err != nil || res.MetricsVersion != metrics.Version {
		t.Errorf("MatchResult() = %+v, %v; want the current formulas", res, err)
	}
	// The samples of the new formulas come in addition to the old one.
	if n := f.count("reference_samples"); n != 11 {
		t.Errorf("%d reference samples, want 1 old and 10 new", n)
	}
}

func TestJobFailures(t *testing.T) {
	otherMap := testDetails()
	otherMap.Map = "de_dust2"
	strangers := testDetails()
	for i := range strangers.Teams {
		for j := range strangers.Teams[i].Players {
			if p := &strangers.Teams[i].Players[j]; p.ID != playerIDOf(1) {
				p.SteamID64 = "76561199999999999"
			}
		}
	}

	tests := []struct {
		name      string
		setup     func(f *fixture)
		want      string
		downloads int32
	}{
		{"link rejected", func(f *fixture) { f.srv.then(status(http.StatusUnauthorized, "UnauthorizedAccess")) }, ReasonLinkRejected, 1},
		{"forbidden", func(f *fixture) { f.srv.then(status(http.StatusForbidden, "AccessDenied")) }, ReasonLinkRejected, 1},
		{"demo gone", func(f *fixture) { f.srv.then(status(http.StatusNotFound, "NoSuchKey")) }, ReasonDemoGone, 1},
		{"cut short twice", func(f *fixture) { f.srv.then(cutShort, cutShort) }, ReasonDownload, 2},
		{"server error twice", func(f *fixture) {
			f.srv.then(status(http.StatusServiceUnavailable, "x"), status(http.StatusServiceUnavailable, "x"))
		}, ReasonDownload, 2},
		{"too large", func(f *fixture) { f.q.download.maxSize = 100 }, ReasonTooLarge, 1},
		{"not a demo", func(f *fixture) {
			f.q.parse = func(context.Context, io.Reader) (*demo.Match, error) { return nil, demo.ErrNotDemo }
		}, ReasonBadDemo, 1},
		{"decompressed too large", func(f *fixture) {
			f.q.parse = func(context.Context, io.Reader) (*demo.Match, error) { return nil, demo.ErrTooLarge }
		}, ReasonTooLarge, 1},
		{"parser panic", func(f *fixture) {
			f.q.parse = func(context.Context, io.Reader) (*demo.Match, error) { panic("boom") }
		}, ReasonInternal, 1},
		{"another map", func(f *fixture) { f.faceit.set(testMatchID, otherMap) }, ReasonBadDemo, 1},
		{"other players", func(f *fixture) { f.faceit.set(testMatchID, strangers) }, ReasonBadDemo, 1},
		{"faceit down", func(f *fixture) { f.faceit.err = errors.New("faceit: get match: status 503") }, ReasonFaceit, 1},
		{"relinked to another account", func(f *fixture) {
			f.linkAccount(userP1, storage.FaceitAccount{PlayerID: playerIDOf(99), SteamID64: "76561190000000099"})
		}, ReasonNotInMatch, 1},
		{"timeout", func(f *fixture) {
			f.q.jobTimeout = 50 * time.Millisecond
			f.q.parse = func(ctx context.Context, _ io.Reader) (*demo.Match, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
		}, ReasonTimeout, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			job := f.accept(userP1, testMatchID)
			tt.setup(f)
			got := f.step()
			if got.Status != storage.JobFailed || got.Reason != tt.want {
				t.Errorf("job = %s %q, want failed %q", got.Status, got.Reason, tt.want)
			}
			if got.DemoURL != "" {
				t.Error("the link of the failed job is kept")
			}
			if n := f.srv.requests.Load(); n != tt.downloads {
				t.Errorf("%d downloads, want %d", n, tt.downloads)
			}
			if !f.notes.heard(job.ID) {
				t.Error("the notifier did not hear of the job")
			}
			if _, err := f.store.MatchResult(f.ctx, testMatchID, playerIDOf(1)); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("MatchResult() error %v, want nothing saved", err)
			}
			f.checkLog()
		})
	}
}

func TestDownloadRetried(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	f.srv.then(cutShort)
	if got := f.step(); got.Status != storage.JobDone {
		t.Errorf("job = %+v, want done on the second try", got)
	}
	if n := f.srv.requests.Load(); n != 2 {
		t.Errorf("%d downloads, want 2", n)
	}
}

func TestLinkExpiredInQueue(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	// The link lives 299 s; 280 s later less than LinkMargin is left.
	f.advance(280 * time.Second)
	if got := f.step(); got.Status != storage.JobFailed || got.Reason != ReasonLinkExpired {
		t.Errorf("job = %s %q, want failed %q", got.Status, got.Reason, ReasonLinkExpired)
	}
	if n := f.srv.requests.Load(); n != 0 {
		t.Errorf("%d downloads, want none by a link that ran out", n)
	}
}

func TestView(t *testing.T) {
	f := newFixture(t)
	f.q.durations.add(40 * time.Second)
	a := f.accept(userP1, testMatchID)
	b := f.accept(userP6, otherMatchID)
	c := f.accept(userP1, thirdMatchID)
	if _, ok, err := f.store.ClaimJob(f.ctx, f.clock()); !ok || err != nil {
		t.Fatal(ok, err)
	}
	f.advance(10 * time.Second)

	v, err := f.q.View(f.ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	// a runs for 30 s more, b takes the other worker, c waits for a.
	if v.Job.Status != storage.JobQueued || v.Position != 2 || v.Wait != 30*time.Second {
		t.Errorf("View() of the third job = %+v, want place 2 in 30 s", v)
	}
	if v, _ := f.q.View(f.ctx, b.ID); v.Position != 1 || v.Wait != 0 {
		t.Errorf("View() of the second job = %+v, want place 1 at once", v)
	}
	if v, _ := f.q.View(f.ctx, a.ID); v.Job.Status != storage.JobRunning || v.Position != 0 {
		t.Errorf("View() of the running job = %+v", v)
	}

	job, _, _ := f.store.ClaimJob(f.ctx, f.clock()) // b
	f.q.run(f.ctx, f.notes, job)
	v, err = f.q.View(f.ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := storage.MatchResult{Map: "de_mirage", Score: [2]int{1, 1}, MetricsVersion: metrics.Version, Team: 1}
	if v.Job.Status != storage.JobDone || !v.HasResult || v.Result != want {
		t.Errorf("View() of a job done = %+v, want the match %+v", v, want)
	}
	if _, err := f.q.View(f.ctx, 999); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("View() of a missing job: error %v, want ErrNotFound", err)
	}
}

// waitFor polls cond until it holds or 10 s pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunParsesTwoAtATime(t *testing.T) {
	f := newFixture(t)
	var (
		running, peak atomic.Int32
		release       = make(chan struct{})
	)
	f.q.parse = func(ctx context.Context, r io.Reader) (*demo.Match, error) {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		if _, err := io.Copy(io.Discard, r); err != nil {
			return nil, err
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return testDemo(), nil
	}
	jobs := []storage.Job{
		f.accept(userP1, testMatchID),
		f.accept(userP1, otherMatchID),
		f.accept(userP1, thirdMatchID),
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.q.Run(ctx, f.notes) }()

	waitFor(t, "two parses", func() bool { return running.Load() == 2 })
	time.Sleep(50 * time.Millisecond)
	if n := running.Load(); n != 2 {
		t.Errorf("%d parses at once, want 2", n)
	}
	close(release)
	waitFor(t, "all jobs done", func() bool {
		for _, j := range jobs {
			if f.job(j.ID).Status != storage.JobDone {
				return false
			}
		}
		return true
	})
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() error: %v", err)
	}
	if p := peak.Load(); p != Workers {
		t.Errorf("at most %d parses at once, want %d", p, Workers)
	}
}

func TestRunStopsAndRequeues(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{})
	var once sync.Once
	f.q.parse = func(ctx context.Context, r io.Reader) (*demo.Match, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	job := f.accept(userP1, testMatchID)
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() { done <- f.q.Run(ctx, f.notes) }()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() error: %v", err)
	}
	got := f.job(job.ID)
	if got.Status != storage.JobQueued || got.Attempts != 0 || got.DemoURL == "" {
		t.Errorf("job after the stop = %+v, want queued with its link, the attempt taken back", got)
	}
}

func TestRunRecovers(t *testing.T) {
	f := newFixture(t)
	once := f.accept(userP1, testMatchID)
	twice := f.accept(userP6, otherMatchID)
	// A bot died with both running; twice was taken again and the bot died
	// again.
	for range 2 {
		if _, ok, err := f.store.ClaimJob(f.ctx, f.clock()); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	if _, err := f.store.RecoverJobs(f.ctx, MaxAttempts, f.clock()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, ok, err := f.store.ClaimJob(f.ctx, f.clock()); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	// Now both were taken twice: make once a first-timer again.
	if _, err := f.db().Exec(`UPDATE jobs SET attempts = 1 WHERE id = ?`, once.ID); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.q.Run(ctx, f.notes) }()
	waitFor(t, "the job taken once to be done", func() bool { return f.job(once.ID).Status == storage.JobDone })
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() error: %v", err)
	}
	if got := f.job(twice.ID); got.Status != storage.JobFailed || got.Reason != ReasonInterrupted {
		t.Errorf("job taken twice = %s %q, want failed %q", got.Status, got.Reason, ReasonInterrupted)
	}
	if !f.notes.heard(twice.ID) {
		t.Error("the notifier did not hear of the job failed on recovery")
	}
}
