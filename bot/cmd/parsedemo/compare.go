package main

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
)

// compare checks a demo against the FACEIT match. The score of each team,
// with the halves and the overtime, and every player's K/D/A and team must
// match; headshots and damage are shown for reference and marked with * when
// they differ. Players are matched by SteamID64, which the rosters of the
// match details give for every FACEIT player. It returns the report lines and
// the number of mismatches.
func compare(m *demo.Match, details faceit.Match, stats faceit.MatchStats) ([]string, int) {
	var (
		lines      []string
		mismatches int
	)
	add := func(ok bool, format string, args ...any) {
		mark := "ok"
		if !ok {
			mark = "MISMATCH"
			mismatches++
		}
		lines = append(lines, fmt.Sprintf(format, args...)+"  "+mark)
	}

	ms, found := mapStats(stats, m.Map)
	if !found {
		add(false, "  FACEIT has no stats of %s", m.Map)
		return lines, mismatches
	}
	if ms.Map != m.Map {
		add(false, "  map %s, FACEIT %s", m.Map, ms.Map)
	}

	steamOf := make(map[string]string) // FACEIT player ID -> SteamID64
	for _, t := range details.Teams {
		for _, p := range t.Players {
			steamOf[p.ID] = p.SteamID64
		}
	}
	ours := make(map[string]demo.Player) // by SteamID64
	for _, p := range m.Players {
		ours[strconv.FormatUint(p.SteamID64, 10)] = p
	}

	seen := make(map[string]bool)
	for _, ft := range ms.Teams {
		team, ok := ourTeam(ft, steamOf, ours)
		if !ok {
			add(false, "  %s: none of its players is in the demo", ft.Name)
			continue
		}
		t := m.Teams[team]
		add(t.Score == ft.FinalScore && t.FirstHalf == ft.FirstHalf && t.SecondHalf == ft.SecondHalf && t.Overtime == ft.Overtime,
			"  %-20s score %2d (%d+%d+%d)  FACEIT %2d (%d+%d+%d)",
			ft.Name, t.Score, t.FirstHalf, t.SecondHalf, t.Overtime, ft.FinalScore, ft.FirstHalf, ft.SecondHalf, ft.Overtime)

		players := slices.Clone(ft.Players)
		slices.SortFunc(players, func(a, b faceit.PlayerStats) int {
			return cmp.Or(cmp.Compare(b.Kills, a.Kills), cmp.Compare(a.Nickname, b.Nickname))
		})
		for _, fp := range players {
			sid := steamOf[fp.ID]
			p, ok := ours[sid]
			if !ok {
				add(false, "    %-16s not in the demo", fp.Nickname)
				continue
			}
			seen[sid] = true
			add(p.Kills == fp.Kills && p.Deaths == fp.Deaths && p.Assists == fp.Assists && p.Team == team,
				"    %-16s K/D/A %2d/%2d/%2d  FACEIT %2d/%2d/%2d  HS %s  damage %s",
				fp.Nickname, p.Kills, p.Deaths, p.Assists, fp.Kills, fp.Deaths, fp.Assists,
				pair(p.Headshots, fp.Headshots), pair(p.Damage, fp.Damage))
		}
	}
	for _, p := range m.Players {
		if !seen[strconv.FormatUint(p.SteamID64, 10)] {
			add(false, "    %-16s in the demo, not in the FACEIT stats", p.Name)
		}
	}
	if mismatches == 0 {
		lines = append(lines, "  result: ok")
	} else {
		lines = append(lines, fmt.Sprintf("  result: %d mismatches", mismatches))
	}
	return lines, mismatches
}

// mapStats returns the FACEIT stats of the map, or of the only map played.
func mapStats(stats faceit.MatchStats, name string) (faceit.MapStats, bool) {
	for _, ms := range stats.Maps {
		if ms.Map == name {
			return ms, true
		}
	}
	if len(stats.Maps) == 1 {
		return stats.Maps[0], true
	}
	return faceit.MapStats{}, false
}

// ourTeam returns the index of the demo's team that most players of the
// FACEIT team play for.
func ourTeam(ft faceit.TeamStats, steamOf map[string]string, ours map[string]demo.Player) (int, bool) {
	var votes [2]int
	for _, fp := range ft.Players {
		if p, ok := ours[steamOf[fp.ID]]; ok {
			votes[p.Team]++
		}
	}
	switch {
	case votes[0] == 0 && votes[1] == 0:
		return 0, false
	case votes[1] > votes[0]:
		return 1, true
	}
	return 0, true
}

// pair shows our number and FACEIT's, with * if they differ.
func pair(ours, theirs int) string {
	s := fmt.Sprintf("%d/%d", ours, theirs)
	if ours != theirs {
		s += "*"
	}
	return s
}
