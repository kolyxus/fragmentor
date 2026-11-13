// Command parsedemo parses FACEIT demos, .dem.zst files, and prints the
// summary of each as JSON: the map, the rounds with the score, every
// player's kills, deaths, assists, headshots and damage, and the metrics of
// internal/metrics. It reports how long each demo took and the peak memory.
// With -faceit it compares the score and K/D/A with the match stats of the
// FACEIT Data API; with -csda, the metrics with those of cs-demo-analyzer;
// with -scope, the shooting metrics with the numbers of Scope.gg. With
// -findings it prints, in place of the JSON, the findings of
// internal/findings for every player of every demo and a summary of the
// rules, see findings.go.
// It exits with status 1 if a demo fails, its summary is suspicious (see
// demo.Match) or anything differs from FACEIT.
//
// Run from bot/:
//
//	go run ./cmd/parsedemo [-faceit] [-findings] [-csda folder] [-scope file.json] [-events] [-timeout 5m] [-parallel 1] <file.dem.zst | folder>...
//
// A folder stands for every *.zst in it. For -faceit the match ID comes
// from the file name, <match_id>-1-1.dem.zst, as FACEIT names its demos, and
// FACEIT_API_KEY comes from the environment or .env, as for the bot. For
// -csda the folder holds the JSON of cs-demo-analyzer for each demo, see
// csda.go; for -scope the file holds the numbers of Scope.gg, see scope.go.
// -events adds the events of the match to the JSON: kills, hits, shots,
// sightings, flashes and who was alive in each round.
//
// The summary holds nicknames and SteamIDs: keep its output out of the
// repository.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/config"
	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// faceitTimeout bounds the two requests to FACEIT for one demo.
const faceitTimeout = 30 * time.Second

func main() {
	withFACEIT := flag.Bool("faceit", false, "compare the score and K/D/A with the FACEIT match stats")
	timeout := flag.Duration("timeout", 5*time.Minute, "time limit for one demo")
	maxSize := flag.Int64("max-size", demo.DefaultMaxSize>>20, "limit of a decompressed demo, MiB")
	parallel := flag.Int("parallel", 1, "how many demos to parse at once")
	printJSON := flag.Bool("json", true, "print the summary as JSON")
	printEvents := flag.Bool("events", false, "print the events of the match and who was alive in each round in the JSON too")
	csdaDir := flag.String("csda", "", "folder with the JSON of cs-demo-analyzer to compare the metrics with")
	scopePath := flag.String("scope", "", "JSON file with the numbers of Scope.gg to compare the shooting metrics with")
	withFindings := flag.Bool("findings", false, "print the findings of every player and a summary of the rules in place of the JSON")
	envPath := flag.String("env", ".env", "file with FACEIT_API_KEY, for -faceit")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: parsedemo [flags] <file.dem.zst | folder>...")
		flag.PrintDefaults()
	}
	flag.Parse()

	files, err := demoFiles(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "parsedemo:", err)
		os.Exit(2)
	}
	if len(files) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	r := &runner{
		opts:        demo.Options{MaxSize: *maxSize << 20},
		timeout:     *timeout,
		printJSON:   *printJSON && !*withFindings,
		printEvents: *printEvents,
		findings:    *withFindings,
		csdaDir:     *csdaDir,
		out:         os.Stdout,
		log:         os.Stderr,
		mem:         startSampler(),
	}
	if *scopePath != "" {
		if r.scope, err = loadScope(*scopePath); err != nil {
			fmt.Fprintln(os.Stderr, "parsedemo:", err)
			os.Exit(2)
		}
		r.scopePairs = make(map[string][]scopePair)
	}
	if *withFACEIT {
		cfg, err := config.Load(*envPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parsedemo: load config:", err)
			os.Exit(2)
		}
		r.faceit = faceit.New(cfg.FaceitAPIKey)
	}
	if !r.run(files, max(*parallel, 1)) {
		os.Exit(1)
	}
}

// demoFiles expands the arguments: a folder stands for its *.zst files, the
// demos FACEIT names *.dem.zst among them.
func demoFiles(args []string) ([]string, error) {
	var files []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, arg)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(arg, "*.zst"))
		if err != nil {
			return nil, err
		}
		slices.Sort(matches)
		files = append(files, matches...)
	}
	return files, nil
}

// matchFileRe is the name FACEIT gives a demo: the match ID, then the map
// and the part.
var matchFileRe = regexp.MustCompile(`^(1-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})-\d+-\d+\.dem\.zst$`)

// matchID returns the FACEIT match ID from the name of a demo file.
func matchID(path string) (string, bool) {
	m := matchFileRe.FindStringSubmatch(strings.ToLower(filepath.Base(path)))
	if m == nil || !faceit.ValidMatchID(m[1]) {
		return "", false
	}
	return m[1], true
}

type runner struct {
	opts        demo.Options
	timeout     time.Duration
	printJSON   bool
	printEvents bool
	findings    bool           // -findings
	faceit      *faceit.Client // nil without -faceit
	csdaDir     string         // "" without -csda
	scope       scopeFile      // nil without -scope
	out, log    io.Writer
	mem         *sampler
	mu          sync.Mutex // one demo writes at a time
	// scopePairs collects the values of every demo for the summary of
	// -scope, under mu.
	scopePairs map[string][]scopePair
	// analyzed collects the demos for -findings, by the index of the file,
	// under mu: the findings need them all, as the past of a player is his
	// other demos of the run.
	analyzed map[int]analyzed
}

// run parses the files, n at a time, and reports whether all went well.
func (r *runner) run(files []string, n int) bool {
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, n)
		ok  = true
		okm sync.Mutex
	)
	for i, f := range files {
		if n == 1 {
			// One at a time, so the peak memory of each demo is its own.
			r.mem.reset()
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			if !r.one(i, f, n == 1) {
				okm.Lock()
				ok = false
				okm.Unlock()
			}
		})
		if n == 1 {
			wg.Wait()
		}
	}
	wg.Wait()
	if r.scope != nil {
		for _, l := range scopeSummary(r.scopePairs) {
			fmt.Fprintln(r.log, l)
		}
	}
	if r.findings {
		// The demos that parsed, in the order of the files.
		var demos []analyzed
		for i := range files {
			if d, ok := r.analyzed[i]; ok {
				demos = append(demos, d)
			}
		}
		findingsReport(r.out, demos)
	}
	fmt.Fprintf(r.log, "peak memory of the process: %s\n", r.mem.peak())
	return ok
}

// one parses the demo of index i among the files and prints its summary and
// report. ownPeak means no other demo was parsed at the same time.
func (r *runner) one(i int, path string, ownPeak bool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	start := time.Now()
	m, err := demo.ParseFile(ctx, path, r.opts)
	elapsed := time.Since(start)
	var (
		players  []metrics.Player
		analysis *metrics.Analysis
	)
	if err == nil {
		analysis = metrics.Analyze(m)
		players = analysis.Players
	}

	var report []string
	mismatches := 0
	if err == nil && r.faceit != nil {
		report, mismatches, err = r.compare(path, m, players)
	}
	if err == nil && r.csdaDir != "" {
		var lines []string
		lines, err = compareCSDA(r.csdaDir, path, m, players)
		report = append(report, lines...)
	}
	var scopePairs map[string][]scopePair
	if err == nil && r.scope != nil {
		var lines []string
		lines, scopePairs = compareScope(r.scope, path, m, players)
		report = append(report, lines...)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for name, ps := range scopePairs {
		r.scopePairs[name] = append(r.scopePairs[name], ps...)
	}
	name := filepath.Base(path)
	if err != nil {
		fmt.Fprintf(r.log, "%s: %v\n", name, err)
		if pe, ok := errors.AsType[*demo.PanicError](err); ok {
			fmt.Fprintf(r.log, "%s\n", pe.Stack)
		}
		return false
	}
	if r.findings {
		if r.analyzed == nil {
			r.analyzed = make(map[int]analyzed)
		}
		r.analyzed[i] = keep(path, m, analysis)
	}
	if r.printJSON {
		out := m
		if !r.printEvents {
			out = withoutEvents(m)
		}
		b, err := json.MarshalIndent(struct {
			File string `json:"file"`
			*demo.Match
			Metrics []metrics.Player `json:"metrics"`
		}{name, out, players}, "", "  ")
		if err != nil {
			fmt.Fprintf(r.log, "%s: encode: %v\n", name, err)
			return false
		}
		fmt.Fprintf(r.out, "%s\n", b)
	}
	line := fmt.Sprintf("%s: %s, %d rounds, %d:%d, parsed in %.1f s", name, m.Map, len(m.Rounds), m.Teams[0].Score, m.Teams[1].Score, elapsed.Seconds())
	if ownPeak {
		line += ", peak memory " + r.mem.peak()
	}
	fmt.Fprintln(r.log, line)
	if m.Suspicious {
		fmt.Fprintln(r.log, "  SUSPICIOUS: the summary may be wrong")
	}
	for _, w := range m.Warnings {
		fmt.Fprintf(r.log, "  warning: %s\n", w)
	}
	for _, l := range report {
		fmt.Fprintln(r.log, l)
	}
	return mismatches == 0 && !m.Suspicious
}

// withoutEvents returns a copy of m without its events and who was alive in
// each round, which make the JSON long.
func withoutEvents(m *demo.Match) *demo.Match {
	out := *m
	out.Kills, out.Hits, out.Shots, out.Sightings, out.Flashes, out.Grenades = nil, nil, nil, nil, nil, nil
	out.Rounds = slices.Clone(m.Rounds)
	for i := range out.Rounds {
		out.Rounds[i].Alive, out.Rounds[i].Equipment = nil, nil
	}
	return &out
}

// compare fetches the match from FACEIT and compares it with the demo.
func (r *runner) compare(path string, m *demo.Match, players []metrics.Player) ([]string, int, error) {
	id, ok := matchID(path)
	if !ok {
		return nil, 0, errors.New("no FACEIT match ID in the file name, expected <match_id>-1-1.dem.zst")
	}
	ctx, cancel := context.WithTimeout(context.Background(), faceitTimeout)
	defer cancel()
	details, err := r.faceit.Match(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	stats, err := r.faceit.MatchStats(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	report, mismatches := compare(m, players, details, stats)
	return report, mismatches, nil
}
