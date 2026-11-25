package review

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
)

// The checkpoint cases keep only the counts and impacts needed to reproduce
// the selection. The tick numbers are synthetic.
func TestRenderMomentSelection(t *testing.T) {
	gotoTick := regexp.MustCompile(`demo_gototick (\d+)`)
	for _, tc := range []struct {
		name     string
		rule     findings.RuleID
		moments  []findings.Evidence
		wantGoto []string
	}{
		{"K2_moving_shots", findings.MovingShots, []findings.Evidence{
			{Round: 2, Tick: 2000, Count: 1, Of: 6, Impact: 1},
			{Round: 5, Tick: 5000, Count: 1, Of: 3, Impact: 1},
			{Round: 9, Tick: 9000, Count: 4, Of: 9, Impact: 1},
			{Round: 18, Tick: 18000, Count: 3, Of: 9, Impact: 1.25},
			{Round: 19, Tick: 19000, Count: 1, Of: 8, Impact: 1.875},
		}, []string{"8680", "17680", "18680"}},
		{"R6_useful_flashes", findings.UsefulGrenades, []findings.Evidence{
			{Round: 1, Tick: 1000, Enemies: 1},
			{Round: 1, Tick: 1100, Enemies: 1, Teammates: 1},
			{Round: 7, Tick: 7000, Enemies: 1},
			{Round: 18, Tick: 18000, Enemies: 3},
			{Round: 20, Tick: 20000, Enemies: 3, Teammates: 1},
		}, []string{"680", "17680", "19680"}},
		{"equal_weight_out_of_order", findings.UsefulGrenades, []findings.Evidence{
			{Round: 4, Tick: 4100, Enemies: 2},
			{Round: 4, Tick: 4000, Enemies: 2},
			{Round: 2, Tick: 2000, Enemies: 2},
			{Round: 1, Tick: 1000, Enemies: 2},
		}, []string{"680", "1680", "3680"}},
		{"two_moments", findings.MovingShots, []findings.Evidence{
			{Round: 2, Tick: 2000, Count: 4, Of: 5, Impact: 1},
			{Round: 1, Tick: 1000, Count: 2, Of: 5, Impact: 1},
		}, []string{"680", "1680"}},
		{"empty", findings.MovingShots, nil, nil},
	} {
		for i := range tc.moments {
			tc.moments[i].Goto = tc.moments[i].Tick - 320
		}
		f := facts()
		if tc.rule == findings.MovingShots {
			f.Findings[1].Evidence = tc.moments
			f.Review = findings.ReviewFacts{Mistakes: []findings.PickFacts{{Finding: "f2"}}}
		} else {
			f.Findings[3].Evidence = tc.moments
			f.Review = findings.ReviewFacts{Strength: &findings.PickFacts{Finding: "f4"}}
		}
		for _, lang := range []i18n.Lang{i18n.RU, i18n.EN} {
			t.Run(tc.name+"/"+string(lang), func(t *testing.T) {
				rendered := Render(f, lang, content.Default())
				var got []string
				for _, m := range gotoTick.FindAllStringSubmatch(rendered, -1) {
					got = append(got, m[1])
				}
				if !slices.Equal(got, tc.wantGoto) {
					t.Errorf("shown ticks %v, want %v", got, tc.wantGoto)
				}
			})
		}
	}
}

// facts is a made-up review: the lone deaths told with the untraded ones,
// the shots on the move as a habit, and the useful flashes.
func facts() findings.Facts {
	death := func(round int, clock float64, tick int) findings.Evidence {
		return findings.Evidence{Round: round, Clock: clock, Tick: tick, Goto: tick - 320, Impact: 1.5, Place: "Apartments",
			EnemyPlace: "TopofMid", TeammateMeters: 23.4, Alive: 3, AliveEnemy: 5, Weapon: "AK-47"}
	}
	return findings.Facts{
		Version: findings.FactsVersion,
		Match:   findings.MatchFacts{Map: "de_mirage", Rounds: 22, Score: [2]int{9, 13}, Result: findings.ResultLoss, TickRate: 64},
		Player:  findings.PlayerFacts{Kills: 14, Deaths: 21, Assists: 3, ADR: 71.26, KAST: 0.636},
		Comparisons: []findings.Comparison{
			{Measure: string(findings.LoneDeath), X: 9, N: 21},
			{Measure: string(findings.UntradedDeath), X: 17, N: 21},
			{Measure: string(findings.MovingShots), X: 28, N: 95},
		},
		Findings: []findings.FactFinding{
			{ID: "f1", Finding: findings.Finding{Rule: findings.LoneDeath, Kind: findings.Mistake, Group: findings.Trades, Model: findings.ModelShare,
				X: 9, N: 21, BaseRate: 0.1234, Expected: 2.59, Excess: 6.41, Z: 3.1, Impact: 1.4, Weight: 4.3,
				Evidence: []findings.Evidence{death(3, 71.2, 16282), death(7, 0.4, 51314), death(9, 104.58, 63420), death(12, 30, 81384)}},
				Drills: []string{"demo_review"}},
			{ID: "f2", Finding: findings.Finding{Rule: findings.MovingShots, Kind: findings.Mistake, Group: findings.Shooting, Model: findings.ModelShare,
				X: 28, N: 95, BaseRate: 0.131, Expected: 12.445, Excess: 15.555, Habit: true, Weight: 3.9,
				Evidence: []findings.Evidence{{Round: 5, Clock: 76, Tick: 29723, Goto: 29403, Impact: 1.5, Weapon: "M4A4", Count: 6, Of: 20}}},
				Drills: []string{"aim_botz_counter_strafe"}},
			{ID: "f3", Finding: findings.Finding{Rule: findings.UntradedDeath, Kind: findings.Mistake, Group: findings.Trades, Model: findings.ModelShare,
				X: 17, N: 21, Weight: 2.1}},
			{ID: "f4", Finding: findings.Finding{Rule: findings.UsefulGrenades, Kind: findings.Strength, Group: findings.Grenades,
				Variant: findings.VariantFlashes, Model: findings.ModelPerRound, X: 16, N: 22, BaseRate: 0.192, Expected: 4.22, Excess: 11.78, Weight: 6.5,
				Evidence: []findings.Evidence{{Round: 15, Clock: 109.4, Planted: false, Tick: 93422, Goto: 93102, Enemies: 3}}}},
		},
		Review: findings.ReviewFacts{
			Mistakes: []findings.PickFacts{{Finding: "f1", With: []string{"f3"}}, {Finding: "f2"}},
			Strength: &findings.PickFacts{Finding: "f4"},
			Task:     &findings.TaskFacts{Drill: "demo_review", Finding: "f1"},
		},
	}
}

func TestRenderRussian(t *testing.T) {
	got := Render(facts(), i18n.RU, content.Default())
	want := `Mirage, 9:13 — поражение
У/С/А 14/21/3 · ADR 71,3 · KAST 64%

Ошибки:
1. Смерти в одиночку: 9 из 21 смерти; по доле лобби (12%) ожидалось 2,6 — на 6,4 больше. Связанные находки: без размена — 17 из 21.
   · раунд 3, 1:12: апсы ← топ мид, тиммейт в 23 м, 3 на 5, AK-47 — demo_gototick 15962
   · раунд 7, 0:01: апсы ← топ мид, тиммейт в 23 м, 3 на 5, AK-47 — demo_gototick 50994
   · раунд 9, 1:45: апсы ← топ мид, тиммейт в 23 м, 3 на 5, AK-47 — demo_gototick 63100
2. Стрельба на ходу: 28 из 95 выстрелов по видимому врагу; по доле лобби (13%) ожидалось 12,4 — на 15,6 больше. Это повторяется: в прошлых матчах то же самое.
   · раунд 5, 1:16: на ходу 6 из 20 (M4A4) — demo_gototick 29403

Сильная сторона: Ослепления врагов и флеш-ассисты: 16 за 22 раунда; по частоте лобби (0,19 за раунд) ожидалось 4,2 — на 11,8 больше.
   · раунд 15, 1:50: ослеплено врагов: 3, своих: 0 — demo_gototick 93102

Задание на неделю: Разбор своих смертей в демке
` + content.Default().Drills()[0].Task.RU + `
Прогресс покажут следующие разборы по мере «Смерти без размена»: в этом матче 17 из 21 смерти.`
	if got != want {
		t.Errorf("review =\n%s\n\nwant\n%s", got, want)
	}
}

func TestRenderEnglish(t *testing.T) {
	got := Render(facts(), i18n.EN, content.Default())
	for _, want := range []string{
		"Mirage, 9:13, a loss\nK/D/A 14/21/3 · ADR 71.3 · KAST 64%",
		"1. Deaths alone: 9 of 21 deaths; the lobby's share (12%) would give 2.6 — 6.4 more. Related findings: untraded: 17 of 21.",
		"   · round 3, 1:12: apartments ← top mid, nearest teammate 23 m away, 3v5, AK-47 — demo_gototick 15962",
		"2. Shots on the move: 28 of 95 shots at an enemy in sight; the lobby's share (13%) would give 12.4 — 15.6 more. This keeps happening",
		"Strength: Enemies blinded and flash assists: 16 in 22 rounds; the lobby's rate (0.19 a round) would give 4.2 — 11.8 more.",
		"Drill of the week: Review your deaths in the demo",
		"The next reviews will show your progress in “Untraded deaths”: 17 of 21 deaths in this match.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
}

func TestRenderParts(t *testing.T) {
	c := content.Default()
	// No mistakes: the review says so and gives no task.
	f := facts()
	f.Review = findings.ReviewFacts{Mistakes: []findings.PickFacts{}}
	f.Match.Suspicious = true
	got := Render(f, i18n.RU, c)
	if !strings.Contains(got, i18n.For(i18n.RU).Review.NoMistakes) || !strings.Contains(got, i18n.For(i18n.RU).Review.Suspicious) ||
		strings.Contains(got, "Задание") || strings.Contains(got, "Сильная") {
		t.Errorf("a review without mistakes:\n%s", got)
	}

	// Each kind of moment, through one finding each.
	for _, tc := range []struct {
		rule    findings.RuleID
		variant string
		model   string
		e       findings.Evidence
		want    string
	}{
		{findings.MissedTrade, findings.VariantNoKill, findings.ModelShare,
			findings.Evidence{Round: 2, Clock: 50, Tick: 5000, Goto: 4680, Place: "Connector", Detail: findings.DetailHit},
			"Упущенные размены: стрелял, но не убил: 5 из 7 шансов разменять;"},
		{findings.MissedTrade, findings.VariantNoShot, findings.ModelShare,
			findings.Evidence{Round: 2, Clock: 50, Tick: 5000, Goto: 4680, Place: "Connector", Detail: findings.DetailNoShot},
			"раунд 2, 0:50: тиммейт погиб: коннектор, ты видел убийцу и не стрелял — demo_gototick 4680"},
		{findings.MissedTrade, findings.VariantNoKill, findings.ModelShare,
			findings.Evidence{Round: 2, Clock: 50, Tick: 5000, Goto: 4680, Place: "Connector", Detail: findings.DetailHit},
			"раунд 2, 0:50: тиммейт погиб: коннектор, ты попал, но не убил — demo_gototick 4680"},
		{findings.MissedTrade, findings.VariantNoKill, findings.ModelShare,
			findings.Evidence{Round: 2, Clock: 50, Tick: 5000, Goto: 4680, Place: "Connector", Detail: findings.DetailMissed},
			"раунд 2, 0:50: тиммейт погиб: коннектор, ты стрелял мимо — demo_gototick 4680"},
		{findings.BadFlashes, findings.VariantTeam, findings.ModelShare,
			findings.Evidence{Round: 4, Clock: 20.2, Planted: true, Tick: 9000, Goto: 8680, Teammates: 2},
			"раунд 4, бомба 0:21: ослеплено врагов: 0, своих: 2 — demo_gototick 8680"},
		{findings.BadFlashes, findings.VariantEmpty, findings.ModelShare,
			findings.Evidence{Round: 4, Clock: 20.2, Planted: true, Tick: 9000, Goto: 8680},
			"раунд 4, бомба 0:21: никого не ослепила — demo_gototick 8680"},
		{findings.FirstShot, "", findings.ModelShare,
			findings.Evidence{Round: 6, Clock: 90, Tick: 7000, Goto: 6680, Weapon: "Galil AR"},
			"раунд 6, 1:30: первый выстрел мимо (Galil AR)"},
		{findings.UnusedGrenades, "", findings.ModelShare,
			findings.Evidence{Round: 8, Clock: 60, Tick: 8000, Goto: 7680, Place: "Jungle", EnemyPlace: "Stairs", Alive: 2, AliveEnemy: 2,
				Weapon: "AWP", Grenades: []string{demo.Flashbang, demo.SmokeGrenade}, GrenadeValue: 500},
			"джунгли ← стейрс, 2 на 2, AWP, остались флешка, дым ($500)"},
		{findings.DeathSpot, "", findings.ModelPerMatch,
			findings.Evidence{Round: 9, Clock: 60, Tick: 9000, Goto: 8680, Place: "Apartments", EnemyPlace: "Apartments"},
			"Повторяющееся место смерти: апсы (за T) — 5 из 6 смертей там без размена; у остальных в худшей зоне в среднем 2,4 — у тебя на 2,6 больше."},
		{findings.OpeningWins, "", findings.ModelShare,
			findings.Evidence{Round: 1, Clock: 95, Tick: 1000, Goto: 680, Place: "Underpass", EnemyPlace: "Apartments", Weapon: "USP-S"},
			"раунд 1, 1:35: андер → апсы, USP-S"},
		{findings.MultiKills, "", findings.ModelPerRound,
			findings.Evidence{Round: 13, Clock: 0, Tick: 0, Count: 4},
			"раунд 13, 0:00: убийств: 4"},
		{findings.Clutches, "", findings.ModelPerRound,
			findings.Evidence{Round: 7, Clock: 25, Planted: true, Tick: 3000, Goto: 2680, Count: 3},
			"раунд 7, бомба 0:25: 1 против 3"},
		{findings.UsefulGrenades, findings.VariantDamage, findings.ModelPerRound,
			findings.Evidence{Round: 3, Clock: 80, Tick: 2000, Goto: 1680, Count: 92, Enemies: 2, Detail: findings.DetailHE},
			"Урон гранатами: 330 за 22 раунда; по частоте лобби (6 за раунд) ожидалось 132 — на 198 больше."},
		{findings.UsefulGrenades, findings.VariantDamage, findings.ModelPerRound,
			findings.Evidence{Round: 3, Clock: 80, Tick: 2000, Goto: 1680, Count: 92, Enemies: 2, Detail: findings.DetailFire},
			"раунд 3, 1:20: огонь: 92 урона, врагов: 2"},
	} {
		f := facts()
		fd := findings.FactFinding{ID: "f9", Finding: findings.Finding{Rule: tc.rule, Variant: tc.variant, Model: tc.model,
			X: 5, N: 7, Sample: 6, BaseRate: 0.3, Expected: 2.1, Excess: 2.9, Place: "Apartments", Side: demo.T, Evidence: []findings.Evidence{tc.e}}}
		switch tc.model {
		case findings.ModelPerRound:
			fd.X, fd.N, fd.BaseRate, fd.Expected, fd.Excess = 3.3, 22, 0.06, 1.32, 1.98
		case findings.ModelPerMatch:
			fd.X, fd.N, fd.BaseRate, fd.Expected, fd.Excess = 5, 1, 2.4, 2.4, 2.6
		}
		f.Findings = append(f.Findings, fd)
		f.Review = findings.ReviewFacts{Mistakes: []findings.PickFacts{{Finding: "f9"}}}
		if got := Render(f, i18n.RU, c); !strings.Contains(got, tc.want) {
			t.Errorf("%s %s: no %q in\n%s", tc.rule, tc.variant, tc.want, got)
		}
	}

	// Of four moments the three heaviest are told, in the order of the
	// match: the lightest, the first, is not.
	f = facts()
	f.Findings[0].Evidence[0].Impact = 0.5
	got = Render(f, i18n.RU, c)
	if strings.Contains(got, "раунд 3, 1:12") || !strings.Contains(got, "раунд 7, 0:01") || !strings.Contains(got, "раунд 12, 0:30") ||
		strings.Index(got, "раунд 7, 0:01") > strings.Index(got, "раунд 12, 0:30") {
		t.Errorf("the heaviest moments, rounds 7, 9 and 12, not told in order:\n%s", got)
	}

	// A drill of the map played gives its link; one the facts lack nothing.
	f = facts()
	f.Review.Task = &findings.TaskFacts{Drill: "prefire_5e", Finding: "f1"}
	if got := Render(f, i18n.RU, c); !strings.Contains(got, "https://steamcommunity.com/sharedfiles/filedetails/?id=3232466864") {
		t.Errorf("no link of the prefires of Mirage:\n%s", got)
	}
	f.Review.Task = &findings.TaskFacts{Drill: "no_such_drill", Finding: "f1"}
	if got := Render(f, i18n.RU, c); strings.Contains(got, "Задание") {
		t.Errorf("a task of an unknown drill:\n%s", got)
	}
}

func TestNumbers(t *testing.T) {
	ru, en := renderer{lang: i18n.RU}, renderer{lang: i18n.EN}
	for _, tc := range []struct {
		v      float64
		digits int
		ru, en string
	}{
		{4.1, 1, "4,1", "4.1"}, {4.05, 1, "4,1", "4.1"}, {12, 1, "12", "12"}, {0.192, 2, "0,19", "0.19"},
		{71.26, 1, "71,3", "71.3"}, {-0.04, 1, "0", "0"}, {3035, 0, "3035", "3035"},
	} {
		if got := ru.number(tc.v, tc.digits); got != tc.ru {
			t.Errorf("ru number(%v, %d) = %q, want %q", tc.v, tc.digits, got, tc.ru)
		}
		if got := en.number(tc.v, tc.digits); got != tc.en {
			t.Errorf("en number(%v, %d) = %q, want %q", tc.v, tc.digits, got, tc.en)
		}
	}
	for v, want := range map[float64]string{0.636: "64%", 0: "0%", 1: "100%", 0.125: "13%"} {
		if got := ru.percent(v); got != want {
			t.Errorf("percent(%v) = %q, want %q", v, got, want)
		}
	}
	ru.t = i18n.For(i18n.RU).Review
	for _, tc := range []struct {
		s       float64
		planted bool
		want    string
	}{{94.58, false, "1:35"}, {115, false, "1:55"}, {0, false, "0:00"}, {9.2, true, "бомба 0:10"}} {
		if got := ru.clock(tc.s, tc.planted); got != tc.want {
			t.Errorf("clock(%v, %t) = %q, want %q", tc.s, tc.planted, got, tc.want)
		}
	}
}
