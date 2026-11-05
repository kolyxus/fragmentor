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

// The comparison with cs-demo-analyzer v1.13 (github.com/akiver/cs-demo-
// analyzer), the analyzer of CS Demo Manager. It reads decompressed demos
// only; for myDemo.dem
//
//	csda -demo-path=myDemo.dem -output=<folder> -format=json -source=faceit
//
// writes <folder>/myDemo.json, the file -csda <folder> reads for
// myDemo.dem.zst. How to build and run it, and why its counts differ from
// ours: docs/notes/cs-demo-analyzer.md and docs/DECISIONS.md.
//
// Its per-player clutch counts add every clutch of the match to the
// player's, so the clutches come from its list of clutches. The rounds where
// a count differs are told by recounting its lists round by round, as its
// pkg/api/player.go counts them.

// csdaMatch is the part of the JSON of cs-demo-analyzer the comparison reads.
type csdaMatch struct {
	Rounds []struct {
		Number int `json:"number"`
	} `json:"rounds"`
	Players  map[string]csdaPlayer `json:"players"` // by SteamID64
	Kills    []csdaKill            `json:"kills"`
	Clutches []csdaClutch          `json:"clutches"`
	Damages  []csdaDamage          `json:"damages"`
}

type csdaPlayer struct {
	SteamID     uint64  `json:"steamId"`
	Kills       int     `json:"killCount"`
	Deaths      int     `json:"deathCount"`
	Assists     int     `json:"assistCount"`
	Headshots   int     `json:"headshotCount"`
	Damage      int     `json:"healthDamage"`
	ADR         float64 `json:"averageDamagePerRound"`
	KAST        float64 `json:"kast"` // percent
	FirstKills  int     `json:"firstKillCount"`
	FirstDeaths int     `json:"firstDeathCount"`
	TradeKills  int     `json:"tradeKillCount"`
	TradeDeaths int     `json:"tradeDeathCount"`
	TwoKills    int     `json:"twoKillCount"`
	ThreeKills  int     `json:"threeKillCount"`
	FourKills   int     `json:"fourKillCount"`
	FiveKills   int     `json:"fiveKillCount"`
}

// csdaKill is a kill; a SteamID is 0 for the world, a bot or nobody, and a
// side is 2 for T and 3 for CT, 0 for the world.
type csdaKill struct {
	Round                int    `json:"roundNumber"`
	Killer               uint64 `json:"killerSteamId"`
	Victim               uint64 `json:"victimSteamId"`
	Assister             uint64 `json:"assisterSteamId"`
	KillerSide           int    `json:"killerSide"`
	VictimSide           int    `json:"victimSide"`
	KillerControllingBot bool   `json:"isKillerControllingBot"`
	VictimControllingBot bool   `json:"isVictimControllingBot"`
	TradeKill            bool   `json:"isTradeKill"`
	TradeDeath           bool   `json:"isTradeDeath"`
}

func (k csdaKill) suicide() bool  { return k.Killer == k.Victim }
func (k csdaKill) teamkill() bool { return k.KillerSide == k.VictimSide }

type csdaClutch struct {
	Round     int    `json:"roundNumber"`
	Opponents int    `json:"opponentCount"`
	Won       bool   `json:"hasWon"`
	Clutcher  uint64 `json:"clutcherSteamId"`
}

type csdaDamage struct {
	Attacker               uint64 `json:"attackerSteamId"`
	AttackerSide           int    `json:"attackerSide"`
	AttackerControllingBot bool   `json:"isAttackerControllingBot"`
	Victim                 uint64 `json:"victimSteamId"`
	VictimSide             int    `json:"victimSide"`
	HitGroup               int    `json:"hitgroup"` // 1 is the head
	WeaponType             string `json:"weaponType"`
}

// csdaHead is the hit group of the head in cs-demo-analyzer's JSON.
const csdaHead = 1

// csdaBullet is its weapon types that count for the headshot rate by hits,
// as metrics counts it: guns without shotguns.
var csdaBullet = map[string]bool{"pistol": true, "smg": true, "rifle": true, "sniper": true, "machine_gun": true}

// csdaFile returns the JSON of cs-demo-analyzer for the demo path in dir.
func csdaFile(dir, path string) string {
	return filepath.Join(dir, strings.TrimSuffix(filepath.Base(path), ".dem.zst")+".json")
}

// compareCSDA compares the metrics of a demo with those cs-demo-analyzer
// exported for it to dir. It returns the report lines; the differences are
// expected, see DECISIONS.md, and are not mismatches.
func compareCSDA(dir, path string, m *demo.Match, players []metrics.Player) ([]string, error) {
	b, err := os.ReadFile(csdaFile(dir, path))
	if err != nil {
		return nil, fmt.Errorf("cs-demo-analyzer: %w", err)
	}
	var cm csdaMatch
	if err := json.Unmarshal(b, &cm); err != nil {
		return nil, fmt.Errorf("cs-demo-analyzer: decode %s: %w", filepath.Base(csdaFile(dir, path)), err)
	}
	return csdaReport(m, players, &cm), nil
}

// roundCounts is what a player did in a round, by our rules or by
// cs-demo-analyzer's.
type roundCounts struct {
	kast         string // "1" or "0", "-" when the round does not count
	opening      string // "W" won, "L" lost, "" none
	tradeKills   int
	tradedDeaths int
	kills        int // the kills multikills count
	clutches     [5]metrics.Clutch
}

func csdaReport(m *demo.Match, players []metrics.Player, cm *csdaMatch) []string {
	var lines []string
	addf := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	addf("  cs-demo-analyzer: %d rounds, ours %d", len(cm.Rounds), len(m.Rounds))

	ours := oursByRound(m)
	theirs := csdaByRound(cm)
	names := make(map[uint64]string)
	for _, p := range m.Players {
		names[p.SteamID64] = p.Name
	}
	differing := 0
	for _, p := range players {
		s := p.Match
		cp, ok := cm.Players[fmt.Sprint(p.SteamID64)]
		if !ok {
			addf("    %-16s not in cs-demo-analyzer's players", names[p.SteamID64])
			differing++
			continue
		}
		differ := false
		mark := func(d bool) string {
			if d {
				differ = true
				return "*"
			}
			return ""
		}
		var clutches [5]metrics.Clutch
		var bullet, head int
		for _, rc := range theirs {
			for i, c := range rc[p.SteamID64].clutches {
				clutches[i].Attempts += c.Attempts
				clutches[i].Wins += c.Wins
			}
		}
		for _, d := range cm.Damages {
			if d.Attacker == p.SteamID64 && !d.AttackerControllingBot && d.Victim != 0 && d.AttackerSide != d.VictimSide && csdaBullet[d.WeaponType] {
				bullet++
				if d.HitGroup == csdaHead {
					head++
				}
			}
		}
		multi := [4]int{cp.TwoKills, cp.ThreeKills, cp.FourKills, cp.FiveKills}
		addf("    %-16s K/D/A %d/%d/%d %d/%d/%d%s  HS %d/%d%s  damage %d/%d%s  ADR %.1f/%.1f%s  KAST %.1f/%.1f%s",
			names[p.SteamID64], s.Kills, s.Deaths, s.Assists, cp.Kills, cp.Deaths, cp.Assists,
			mark(s.Kills != cp.Kills || s.Deaths != cp.Deaths || s.Assists != cp.Assists),
			s.HeadshotKills, cp.Headshots, mark(s.HeadshotKills != cp.Headshots),
			s.Damage, cp.Damage, mark(s.Damage != cp.Damage),
			s.ADR, cp.ADR, mark(math.Abs(s.ADR-cp.ADR) >= 0.05),
			100*s.KAST, cp.KAST, mark(math.Abs(100*s.KAST-cp.KAST) >= 0.05))
		addf("    %-16s opening %d:%d %d:%d%s  trades %d/%d%s  traded %d/%d%s  multikills %v %v%s  clutches %s %s%s  head hits %d of %d, %d of %d%s",
			"", s.OpeningWins, s.OpeningAttempts-s.OpeningWins, cp.FirstKills, cp.FirstDeaths,
			mark(s.OpeningWins != cp.FirstKills || s.OpeningAttempts-s.OpeningWins != cp.FirstDeaths),
			s.TradeKills, cp.TradeKills, mark(s.TradeKills != cp.TradeKills),
			s.TradedDeaths, cp.TradeDeaths, mark(s.TradedDeaths != cp.TradeDeaths),
			s.MultiKills, multi, mark(s.MultiKills != multi),
			clutchText(s.Clutches), clutchText(clutches), mark(s.Clutches != clutches),
			s.HeadHits, s.BulletHits, head, bullet, mark(s.HeadHits != head || s.BulletHits != bullet))
		if differ {
			differing++
		}
	}

	// The rounds where a player's counts differ.
	type field struct {
		name string
		get  func(roundCounts) string
	}
	fields := []field{
		{"KAST", func(c roundCounts) string { return c.kast }},
		{"opening duel", func(c roundCounts) string { return c.opening }},
		{"trade kills", func(c roundCounts) string { return fmt.Sprint(c.tradeKills) }},
		{"traded deaths", func(c roundCounts) string { return fmt.Sprint(c.tradedDeaths) }},
		{"kills", func(c roundCounts) string { return fmt.Sprint(c.kills) }},
		{"clutch", func(c roundCounts) string { return clutchText(c.clutches) }},
	}
	numbers := make([]int, 0, len(m.Rounds))
	for _, r := range m.Rounds {
		numbers = append(numbers, r.Number)
	}
	for _, f := range fields {
		var parts []string
		for _, p := range players {
			var rounds []string
			for _, n := range numbers {
				a, b := f.get(ours[n][p.SteamID64]), f.get(theirs[n][p.SteamID64])
				if a != b {
					rounds = append(rounds, fmt.Sprintf("r%d %s/%s", n, show(a), show(b)))
				}
			}
			if len(rounds) > 0 {
				parts = append(parts, fmt.Sprintf("%s: %s", names[p.SteamID64], strings.Join(rounds, ", ")))
			}
		}
		if len(parts) > 0 {
			addf("    %s differs, ours/csda: %s", f.name, strings.Join(parts, "; "))
		}
	}
	for _, p := range players {
		if cp, ok := cm.Players[fmt.Sprint(p.SteamID64)]; ok && !csdaAddsUp(cp, cm, theirs, p.SteamID64) {
			addf("    %s: the totals of cs-demo-analyzer do not add up from its lists: its format or rules changed", names[p.SteamID64])
		}
	}
	addf("  cs-demo-analyzer: %d of %d players differ", differing, len(players))
	return lines
}

// csdaAddsUp reports whether the recount of cs-demo-analyzer's lists round
// by round adds up to its own totals of the player id: the rounds the
// report shows are then the rounds its totals come from.
func csdaAddsUp(cp csdaPlayer, cm *csdaMatch, theirs map[int]map[uint64]roundCounts, id uint64) bool {
	var wins, losses, trades, traded, kast int
	var multi [4]int
	for _, r := range cm.Rounds {
		c := theirs[r.Number][id]
		switch c.opening {
		case "W":
			wins++
		case "L":
			losses++
		}
		trades += c.tradeKills
		traded += c.tradedDeaths
		if c.kast == "1" {
			kast++
		}
		if c.kills >= 2 {
			multi[min(c.kills, 5)-2]++
		}
	}
	kastPct := 0.0
	if len(cm.Rounds) > 0 {
		kastPct = 100 * float64(kast) / float64(len(cm.Rounds))
	}
	return wins == cp.FirstKills && losses == cp.FirstDeaths && trades == cp.TradeKills && traded == cp.TradeDeaths &&
		multi == [4]int{cp.TwoKills, cp.ThreeKills, cp.FourKills, cp.FiveKills} && math.Abs(kastPct-cp.KAST) < 0.05
}

func show(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// clutchText shows clutches as "1v2 1:0 1v3 0:1", won and lost, "" for
// none.
func clutchText(cs [5]metrics.Clutch) string {
	var parts []string
	for i, c := range cs {
		if c.Attempts > 0 {
			parts = append(parts, fmt.Sprintf("1v%d %d:%d", i+1, c.Wins, c.Attempts-c.Wins))
		}
	}
	return strings.Join(parts, " ")
}

// oursByRound counts our metrics round by round: the metrics of a match of
// that round alone.
func oursByRound(m *demo.Match) map[int]map[uint64]roundCounts {
	out := make(map[int]map[uint64]roundCounts)
	for _, r := range m.Rounds {
		one := *m
		one.Rounds = []demo.Round{r}
		one.Kills = slices.DeleteFunc(slices.Clone(m.Kills), func(k demo.Kill) bool { return k.Round != r.Number })
		one.Hits = slices.DeleteFunc(slices.Clone(m.Hits), func(h demo.Hit) bool { return h.Round != r.Number })
		counts := make(map[uint64]roundCounts)
		for _, p := range metrics.Compute(&one) {
			s := p.Match
			c := roundCounts{kast: "-", tradeKills: s.TradeKills, tradedDeaths: s.TradedDeaths, kills: s.Kills, clutches: s.Clutches}
			if s.RoundsPlayed > 0 {
				c.kast = fmt.Sprint(s.KASTRounds)
			}
			switch {
			case s.OpeningWins > 0:
				c.opening = "W"
			case s.OpeningAttempts > 0:
				c.opening = "L"
			}
			counts[p.SteamID64] = c
		}
		out[r.Number] = counts
	}
	return out
}

// csdaByRound recounts cs-demo-analyzer's lists round by round, by its rules.
func csdaByRound(cm *csdaMatch) map[int]map[uint64]roundCounts {
	kills := make(map[int][]csdaKill)
	for _, k := range cm.Kills {
		kills[k.Round] = append(kills[k.Round], k)
	}
	out := make(map[int]map[uint64]roundCounts)
	for _, r := range cm.Rounds {
		n := r.Number
		counts := make(map[uint64]roundCounts)
		for _, p := range cm.Players {
			id := p.SteamID
			c := roundCounts{}
			// KAST: teamkills, suicides and leaves are skipped altogether.
			kast, survived := false, true
			for _, k := range kills[n] {
				if k.teamkill() {
					continue
				}
				if k.Assister == id {
					kast = true
					continue
				}
				if k.Killer == id && k.Victim != id {
					kast = true
					continue
				}
				if k.Victim == id {
					survived = false
					if k.TradeDeath {
						kast = true
					}
				}
			}
			c.kast = "0"
			if kast || survived {
				c.kast = "1"
			}
			for _, k := range kills[n] {
				if k.KillerControllingBot || k.suicide() || k.teamkill() {
					continue
				}
				if k.Killer == id {
					c.opening = "W"
				} else if k.Victim == id {
					c.opening = "L"
				}
				break
			}
			for _, k := range kills[n] {
				if k.suicide() || k.teamkill() {
					continue
				}
				if k.Killer == id && !k.KillerControllingBot {
					c.kills++
					if k.TradeKill {
						c.tradeKills++
					}
				}
				if k.Victim == id && !k.VictimControllingBot && k.TradeDeath {
					c.tradedDeaths++
				}
			}
			for _, cl := range cm.Clutches {
				if cl.Round == n && cl.Clutcher == id && cl.Opponents >= 1 && cl.Opponents <= 5 {
					c.clutches[cl.Opponents-1].Attempts++
					if cl.Won {
						c.clutches[cl.Opponents-1].Wins++
					}
				}
			}
			counts[id] = c
		}
		out[n] = counts
	}
	return out
}
