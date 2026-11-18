package content_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
)

var cyrillic = regexp.MustCompile(`\p{Cyrillic}`)

// checkLanguages checks that a text is in both languages and that neither
// is the other one: Russian has Cyrillic letters, English none.
func checkLanguages(t *testing.T, name string, tx content.Text) {
	t.Helper()
	if !cyrillic.MatchString(tx.RU) {
		t.Errorf("%s: the Russian text %q has no Cyrillic letter", name, tx.RU)
	}
	if cyrillic.MatchString(tx.EN) || strings.TrimSpace(tx.EN) == "" {
		t.Errorf("%s: the English text %q is empty or has Cyrillic letters", name, tx.EN)
	}
}

func TestCatalogs(t *testing.T) {
	c, err := content.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c != content.Default() && len(c.Drills()) != len(content.Default().Drills()) {
		t.Error("Default is not the catalogs embedded")
	}
	for _, d := range c.Drills() {
		checkLanguages(t, d.ID+" title", d.Title)
		checkLanguages(t, d.ID+" task", d.Task)
		if got, ok := c.Drill(d.ID); !ok || got.ID != d.ID {
			t.Errorf("Drill(%q) = %+v, %t", d.ID, got, ok)
		}
		switch {
		case d.Workshop != 0:
			if u := d.URL("de_whatever"); u != content.WorkshopURL(d.Workshop) || !strings.HasPrefix(u, "https://steamcommunity.com/") {
				t.Errorf("%s: URL %q", d.ID, u)
			}
		case len(d.ByMap) > 0:
			for m, id := range d.ByMap {
				if !d.Fits(m) || d.URL(m) != content.WorkshopURL(id) {
					t.Errorf("%s on %s: fits %t, URL %q", d.ID, m, d.Fits(m), d.URL(m))
				}
			}
			if d.Fits("de_whatever") || d.URL("de_whatever") != "" {
				t.Errorf("%s fits a map it has no version of", d.ID)
			}
		default:
			if !d.Builtin || d.URL("de_mirage") != "" {
				t.Errorf("%s: builtin %t, URL %q", d.ID, d.Builtin, d.URL("de_mirage"))
			}
		}
	}
	// Every mistake has a drill for a match on any map, so that a review
	// can always give a task for it.
	for _, r := range findings.Rules() {
		ids := c.DrillsFor(r.ID, "de_whatever")
		if r.Kind == findings.Mistake && len(ids) == 0 {
			t.Errorf("no drill against %s fits every map", r.ID)
		}
		if r.Kind == findings.Strength && len(ids) > 0 {
			t.Errorf("drills %v against the strength %s", ids, r.ID)
		}
	}
	for id, m := range c.Maps() {
		checkLanguages(t, id+" name", content.Text{RU: "карта " + m.Name.RU, EN: m.Name.EN})
		for p, tx := range m.Places {
			checkLanguages(t, id+" "+p, tx)
			if got := c.Place(id, p, i18n.RU); got != tx.RU {
				t.Errorf("Place(%s, %s, ru) = %q, want %q", id, p, got, tx.RU)
			}
			if got := c.Place(id, p, i18n.EN); got != tx.EN {
				t.Errorf("Place(%s, %s, en) = %q, want %q", id, p, got, tx.EN)
			}
		}
	}
}

func TestDrillsFor(t *testing.T) {
	c := content.Default()
	// The drills of the map played come first, in the order of the file.
	if got := c.DrillsFor(findings.OpeningLoss, "de_mirage"); !slices.Equal(got, []string{"prefire_5e", "yprac_prefire"}) {
		t.Errorf("drills against the opening duels on Mirage = %v", got)
	}
	if got := c.DrillsFor(findings.OpeningLoss, "de_dust9"); !slices.Equal(got, []string{"yprac_prefire"}) {
		t.Errorf("drills against the opening duels on a map without prefires of 5E = %v", got)
	}
	if got := c.DrillsFor(findings.LoneDeath, "de_mirage"); len(got) == 0 || got[0] != "demo_review" {
		t.Errorf("drills against lone deaths = %v", got)
	}
	if got := c.DrillsFor(findings.TradeKills, "de_mirage"); got != nil {
		t.Errorf("drills against a strength = %v", got)
	}
	if _, ok := c.Drill("no_such_drill"); ok {
		t.Error("a drill of an unknown ID")
	}
	if got, want := content.WorkshopURL(3070244462), "https://steamcommunity.com/sharedfiles/filedetails/?id=3070244462"; got != want {
		t.Errorf("WorkshopURL() = %q, want %q", got, want)
	}
}

func TestPlaces(t *testing.T) {
	c := content.Default()
	for _, tc := range []struct {
		mapName, place string
		lang           i18n.Lang
		want           string
	}{
		{"de_mirage", "TopofMid", i18n.RU, "топ мид"},
		{"de_mirage", "TopofMid", i18n.EN, "top mid"},
		{"de_mirage", "Apartments", i18n.RU, "апсы"},
		{"de_mirage", "NoSuchPlace", i18n.RU, "No Such Place"}, // a place the dictionary lacks
		{"de_dust9", "BackofB", i18n.EN, "Back of B"},          // a map it lacks
		{"de_mirage", "", i18n.RU, ""},
	} {
		if got := c.Place(tc.mapName, tc.place, tc.lang); got != tc.want {
			t.Errorf("Place(%q, %q, %s) = %q, want %q", tc.mapName, tc.place, tc.lang, got, tc.want)
		}
	}
	for in, want := range map[string]string{
		"de_mirage":               "Mirage",
		"de_dust2":                "Dust2",
		"workshop/123/de_vertigo": "Vertigo",
		"cs_office":               "Office",
		"mirage":                  "Mirage",
	} {
		if got := c.MapName(in, i18n.RU); got != want {
			t.Errorf("MapName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitPlace(t *testing.T) {
	for in, want := range map[string]string{
		"TopofMid":      "Top of Mid",
		"BackofB":       "Back of B",
		"CTSpawn":       "CT Spawn",
		"TSideUpper":    "T Side Upper",
		"BHalls":        "B Halls",
		"BombsiteA":     "Bombsite A",
		"SnipersNest":   "Snipers Nest",
		"MidRoof":       "Mid Roof", // an "of" that ends the name is no word
		"Mid":           "Mid",
		"Room2":         "Room 2",
		"Floor2Stairs":  "Floor 2 Stairs", // a word after a digit
		"ElectricalBox": "Electrical Box",
	} {
		if got := content.SplitPlace(in); got != want {
			t.Errorf("SplitPlace(%q) = %q, want %q", in, got, want)
		}
	}
}

// drill is a valid drill in YAML, to break one rule at a time.
const drill = `
  - id: a_drill
    title: {ru: Упражнение, en: A drill}
    task: {ru: Сделай так, en: Do so}
    workshop: 123
    findings: [first_shot]
    metric: first_shot
    better: lower
    min_sample: 10
`

const places = `
maps:
  de_mirage:
    name: {ru: Mirage, en: Mirage}
    places:
      TopofMid: {ru: топ мид, en: top mid}
`

func TestParseErrors(t *testing.T) {
	if _, err := content.Parse([]byte("drills:"+drill), []byte(places)); err != nil {
		t.Fatalf("the valid catalogs: %v", err)
	}
	for _, tc := range []struct {
		name           string
		drills, places string
		want           string
	}{
		{"an unknown key", strings.Replace(drill, "    better: lower", "    better: lower\n    level: 5", 1), places, "level"},
		{"a key twice", strings.Replace(drill, "    better: lower", "    better: lower\n    better: lower", 1), places, "already defined"},
		{"a drill twice", drill + drill, places, "twice"},
		{"an unknown metric", strings.Replace(drill, "metric: first_shot", "metric: aim", 1), places, "no measure"},
		{"the wrong direction", strings.Replace(drill, "better: lower", "better: higher", 1), places, "more is better: false"},
		{"no direction", strings.Replace(drill, "better: lower", "better: less", 1), places, "want"},
		{"an unknown rule", strings.Replace(drill, "[first_shot]", "[first_bullet]", 1), places, "no mistake"},
		{"a strength", strings.Replace(drill, "[first_shot]", "[clutches]", 1), places, "no mistake"},
		{"no findings", strings.Replace(drill, "[first_shot]", "[]", 1), places, "no findings"},
		{"no English", strings.Replace(drill, "{ru: Сделай так, en: Do so}", "{ru: Сделай так}", 1), places, "both ru and en"},
		{"nowhere", strings.Replace(drill, "    workshop: 123\n", "", 1), places, "exactly one"},
		{"two places", strings.Replace(drill, "    workshop: 123\n", "    workshop: 123\n    builtin: true\n    source: the game\n", 1), places, "exactly one"},
		{"builtin without a source", strings.Replace(drill, "    workshop: 123\n", "    builtin: true\n", 1), places, "source"},
		{"a bad map", strings.Replace(drill, "    workshop: 123\n", "    by_map: {Mirage: 5}\n", 1), places, "by_map"},
		{"a bad ID", strings.Replace(drill, "id: a_drill", "id: A Drill", 1), places, "the ID"},
		{"no sample", strings.Replace(drill, "min_sample: 10", "min_sample: 0", 1), places, "min_sample"},
		{"a second document", drill + "\n---\ndrills: []\n", places, "more than one document"},
		{"no drills", "", places, "no drills"},
		{"a place without English", drill, strings.Replace(places, "{ru: топ мид, en: top mid}", "{ru: топ мид}", 1), "both ru and en"},
		{"a place with a space", drill, strings.Replace(places, "TopofMid:", "Top of Mid:", 1), "navigation"},
		{"a map ID", drill, strings.Replace(places, "de_mirage:", "Mirage:", 1), "like de_mirage"},
		{"no maps", drill, "maps: {}\n", "no maps"},
		{"a map without English", drill, strings.Replace(places, "{ru: Mirage, en: Mirage}", "{ru: Mirage}", 1), "name needs both"},
		{"a map without places", drill, strings.Replace(places, "      TopofMid: {ru: топ мид, en: top mid}\n", "", 1), "no places"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := "drills:" + tc.drills
			if tc.drills == "" {
				ds = "drills: []\n"
			}
			_, err := content.Parse([]byte(ds), []byte(tc.places))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one with %q", err, tc.want)
			}
		})
	}
}
