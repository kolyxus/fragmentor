package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
)

// A one-round match of a player a side: a1 on CT kills b1 with a headshot.
func csdaFixture() (*demo.Match, string) {
	a := demo.Ref{ID: 76561190000000001, Side: demo.CT}
	b := demo.Ref{ID: 76561190000000002, Side: demo.T}
	m := &demo.Match{
		TickRate: 64,
		Players: []demo.Player{
			{SteamID64: a.ID, Name: "a1", Team: 0, Kills: 1, Headshots: 1, Damage: 100},
			{SteamID64: b.ID, Name: "b1", Team: 1, Deaths: 1},
		},
		Rounds: []demo.Round{{Number: 1, Winner: 0, WinnerSide: demo.CT, EndTick: 200, Alive: []demo.Ref{a, b}}},
		Kills:  []demo.Kill{{Tick: 100, Round: 1, Killer: &a, Victim: b, Headshot: true, Class: demo.ClassRifle}},
		Hits:   []demo.Hit{{Tick: 100, Round: 1, Attacker: &a, Victim: b, Damage: 100, HitGroup: demo.HitHead, Class: demo.ClassRifle}},
	}
	// What cs-demo-analyzer exports for it. Its per-player clutch counts are
	// wrong and not read; the list of clutches is.
	js := `{
		"rounds": [{"number": 1}],
		"players": {
			"76561190000000001": {"steamId": 76561190000000001, "killCount": 1, "headshotCount": 1, "healthDamage": 100,
				"averageDamagePerRound": 100, "kast": 100, "firstKillCount": 1, "oneVsOneCount": 7},
			"76561190000000002": {"steamId": 76561190000000002, "deathCount": 1, "kast": 0, "firstDeathCount": 1}
		},
		"kills": [{"roundNumber": 1, "killerSteamId": 76561190000000001, "victimSteamId": 76561190000000002,
			"killerSide": 3, "victimSide": 2, "isHeadshot": true}],
		"clutches": [],
		"damages": [{"roundNumber": 1, "attackerSteamId": 76561190000000001, "victimSteamId": 76561190000000002,
			"attackerSide": 3, "victimSide": 2, "hitgroup": 1, "weaponType": "rifle", "healthDamage": 100}]
	}`
	return m, js
}

func writeCSDA(t *testing.T, js string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1-abc-1-1.json"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCompareCSDAAgrees(t *testing.T) {
	m, js := csdaFixture()
	lines, err := compareCSDA(writeCSDA(t, js), filepath.Join("testdata", "1-abc-1-1.dem.zst"), m, metrics.Compute(m))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "0 of 2 players differ") || strings.Contains(text, "*") || strings.Contains(text, " differs, ") {
		t.Errorf("want no differences:\n%s", text)
	}
	for _, want := range []string{
		"a1               K/D/A 1/0/0 1/0/0  HS 1/1  damage 100/100  ADR 100.0/100.0  KAST 100.0/100.0",
		"opening 1:0 1:0  trades 0/0  traded 0/0",
		"head hits 1 of 1, 1 of 1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in:\n%s", want, text)
		}
	}
}

func TestCompareCSDAFindsRounds(t *testing.T) {
	m, js := csdaFixture()
	// cs-demo-analyzer has a clutch of b1 in its list.
	js = strings.Replace(js, `"clutches": []`, `"clutches": [{"roundNumber": 1, "opponentCount": 1, "hasWon": false, "clutcherSteamId": 76561190000000002}]`, 1)
	// And it saw the kill as a teamkill, as its totals say: no kill, no
	// opening duel, and its KAST skips the kill, so b1 survived.
	js = strings.Replace(js, `"killerSide": 3, "victimSide": 2, "isHeadshot"`, `"killerSide": 2, "victimSide": 2, "isHeadshot"`, 1)
	js = strings.Replace(js, `"killCount": 1, "headshotCount": 1`, `"killCount": 0, "headshotCount": 0`, 1)
	js = strings.Replace(js, `"kast": 100, "firstKillCount": 1`, `"kast": 100, "firstKillCount": 0`, 1)
	js = strings.Replace(js, `"kast": 0, "firstDeathCount": 1`, `"kast": 100, "firstDeathCount": 0`, 1)
	lines, err := compareCSDA(writeCSDA(t, js), "1-abc-1-1.dem.zst", m, metrics.Compute(m))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{
		"a1               K/D/A 1/0/0 0/0/0*  HS 1/0*",
		"b1               K/D/A 0/1/0 0/1/0  HS 0/0  damage 0/0  ADR 0.0/0.0  KAST 0.0/100.0*",
		"KAST differs, ours/csda: b1: r1 0/1",
		"opening duel differs, ours/csda: a1: r1 W/-; b1: r1 L/-",
		"kills differs, ours/csda: a1: r1 1/0",
		"clutch differs, ours/csda: b1: r1 -/1v1 0:1",
		"2 of 2 players differ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "do not add up") {
		t.Errorf("the totals add up from the lists:\n%s", text)
	}

	// Totals that the lists do not make are told.
	js = strings.Replace(js, `"kast": 100, "firstDeathCount": 0`, `"kast": 0, "firstDeathCount": 0`, 1)
	lines, err = compareCSDA(writeCSDA(t, js), "1-abc-1-1.dem.zst", m, metrics.Compute(m))
	if err != nil {
		t.Fatal(err)
	}
	if text := strings.Join(lines, "\n"); !strings.Contains(text, "b1: the totals of cs-demo-analyzer do not add up from its lists") {
		t.Errorf("no word of the totals of b1:\n%s", text)
	}
}

func TestCompareCSDAMissing(t *testing.T) {
	m, _ := csdaFixture()
	if _, err := compareCSDA(t.TempDir(), "1-abc-1-1.dem.zst", m, metrics.Compute(m)); err == nil || !strings.Contains(err.Error(), "cs-demo-analyzer") {
		t.Errorf("err = %v, want a missing file of cs-demo-analyzer", err)
	}
	dir := writeCSDA(t, "{not json")
	if _, err := compareCSDA(dir, "1-abc-1-1.dem.zst", m, metrics.Compute(m)); err == nil || !strings.Contains(err.Error(), "decode 1-abc-1-1.json") {
		t.Errorf("err = %v, want a decode error", err)
	}
}
