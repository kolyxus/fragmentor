package analysis

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
	"github.com/kolyxus/fragmentor/bot/internal/testdemos"
)

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
	file := testdemos.Smallest(t)
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
		// Users 42 and 43 are players 1 and 6 of the demo, by their
		// SteamIDs: the facts are kept for them.
		for user, i := range map[int64]int{userP1: 0, userP6: 5} {
			f.linkAccount(user, storage.FaceitAccount{PlayerID: playerIDOf(i + 1), SteamID64: strconv.FormatUint(m.Players[i].SteamID64, 10)})
		}
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

	res, err := f.store.MatchResult(ctx, testMatchID, strconv.FormatUint(parsed.Players[0].SteamID64, 10))
	if err != nil {
		t.Fatal(err)
	}
	if res.Map != parsed.Map || res.Score != [2]int{parsed.Teams[0].Score, parsed.Teams[1].Score} || res.Team != parsed.Players[0].Team ||
		res.Seat != 1 || res.JournalVersion != demo.JournalVersion || res.FactsVersion != findings.FactsVersion {
		t.Errorf("MatchResult() = %+v, the demo is %s %d:%d", res, parsed.Map, parsed.Teams[0].Score, parsed.Teams[1].Score)
	}
	// Every player is kept with the metrics; the facts only of users 42 and
	// 43, players 1 and 6.
	_, j, players, err := f.store.LoadAnalysis(ctx, testMatchID)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != len(parsed.Players) {
		t.Fatalf("%d players kept, the demo has %d", len(players), len(parsed.Players))
	}
	if got := f.withFacts(testMatchID); !slices.Equal(got, []int{1, 6}) {
		t.Errorf("facts kept for seats %v, want those of users 42 and 43", got)
	}
	computed := metrics.Compute(parsed)
	for i, p := range players {
		var m storedMetrics
		if err := json.Unmarshal(p.Metrics, &m); err != nil {
			t.Fatal(err)
		}
		if m.Match != computed[i].Match || m.T != computed[i].T || m.CT != computed[i].CT {
			t.Errorf("player %d: stored metrics differ from metrics.Compute of the demo", i+1)
		}
		if m.Match.Kills != parsed.Players[i].Kills || m.Match.Damage != parsed.Players[i].Damage {
			t.Errorf("player %d: stored kills %d, damage %d; the summary has %d, %d", i+1,
				m.Match.Kills, m.Match.Damage, parsed.Players[i].Kills, parsed.Players[i].Damage)
		}
	}
	// The journal holds no SteamID and no name, and is the match again: a
	// recount from it gives the same metrics.
	anon, err := demo.DecodeJournal(j.Data)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(anon)
	for _, p := range parsed.Players {
		for _, secret := range []string{strconv.FormatUint(p.SteamID64, 10), `"` + p.Name + `"`} {
			if strings.Contains(string(raw), secret) || strings.Contains(string(players[0].Facts), secret) {
				t.Errorf("the journal or the facts hold %s", secret)
			}
		}
	}
	if err := f.q.recount(ctx, testMatchID); err != nil {
		t.Fatal(err)
	}
	_, _, again, err := f.store.LoadAnalysis(ctx, testMatchID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range players {
		if string(again[i].Metrics) != string(players[i].Metrics) || string(again[i].Facts) != string(players[i].Facts) {
			t.Errorf("player %d: the recount from the journal differs from the parse", i+1)
		}
	}
	t.Logf("journal: %d KiB, facts of a player: %d KiB", len(j.Data)>>10, len(players[0].Facts)>>10)
	f.checkLog()
}

// TestRealDemoCutShort breaks the download of a real demo off midway, twice:
// the parser sees a truncated demo, the job knows it was the network.
func TestRealDemoCutShort(t *testing.T) {
	file := testdemos.Smallest(t)
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
