package storage

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var jobsT0 = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

const (
	matchM = "1-00000000-0000-4000-8000-0000000000a1"
	matchN = "1-00000000-0000-4000-8000-0000000000a2"
	matchO = "1-00000000-0000-4000-8000-0000000000a3"
)

// newJobStore opens a database with users 42 and 43, who linked accountA
// and accountB.
func newJobStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(tempDir(t), "test.db"))
	for id, acc := range map[int64]FaceitAccount{42: accountA, 43: accountB} {
		if err := s.LinkFaceitAccount(ctx, User{TelegramID: id, Language: "ru"}, acc, jobsT0); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func admitAll(QueueState) error { return nil }

func addJob(t *testing.T, s *Store, telegramID int64, matchID string, now time.Time) Job {
	t.Helper()
	j, err := s.AddJob(context.Background(), NewJob{
		TelegramID:  telegramID,
		MatchID:     matchID,
		DemoURL:     "https://example.com/" + matchID,
		LinkExpires: now.Add(5 * time.Minute),
		Language:    "ru",
	}, now, admitAll)
	if err != nil {
		t.Fatalf("AddJob() error: %v", err)
	}
	return j
}

func claim(t *testing.T, s *Store, now time.Time) (Job, bool) {
	t.Helper()
	j, ok, err := s.ClaimJob(context.Background(), now)
	if err != nil {
		t.Fatalf("ClaimJob() error: %v", err)
	}
	return j, ok
}

func TestAddJob(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)

	j := addJob(t, s, 42, matchM, jobsT0)
	want := Job{
		ID: j.ID, TelegramID: 42, MatchID: matchM, DemoURL: "https://example.com/" + matchM,
		LinkExpires: jobsT0.Add(5 * time.Minute), Language: "ru", Status: JobQueued, CreatedAt: jobsT0,
	}
	if j != want {
		t.Errorf("AddJob() = %+v, want %+v", j, want)
	}
	got, err := s.Job(ctx, j.ID)
	if err != nil || got != want {
		t.Errorf("Job() = %+v, %v; want %+v", got, err, want)
	}
	if _, err := s.Job(ctx, j.ID+100); !errors.Is(err, ErrNotFound) {
		t.Errorf("Job() of a missing job: error %v, want ErrNotFound", err)
	}

	// The same match again from the same user, waiting or running.
	if _, err := s.AddJob(ctx, NewJob{TelegramID: 42, MatchID: matchM, Language: "ru"}, jobsT0, admitAll); !errors.Is(err, ErrJobExists) {
		t.Errorf("AddJob() of a queued match: error %v, want ErrJobExists", err)
	}
	claim(t, s, jobsT0)
	if _, err := s.AddJob(ctx, NewJob{TelegramID: 42, MatchID: matchM, Language: "ru"}, jobsT0, admitAll); !errors.Is(err, ErrJobExists) {
		t.Errorf("AddJob() of a running match: error %v, want ErrJobExists", err)
	}
	// Another user may queue the same match, and the same user may once
	// the first job is over.
	addJob(t, s, 43, matchM, jobsT0)
	if err := s.FinishJob(ctx, j.ID, jobsT0); err != nil {
		t.Fatal(err)
	}
	addJob(t, s, 42, matchM, jobsT0)
}

func TestAddJobAdmit(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	addJob(t, s, 42, matchM, jobsT0)
	addJob(t, s, 42, matchN, jobsT0.Add(time.Second))
	claim(t, s, jobsT0.Add(10*time.Second))

	var seen QueueState
	refused := errors.New("refused")
	_, err := s.AddJob(ctx, NewJob{TelegramID: 43, MatchID: matchO, Language: "en"}, jobsT0.Add(20*time.Second), func(q QueueState) error {
		seen = q
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("AddJob() error %v, want the error of admit", err)
	}
	want := QueueState{Running: []time.Time{jobsT0.Add(10 * time.Second)}, Ahead: 1}
	if !slices.Equal(seen.Running, want.Running) || seen.Ahead != want.Ahead {
		t.Errorf("admit saw %+v, want %+v", seen, want)
	}
	if ids, err := s.QueuedJobIDs(ctx); err != nil || len(ids) != 1 {
		t.Errorf("QueuedJobIDs() = %v, %v; want the job refused not to be added", ids, err)
	}
}

func TestClaimJobOrder(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	a := addJob(t, s, 42, matchM, jobsT0)
	b := addJob(t, s, 43, matchM, jobsT0.Add(time.Second))
	c := addJob(t, s, 42, matchN, jobsT0.Add(2*time.Second))

	got, ok := claim(t, s, jobsT0.Add(5*time.Second))
	if !ok || got.ID != a.ID || got.Status != JobRunning || got.Attempts != 1 || !got.StartedAt.Equal(jobsT0.Add(5*time.Second)) {
		t.Fatalf("first ClaimJob() = %+v, %v; want job %d running, attempt 1", got, ok, a.ID)
	}
	// b waits while a parses the same match; c goes first.
	if got, ok := claim(t, s, jobsT0.Add(6*time.Second)); !ok || got.ID != c.ID {
		t.Fatalf("second ClaimJob() = job %d, %v; want job %d", got.ID, ok, c.ID)
	}
	if got, ok := claim(t, s, jobsT0.Add(7*time.Second)); ok {
		t.Fatalf("third ClaimJob() = job %d, want none while its match runs", got.ID)
	}
	state, err := s.QueueAhead(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Running) != 2 || state.Ahead != 0 {
		t.Errorf("QueueAhead() = %+v, want 2 running, none ahead", state)
	}
	if err := s.FailJob(ctx, a.ID, "link_rejected", jobsT0.Add(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got, ok := claim(t, s, jobsT0.Add(9*time.Second)); !ok || got.ID != b.ID {
		t.Fatalf("ClaimJob() after the match is free = job %d, %v; want job %d", got.ID, ok, b.ID)
	}

	failed, err := s.Job(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != JobFailed || failed.Reason != "link_rejected" || failed.DemoURL != "" || !failed.FinishedAt.Equal(jobsT0.Add(8*time.Second)) {
		t.Errorf("failed job = %+v, want failed with the reason, without the link", failed)
	}
}

func TestFinishJobWipesLink(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	j := addJob(t, s, 42, matchM, jobsT0)
	claim(t, s, jobsT0)
	if err := s.FinishJob(ctx, j.ID, jobsT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Job(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobDone || got.DemoURL != "" || got.Reason != "" || !got.FinishedAt.Equal(jobsT0.Add(time.Minute)) {
		t.Errorf("finished job = %+v", got)
	}
	var url *string
	if err := s.db.QueryRow(`SELECT demo_url FROM jobs WHERE id = ?`, j.ID).Scan(&url); err != nil || url != nil {
		t.Errorf("demo_url = %v, %v; want NULL", url, err)
	}
}

func TestRequeueJob(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	j := addJob(t, s, 42, matchM, jobsT0)
	claim(t, s, jobsT0)
	if err := s.RequeueJob(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Job(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobQueued || got.Attempts != 0 || !got.StartedAt.IsZero() {
		t.Errorf("requeued job = %+v, want queued, the attempt taken back", got)
	}
	// It keeps its place in the queue.
	later := addJob(t, s, 43, matchN, jobsT0.Add(time.Second))
	if got, _ := claim(t, s, jobsT0.Add(2*time.Second)); got.ID != j.ID {
		t.Errorf("ClaimJob() = job %d, want the requeued job %d before job %d", got.ID, j.ID, later.ID)
	}
}

func TestRecoverJobs(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	once := addJob(t, s, 42, matchM, jobsT0)
	twice := addJob(t, s, 42, matchN, jobsT0)
	waiting := addJob(t, s, 43, matchO, jobsT0)
	// once was taken once; twice was taken, recovered and taken again.
	claim(t, s, jobsT0)
	claim(t, s, jobsT0)
	if _, err := s.RecoverJobs(ctx, 2, jobsT0); err != nil {
		t.Fatal(err)
	}
	if err := s.FailJob(ctx, once.ID, "x", jobsT0); err != nil { // out of the way
		t.Fatal(err)
	}
	if got, _ := claim(t, s, jobsT0); got.ID != twice.ID || got.Attempts != 2 {
		t.Fatalf("ClaimJob() = %+v, want job %d at attempt 2", got, twice.ID)
	}
	if got, _ := claim(t, s, jobsT0); got.ID != waiting.ID {
		t.Fatalf("ClaimJob() = job %d, want job %d", got.ID, waiting.ID)
	}

	ids, err := s.RecoverJobs(ctx, 2, jobsT0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []int64{twice.ID, waiting.ID}) {
		t.Errorf("RecoverJobs() = %v, want %v", ids, []int64{twice.ID, waiting.ID})
	}
	got, err := s.Job(ctx, twice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobFailed || got.Reason != ReasonInterrupted || got.DemoURL != "" {
		t.Errorf("job taken twice = %+v, want failed as interrupted", got)
	}
	got, err = s.Job(ctx, waiting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobQueued || got.Attempts != 1 || !got.StartedAt.IsZero() {
		t.Errorf("job taken once = %+v, want queued again, attempt 1 kept", got)
	}
}

func TestActiveJobs(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	a := addJob(t, s, 42, matchM, jobsT0)
	b := addJob(t, s, 42, matchN, jobsT0)
	c := addJob(t, s, 43, matchO, jobsT0)
	claim(t, s, jobsT0)
	done := addJob(t, s, 42, matchO, jobsT0)
	if err := s.FailJob(ctx, done.ID, "x", jobsT0); err != nil {
		t.Fatal(err)
	}

	jobs, err := s.ActiveJobs(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, j := range jobs {
		ids = append(ids, j.ID)
	}
	if !slices.Equal(ids, []int64{a.ID, b.ID}) {
		t.Errorf("ActiveJobs(42) = %v, want %v", ids, []int64{a.ID, b.ID})
	}
	if ids, err := s.ActiveJobIDs(ctx); err != nil || !slices.Equal(ids, []int64{a.ID, b.ID, c.ID}) {
		t.Errorf("ActiveJobIDs() = %v, %v; want %v", ids, err, []int64{a.ID, b.ID, c.ID})
	}
	if ids, err := s.QueuedJobIDs(ctx); err != nil || !slices.Equal(ids, []int64{b.ID, c.ID}) {
		t.Errorf("QueuedJobIDs() = %v, %v; want %v", ids, err, []int64{b.ID, c.ID})
	}
	if err := s.SetJobMessage(ctx, b.ID, 77); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Job(ctx, b.ID); err != nil || got.StatusMessageID != 77 {
		t.Errorf("StatusMessageID = %d, %v; want 77", got.StatusMessageID, err)
	}
}

func TestJobsOfDeletedUserGo(t *testing.T) {
	ctx := context.Background()
	s := newJobStore(t)
	addJob(t, s, 42, matchM, jobsT0)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE telegram_id = 42`); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "jobs"); n != 0 {
		t.Errorf("jobs left after the user is deleted: %d", n)
	}
}
