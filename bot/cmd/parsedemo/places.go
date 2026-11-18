package main

import (
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"text/tabwriter"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// The places of the maps, with -places: every place of the game's navigation
// the kills of the demos name, map by map, with how many died in it and how
// many were killed from it, where it is, and how the dictionary of places,
// content/places.yaml, calls it. That is how the dictionary is filled for a
// new map: the demos of the maps it lacks lie in testdata/maps/ for it, see
// bot/testdata/README.md. A place the dictionary lacks is MISSING, and then
// parsedemo exits with status 1.
//
// Where a place is: the mean position of the deaths in it, and the nearest
// of the landmarks, the bomb sites and the spawns, by the mean position of
// the deaths in them, with how far it is in meters. A place nobody died in
// is placed by the killers who stood in it.

// landmarks are the places every map has, by which the others are placed.
var landmarks = []string{"BombsiteA", "BombsiteB", "CTSpawn", "TSpawn"}

// meterPerUnit is how long a unit of the game is, an inch, in meters.
const meterPerUnit = 0.0254

// point is a position of the game, or a sum of them.
type point struct{ x, y, z float64 }

func (p point) add(q demo.Position) point { return point{p.x + q.X, p.y + q.Y, p.z + q.Z} }

func (p point) div(n int) point {
	return point{p.x / float64(n), p.y / float64(n), p.z / float64(n)}
}

func (p point) dist(q point) float64 {
	return math.Sqrt((p.x-q.x)*(p.x-q.x) + (p.y-q.y)*(p.y-q.y) + (p.z-q.z)*(p.z-q.z))
}

// placeStats is what the kills tell of a place of a map: how many died in
// it and were killed from it, and the sums of the positions of the dead and
// of the killers in it, dn and kn of them: a kill may not tell them.
type placeStats struct {
	deaths, kills int
	died, killed  point
	dn, kn        int
}

// where returns the mean position of the deaths in the place, or of the
// killers in it if no death tells one; ok is false if no kill does.
func (s *placeStats) where() (at point, byKills, ok bool) {
	switch {
	case s.dn > 0:
		return s.died.div(s.dn), false, true
	case s.kn > 0:
		return s.killed.div(s.kn), true, true
	}
	return point{}, false, false
}

// mapPlaces is the places of a map over the demos of the run.
type mapPlaces struct {
	demos  int
	places map[string]*placeStats
}

// addPlaces adds the places of the kills of m to those of its map.
func addPlaces(byMap map[string]*mapPlaces, m *demo.Match) {
	mp := byMap[m.Map]
	if mp == nil {
		mp = &mapPlaces{places: make(map[string]*placeStats)}
		byMap[m.Map] = mp
	}
	mp.demos++
	stats := func(place string) *placeStats {
		s := mp.places[place]
		if s == nil {
			s = &placeStats{}
			mp.places[place] = s
		}
		return s
	}
	for _, k := range m.Kills {
		if k.VictimPlace != "" {
			s := stats(k.VictimPlace)
			s.deaths++
			if p, ok := positionOf(k, k.Victim); ok {
				s.died, s.dn = s.died.add(p), s.dn+1
			}
		}
		if k.Killer != nil && k.KillerPlace != "" {
			s := stats(k.KillerPlace)
			s.kills++
			if p, ok := positionOf(k, *k.Killer); ok {
				s.killed, s.kn = s.killed.add(p), s.kn+1
			}
		}
	}
}

// positionOf returns where the body of r stood at the kill, if the kill
// tells.
func positionOf(k demo.Kill, r demo.Ref) (demo.Position, bool) {
	for _, p := range k.Positions {
		if p.Body.ID == r.ID && p.Body.Bot == r.Bot {
			return p, true
		}
	}
	return demo.Position{}, false
}

// placesReport prints the places of each map, by the ID of the map, and
// what dict, the dictionary of places by map, calls them. It reports
// whether the dictionary names every place.
func placesReport(w io.Writer, byMap map[string]*mapPlaces, dict map[string]content.Map) bool {
	complete := true
	for _, id := range slices.Sorted(maps.Keys(byMap)) {
		mp := byMap[id]
		known, ok := dict[id]
		head := fmt.Sprintf("%s, %d %s", id, mp.demos, plural(mp.demos, "demo", "demos"))
		if !ok {
			head += ", the map is not in places.yaml"
		}
		fmt.Fprintln(w, head+":")
		marks := make(map[string]point)
		for _, l := range landmarks {
			if s := mp.places[l]; s != nil {
				if at, _, ok := s.where(); ok {
					marks[l] = at
				}
			}
		}
		missing := 0
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, name := range slices.Sorted(maps.Keys(mp.places)) {
			s := mp.places[name]
			at, near := "at ?", ""
			if p, byKills, ok := s.where(); ok {
				at = fmt.Sprintf("at %d %d %d", round(p.x), round(p.y), round(p.z))
				if byKills {
					at += " by kills"
				}
				near = nearest(name, p, marks)
			}
			names := "MISSING"
			if t, ok := known.Places[name]; ok {
				names = t.RU + " / " + t.EN
			} else {
				missing++
			}
			fmt.Fprintf(tw, "  %s\tdeaths %d\tkills %d\t%s\t%s\t%s\n", name, s.deaths, s.kills, at, near, names)
		}
		tw.Flush()
		fmt.Fprintf(w, "%s: %d places, %d not in places.yaml\n\n", id, len(mp.places), missing)
		if missing > 0 {
			complete = false
		}
	}
	return complete
}

// nearest names the landmark nearest to the place at p, the place itself
// aside, with how far it is: "12 m from BombsiteB", "" if no other landmark
// is placed.
func nearest(place string, p point, marks map[string]point) string {
	best, d := "", math.Inf(1)
	for _, l := range landmarks {
		at, ok := marks[l]
		if !ok || l == place {
			continue
		}
		if dl := p.dist(at); dl < d {
			best, d = l, dl
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf("%d m from %s", round(d*meterPerUnit), best)
}

// round rounds v to a whole number, -0 to 0.
func round(v float64) int { return int(math.Round(v)) }

// plural returns one for n of 1, many otherwise.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
