package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// The comparison with Scope.gg. Scope.gg publishes no formulas: the
// metrics that match its numbers were found by trying formulas on three
// matches, see docs/notes/scope-gg.md. Its numbers are copied by hand from
// its match pages into a JSON file:
//
//	{"<demo file name>": {"<nickname as in the demo>": {
//	    "accuracy": 58, "first_shot": 50, "hs_rifle": 17, "hs_pistol": 25, "kill_time": 0.95}}}
//
// A value Scope.gg shows as N/A is null or left out. The file holds
// nicknames: keep it out of the repository, in bot/testdata for one.
//
// Scope.gg rounds its percentages down, so ours are compared rounded down.
// Its time to kill is not ours (docs/METRICS.md) and is shown for reference.

// scopeRow is the numbers Scope.gg shows for a player in a match.
type scopeRow struct {
	Accuracy  *float64 `json:"accuracy"`   // Точность, %
	FirstShot *float64 `json:"first_shot"` // Точность первой пули, %
	HSRifle   *float64 `json:"hs_rifle"`   // HS, %
	HSPistol  *float64 `json:"hs_pistol"`  // HS, % (2)
	KillTime  *float64 `json:"kill_time"`  // Время убийства, s
}

// scopeFile is the numbers of Scope.gg by demo file name, then nickname.
type scopeFile map[string]map[string]scopeRow

func loadScope(path string) (scopeFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scope.gg: %w", err)
	}
	var f scopeFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("scope.gg: decode %s: %w", filepath.Base(path), err)
	}
	return f, nil
}

// scopeMetric is a metric compared with Scope.gg.
type scopeMetric struct {
	name    string
	percent bool // a percentage Scope.gg rounds down; else seconds
	theirs  func(scopeRow) *float64
	// ours returns our value and whether there was anything to count.
	ours func(metrics.Stats) (float64, bool)
}

var scopeMetrics = []scopeMetric{
	{"accuracy", true, func(r scopeRow) *float64 { return r.Accuracy },
		func(s metrics.Stats) (float64, bool) { return 100 * s.Accuracy, s.AccuracyShots > 0 }},
	{"first shot", true, func(r scopeRow) *float64 { return r.FirstShot },
		func(s metrics.Stats) (float64, bool) { return 100 * s.FirstShotAccuracy, s.FirstShotFights > 0 }},
	{"HS rifles", true, func(r scopeRow) *float64 { return r.HSRifle },
		func(s metrics.Stats) (float64, bool) { return 100 * s.RifleHeadshotRate, s.RifleHits > 0 }},
	{"HS pistols", true, func(r scopeRow) *float64 { return r.HSPistol },
		func(s metrics.Stats) (float64, bool) { return 100 * s.PistolHeadshotRate, s.PistolHits > 0 }},
	{"kill time", false, func(r scopeRow) *float64 { return r.KillTime },
		func(s metrics.Stats) (float64, bool) { return s.RifleKillTime, s.RifleKillDuels > 0 }},
}

// scopePair is our value of a metric and Scope.gg's for a player.
type scopePair struct{ ours, theirs float64 }

// compareScope compares the metrics of a demo with the numbers of Scope.gg.
// It returns the report lines and the pairs of values, by metric name.
func compareScope(f scopeFile, path string, m *demo.Match, players []metrics.Player) ([]string, map[string][]scopePair) {
	rows, ok := f[filepath.Base(path)]
	if !ok {
		return []string{"  scope.gg: no numbers for this demo"}, nil
	}
	byName := make(map[string]metrics.Player)
	for i, p := range m.Players {
		byName[p.Name] = players[i]
	}
	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}
	slices.Sort(names)

	lines := []string{"  scope.gg, ours/theirs:"}
	pairs := make(map[string][]scopePair)
	for _, name := range names {
		p, ok := byName[name]
		if !ok {
			lines = append(lines, fmt.Sprintf("    %-16s not in the demo", name))
			continue
		}
		row := rows[name]
		var parts []string
		for _, sm := range scopeMetrics {
			v, counted := sm.ours(p.Match)
			theirs := sm.theirs(row)
			parts = append(parts, sm.name+" "+scopeShow(sm, v, counted, theirs))
			if counted && theirs != nil {
				pairs[sm.name] = append(pairs[sm.name], scopePair{v, *theirs})
			}
		}
		parts[len(parts)-1] += fmt.Sprintf(" (%d duels)", p.Match.RifleKillDuels)
		lines = append(lines, fmt.Sprintf("    %-16s %s", name, strings.Join(parts, "  ")))
	}
	return lines, pairs
}

// scopeShow shows our value and Scope.gg's, with * if a percentage
// differs once ours is rounded down.
func scopeShow(sm scopeMetric, v float64, counted bool, theirs *float64) string {
	ours, their := "-", "-"
	if counted {
		ours = scopeFormat(sm, v)
	}
	if theirs != nil {
		their = scopeFormat(sm, *theirs)
	}
	s := ours + "/" + their
	if sm.percent && counted && theirs != nil && scopeFloor(v) != *theirs {
		s += "*"
	}
	return s
}

func scopeFormat(sm scopeMetric, v float64) string {
	if sm.percent {
		return fmt.Sprintf("%.0f", scopeFloor(v))
	}
	return fmt.Sprintf("%.2f", v)
}

// scopeFloor rounds a percentage down as Scope.gg does; the epsilon keeps
// 58 from becoming 57 when 0.58 * 100 comes out a hair below 58.
func scopeFloor(v float64) float64 { return math.Floor(v + 1e-9) }

// scopeSummary sums up the pairs of every demo: for each metric how many
// players, the correlation, the mean absolute difference and, for a
// percentage, how many match once ours is rounded down.
func scopeSummary(pairs map[string][]scopePair) []string {
	lines := []string{"scope.gg, all demos:"}
	for _, sm := range scopeMetrics {
		ps := pairs[sm.name]
		if len(ps) == 0 {
			continue
		}
		var diff float64
		exact := 0
		xs, ys := make([]float64, len(ps)), make([]float64, len(ps))
		for i, p := range ps {
			ours := p.ours
			if sm.percent {
				ours = scopeFloor(ours)
				if ours == p.theirs {
					exact++
				}
			}
			diff += math.Abs(ours - p.theirs)
			xs[i], ys[i] = p.ours, p.theirs
		}
		line := fmt.Sprintf("  %-10s %2d players  r %5.2f  mean difference %.2f", sm.name, len(ps), pearson(xs, ys), diff/float64(len(ps)))
		if sm.percent {
			line += fmt.Sprintf("  equal %d", exact)
		}
		lines = append(lines, line)
	}
	return lines
}

// pearson returns the correlation of xs and ys, NaN when either does not
// vary.
func pearson(xs, ys []float64) float64 {
	n := float64(len(xs))
	var mx, my float64
	for i := range xs {
		mx += xs[i] / n
		my += ys[i] / n
	}
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return math.NaN()
	}
	return sxy / math.Sqrt(sxx*syy)
}
