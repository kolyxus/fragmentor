// Package content holds the catalogs the reviews are made of: the drills a
// review gives as the task of the week (drills.yaml) and how players call the
// places of the maps (places.yaml). The catalogs are YAML embedded in the
// binary. Their texts in Russian and English live next to the data, not in
// internal/i18n: a drill and its description, a place and its names, are one
// entry each, and the tests require both languages (CLAUDE.md). How the YAML
// is read is in docs/notes/go-yaml.md.
package content

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
)

//go:embed drills.yaml places.yaml
var files embed.FS

// Text is a text in Russian and in English.
type Text struct {
	RU string `yaml:"ru"`
	EN string `yaml:"en"`
}

// In returns the text in lang, the English one for a language other than
// Russian.
func (t Text) In(lang i18n.Lang) string {
	if lang == i18n.RU {
		return t.RU
	}
	return t.EN
}

// Directions of a metric of progress.
const (
	Lower  = "lower"  // the fewer events, the better: mistakes
	Higher = "higher" // the more, the better: strengths
)

// Drill is a drill a review may give as the task of the week: a map of the
// Steam Workshop, a mode of the game or what to do with the demo of the match.
type Drill struct {
	// ID names the drill in the facts of a review, see findings.Facts.
	ID string `yaml:"id"`
	// Title names the drill; Task says what to do, in a sentence or two.
	Title Text `yaml:"title"`
	Task  Text `yaml:"task"`
	// Where the drill is, exactly one of:
	//   - Workshop: the ID of a map of the Steam Workshop;
	//   - ByMap: a map of the Steam Workshop for each map of the game, by
	//     the ID of the map, like "de_mirage": the drill fits a match of
	//     those maps only;
	//   - Builtin: the game itself has it, a mode or the demo player, and
	//     Source says where that is told.
	Workshop uint64            `yaml:"workshop,omitempty"`
	ByMap    map[string]uint64 `yaml:"by_map,omitempty"`
	Builtin  bool              `yaml:"builtin,omitempty"`
	Source   string            `yaml:"source,omitempty"`
	// Findings is the mistakes the drill trains against, rules of
	// internal/findings.
	Findings []findings.RuleID `yaml:"findings"`
	// The progress of the drill shows in Metric, a measure of
	// internal/findings: the next reviews tell it, events of opportunities.
	// Better is whether Lower or Higher is better, and MinSample how many
	// opportunities a match must have for its number to count.
	Metric    string `yaml:"metric"`
	Better    string `yaml:"better"`
	MinSample int    `yaml:"min_sample"`
}

// WorkshopURL returns the page of a map of the Steam Workshop.
func WorkshopURL(id uint64) string {
	return fmt.Sprintf("https://steamcommunity.com/sharedfiles/filedetails/?id=%d", id)
}

// Fits reports whether the drill can be given after a match on mapName: a
// drill of the map played only if it has a version for the map.
func (d Drill) Fits(mapName string) bool {
	if len(d.ByMap) == 0 {
		return true
	}
	_, ok := d.ByMap[mapName]
	return ok
}

// URL returns the page of the drill for a match on mapName, "" for a drill
// the game itself has.
func (d Drill) URL(mapName string) string {
	switch {
	case d.Workshop != 0:
		return WorkshopURL(d.Workshop)
	case d.ByMap[mapName] != 0:
		return WorkshopURL(d.ByMap[mapName])
	}
	return ""
}

// Catalog is the drills and the places of the maps.
type Catalog struct {
	drills []Drill
	byID   map[string]int
	maps   map[string]Map
}

// Map is a map of the game: its name and its places by the names of the
// game's navigation, like "TopofMid", which demo.Kill gives.
type Map struct {
	Name   Text            `yaml:"name"`
	Places map[string]Text `yaml:"places"`
}

// Drills returns the drills, in the order of drills.yaml.
func (c *Catalog) Drills() []Drill { return append([]Drill(nil), c.drills...) }

// Drill returns the drill with the ID.
func (c *Catalog) Drill(id string) (Drill, bool) {
	i, ok := c.byID[id]
	if !ok {
		return Drill{}, false
	}
	return c.drills[i], true
}

// DrillsFor returns the IDs of the drills against the rule that fit a match
// on mapName, in the order of drills.yaml.
func (c *Catalog) DrillsFor(rule findings.RuleID, mapName string) []string {
	var out []string
	for _, d := range c.drills {
		for _, r := range d.Findings {
			if r == rule && d.Fits(mapName) {
				out = append(out, d.ID)
				break
			}
		}
	}
	return out
}

// Maps returns the maps of places.yaml, by ID.
func (c *Catalog) Maps() map[string]Map {
	out := make(map[string]Map, len(c.maps))
	for id, m := range c.maps {
		out[id] = m
	}
	return out
}

// MapName returns the name of the map to show, like "Mirage" for
// "de_mirage": from places.yaml, or made of the ID for a map it lacks.
func (c *Catalog) MapName(mapName string, lang i18n.Lang) string {
	if m, ok := c.maps[mapName]; ok {
		return m.Name.In(lang)
	}
	name := mapName[strings.LastIndexByte(mapName, '/')+1:]
	if _, rest, ok := strings.Cut(name, "_"); ok && rest != "" {
		name = rest
	}
	r := []rune(name)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// Place returns how players call the place of the game's navigation on the
// map, like "топ мид" for "TopofMid" on de_mirage; for a place places.yaml
// lacks, its name in the game with spaces, "Top of Mid", in either language.
// An empty place is "".
func (c *Catalog) Place(mapName, place string, lang i18n.Lang) string {
	if place == "" {
		return ""
	}
	if t, ok := c.maps[mapName].Places[place]; ok {
		return t.In(lang)
	}
	return SplitPlace(place)
}

var (
	// ofRe finds "of" between two words of a place name: "TopofMid".
	ofRe = regexp.MustCompile(`([a-z])of([A-Z])`)
	// The IDs of drills, of maps and of places.
	drillIDRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	mapIDRe   = regexp.MustCompile(`^[a-z]+_[a-z0-9_]+$`)
	placeRe   = regexp.MustCompile(`^[A-Za-z0-9]+$`)
)

// SplitPlace puts spaces between the words of a place name of the game's
// navigation: "TopofMid" is "Top of Mid", "CTSpawn" "CT Spawn", "BHalls"
// "B Halls". A lower-case "of" counts as a word only between two others.
func SplitPlace(place string) string {
	s := []rune(ofRe.ReplaceAllString(place, "$1 of $2"))
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r != ' ' && s[i-1] != ' ' {
			prev := s[i-1]
			next := rune(0)
			if i+1 < len(s) {
				next = s[i+1]
			}
			switch {
			case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)),
				unicode.IsUpper(r) && unicode.IsUpper(prev) && unicode.IsLower(next),
				unicode.IsDigit(r) && unicode.IsLetter(prev):
				b.WriteRune(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Default returns the catalogs embedded in the binary. It panics if they are
// invalid, which the tests of the package rule out.
var Default = sync.OnceValue(func() *Catalog {
	c, err := Load()
	if err != nil {
		panic(err)
	}
	return c
})

// Load reads and checks the catalogs embedded in the binary.
func Load() (*Catalog, error) {
	drills, err := files.ReadFile("drills.yaml")
	if err != nil {
		return nil, fmt.Errorf("read drills: %w", err)
	}
	places, err := files.ReadFile("places.yaml")
	if err != nil {
		return nil, fmt.Errorf("read places: %w", err)
	}
	return Parse(drills, places)
}

// Parse reads and checks the catalogs from the YAML of drills.yaml and of
// places.yaml. Every rule of the files is checked: a key the catalog does
// not know is an error, and so is a key given twice.
func Parse(drillsYAML, placesYAML []byte) (*Catalog, error) {
	var df struct {
		Drills []Drill `yaml:"drills"`
	}
	if err := decode(drillsYAML, &df); err != nil {
		return nil, fmt.Errorf("drills.yaml: %w", err)
	}
	var pf struct {
		Maps map[string]Map `yaml:"maps"`
	}
	if err := decode(placesYAML, &pf); err != nil {
		return nil, fmt.Errorf("places.yaml: %w", err)
	}
	c := &Catalog{drills: df.Drills, byID: make(map[string]int, len(df.Drills)), maps: pf.Maps}
	var errs []error
	for i, d := range c.drills {
		if _, dup := c.byID[d.ID]; dup {
			errs = append(errs, fmt.Errorf("drills.yaml: drill %q twice", d.ID))
		}
		c.byID[d.ID] = i
		if err := checkDrill(d); err != nil {
			errs = append(errs, fmt.Errorf("drills.yaml: drill %d %q: %w", i+1, d.ID, err))
		}
	}
	if len(c.drills) == 0 {
		errs = append(errs, errors.New("drills.yaml: no drills"))
	}
	for id, m := range c.maps {
		if err := checkMap(id, m); err != nil {
			errs = append(errs, fmt.Errorf("places.yaml: map %q: %w", id, err))
		}
	}
	if len(c.maps) == 0 {
		errs = append(errs, errors.New("places.yaml: no maps"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return c, nil
}

// decode decodes the only document of b into v: a key of a mapping that v
// lacks is an error, as is a second document.
func decode(b []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return err
	}
	var more any
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		return errors.New("more than one document")
	}
	return nil
}

func checkText(name string, t Text) error {
	if strings.TrimSpace(t.RU) == "" || strings.TrimSpace(t.EN) == "" {
		return fmt.Errorf("%s needs both ru and en", name)
	}
	return nil
}

func checkDrill(d Drill) error {
	var errs []error
	if !drillIDRe.MatchString(d.ID) {
		errs = append(errs, fmt.Errorf("the ID %q is not lower-case letters, digits and _", d.ID))
	}
	errs = append(errs, checkText("title", d.Title), checkText("task", d.Task))
	where := 0
	if d.Workshop != 0 {
		where++
	}
	if len(d.ByMap) > 0 {
		where++
		for m, id := range d.ByMap {
			if !mapIDRe.MatchString(m) || id == 0 {
				errs = append(errs, fmt.Errorf("by_map: map %q, workshop ID %d", m, id))
			}
		}
	}
	if d.Builtin {
		where++
		if strings.TrimSpace(d.Source) == "" {
			errs = append(errs, errors.New("a builtin drill needs the source that tells of it"))
		}
	}
	if where != 1 {
		errs = append(errs, errors.New("exactly one of workshop, by_map and builtin"))
	}
	kinds := make(map[findings.RuleID]findings.Kind)
	for _, r := range findings.Rules() {
		kinds[r.ID] = r.Kind
	}
	if len(d.Findings) == 0 {
		errs = append(errs, errors.New("no findings"))
	}
	for _, r := range d.Findings {
		if kinds[r] != findings.Mistake {
			errs = append(errs, fmt.Errorf("%q is no mistake of internal/findings", r))
		}
	}
	more, ok := findings.MoreIsBetter(d.Metric)
	switch {
	case !ok:
		errs = append(errs, fmt.Errorf("the metric %q is no measure of internal/findings", d.Metric))
	case d.Better != Lower && d.Better != Higher:
		errs = append(errs, fmt.Errorf("better is %q, want %q or %q", d.Better, Lower, Higher))
	case more != (d.Better == Higher):
		errs = append(errs, fmt.Errorf("better is %q, but for %q more is better: %t", d.Better, d.Metric, more))
	}
	if d.MinSample < 1 {
		errs = append(errs, fmt.Errorf("min_sample %d, want 1 or more", d.MinSample))
	}
	return errors.Join(errs...)
}

func checkMap(id string, m Map) error {
	var errs []error
	if !mapIDRe.MatchString(id) {
		errs = append(errs, fmt.Errorf("the ID %q is not like de_mirage", id))
	}
	errs = append(errs, checkText("name", m.Name))
	if len(m.Places) == 0 {
		errs = append(errs, errors.New("no places"))
	}
	for p, t := range m.Places {
		if !placeRe.MatchString(p) {
			errs = append(errs, fmt.Errorf("place %q is not a name of the game's navigation", p))
		}
		if err := checkText("place "+p, t); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
