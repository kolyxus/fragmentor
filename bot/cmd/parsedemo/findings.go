package main

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// The findings of docs/FINDINGS.md, with -findings: for every player of
// every demo what he is compared with, the rules that fired with their
// weights and the moments that show them, and what a review would tell
// about; in the end how often each rule fired over all the demos and what
// its findings weigh. It is how the rules and their thresholds are checked
// by eye, and how docs/FINDINGS.md, "Проверка правил", is filled.
//
// The past of a player, which makes a finding a habit, is his other demos
// of the run, whenever they were played: a demo does not tell its date.
// The bot takes the matches played before.

// analyzed is what the findings keep of a parsed demo.
type analyzed struct {
	file   string
	mapID  string
	rounds int
	score  [2]int
	teams  [2]string
	names  map[uint64]string
	a      *metrics.Analysis
}

func keep(path string, m *demo.Match, a *metrics.Analysis) analyzed {
	d := analyzed{
		file: filepath.Base(path), mapID: m.Map, rounds: len(m.Rounds),
		score: [2]int{m.Teams[0].Score, m.Teams[1].Score}, teams: [2]string{m.Teams[0].Name, m.Teams[1].Name},
		names: make(map[uint64]string, len(m.Players)), a: a,
	}
	for _, p := range m.Players {
		d.names[p.SteamID64] = p.Name
	}
	return d
}

// withPast returns the reports of the players of every demo, each with his
// other demos of the run as his past, findings.MaxHistory of them at most.
func withPast(demos []analyzed) [][]findings.Report {
	alone := make([][]findings.Report, len(demos))
	for i, d := range demos {
		alone[i] = findings.Analyze(d.a, nil)
	}
	out := make([][]findings.Report, len(demos))
	for i, d := range demos {
		past := make(map[uint64]findings.History)
		for j, other := range demos {
			if j == i {
				continue
			}
			for k, p := range other.a.Players {
				h := past[p.SteamID64]
				if h.Matches < findings.MaxHistory {
					h.Add(alone[j][k])
					past[p.SteamID64] = h
				}
			}
		}
		out[i] = findings.Analyze(d.a, past)
	}
	return out
}

// findingsReport writes the findings of every player of the demos and the
// summary of the rules.
func findingsReport(w io.Writer, demos []analyzed) {
	reports := withPast(demos)
	for i, d := range demos {
		fmt.Fprintf(w, "=== %s: %s, %d rounds, %d:%d\n", d.file, d.mapID, d.rounds, d.score[0], d.score[1])
		for k, p := range d.a.Players {
			writePlayer(w, d, p, reports[i][k])
		}
		fmt.Fprintln(w)
	}
	writeSummary(w, reports)
}

// writePlayer writes the report of a player: who he is, his measures next
// to the lobby, the findings and what is picked of them.
func writePlayer(w io.Writer, d analyzed, p metrics.Player, r findings.Report) {
	st := p.Match
	fmt.Fprintf(w, "\n--- %s, %s: K/D/A %d/%d/%d, ADR %.1f, KAST %.0f%%\n", d.names[p.SteamID64], d.teams[p.Team],
		st.Kills, st.Deaths, st.Assists, st.ADR, 100*st.KAST)
	// What tells a sniper, an entry and a support apart, to pick the players
	// to read by role.
	var awp, flashAssists int
	for _, dt := range d.a.Deaths {
		if dt.Killer == p.SteamID64 && dt.Weapon == "AWP" {
			awp++
		}
		if dt.Assister == p.SteamID64 && dt.FlashAssist {
			flashAssists++
		}
	}
	fmt.Fprintf(w, "    role: AWP kills %d of %d, opening duels %d, flashbangs %d, flash assists %d\n",
		awp, st.Kills, st.OpeningAttempts, st.FlashesThrown, flashAssists)

	fmt.Fprintf(w, "    %-16s %-18s %-9s %-9s %-9s %-6s %s\n", "measure", "player", "lobby", "team", "opponents", "place", "own")
	for _, c := range r.Comparisons {
		own := "-"
		if c.Own != nil {
			own = fmt.Sprintf("%s (%s of %s)", rate(c.Measure, c.Own.Rate), number(c.Own.X), number(c.Own.N))
		}
		place := "-"
		if c.Rank > 0 {
			place = fmt.Sprintf("%d/%d", c.Rank, c.Of)
		}
		fmt.Fprintf(w, "    %-16s %-18s %-9s %-9s %-9s %-6s %s\n", c.Measure,
			fmt.Sprintf("%s of %s  %s", number(c.X), number(c.N), rate(c.Measure, c.Rate)),
			rate(c.Measure, c.Lobby.Rate), rate(c.Measure, c.Teammates.Rate), rate(c.Measure, c.Opponents.Rate), place, own)
	}

	if len(r.Findings) == 0 {
		fmt.Fprintln(w, "    findings: none")
		return
	}
	fmt.Fprintln(w, "    findings:")
	for _, f := range r.Findings {
		fmt.Fprintf(w, "    %-8s %s [%s]%s: %s of %s, the lobby's %s gives %.1f: +%.1f, z %.2f, impact %.2f%s, weight %.2f\n",
			f.Kind, f.Rule, f.Group, variant(f), number(f.X), number(f.N), rate(measureOf(f), f.BaseRate), f.Expected, f.Excess,
			f.Z, f.Impact, habit(f), f.Weight)
		for _, e := range f.Evidence {
			fmt.Fprintf(w, "        %s\n", moment(f, e))
		}
	}
	fmt.Fprintln(w, "    review:")
	for n, pk := range r.Mistakes {
		f := r.Findings[pk.Finding]
		line := fmt.Sprintf("      mistake %d: %s, weight %.2f", n+1, f.Rule, pk.Weight)
		for _, j := range pk.With {
			g := r.Findings[j]
			line += fmt.Sprintf("; with %s %s of %s", g.Rule, number(g.X), number(g.N))
		}
		fmt.Fprintln(w, line)
	}
	if len(r.Mistakes) == 0 {
		fmt.Fprintln(w, "      mistakes: none")
	}
	if r.Strength != nil {
		fmt.Fprintf(w, "      strength: %s, weight %.2f\n", r.Findings[r.Strength.Finding].Rule, r.Strength.Weight)
	} else {
		fmt.Fprintln(w, "      strength: none")
	}
}

// measureOf returns the measure the finding fired on.
func measureOf(f findings.Finding) string {
	switch f.Variant {
	case findings.VariantFlashes:
		return findings.MeasureEnemyFlashes
	case findings.VariantDamage:
		return findings.MeasureGrenadeDamage
	}
	return string(f.Rule)
}

func variant(f findings.Finding) string {
	s := ""
	if f.Variant != "" {
		s += " " + f.Variant
	}
	if f.Place != "" {
		s += fmt.Sprintf(" %s as %s, %s deaths there", f.Place, f.Side, number(f.Sample))
	}
	return s
}

func habit(f findings.Finding) string {
	if f.Habit {
		return ", a habit"
	}
	return ""
}

// rate shows the rate of a measure in its units: a count a round or a
// match, a ratio, damage a round, or a share in percent.
func rate(measure string, v float64) string {
	switch measure {
	case string(findings.MultiKills), string(findings.Clutches), findings.MeasureEnemyFlashes, findings.MeasureGrenadeDamage:
		return fmt.Sprintf("%.2f/r", v)
	case string(findings.DeathSpot), findings.MeasureKD:
		return fmt.Sprintf("%.2f", v)
	case findings.MeasureADR:
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.0f%%", 100*v)
}

// number shows a count, which is whole unless it is grenade damage in
// units.
func number(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.1f", v)
}

// clockText shows the round clock as the game does: whole seconds, rounded
// up, and the timer of the bomb once it is planted.
func clockText(clock float64, planted bool) string {
	s := findings.ClockSeconds(clock)
	text := fmt.Sprintf("%d:%02d", s/60, s%60)
	if planted {
		return "bomb " + text
	}
	return text
}

// moment shows a moment of a finding: when, where, what, and the command
// that takes the demo there.
func moment(f findings.Finding, e findings.Evidence) string {
	parts := []string{fmt.Sprintf("round %d", e.Round), clockText(e.Clock, e.Planted)}
	if e.Place != "" || e.EnemyPlace != "" {
		parts = append(parts, fmt.Sprintf("%s <- %s", cmpOr(e.Place, "?"), cmpOr(e.EnemyPlace, "?")))
	}
	if e.TeammateMeters > 0 {
		parts = append(parts, fmt.Sprintf("teammate %.0f m", e.TeammateMeters))
	}
	if e.Alive > 0 {
		parts = append(parts, fmt.Sprintf("%dv%d", e.Alive, e.AliveEnemy))
	}
	if e.Weapon != "" {
		parts = append(parts, e.Weapon)
	}
	switch {
	case e.Of > 0:
		parts = append(parts, fmt.Sprintf("%d of %d", e.Count, e.Of))
	case e.Count > 0:
		parts = append(parts, fmt.Sprintf("x%d", e.Count))
	}
	if f.Rule == findings.BadFlashes || e.Enemies > 0 || e.Teammates > 0 {
		parts = append(parts, fmt.Sprintf("enemies %d, teammates %d", e.Enemies, e.Teammates))
	}
	if e.GrenadeValue > 0 {
		parts = append(parts, fmt.Sprintf("$%d left: %s", e.GrenadeValue, strings.Join(e.Grenades, ", ")))
	}
	if e.Detail != "" {
		parts = append(parts, e.Detail)
	}
	result := "lost"
	if e.Won {
		result = "won"
	}
	parts = append(parts, result)
	if e.Buy != "" {
		parts = append(parts, string(e.Buy))
	}
	if f.Kind == findings.Mistake {
		parts = append(parts, fmt.Sprintf("impact %.2f", e.Impact))
	}
	if e.Tick == 0 {
		return strings.Join(parts, ", ")
	}
	return strings.Join(parts, ", ") + fmt.Sprintf(": demo_gototick %d", e.Goto)
}

func cmpOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

// writeSummary writes how often each rule fired over all the players of the
// demos, how often it was picked for the review and what its findings
// weigh, and how many mistakes the players got.
func writeSummary(w io.Writer, reports [][]findings.Report) {
	type count struct {
		fired, picked, first, habits int
		weights                      []float64
	}
	by := make(map[findings.RuleID]*count)
	for _, rl := range findings.Rules() {
		by[rl.ID] = &count{}
	}
	var total, withStrength int
	var mistakes [findings.MaxMistakes + 1]int
	for _, rs := range reports {
		for _, r := range rs {
			total++
			for _, f := range r.Findings {
				c := by[f.Rule]
				c.fired++
				c.weights = append(c.weights, f.Weight)
				if f.Habit {
					c.habits++
				}
			}
			mistakes[min(len(r.Mistakes), findings.MaxMistakes)]++
			for n, pk := range r.Mistakes {
				c := by[r.Findings[pk.Finding].Rule]
				c.picked++
				if n == 0 {
					c.first++
				}
			}
			if r.Strength != nil {
				withStrength++
				by[r.Findings[r.Strength.Finding].Rule].picked++
			}
		}
	}
	fmt.Fprintf(w, "=== the rules over %d players of %d demos\n", total, len(reports))
	fmt.Fprintf(w, "%-16s %-8s %-8s %5s %6s %5s %6s   %s\n", "rule", "kind", "group", "fired", "picked", "first", "habits", "weight: min, median, max")
	for _, rl := range findings.Rules() {
		c := by[rl.ID]
		weights := "-"
		if len(c.weights) > 0 {
			slices.Sort(c.weights)
			weights = fmt.Sprintf("%.2f, %.2f, %.2f", c.weights[0], c.weights[len(c.weights)/2], c.weights[len(c.weights)-1])
		}
		fmt.Fprintf(w, "%-16s %-8s %-8s %5d %6d %5d %6d   %s\n", rl.ID, rl.Kind, rl.Group, c.fired, c.picked, c.first, c.habits, weights)
	}
	fmt.Fprint(w, "mistakes picked:")
	for n, players := range mistakes {
		fmt.Fprintf(w, " %d for %d players,", n, players)
	}
	fmt.Fprintf(w, " a strength for %d\n", withStrength)
}
