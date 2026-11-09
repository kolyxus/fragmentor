package analysis

import (
	"errors"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

func TestSubmitAccepts(t *testing.T) {
	f := newFixture(t)
	out := f.submit(userP1, testMatchID)
	if out.Verdict != Accepted || out.Position != 1 || out.Wait != 0 {
		t.Fatalf("Submit() = %+v, want Accepted at place 1 at once", out)
	}
	j := f.job(out.Job.ID)
	link := f.demoLink(testMatchID)
	if j.Status != storage.JobQueued || j.TelegramID != userP1 || j.MatchID != testMatchID ||
		j.DemoURL != link.URL || !j.LinkExpires.Equal(link.Expires) || j.Language != "ru" {
		t.Errorf("job = %+v", j)
	}
	// A second link behind it waits for a worker that is free: none runs.
	if out := f.submit(userP6, otherMatchID); out.Verdict != Accepted || out.Position != 2 || out.Wait != 0 {
		t.Errorf("second Submit() = %+v, want place 2 at once with two workers free", out)
	}
	f.checkLog()
}

func TestSubmitVerdicts(t *testing.T) {
	notFinished := testDetails()
	notFinished.Status = "ONGOING"
	bestOf3 := testDetails()
	bestOf3.BestOf = 3

	tests := []struct {
		name    string
		user    int64
		link    func(f *fixture) faceit.DemoLink
		details *faceit.Match
		err     error // of FACEIT
		want    Verdict
	}{
		{name: "not linked", user: 99, want: NotLinked},
		{name: "link has run out", user: userP1, want: LinkExpired, link: func(f *fixture) faceit.DemoLink {
			l := f.demoLink(testMatchID)
			l.Expires = f.clock().Add(-time.Second)
			return l
		}},
		{name: "link about to run out", user: userP1, want: LinkExpired, link: func(f *fixture) faceit.DemoLink {
			l := f.demoLink(testMatchID)
			l.Expires = f.clock().Add(LinkMargin - time.Second)
			return l
		}},
		{name: "match not found", user: userP1, err: &faceit.StatusError{Op: "get match", Status: 404}, want: MatchNotFound},
		{name: "not finished", user: userP1, details: &notFinished, want: MatchNotFinished},
		{name: "best of 3", user: userP1, details: &bestOf3, want: MultiMap},
		{name: "not in the match", user: userOther, want: NotInMatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.details != nil {
				f.faceit.set(testMatchID, *tt.details)
			}
			f.faceit.err = tt.err
			link := f.demoLink(testMatchID)
			if tt.link != nil {
				link = tt.link(f)
			}
			out, err := f.q.Submit(f.ctx, Request{TelegramID: tt.user, Language: "en", Link: link})
			if err != nil {
				t.Fatalf("Submit() error: %v", err)
			}
			if out.Verdict != tt.want {
				t.Errorf("Submit() verdict %v, want %v", out.Verdict, tt.want)
			}
			if tt.want == NotInMatch && out.Account.PlayerID != playerIDOf(99) {
				t.Errorf("Account = %+v, want the account linked", out.Account)
			}
			if ids, _ := f.store.QueuedJobIDs(f.ctx); len(ids) != 0 {
				t.Errorf("jobs %v added for verdict %v", ids, out.Verdict)
			}
		})
	}
}

func TestSubmitFaceitDown(t *testing.T) {
	f := newFixture(t)
	f.faceit.err = &faceit.StatusError{Op: "get match", Status: 503}
	_, err := f.q.Submit(f.ctx, Request{TelegramID: userP1, Language: "ru", Link: f.demoLink(testMatchID)})
	if !errors.Is(err, ErrFaceit) {
		t.Errorf("Submit() error %v, want ErrFaceit", err)
	}
}

func TestSubmitDuplicateAndTooMany(t *testing.T) {
	f := newFixture(t)
	first := f.accept(userP1, testMatchID)
	out := f.submit(userP1, testMatchID)
	if out.Verdict != Duplicate || out.Job.ID != first.ID {
		t.Errorf("the same link again = %+v, want Duplicate of job %d", out, first.ID)
	}
	// Another player of the match may queue it too.
	f.accept(userP6, testMatchID)

	f.accept(userP1, otherMatchID)
	f.accept(userP1, thirdMatchID)
	if out := f.submit(userP1, fourthMatchID); out.Verdict != TooMany {
		t.Errorf("a fourth demo = %v, want TooMany", out.Verdict)
	}
	// Once one of them is over, there is room again.
	f.step()
	f.accept(userP1, fourthMatchID)
}

func TestSubmitQueueTooLong(t *testing.T) {
	f := newFixture(t)
	// Jobs take 150 s by the estimate; two run, started now.
	f.q.durations.add(150 * time.Second)
	f.accept(userP1, testMatchID)
	f.accept(userP6, otherMatchID)
	for range Workers {
		if _, ok, err := f.store.ClaimJob(f.ctx, f.clock()); !ok || err != nil {
			t.Fatalf("ClaimJob() = %v, %v", ok, err)
		}
	}
	// The next two wait 150 s, until both workers are free: their links
	// live 299 s, they fit.
	for i, user := range []int64{userP1, userP6} {
		out := f.submit(user, thirdMatchID)
		if out.Verdict != Accepted || out.Position != i+1 || out.Wait != 150*time.Second {
			t.Fatalf("Submit() %d = %+v, want Accepted at place %d in 150 s", i+3, out, i+1)
		}
	}
	// A fifth would start in 300 s, after its link runs out.
	out := f.submit(userP1, fourthMatchID)
	want := Outcome{
		Verdict:  QueueTooLong,
		Position: 3,
		Wait:     300 * time.Second,
		LinkLeft: 299 * time.Second,
		Lifetime: 299 * time.Second,
		// A fresh link sent in a minute waits 240 s of its 299.
		RetryIn: time.Minute,
	}
	if out != want {
		t.Errorf("fifth Submit() = %+v, want %+v", out, want)
	}
	if ids, _ := f.store.QueuedJobIDs(f.ctx); len(ids) != 2 {
		t.Errorf("queued jobs %v, want the two accepted", ids)
	}

	// A longer queue tells to wait longer.
	f.q.durations.add(600 * time.Second)
	f.q.durations.add(600 * time.Second)
	out = f.submit(userP1, fourthMatchID)
	if out.Verdict != QueueTooLong || out.Wait != 1200*time.Second || out.RetryIn != 1200*time.Second-299*time.Second+LinkMargin {
		t.Errorf("Submit() to a long queue = %+v", out)
	}
	f.checkLog()
}

func TestSubmitAnalyzed(t *testing.T) {
	f := newFixture(t)
	f.accept(userP1, testMatchID)
	f.step()
	out := f.submit(userP1, testMatchID)
	want := storage.MatchResult{Map: "de_mirage", Score: [2]int{1, 1}, MetricsVersion: metrics.Version, Team: 0}
	if out.Verdict != Analyzed || out.Result != want {
		t.Errorf("Submit() of a parsed match = %+v, want Analyzed %+v", out, want)
	}
	// Player 6 used the bot when the match was parsed: kept with it too.
	if out := f.submit(userP6, testMatchID); out.Verdict != Analyzed || out.Result.Team != 1 {
		t.Errorf("Submit() of player 6 = %+v, want Analyzed for team 1", out)
	}
	if calls := f.faceit.calls; calls != 2 {
		t.Errorf("%d FACEIT calls, want 2: the parsed match is not asked for again", calls)
	}
}
