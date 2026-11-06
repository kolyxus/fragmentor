package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/analysis"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// stubJobs stands in for the queue of analyses: Submit answers with
// outcome, View with the views set, and Run waits for the end.
type stubJobs struct {
	mu       sync.Mutex
	outcome  analysis.Outcome
	err      error
	requests []analysis.Request
	views    map[int64]analysis.View
	messages map[int64]int // the status message of each job
}

func newStubJobs() *stubJobs {
	return &stubJobs{views: map[int64]analysis.View{}, messages: map[int64]int{}}
}

func (s *stubJobs) Submit(_ context.Context, r analysis.Request) (analysis.Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r)
	return s.outcome, s.err
}

func (s *stubJobs) View(_ context.Context, id int64) (analysis.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.views[id]
	if !ok {
		return analysis.View{}, storage.ErrNotFound
	}
	v.Job.ID, v.Job.StatusMessageID = id, s.messages[id]
	return v, nil
}

func (s *stubJobs) SetMessage(_ context.Context, id int64, messageID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[id] = messageID
	return nil
}

func (s *stubJobs) Run(ctx context.Context, _ analysis.Notifier) error {
	<-ctx.Done()
	return nil
}

func (s *stubJobs) setView(id int64, v analysis.View) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.views[id] = v
}

// testDemoLink is a signed link in the form FACEIT gives, signed now for
// 299 s, with a made-up key and signature.
func testDemoLink(now time.Time) string {
	return "https://demos-europe-central-faceit-cdn.s3.eu-central-003.backblazeb2.com/cs2/" +
		"1-00000000-0000-4000-8000-0000000000a1-1-1.dem.zst" +
		"?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=0000000000000000000000000%2F20261106%2F%2Fs3%2Faws4_request" +
		"&X-Amz-Date=" + now.UTC().Format("20060102T150405Z") + "&X-Amz-Expires=299&X-Amz-SignedHeaders=host&x-id=GetObject" +
		"&X-Amz-Signature=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}

// newDemoBot returns a test bot whose user 42 is an admin, with the queue
// stubbed.
func newDemoBot(t *testing.T) (*Bot, *fakeAPI, *stubJobs) {
	t.Helper()
	b, api := newTestBot(t, &stubUsers{})
	jobs := newStubJobs()
	b.jobs = jobs
	b.admins[42] = true
	return b, api, jobs
}

// hasHowTo reports whether the call carries the button that tells how to
// get a link.
func hasHowTo(c apiCall) bool {
	return strings.Contains(c.params["reply_markup"], demoButtonHowTo)
}

func TestDemoLinkIn(t *testing.T) {
	link := testDemoLink(testNow)
	tests := []struct {
		in, want string
		ok       bool
	}{
		{link, link, true},
		{"вот демка: " + link + ".", link, true},
		{"«" + link + "»", link, true},
		{"(" + link + ")!", link, true},
		{"https://demos-europe-central.backblaze.faceit-cdn.net/cs2/x-1-1.dem.zst", "https://demos-europe-central.backblaze.faceit-cdn.net/cs2/x-1-1.dem.zst", true},
		{"match.dem.zst", "match.dem.zst", true},
		{"https://www.faceit.com/ru/players/s1mple", "", false},
		{"привет", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := demoLinkIn(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("demoLinkIn(%.40q) = %.40q, %v; want %.40q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDemoLinkClosedToUsers(t *testing.T) {
	b, api, jobs := newDemoBot(t)
	b.admins = map[int64]bool{}
	process(b, privateMessage(testDemoLink(testNow), "ru"))
	if got, want := api.sentTexts(), []string{i18n.For(i18n.RU).DemoClosed}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
	if len(jobs.requests) != 0 {
		t.Errorf("Submit() called for a user who is not an admin")
	}
}

func TestDemoBadLinks(t *testing.T) {
	ru := i18n.For(i18n.RU)
	tests := []struct {
		name, text, want string
	}{
		{"other host", "https://example.com/cs2/1-00000000-0000-4000-8000-0000000000a1-1-1.dem.zst?X-Amz-Signature=x", ru.DemoBadLink},
		{"http", strings.Replace(testDemoLink(testNow), "https://", "http://", 1), ru.DemoBadLink},
		{"demo_url of the Data API", "https://demos-europe-central.backblaze.faceit-cdn.net/cs2/1-00000000-0000-4000-8000-0000000000a1-1-1.dem.zst", ru.DemoUnsigned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, api, jobs := newDemoBot(t)
			process(b, privateMessage(tt.text, "ru"))
			calls := api.callsTo("sendMessage")
			if len(calls) != 1 || calls[0].params["text"] != tt.want || !hasHowTo(calls[0]) {
				t.Fatalf("sent %v, want %q with the button", calls, tt.want)
			}
			if len(jobs.requests) != 0 {
				t.Errorf("Submit() called for a bad link")
			}
		})
	}
}

func TestDemoVerdicts(t *testing.T) {
	ru := i18n.For(i18n.RU)
	tests := []struct {
		name    string
		outcome analysis.Outcome
		want    string
		howTo   bool
	}{
		{"analyzed, team 1", analysis.Outcome{Verdict: analysis.Analyzed,
			Result: storage.MatchResult{Map: "de_mirage", Score: [2]int{9, 13}, Team: 1}},
			fmt.Sprintf(ru.DemoAlready, "Mirage", 13, 9), false},
		{"duplicate", analysis.Outcome{Verdict: analysis.Duplicate}, ru.DemoDuplicate, false},
		{"too many", analysis.Outcome{Verdict: analysis.TooMany}, fmt.Sprintf(ru.DemoTooMany, analysis.MaxActiveJobs), false},
		{"not linked", analysis.Outcome{Verdict: analysis.NotLinked}, ru.DemoNoAccount, false},
		{"link expired", analysis.Outcome{Verdict: analysis.LinkExpired}, ru.DemoExpired, true},
		{"queue too long", analysis.Outcome{Verdict: analysis.QueueTooLong, Position: 5,
			Wait: 290 * time.Second, Lifetime: 299 * time.Second, RetryIn: time.Minute},
			fmt.Sprintf(ru.DemoQueueTooLong, 5, 5, 1), false},
		{"not found", analysis.Outcome{Verdict: analysis.MatchNotFound}, ru.DemoNotFound, false},
		{"not finished", analysis.Outcome{Verdict: analysis.MatchNotFinished}, ru.DemoNotFinished, false},
		{"several maps", analysis.Outcome{Verdict: analysis.MultiMap}, ru.DemoMultiMap, false},
		{"not yours", analysis.Outcome{Verdict: analysis.NotInMatch, Account: storage.FaceitAccount{Nickname: "kolyx"}},
			fmt.Sprintf(ru.DemoNotYours, "kolyx"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, api, jobs := newDemoBot(t)
			jobs.outcome = tt.outcome
			process(b, privateMessage("вот: "+testDemoLink(testNow), "ru"))
			calls := api.callsTo("sendMessage")
			if len(calls) != 1 || calls[0].params["text"] != tt.want {
				t.Fatalf("sent %v, want %q", calls, tt.want)
			}
			if hasHowTo(calls[0]) != tt.howTo {
				t.Errorf("the button is there: %v, want %v", hasHowTo(calls[0]), tt.howTo)
			}
			want := analysis.Request{TelegramID: 42, Language: "ru"}
			if len(jobs.requests) != 1 || jobs.requests[0].TelegramID != want.TelegramID || jobs.requests[0].Language != want.Language ||
				jobs.requests[0].Link.MatchID != "1-00000000-0000-4000-8000-0000000000a1" {
				t.Errorf("Submit() got %+v", jobs.requests)
			}
		})
	}
}

func TestDemoSubmitFails(t *testing.T) {
	ru := i18n.For(i18n.RU)
	for err, want := range map[error]string{
		fmt.Errorf("%w: status 503", analysis.ErrFaceit): ru.FaceitDown,
		errors.New("database is locked"):                 ru.Failure,
	} {
		b, api, jobs := newDemoBot(t)
		jobs.err = err
		process(b, privateMessage(testDemoLink(testNow), "ru"))
		if got := api.sentTexts(); !slices.Equal(got, []string{want}) {
			t.Errorf("Submit() error %v: sent %q, want %q", err, got, want)
		}
	}
}

func TestDemoStatusMessage(t *testing.T) {
	ru := i18n.For(i18n.RU)
	b, api, jobs := newDemoBot(t)
	ctx := context.Background()
	const id = 7
	jobs.outcome = analysis.Outcome{Verdict: analysis.Accepted, Job: storage.Job{ID: id}, Position: 2, Wait: 90 * time.Second}
	jobs.setView(id, analysis.View{Job: storage.Job{TelegramID: 42, Language: "ru", Status: storage.JobQueued}, Position: 2, Wait: 90 * time.Second})

	process(b, privateMessage(testDemoLink(testNow), "ru"))
	if n := len(api.callsTo("sendMessage")); n != 0 {
		t.Fatalf("the handler sent %d messages, want the status loop to send it", n)
	}
	b.drawStatus(ctx)
	calls := api.callsTo("sendMessage")
	queued := fmt.Sprintf(ru.DemoQueued, 2, fmt.Sprintf(ru.DemoInMinutes, 2))
	if len(calls) != 1 || calls[0].params["text"] != queued || calls[0].params["chat_id"] != "42" {
		t.Fatalf("sent %v, want %q", calls, queued)
	}
	messageID := jobs.messages[id]
	if messageID == 0 {
		t.Fatal("the status message is not saved with the job")
	}

	// The same state again: nothing is sent.
	b.status.JobsChanged(id)
	b.drawStatus(ctx)
	if n := len(api.callsTo("editMessageText")); n != 0 {
		t.Errorf("%d edits of an unchanged status, want none", n)
	}

	steps := []struct {
		view  analysis.View
		want  string
		howTo bool
	}{
		{analysis.View{Job: storage.Job{TelegramID: 42, Language: "ru", Status: storage.JobQueued}, Position: 1, Wait: 20 * time.Second},
			fmt.Sprintf(ru.DemoQueued, 1, ru.DemoSoon), false},
		{analysis.View{Job: storage.Job{TelegramID: 42, Language: "ru", Status: storage.JobRunning}}, ru.DemoRunning, false},
		{analysis.View{Job: storage.Job{TelegramID: 42, Language: "ru", Status: storage.JobDone}, HasResult: true,
			Result: storage.MatchResult{Map: "de_ancient", Score: [2]int{13, 6}, Team: 0}},
			fmt.Sprintf(ru.DemoDone, "Ancient", 13, 6), false},
	}
	for i, s := range steps {
		jobs.setView(id, s.view)
		b.status.JobsChanged(id)
		b.drawStatus(ctx)
		edits := api.callsTo("editMessageText")
		if len(edits) != i+1 {
			t.Fatalf("step %d: %d edits, want %d", i, len(edits), i+1)
		}
		e := edits[i].params
		if e["text"] != s.want || e["message_id"] != fmt.Sprint(messageID) || e["chat_id"] != "42" {
			t.Errorf("step %d: edit %v, want %q of message %d", i, e, s.want, messageID)
		}
	}
	if n := len(api.callsTo("sendMessage")); n != 1 {
		t.Errorf("%d messages sent, want the one status message", n)
	}
}

func TestDemoFailureTexts(t *testing.T) {
	ru, en := i18n.For(i18n.RU), i18n.For(i18n.EN)
	tests := []struct {
		reason, lang, want string
		howTo              bool
	}{
		{analysis.ReasonLinkExpired, "ru", ru.DemoFailExpired, true},
		{analysis.ReasonLinkRejected, "ru", ru.DemoFailRejected, true},
		{analysis.ReasonDownload, "ru", ru.DemoFailDownload, true},
		{analysis.ReasonDemoGone, "ru", ru.DemoFailGone, false},
		{analysis.ReasonTooLarge, "ru", ru.DemoFailTooLarge, false},
		{analysis.ReasonBadDemo, "ru", ru.DemoFailBroken, false},
		{analysis.ReasonFaceit, "ru", ru.DemoFailFaceit, false},
		{analysis.ReasonNotLinked, "ru", ru.DemoFailNotLinked, false},
		{analysis.ReasonNotInMatch, "ru", ru.DemoFailNotYours, false},
		{analysis.ReasonTimeout, "ru", ru.DemoFailed, false},
		{analysis.ReasonInterrupted, "ru", ru.DemoFailed, false},
		{analysis.ReasonInternal, "en", en.DemoFailed, false},
	}
	for _, tt := range tests {
		b, api, jobs := newDemoBot(t)
		jobs.setView(1, analysis.View{Job: storage.Job{TelegramID: 42, Language: tt.lang, Status: storage.JobFailed, Reason: tt.reason}})
		b.status.JobsChanged(1)
		b.drawStatus(context.Background())
		calls := api.callsTo("sendMessage")
		if len(calls) != 1 || calls[0].params["text"] != tt.want || hasHowTo(calls[0]) != tt.howTo {
			t.Errorf("%s: sent %v, want %q, button %v", tt.reason, calls, tt.want, tt.howTo)
		}
	}
}

func TestDemoStatusMessageDeleted(t *testing.T) {
	b, api, jobs := newDemoBot(t)
	jobs.setView(1, analysis.View{Job: storage.Job{TelegramID: 42, Language: "en", Status: storage.JobRunning}})
	jobs.messages[1] = 5
	api.failEdits("Bad Request: message to edit not found")
	b.status.JobsChanged(1)
	b.drawStatus(context.Background())
	calls := api.callsTo("sendMessage")
	if len(calls) != 1 || calls[0].params["text"] != i18n.For(i18n.EN).DemoRunning {
		t.Fatalf("sent %v, want the status as a new message", calls)
	}
	if jobs.messages[1] == 5 {
		t.Error("the new status message is not saved with the job")
	}
}

func TestDemoHowToButton(t *testing.T) {
	b, api, _ := newDemoBot(t)
	process(b, buttonPress(demoButtonHowTo, "en"))
	if got := api.sentTexts(); !slices.Equal(got, []string{i18n.For(i18n.EN).DemoHowTo}) {
		t.Errorf("sent %q, want the instructions", got)
	}
	if n := len(api.callsTo("answerCallbackQuery")); n != 1 {
		t.Errorf("answerCallbackQuery called %d times, want 1", n)
	}
}

func TestHelpForAdmins(t *testing.T) {
	ru := i18n.For(i18n.RU)
	b, api, _ := newDemoBot(t)
	process(b, privateMessage("/help", "ru"))
	calls := api.callsTo("sendMessage")
	if len(calls) != 1 || calls[0].params["text"] != ru.Help+"\n\n"+ru.HelpDemo || !hasHowTo(calls[0]) {
		t.Errorf("admin /help sent %v, want the help with the demo part and the button", calls)
	}

	b.admins = map[int64]bool{}
	process(b, privateMessage("/help", "ru"))
	calls = api.callsTo("sendMessage")
	if len(calls) != 2 || calls[1].params["text"] != ru.Help || hasHowTo(calls[1]) {
		t.Errorf("user /help sent %v, want the help as it was", calls[1:])
	}
}

// matchesStub stands in for FACEIT in the queue: player 1 of the match is
// the account of user 42.
type matchesStub struct{}

func (matchesStub) Match(_ context.Context, id string) (faceit.Match, error) {
	return faceit.Match{ID: id, Status: "FINISHED", BestOf: 1, Map: "de_mirage", Teams: []faceit.Team{
		{Faction: "faction1", Players: []faceit.MatchPlayer{{ID: playerKolyx.ID, SteamID64: playerKolyx.SteamID64, SkillLevel: 8}}},
	}}, nil
}

// TestDemoWithQueue runs a link through the real queue and database: the
// job is added, and its status message is sent and saved with it.
func TestDemoWithQueue(t *testing.T) {
	store, path := newTestStore(t)
	b, api := newTestBot(t, store)
	b.jobs = analysis.New(store, matchesStub{}, slog.New(slog.DiscardHandler))
	b.admins[42] = true
	ctx := context.Background()
	err := store.LinkFaceitAccount(ctx, storage.User{TelegramID: 42, Language: "ru"},
		storage.FaceitAccount{PlayerID: playerKolyx.ID, Nickname: playerKolyx.Nickname, SteamID64: playerKolyx.SteamID64}, testNow)
	if err != nil {
		t.Fatal(err)
	}

	process(b, privateMessage(testDemoLink(time.Now()), "ru"))
	b.drawStatus(ctx)
	ru := i18n.For(i18n.RU)
	want := fmt.Sprintf(ru.DemoQueued, 1, ru.DemoSoon)
	if got := api.sentTexts(); !slices.Equal(got, []string{want}) {
		t.Fatalf("sent %q, want %q", got, want)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	var messageID int
	if err := db.QueryRow(`SELECT status, status_message_id FROM jobs`).Scan(&status, &messageID); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || messageID == 0 {
		t.Errorf("job %s with status message %d, want queued with the message saved", status, messageID)
	}

	// The same link again.
	process(b, privateMessage(testDemoLink(time.Now()), "ru"))
	if got := api.sentTexts(); len(got) != 2 || got[1] != ru.DemoDuplicate {
		t.Errorf("sent %q, want %q second", got, ru.DemoDuplicate)
	}
}
