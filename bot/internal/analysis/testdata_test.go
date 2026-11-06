package analysis

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// smallestDemo returns the smallest demo in bot/testdata, skipping the test
// without one, or with -short. The demos are not in git.
func smallestDemo(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("parsing a real demo takes seconds")
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "*.dem.zst"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no demos in bot/testdata")
	}
	size := func(f string) int64 {
		info, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	return slices.MinFunc(files, func(a, b string) int { return cmp.Compare(size(a), size(b)) })
}

// serveFile makes the demo server answer the next request with the file,
// cut off after limit bytes if limit > 0.
func serveFile(t *testing.T, srv *demoServer, path string, limit int64) {
	srv.then(func(w http.ResponseWriter, _ *http.Request) {
		f, err := os.Open(path)
		if err != nil {
			t.Error(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		if limit <= 0 {
			_, _ = io.Copy(w, f)
			return
		}
		_, _ = io.CopyN(w, f, limit)
		w.(http.Flusher).Flush()
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
	})
}

// TestRealDemo takes a real demo from a link to the metrics in the
// database: the download, the zstd stream and the parser, with FACEIT
// stubbed by a roster made of the players of the demo.
func TestRealDemo(t *testing.T) {
	file := smallestDemo(t)
	f := newFixture(t)
	serveFile(t, f.srv, file, 0)

	var parsed *demo.Match
	f.q.parse = func(ctx context.Context, r io.Reader) (*demo.Match, error) {
		m, err := demo.Parse(ctx, r, demo.Options{})
		if err != nil {
			return nil, err
		}
		parsed = m
		// The roster of the match: player i of the demo is FACEIT player
		// i+1, the first of them user 42's account.
		details := testDetails()
		details.Map = m.Map
		details.Teams = []faceit.Team{{Faction: "faction1"}}
		for i, p := range m.Players {
			details.Teams[0].Players = append(details.Teams[0].Players, faceit.MatchPlayer{
				ID: playerIDOf(i + 1), SteamID64: strconv.FormatUint(p.SteamID64, 10), SkillLevel: levelOf(i + 1),
			})
		}
		f.faceit.set(testMatchID, details)
		return m, nil
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Minute)
	defer cancel()
	job, err := f.store.AddJob(ctx, storage.NewJob{
		TelegramID: userP1, MatchID: testMatchID, DemoURL: f.demoLink(testMatchID).URL, Language: "ru",
	}, f.clock(), func(storage.QueueState) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	f.q.jobTimeout = 10 * time.Minute
	got := f.step()
	if got.Status != storage.JobDone {
		t.Fatalf("job %d = %s %q, want done", job.ID, got.Status, got.Reason)
	}
	if parsed == nil || len(parsed.Players) == 0 {
		t.Fatal("the demo was not parsed")
	}

	res, err := f.store.MatchResult(ctx, testMatchID, playerIDOf(1))
	if err != nil {
		t.Fatal(err)
	}
	if res.Map != parsed.Map || res.Score != [2]int{parsed.Teams[0].Score, parsed.Teams[1].Score} || res.Team != parsed.Players[0].Team {
		t.Errorf("MatchResult() = %+v, the demo is %s %d:%d", res, parsed.Map, parsed.Teams[0].Score, parsed.Teams[1].Score)
	}
	// With IDs, only the accounts of users 42 and 43, players 1 and 6;
	// every player gives a sample.
	if got := f.keptPlayers(testMatchID); !slices.Equal(got, []string{playerIDOf(1), playerIDOf(6)}) {
		t.Errorf("match_players = %v, want the accounts of users 42 and 43", got)
	}
	if n := f.count("reference_samples"); n != len(parsed.Players) {
		t.Errorf("%d reference samples, want %d", n, len(parsed.Players))
	}
	var stored string
	if err := f.db().QueryRow(`SELECT metrics FROM match_players`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var m storedMetrics
	if err := json.Unmarshal([]byte(stored), &m); err != nil {
		t.Fatal(err)
	}
	want := metrics.Compute(parsed)[0]
	if m.Match != want.Match || m.T != want.T || m.CT != want.CT {
		t.Error("stored metrics differ from metrics.Compute of the demo")
	}
	if m.Match.Kills != parsed.Players[0].Kills || m.Match.Damage != parsed.Players[0].Damage {
		t.Errorf("stored kills %d, damage %d; the summary has %d, %d",
			m.Match.Kills, m.Match.Damage, parsed.Players[0].Kills, parsed.Players[0].Damage)
	}
	f.checkLog()
}

// TestRealDemoCutShort breaks the download of a real demo off midway, twice:
// the parser sees a truncated demo, the job knows it was the network.
func TestRealDemoCutShort(t *testing.T) {
	file := smallestDemo(t)
	f := newFixture(t)
	serveFile(t, f.srv, file, 5<<20)
	serveFile(t, f.srv, file, 5<<20)
	f.q.parse = func(ctx context.Context, r io.Reader) (*demo.Match, error) {
		return demo.Parse(ctx, r, demo.Options{})
	}
	f.accept(userP1, testMatchID)
	got := f.step()
	if got.Status != storage.JobFailed || got.Reason != ReasonDownload {
		t.Errorf("job = %s %q, want failed %q", got.Status, got.Reason, ReasonDownload)
	}
	if n := f.srv.requests.Load(); n != 2 {
		t.Errorf("%d downloads, want 2", n)
	}
}
