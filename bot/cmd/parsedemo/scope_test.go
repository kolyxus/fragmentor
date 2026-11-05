package main

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

func num(v float64) *float64 { return &v }

func TestCompareScope(t *testing.T) {
	m := &demo.Match{Players: []demo.Player{{SteamID64: 1, Name: "alpha"}, {SteamID64: 2, Name: "bravo"}}}
	players := []metrics.Player{
		{SteamID64: 1, Match: metrics.Stats{
			AccuracyShots: 3, AccuracyHits: 2, Accuracy: 2.0 / 3,
			FirstShotFights: 4, FirstShotHits: 1, FirstShotAccuracy: 0.25,
			RifleHits: 7, RifleHeadHits: 1, RifleHeadshotRate: 1.0 / 7,
			RifleKillDuels: 2, RifleKillTime: 0.31,
		}},
		{SteamID64: 2, Match: metrics.Stats{
			AccuracyShots: 50, AccuracyHits: 29, Accuracy: 0.58,
			PistolHits: 2, PistolHeadHits: 1, PistolHeadshotRate: 0.5,
		}},
	}
	f := scopeFile{"demo.dem.zst": {
		"alpha": {Accuracy: num(66), FirstShot: num(30), HSRifle: num(14), KillTime: num(0.95)},
		"bravo": {Accuracy: num(58), HSPistol: num(50)},
		"ghost": {Accuracy: num(10)},
	}}
	lines, pairs := compareScope(f, filepath.Join("testdata", "demo.dem.zst"), m, players)

	want := []string{
		"  scope.gg, ours/theirs:",
		// 66.7 rounds down to 66; 0.58 * 100 is a hair below 58 and still 58.
		"    alpha            accuracy 66/66  first shot 25/30*  HS rifles 14/14  HS pistols -/-  kill time 0.31/0.95 (2 duels)",
		"    bravo            accuracy 58/58  first shot -/-  HS rifles -/-  HS pistols 50/50  kill time -/- (0 duels)",
		"    ghost            not in the demo",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("lines =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if n := len(pairs["accuracy"]); n != 2 || len(pairs["first shot"]) != 1 || len(pairs["HS pistols"]) != 1 || len(pairs["kill time"]) != 1 {
		t.Errorf("pairs = %v", pairs)
	}

	summary := scopeSummary(pairs)
	if len(summary) != 6 || summary[0] != "scope.gg, all demos:" ||
		!strings.Contains(summary[1], "accuracy    2 players  r  1.00  mean difference 0.00  equal 2") ||
		!strings.Contains(summary[2], "first shot  1 players  r   NaN  mean difference 5.00  equal 0") ||
		!strings.Contains(summary[5], "kill time   1 players") || strings.Contains(summary[5], "equal") {
		t.Errorf("summary =\n%s", strings.Join(summary, "\n"))
	}

	lines, pairs = compareScope(f, "other.dem.zst", m, players)
	if len(lines) != 1 || !strings.Contains(lines[0], "no numbers for this demo") || pairs != nil {
		t.Errorf("a demo without numbers: %q, %v", lines, pairs)
	}
}

func TestLoadScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scope.json")
	data := `{"a.dem.zst": {"alpha": {"accuracy": 58, "hs_pistol": null, "kill_time": 0.95}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := loadScope(path)
	if err != nil {
		t.Fatal(err)
	}
	row := f["a.dem.zst"]["alpha"]
	if row.Accuracy == nil || *row.Accuracy != 58 || row.HSPistol != nil || row.FirstShot != nil || row.KillTime == nil || *row.KillTime != 0.95 {
		t.Errorf("row = %+v", row)
	}
	if _, err := loadScope(filepath.Join(t.TempDir(), "none.json")); err == nil {
		t.Error("a missing file loads")
	}
}

func TestPearson(t *testing.T) {
	if r := pearson([]float64{1, 2, 3}, []float64{2, 4, 6}); math.Abs(r-1) > 1e-12 {
		t.Errorf("r = %v, want 1", r)
	}
	if r := pearson([]float64{1, 2, 3}, []float64{3, 2, 1}); math.Abs(r+1) > 1e-12 {
		t.Errorf("r = %v, want -1", r)
	}
	if r := pearson([]float64{1, 1}, []float64{1, 2}); !math.IsNaN(r) {
		t.Errorf("r = %v, want NaN without variance", r)
	}
}
