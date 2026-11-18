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
	"github.com/kolyxus/fragmentor/bot/internal/findings"
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

// withFacts returns the seats of the match whose players have facts kept,
// of the current version.
func (f *fixture) withFacts(matchID string) []int {
	f.t.Helper()
	rows, err := f.db().Query(`SELECT seat FROM match_players WHERE match_id = ? AND facts IS NOT NULL AND facts_version = ? ORDER BY seat`,
		matchID, findings.FactsVersion)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var seats []int
	for rows.Next() {
		var s int
		if err := rows.Scan(&s); err != nil {
			f.t.Fatal(err)
		}
		seats = append(seats, s)
	}
	return seats
}

// done is the match of a job done for the user who played seat.
func done(seat, team int) storage.MatchResult {
	return storage.MatchResult{Map: "de_mirage", Score: [2]int{1, 1}, MetricsVersion: metrics.Version, JournalVersion: demo.JournalVersion,
		Team: team, Seat: seat, FactsVersion: findings.FactsVersion}
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
	res, err := f.store.MatchResult(f.ctx, testMatchID, strconv.FormatUint(steamOf(1), 10))
	if err != nil {
		t.Fatal(err)
	}
	if want := done(1, 0); res != want {
		t.Errorf("MatchResult() = %+v, want %+v", res, want)
	}
	var started int64
	if err := f.db().QueryRow(`SELECT started_at FROM matches`).Scan(&started); err != nil || started != t0.Add(-time.Hour).Unix() {
		t.Errorf("started_at = %d, %v; want the start of the match on FACEIT", started, err)
	}

	// Every player, at his seat, with the level and the ID of the roster;
	// the facts only of the players who use the bot, 1 and 6.
	m, j, players, err := f.store.LoadAnalysis(f.ctx, testMatchID)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 10 || m.MetricsVersion != metrics.Version {
		t.Fatalf("%d players of metrics %d kept", len(players), m.MetricsVersion)
	}
	computed := metrics.Compute(testDemo())
	for i, p := range players {
		if p.Seat != i+1 || p.SteamID64 != strconv.FormatUint(steamOf(i+1), 10) || p.PlayerID != playerIDOf(i+1) ||
			p.SkillLevel != levelOf(i+1) || p.Team != i/5 {
			t.Errorf("player %d kept as %+v", i+1, p)
		}
		// The metrics are those of the formulas, without IDs.
		var stored storedMetrics
		if err := json.Unmarshal(p.Metrics, &stored); err != nil {
			t.Fatal(err)
		}
		if stored.Match != computed[i].Match || stored.T != computed[i].T || stored.CT != computed[i].CT {
			t.Errorf("player %d: stored metrics differ from metrics.Compute:\n%s", i+1, p.Metrics)
		}
		if strings.Contains(string(p.Metrics), strconv.FormatUint(steamOf(i+1), 10)) || strings.Contains(string(p.Metrics), "steam_id64") {
			t.Errorf("stored metrics hold the SteamID: %s", p.Metrics)
		}
	}
	if got := f.withFacts(testMatchID); !slices.Equal(got, []int{1, 6}) {
		t.Errorf("facts kept for seats %v, want 1 and 6", got)
	}
	var facts findings.Facts
	if err := json.Unmarshal(players[0].Facts, &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Version != findings.FactsVersion || facts.Match.Map != "de_mirage" || facts.Player.Kills != 1 || facts.Player.Level != levelOf(1) ||
		len(facts.Events.Kills) != 1 {
		t.Errorf("facts of player 1 = %+v", facts)
	}

	// The journal is the anonymous match: seats for the SteamIDs, no names.
	if j.Version != demo.JournalVersion {
		t.Errorf("journal of version %d", j.Version)
	}
	journal, err := demo.DecodeJournal(j.Data)
	if err != nil {
		t.Fatal(err)
	}
	anon, _ := demo.Anonymize(testDemo())
	raw, _ := json.Marshal(journal)
	if want, _ := json.Marshal(anon); string(raw) != string(want) {
		t.Errorf("journal = %s\nwant %s", raw, want)
	}
	for _, secret := range []string{strconv.FormatUint(steamOf(1), 10), `"p1"`, "team_p1"} {
		if strings.Contains(string(raw), secret) || strings.Contains(string(players[0].Facts), secret) {
			t.Errorf("the journal or the facts hold %s", secret)
		}
	}
	if !strings.Contains(f.logText(), "journal_kib=") {
		t.Error("the log does not tell the size of the journal")
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

func TestNewUserFromTheJournal(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	f.step()

	// Player 7 starts using the bot after the match was parsed: he is in it
	// by his SteamID64, and his facts are counted from the journal at once,
	// with no link to download.
	const userP7, userP8 = 45, 46
	f.link(userP7, 7)
	out := f.submit(userP7, testMatchID)
	if out.Verdict != Analyzed || out.Result.Team != 1 || out.Result.Seat != 7 {
		t.Fatalf("Submit() of player 7 = %+v, want Analyzed, team 1", out)
	}
	if got := f.withFacts(testMatchID); !slices.Equal(got, []int{1, 6, 7}) {
		t.Errorf("facts for seats %v, want 1, 6 and 7", got)
	}
	// Player 8 links his account and sends nothing: the idle worker counts
	// his facts from the journal.
	f.link(userP8, 8)
	if !f.q.recountStale(f.ctx) {
		t.Error("the idle worker counted nothing again")
	}
	if got := f.withFacts(testMatchID); !slices.Equal(got, []int{1, 6, 7, 8}) {
		t.Errorf("facts for seats %v, want 1, 6, 7 and 8", got)
	}
	if f.q.recountStale(f.ctx) {
		t.Error("the idle worker counted again a match up to date")
	}
	if n := f.srv.requests.Load(); n != 1 {
		t.Errorf("%d downloads, want the 1 of the first parse", n)
	}
	if !strings.Contains(f.logText(), "match counted again") {
		t.Error("the log does not tell of the recount")
	}
	f.checkLog()
}

func TestOldFormulasCountedAgain(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	second := f.accept(userP6, testMatchID)
	f.step()
	old := func() {
		t.Helper()
		for _, q := range []string{
			`UPDATE matches SET metrics_version = metrics_version - 1`,
			`UPDATE match_players SET metrics = '{}'`,
			`UPDATE match_players SET facts = '{}', facts_version = facts_version - 1 WHERE facts IS NOT NULL`,
		} {
			if _, err := f.db().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	current := func(what string) {
		t.Helper()
		for i, steam := range map[int]uint64{1: steamOf(1), 6: steamOf(6)} {
			if res, err := f.store.MatchResult(f.ctx, testMatchID, strconv.FormatUint(steam, 10)); err != nil || res != done(i, (i-1)/5) {
				t.Errorf("%s: MatchResult() of player %d = %+v, %v; want the current versions", what, i, res, err)
			}
		}
		_, _, players, err := f.store.LoadAnalysis(f.ctx, testMatchID)
		if err != nil || len(players) != 10 || string(players[9].Metrics) == "{}" {
			t.Errorf("%s: the metrics of every player are not counted again: %v", what, err)
		}
		if got := f.withFacts(testMatchID); !slices.Equal(got, []int{1, 6}) {
			t.Errorf("%s: facts for seats %v, want 1 and 6", what, got)
		}
	}

	// New formulas: the job of player 6, queued meanwhile, counts the match
	// again from the journal, with no download.
	old()
	if got := f.step(); got.ID != second.ID || got.Status != storage.JobDone {
		t.Fatalf("job = %+v, want done", got)
	}
	current("the job")
	// A request of a user counts his match again at once.
	old()
	if out := f.submit(userP1, testMatchID); out.Verdict != Analyzed {
		t.Fatalf("Submit() of a match of old formulas = %v, want Analyzed", out.Verdict)
	}
	current("the request")
	// Old formulas alone, the facts of the current version, too.
	if _, err := f.db().Exec(`UPDATE matches SET metrics_version = metrics_version - 1`); err != nil {
		t.Fatal(err)
	}
	if out := f.submit(userP1, testMatchID); out.Verdict != Analyzed {
		t.Fatalf("Submit() of a match of old formulas = %v, want Analyzed", out.Verdict)
	}
	current("old formulas alone")
	// With nobody asking, the idle worker does.
	old()
	if !f.q.recountStale(f.ctx) {
		t.Error("the idle worker counted nothing again")
	}
	current("the idle worker")
	if n := f.srv.requests.Load(); n != 1 {
		t.Errorf("%d downloads, want the 1 of the first parse", n)
	}
	f.checkLog()
}

func TestOldJournalParsedAgain(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	f.step()
	// The journal is of an older version: the match needs its demo again,
	// and the idle worker cannot count it.
	if _, err := f.db().Exec(`UPDATE match_journals SET version = version - 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db().Exec(`UPDATE matches SET metrics_version = metrics_version - 1`); err != nil {
		t.Fatal(err)
	}
	if f.q.recountStale(f.ctx) {
		t.Error("a match of an old journal was counted again")
	}
	if err := f.q.recount(f.ctx, testMatchID); !errors.Is(err, errOldJournal) {
		t.Errorf("recount() of a match of an old journal: %v, want errOldJournal", err)
	}
	if out := f.submit(userP1, testMatchID); out.Verdict != Accepted {
		t.Fatalf("Submit() of a match of an old journal = %v, want Accepted", out.Verdict)
	}
	if got := f.step(); got.Status != storage.JobDone {
		t.Fatalf("job = %+v, want done", got)
	}
	if n := f.srv.requests.Load(); n != 2 {
		t.Errorf("%d downloads, want 2", n)
	}
	if res, err := f.store.MatchResult(f.ctx, testMatchID, strconv.FormatUint(steamOf(1), 10)); err != nil || res != done(1, 0) {
		t.Errorf("MatchResult() = %+v, %v; want the current versions", res, err)
	}
}

// TestBadJournal breaks the journal of a parsed match: a journal that cannot
// be read leaves the match not parsed. The job of the match and a new link
// download the demo again and replace the journal, whether the numbers of
// the match are of the current versions or not; the idle worker, which has
// no link, fails on it once and does not try again until the bot restarts.
func TestBadJournal(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	waiting := f.accept(userP6, testMatchID)
	f.step()
	broken := func() {
		t.Helper()
		if _, err := f.db().Exec(`UPDATE match_journals SET journal = x'00010203'`); err != nil {
			t.Fatal(err)
		}
	}
	// replaced checks the journal was replaced by that of the demo
	// downloaded once more, with the numbers of the current versions.
	replaced := func(what string, downloads int32) {
		t.Helper()
		if n := f.srv.requests.Load(); n != downloads {
			t.Errorf("%s: %d downloads, want %d", what, n, downloads)
		}
		_, j, _, err := f.store.LoadAnalysis(f.ctx, testMatchID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := demo.DecodeJournal(j.Data); err != nil {
			t.Errorf("%s: the journal is not replaced: %v", what, err)
		}
		for i, steam := range map[int]uint64{1: steamOf(1), 6: steamOf(6)} {
			if res, err := f.store.MatchResult(f.ctx, testMatchID, strconv.FormatUint(steam, 10)); err != nil || res != done(i, (i-1)/5) {
				t.Errorf("%s: MatchResult() of player %d = %+v, %v; want the current versions", what, i, res, err)
			}
		}
		if got := f.withFacts(testMatchID); !slices.Equal(got, []int{1, 6}) {
			t.Errorf("%s: facts for seats %v, want 1 and 6", what, got)
		}
	}

	// The job of the match queued before the journal broke.
	broken()
	if got := f.step(); got.ID != waiting.ID || got.Status != storage.JobDone {
		t.Fatalf("job = %+v, want done", got)
	}
	replaced("the job", 2)

	// A new link, the numbers of the current versions.
	broken()
	if out := f.submit(userP1, testMatchID); out.Verdict != Accepted {
		t.Fatalf("Submit() of a match whose journal cannot be read = %v, want Accepted", out.Verdict)
	}
	if got := f.step(); got.Status != storage.JobDone {
		t.Fatalf("job = %+v, want done", got)
	}
	replaced("the link", 3)

	// Older formulas: nothing to count again from.
	broken()
	if _, err := f.db().Exec(`UPDATE matches SET metrics_version = metrics_version - 1`); err != nil {
		t.Fatal(err)
	}
	if f.q.recountStale(f.ctx) || f.q.recountStale(f.ctx) {
		t.Error("a journal that cannot be read was counted again")
	}
	if n := strings.Count(f.logText(), "count a match again"); n != 1 {
		t.Errorf("%d failures in the log, want 1:\n%s", n, f.logText())
	}
	if out := f.submit(userP6, testMatchID); out.Verdict != Accepted {
		t.Fatalf("Submit() of a match of older formulas whose journal cannot be read = %v, want Accepted", out.Verdict)
	}
	if got := f.step(); got.Status != storage.JobDone {
		t.Fatalf("job = %+v, want done", got)
	}
	replaced("older formulas", 4)
	if n := strings.Count(f.logText(), "the journal of a match cannot be read"); n != 5 {
		t.Errorf("%d warnings of a journal that cannot be read, want 5: the job, then a link and its job twice:\n%s", n, f.logText())
	}
	f.checkLog()
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
			if _, err := f.store.MatchResult(f.ctx, testMatchID, strconv.FormatUint(steamOf(1), 10)); !errors.Is(err, storage.ErrNotFound) {
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
	want := done(6, 1)
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
