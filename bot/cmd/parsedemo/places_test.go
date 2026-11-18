package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// killAt is a kill of the T at victim in vPlace by the CT at killer in
// kPlace, both positions told by the kill.
func killAt(vPlace string, victim [3]float64, kPlace string, killer [3]float64) demo.Kill {
	v, k := demo.Ref{ID: 6, Side: demo.T}, demo.Ref{ID: 1, Side: demo.CT}
	return demo.Kill{
		Killer: &k, Victim: v, VictimPlace: vPlace, KillerPlace: kPlace,
		Positions: []demo.Position{
			{Body: demo.Ref{ID: 2, Side: demo.CT}, X: 9999, Y: 9999, Z: 9999},
			{Body: k, X: killer[0], Y: killer[1], Z: killer[2]},
			{Body: v, X: victim[0], Y: victim[1], Z: victim[2]},
		},
	}
}

// testDict is the dictionary of places of the tests: de_test, without its
// place Window.
func testDict() map[string]content.Map {
	return map[string]content.Map{"de_test": {
		Name: content.Text{RU: "Тест", EN: "Test"},
		Places: map[string]content.Text{
			"BombsiteA": {RU: "плент A", EN: "A site"},
			"BombsiteB": {RU: "плент B", EN: "B site"},
			"CTSpawn":   {RU: "КТ-спавн", EN: "CT spawn"},
			"TSpawn":    {RU: "Т-спавн", EN: "T spawn"},
			"Middle":    {RU: "мид", EN: "mid"},
		},
	}}
}

// lineOf returns the line of the report that starts with the place.
func lineOf(t *testing.T, report, place string) string {
	t.Helper()
	for _, l := range strings.Split(report, "\n") {
		if strings.HasPrefix(l, "  "+place+" ") {
			return l
		}
	}
	t.Fatalf("no line of %s in\n%s", place, report)
	return ""
}

func TestPlacesReport(t *testing.T) {
	first := &demo.Match{Map: "de_test", Kills: []demo.Kill{
		killAt("BombsiteA", [3]float64{0, 0, 0}, "TSpawn", [3]float64{0, -4000, 0}),
		killAt("BombsiteA", [3]float64{100, 0, 0}, "Window", [3]float64{2000, 300, 0}),
		killAt("BombsiteB", [3]float64{2000, 0, 0}, "CTSpawn", [3]float64{0, 3000, 0}),
		// The world kills nobody from anywhere.
		{Victim: demo.Ref{ID: 7, Side: demo.T}, VictimPlace: "Middle", Weapon: "World",
			Positions: []demo.Position{{Body: demo.Ref{ID: 7, Side: demo.T}, X: 50, Y: 0, Z: 400}}},
		// A kill that tells no positions, and one that tells no places.
		{Killer: &demo.Ref{ID: 1, Side: demo.CT}, Victim: demo.Ref{ID: 8, Side: demo.T}, VictimPlace: "TSpawn", KillerPlace: "BombsiteA"},
		killAt("", [3]float64{1, 1, 1}, "", [3]float64{2, 2, 2}),
	}}
	second := &demo.Match{Map: "de_test", Kills: []demo.Kill{
		killAt("CTSpawn", [3]float64{0, 3000, 0}, "Middle", [3]float64{500, 0, 0}),
	}}
	byMap := make(map[string]*mapPlaces)
	addPlaces(byMap, first)
	addPlaces(byMap, second)

	var out bytes.Buffer
	if placesReport(&out, byMap, testDict()) {
		t.Error("placesReport() = true with Window not in the dictionary")
	}
	report := out.String()
	for place, want := range map[string][]string{
		// The landmark is placed by the others, itself aside.
		"BombsiteA": {"deaths 2", "kills 1", "at 50 0 0 ", "50 m from BombsiteB", "плент A / A site"},
		"BombsiteB": {"deaths 1", "kills 0", "at 2000 0 0 ", "50 m from BombsiteA", "плент B / B site"},
		"CTSpawn":   {"deaths 1", "kills 1", "at 0 3000 0 ", "76 m from BombsiteA", "КТ-спавн / CT spawn"},
		// Its only death tells no position: placed by the killer in it.
		"TSpawn": {"deaths 1", "kills 1", "at 0 -4000 0 by kills", "102 m from BombsiteA", "Т-спавн / T spawn"},
		// The distance counts the height: 400 units over the site.
		"Middle": {"deaths 1", "kills 1", "at 50 0 400 ", "10 m from BombsiteA", "мид / mid"},
		"Window": {"deaths 0", "kills 1", "at 2000 300 0 by kills", "8 m from BombsiteB", "MISSING"},
	} {
		line := lineOf(t, report, place)
		for _, w := range want {
			if !strings.Contains(line, w) {
				t.Errorf("%s: %q lacks %q", place, line, w)
			}
		}
	}
	for _, want := range []string{"de_test, 2 demos:\n", "de_test: 6 places, 1 not in places.yaml\n"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "the map is not in places.yaml") {
		t.Errorf("the map is in the dictionary:\n%s", report)
	}

	// With Window in the dictionary every place is named.
	dict := testDict()
	dict["de_test"].Places["Window"] = content.Text{RU: "окно", EN: "window"}
	out.Reset()
	if !placesReport(&out, byMap, dict) {
		t.Errorf("placesReport() = false with every place in the dictionary:\n%s", out.String())
	}
	if line := lineOf(t, out.String(), "Window"); !strings.Contains(line, "окно / window") {
		t.Errorf("Window: %q", line)
	}
	if !strings.Contains(out.String(), "de_test: 6 places, 0 not in places.yaml\n") {
		t.Errorf("the summary of the map:\n%s", out.String())
	}
}

func TestPlacesReportNewMap(t *testing.T) {
	byMap := make(map[string]*mapPlaces)
	addPlaces(byMap, &demo.Match{Map: "de_new", Kills: []demo.Kill{
		killAt("Bridge", [3]float64{10, 20, 30}, "Bridge", [3]float64{10, 20, 30}),
		{Victim: demo.Ref{ID: 9, Bot: true, Side: demo.T}, VictimPlace: "Ramp"},
	}})
	var out bytes.Buffer
	if placesReport(&out, byMap, testDict()) {
		t.Error("placesReport() = true for a map not in the dictionary")
	}
	report := out.String()
	for _, want := range []string{
		"de_new, 1 demo, the map is not in places.yaml:\n",
		"de_new: 2 places, 2 not in places.yaml\n",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
	// No landmark is placed: no distance.
	if line := lineOf(t, report, "Bridge"); !strings.Contains(line, "at 10 20 30 ") || strings.Contains(line, " m from ") ||
		!strings.Contains(line, "MISSING") {
		t.Errorf("Bridge: %q", line)
	}
	if line := lineOf(t, report, "Ramp"); !strings.Contains(line, "at ?") || !strings.Contains(line, "MISSING") {
		t.Errorf("Ramp: %q", line)
	}
}

// TestRunPlacesExitCode checks that parsedemo -places exits with status 1
// when the dictionary of content lacks a place of the kills, and 0 when it
// names them all.
func TestRunPlacesExitCode(t *testing.T) {
	for _, tt := range []struct {
		place string
		ok    bool
	}{
		{"BombsiteA", true},
		{"NoSuchPlace", false},
	} {
		r := &runner{places: true, out: &bytes.Buffer{}, log: &bytes.Buffer{}, mem: &sampler{}, placeMaps: make(map[string]*mapPlaces)}
		addPlaces(r.placeMaps, &demo.Match{Map: "de_mirage", Kills: []demo.Kill{
			killAt(tt.place, [3]float64{0, 0, 0}, tt.place, [3]float64{0, 0, 0}),
		}})
		if got := r.run(nil, 1); got != tt.ok {
			t.Errorf("run() with a kill in %s = %t, want %t:\n%s", tt.place, got, tt.ok, r.out)
		}
	}
}
