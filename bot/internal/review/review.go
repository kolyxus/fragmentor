// Package review makes the template review of a match out of the facts of a
// player, findings.Facts: the head with the score and the scoreboard, up to
// three mistakes with their numbers and moments, the strength, and the task
// of the week with its drill of bot/content. It is plain text in Russian or
// English, the texts in internal/i18n; HTML for Telegram comes with step 9.
// It is the review the bot gives until the text of step 8в, and the one it
// falls back on after.
//
// A review tells only what the facts hold: every number of it is there.
package review

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
)

// MaxMoments is how many moments of a finding a review shows, the heaviest:
// the facts keep up to findings.MaxEvidence.
const MaxMoments = 3

// Render returns the template review of the facts in lang, with the drills
// and the names of the places of c.
func Render(f findings.Facts, lang i18n.Lang, c *content.Catalog) string {
	r := renderer{f: f, lang: lang, t: i18n.For(lang).Review, c: c, byID: make(map[string]findings.FactFinding, len(f.Findings))}
	for _, ff := range f.Findings {
		r.byID[ff.ID] = ff
	}
	var b strings.Builder
	results := map[string]string{findings.ResultWin: r.t.Win, findings.ResultLoss: r.t.Loss, findings.ResultDraw: r.t.Draw}
	fmt.Fprintf(&b, r.t.Header, c.MapName(f.Match.Map, lang), f.Match.Score[0], f.Match.Score[1], results[f.Match.Result])
	b.WriteString("\n")
	fmt.Fprintf(&b, r.t.Scoreboard, f.Player.Kills, f.Player.Deaths, f.Player.Assists, r.number(f.Player.ADR, 1), r.percent(f.Player.KAST))
	if f.Match.Suspicious {
		b.WriteString("\n\n" + r.t.Suspicious)
	}

	b.WriteString("\n\n")
	if len(f.Review.Mistakes) == 0 {
		b.WriteString(r.t.NoMistakes)
	} else {
		b.WriteString(r.t.Mistakes)
		for i, pk := range f.Review.Mistakes {
			fmt.Fprintf(&b, "\n%d. %s", i+1, r.pick(pk))
			r.writeMoments(&b, pk.Finding)
		}
	}
	if s := f.Review.Strength; s != nil {
		b.WriteString("\n\n")
		fmt.Fprintf(&b, r.t.Strength, r.pick(*s))
		r.writeMoments(&b, s.Finding)
	}
	if task := f.Review.Task; task != nil {
		if d, ok := c.Drill(task.Drill); ok {
			b.WriteString("\n\n")
			fmt.Fprintf(&b, r.t.Task, d.Title.In(lang))
			b.WriteString("\n" + d.Task.In(lang))
			if u := d.URL(f.Match.Map); u != "" {
				b.WriteString("\n" + u)
			}
			if line := r.progress(d); line != "" {
				b.WriteString("\n" + line)
			}
		}
	}
	return b.String()
}

type renderer struct {
	f    findings.Facts
	lang i18n.Lang
	t    i18n.ReviewTexts
	c    *content.Catalog
	byID map[string]findings.FactFinding
}

// pick tells a finding picked for the review: what it is against its base,
// whether it repeats, and the findings nested with it.
func (r renderer) pick(pk findings.PickFacts) string {
	f := r.byID[pk.Finding]
	text := r.title(f) + ": " + r.against(f)
	if f.Habit {
		text += " " + r.t.Habit
	}
	if len(pk.With) > 0 {
		var items []string
		for _, id := range pk.With {
			w := r.byID[id]
			items = append(items, fmt.Sprintf(r.t.WithItem, r.rule(w.Rule).Short, r.count(w)))
		}
		text += " " + fmt.Sprintf(r.t.With, strings.Join(items, ", "))
	}
	return text
}

func (r renderer) rule(id findings.RuleID) i18n.RuleText { return r.t.Rules[string(id)] }

// title names the finding: by its variant, if the rule has a text of it.
func (r renderer) title(f findings.FactFinding) string {
	rt := r.rule(f.Rule)
	if v, ok := rt.Variants[f.Variant]; ok {
		return v
	}
	return rt.Title
}

// scale is what the numbers of a finding are shown in: the grenade damage
// counts in findings.DamageUnit, the review shows the damage itself.
func scale(f findings.FactFinding) float64 {
	if f.Rule == findings.UsefulGrenades && f.Variant == findings.VariantDamage {
		return findings.DamageUnit
	}
	return 1
}

// against tells the finding against its base: the events of the player,
// what the base would give and how many more.
func (r renderer) against(f findings.FactFinding) string {
	k := scale(f)
	switch f.Model {
	case findings.ModelPerRound:
		return fmt.Sprintf(r.t.PerRound, r.number(f.X*k, 1), r.rounds(int(f.N)), r.number(f.BaseRate*k, 2),
			r.number(f.Expected*k, 1), r.number(f.Excess*k, 1))
	case findings.ModelPerMatch:
		return fmt.Sprintf(r.t.Spot, r.place(f.Place), r.t.Sides[string(f.Side)], int(f.X), r.of(f.Rule, int(f.Sample)),
			r.number(f.BaseRate, 1), r.number(f.Excess, 1))
	}
	return fmt.Sprintf(r.t.Share, r.number(f.X, 0), r.of(f.Rule, int(f.N)), r.percent(f.BaseRate),
		r.number(f.Expected, 1), r.number(f.Excess, 1))
}

// count is the events of a finding, as a list of findings shows them.
func (r renderer) count(f findings.FactFinding) string {
	switch f.Model {
	case findings.ModelShare:
		return r.number(f.X, 0) + " " + r.ofWord(r.lang) + " " + r.number(f.N, 0)
	case findings.ModelPerMatch:
		return r.number(f.X, 0)
	}
	return r.number(f.X*scale(f), 1)
}

// ofWord is "из" or "of".
func (r renderer) ofWord(lang i18n.Lang) string {
	if lang == i18n.RU {
		return "из"
	}
	return "of"
}

// of is n opportunities of the rule with their noun: "16 смертей".
func (r renderer) of(rule findings.RuleID, n int) string {
	return strconv.Itoa(n) + " " + i18n.Plural(r.lang, n, r.rule(rule).Of)
}

// rounds is n rounds with their noun: "22 раунда".
func (r renderer) rounds(n int) string {
	return strconv.Itoa(n) + " " + i18n.Plural(r.lang, n, r.t.Rounds)
}

// writeMoments writes the heaviest moments of the finding, in the order of
// the match.
func (r renderer) writeMoments(b *strings.Builder, id string) {
	f := r.byID[id]
	ev := slices.Clone(f.Evidence)
	slices.SortStableFunc(ev, func(a, b findings.Evidence) int {
		switch {
		case a.Impact > b.Impact:
			return -1
		case a.Impact < b.Impact:
			return 1
		}
		return 0
	})
	ev = ev[:min(len(ev), MaxMoments)]
	slices.SortStableFunc(ev, func(a, b findings.Evidence) int {
		if a.Round != b.Round {
			return a.Round - b.Round
		}
		return a.Tick - b.Tick
	})
	for _, e := range ev {
		b.WriteString("\n   · " + r.moment(f, e))
	}
}

// moment tells a moment of the finding.
func (r renderer) moment(f findings.FactFinding, e findings.Evidence) string {
	var parts []string
	add := func(format string, args ...any) { parts = append(parts, fmt.Sprintf(format, args...)) }
	switch f.Rule {
	case findings.MissedTrade:
		if e.Place != "" {
			add(r.t.TeammateDied, r.place(e.Place))
		}
		switch e.Detail {
		case findings.DetailHit:
			parts = append(parts, r.t.Hit)
		case findings.DetailMissed:
			parts = append(parts, r.t.Missed)
		default:
			parts = append(parts, r.t.NoShot)
		}
	case findings.BadFlashes:
		if e.Enemies == 0 && e.Teammates == 0 {
			parts = append(parts, r.t.BlindedNone)
		} else {
			add(r.t.Blinded, e.Enemies, e.Teammates)
		}
	case findings.MovingShots:
		add(r.t.OnTheMove, e.Count, e.Of, e.Weapon)
	case findings.FirstShot:
		add(r.t.FirstMiss, e.Weapon)
	case findings.MultiKills:
		add(r.t.Kills, e.Count)
	case findings.Clutches:
		add(r.t.Clutch, e.Count)
	case findings.UsefulGrenades:
		if e.Detail == findings.DetailHE || e.Detail == findings.DetailFire {
			kind := r.t.HE
			if e.Detail == findings.DetailFire {
				kind = r.t.Fire
			}
			add(r.t.GrenadeHit, kind, e.Count, e.Enemies)
		} else {
			add(r.t.Blinded, e.Enemies, e.Teammates)
		}
	case findings.OpeningWins, findings.TradeKills:
		if e.Place != "" || e.EnemyPlace != "" {
			add(r.t.Kill, r.place(e.Place), r.place(e.EnemyPlace))
		}
		if e.Weapon != "" {
			parts = append(parts, e.Weapon)
		}
	default: // the deaths of the player
		if e.Place != "" || e.EnemyPlace != "" {
			add(r.t.Death, r.place(e.Place), r.place(e.EnemyPlace))
		}
		if e.TeammateMeters > 0 {
			add(r.t.Teammate, r.number(e.TeammateMeters, 0))
		}
		if e.Alive > 0 {
			add(r.t.Alive, e.Alive, e.AliveEnemy)
		}
		if e.Weapon != "" {
			parts = append(parts, e.Weapon)
		}
		if f.Rule == findings.UnusedGrenades && len(e.Grenades) > 0 {
			var names []string
			for _, g := range e.Grenades {
				if n, ok := r.t.Grenades[g]; ok {
					g = n
				}
				names = append(names, g)
			}
			add(r.t.Left, strings.Join(names, ", "), e.GrenadeValue)
		}
	}
	what := strings.Join(parts, ", ")
	if e.Tick == 0 {
		return fmt.Sprintf(r.t.MomentNoGoto, e.Round, r.clock(e.Clock, e.Planted), what)
	}
	return fmt.Sprintf(r.t.Moment, e.Round, r.clock(e.Clock, e.Planted), what, e.Goto)
}

// progress tells how the progress of the drill shows: its metric in this
// match, "" if the facts do not have it.
func (r renderer) progress(d content.Drill) string {
	i := slices.IndexFunc(r.f.Comparisons, func(c findings.Comparison) bool { return c.Measure == d.Metric })
	if i < 0 {
		return ""
	}
	c := r.f.Comparisons[i]
	return fmt.Sprintf(r.t.Progress, r.rule(findings.RuleID(d.Metric)).Title, int(c.X), r.of(findings.RuleID(d.Metric), int(c.N)))
}

// place names a place of the map of the match as players call it.
func (r renderer) place(p string) string { return r.c.Place(r.f.Match.Map, p, r.lang) }

// clock shows the round clock as the game does: whole seconds rounded up,
// the timer of the bomb once it is planted.
func (r renderer) clock(seconds float64, planted bool) string {
	s := findings.ClockSeconds(seconds)
	text := fmt.Sprintf("%d:%02d", s/60, s%60)
	if planted {
		return fmt.Sprintf(r.t.Bomb, text)
	}
	return text
}

// number shows v with up to digits decimals and no trailing zeros, with a
// decimal comma in Russian.
func (r renderer) number(v float64, digits int) string {
	p := math.Pow10(digits)
	v = math.Round(v*p) / p
	if v == 0 {
		v = 0 // no "-0"
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if r.lang == i18n.RU {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// percent shows a rate from 0 to 1 in whole percent.
func (r renderer) percent(rate float64) string {
	return strconv.Itoa(int(math.Round(rate*100))) + "%"
}
