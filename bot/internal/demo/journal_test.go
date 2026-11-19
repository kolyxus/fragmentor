package demo

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// journalMatch is a made-up match of two players a side, with every kind of
// event, a bot that player 3 controls and a stranger in a sighting.
func journalMatch() *Match {
	const (
		a1 = 76561190000000001
		a2 = 76561190000000002
		b1 = 76561190000000003
		b2 = 76561190000000004
		x  = 76561190000000099 // nobody of the players
	)
	p := func(id uint64, side Side) Ref { return Ref{ID: id, Side: side} }
	pp := func(id uint64, side Side) *Ref { r := p(id, side); return &r }
	bot := Ref{ID: 7, Bot: true, Side: T, Controller: b1}
	inBot := func() *Ref { r := bot; return &r } // player 3 kills and hits in the bot
	return &Match{
		Map: "de_mirage", TickRate: 64, MaxRounds: 24, RoundTime: 115, BombTime: 40,
		Teams: [2]Team{{Name: "team_a1", StartSide: CT, Score: 1}, {Name: "team_b1", StartSide: T}},
		Rounds: []Round{{Number: 1, Winner: 0, WinnerSide: CT, Reason: "ct_win", Score: [2]int{1, 0}, EndTick: 900, FreezeEndTick: 100,
			Alive:     []Ref{p(a1, CT), p(a2, CT), p(b1, T), bot},
			Equipment: []Loadout{{Body: p(a1, CT), Value: 850}, {Body: bot, Value: 200}}, BombPlantTick: 500, BombSite: "A"}},
		Players: []Player{
			{SteamID64: a1, Name: "alpha one", Team: 0, StartSide: CT, Kills: 2, Damage: 200},
			{SteamID64: a2, Name: "alpha two", Team: 0, StartSide: CT},
			{SteamID64: b1, Name: "bravo one", Team: 1, StartSide: T, Deaths: 1},
			{SteamID64: b2, Name: "bravo two", Team: 1, StartSide: T, Deaths: 1},
		},
		Warnings: []string{"a warning"},
		Kills: []Kill{
			{Tick: 200, Round: 1, Killer: pp(a1, CT), Victim: p(b1, T), Assister: pp(a2, CT), FlashAssist: true, Weapon: "AK-47", Class: ClassRifle,
				Grenades: []string{Flashbang}, VictimPlace: "TopofMid", KillerPlace: "Middle",
				Positions: []Position{{Body: p(a1, CT), X: 1, Y: 2, Z: 3}, {Body: p(b1, T), X: 4, Y: 5}, {Body: bot, X: 6}}},
			{Tick: 260, Round: 1, Killer: inBot(), Victim: p(a2, CT), Assister: pp(b2, T), Weapon: "MP9", Class: ClassSMG},
			{Tick: 300, Round: 1, Killer: pp(a1, CT), Victim: bot, Weapon: "AK-47", Class: ClassRifle},
			{Tick: 400, Round: 1, Victim: p(b2, T), Weapon: "World", Class: ClassWorld},
		},
		Hits: []Hit{
			{Tick: 199, Round: 1, Attacker: pp(a1, CT), Victim: p(b1, T), Damage: 100, HitGroup: HitHead, Weapon: "AK-47", Class: ClassRifle},
			{Tick: 260, Round: 1, Attacker: inBot(), Victim: p(a2, CT), Damage: 30, HitGroup: HitChest, Weapon: "MP9", Class: ClassSMG},
		},
		Shots:     []Shot{{Tick: 199, Round: 1, Shooter: p(a1, CT), Weapon: "AK-47", Class: ClassRifle, Speed: 12.5, MaxSpeed: 215}, {Tick: 250, Round: 1, Shooter: bot}},
		Sightings: []Sighting{{Round: 1, Spotter: p(a1, CT), Target: p(b1, T), From: 150, To: 200}, {Round: 1, Spotter: p(x, CT), Target: bot, From: 1, To: 2}},
		Grenades:  []Grenade{{ID: 1, Tick: 120, Round: 1, Thrower: p(a2, CT), Weapon: Flashbang}},
		Flashes:   []Flash{{Tick: 130, Round: 1, Thrower: pp(a2, CT), Victim: p(b1, T), Duration: 2.25, Grenade: 1}, {Tick: 131, Round: 1, Victim: p(b2, T), Duration: 1}},
	}
}

func TestAnonymize(t *testing.T) {
	m := journalMatch()
	before, _ := json.Marshal(m)
	anon, seats := Anonymize(m)

	if after, _ := json.Marshal(m); !bytes.Equal(before, after) {
		t.Error("Anonymize changed the match")
	}
	want := []uint64{76561190000000001, 76561190000000002, 76561190000000003, 76561190000000004}
	if len(seats) != len(want) {
		t.Fatalf("seats = %v", seats)
	}
	for i, id := range want {
		if seats[i] != id || anon.Players[i].SteamID64 != uint64(i+1) || anon.Players[i].Name != "" {
			t.Errorf("seat %d = %d, player %+v", i+1, seats[i], anon.Players[i])
		}
	}
	b, _ := json.Marshal(anon)
	for _, secret := range []string{"7656119", "alpha", "bravo", "team_a1", "team_b1"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("the anonymous match holds %q", secret)
		}
	}
	// The refs are the seats; the bot keeps its ID and gets the seat of its
	// controller; the stranger takes the seat after the players.
	k := anon.Kills[0]
	if k.Killer.ID != 1 || k.Victim.ID != 3 || k.Assister.ID != 2 || k.Positions[2] != (Position{Body: Ref{ID: 7, Bot: true, Side: T, Controller: 3}, X: 6}) {
		t.Errorf("kill = %+v, positions %+v", k, k.Positions)
	}
	inBot := Ref{ID: 7, Bot: true, Side: T, Controller: 3}
	if v := anon.Kills[2].Victim; v != inBot {
		t.Errorf("the bot = %+v", v)
	}
	// What player 3 does in the bot is the bot's, with his seat.
	if k := anon.Kills[1]; k.Killer == nil || *k.Killer != inBot || k.Assister.ID != 4 || k.Victim.ID != 2 {
		t.Errorf("the kill of the bot = %+v", k)
	}
	if h := anon.Hits[1].Attacker; h == nil || *h != inBot {
		t.Errorf("the hit of the bot = %+v", h)
	}
	if anon.Kills[3].Killer != nil || anon.Sightings[1].Spotter.ID != 5 || anon.Flashes[1].Thrower != nil || anon.Flashes[0].Thrower.ID != 2 ||
		anon.Rounds[0].Alive[2].ID != 3 || anon.Rounds[0].Equipment[1].Body.Controller != 3 || anon.Hits[0].Attacker.ID != 1 ||
		anon.Shots[1].Shooter.Controller != 3 || anon.Grenades[0].Thrower.ID != 2 {
		t.Errorf("the refs of the events: %+v", anon)
	}
	// The copy is deep: changing it leaves the match alone.
	anon.Kills[0].Killer.ID, anon.Kills[0].Positions[0].X, anon.Rounds[0].Alive[0].ID = 42, 42, 42
	if m.Kills[0].Killer.ID != 76561190000000001 || m.Kills[0].Positions[0].X != 1 || m.Rounds[0].Alive[0].ID != 76561190000000001 {
		t.Error("the anonymous match shares its events with the match")
	}
	// The rest is the same: the counts, the clock, the places.
	if anon.Map != m.Map || anon.Teams[0].Score != 1 || anon.Rounds[0].BombPlantTick != 500 || anon.Kills[0].VictimPlace != "TopofMid" ||
		anon.Flashes[0].Duration != 2.25 || anon.Shots[0].Speed != 12.5 || anon.Players[0].Damage != 200 {
		t.Errorf("anonymous match = %+v", anon)
	}
}

func TestJournal(t *testing.T) {
	anon, _ := Anonymize(journalMatch())
	b, err := EncodeJournal(anon)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte{0x28, 0xb5, 0x2f, 0xfd}) {
		t.Errorf("the journal is no zstd: % x", b[:4])
	}
	back, err := DecodeJournal(b)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(anon)
	if got, _ := json.Marshal(back); !bytes.Equal(got, want) {
		t.Errorf("the journal changed the match:\n%s\nwant\n%s", got, want)
	}

	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	// A match of n bytes and more, all of them its map.
	large := func(n int) []byte { return []byte(`{"map":"` + strings.Repeat("x", n) + `"}`) }
	// A journal larger than those of real matches, up to 1.2 MiB, reads.
	if m, err := DecodeJournal(enc.EncodeAll(large(2<<20), nil)); err != nil || len(m.Map) != 2<<20 {
		t.Errorf("a journal of 2 MiB: err = %v", err)
	}
	for name, in := range map[string][]byte{
		"no zstd":         []byte("{}"),
		"no JSON":         enc.EncodeAll([]byte("not JSON"), nil),
		"a truncated one": b[:len(b)/2],
		"too large":       enc.EncodeAll(large(maxJournal), nil),
	} {
		if _, err := DecodeJournal(in); !errors.Is(err, ErrBadJournal) {
			t.Errorf("%s: err = %v, want ErrBadJournal", name, err)
		}
	}
}

// checkJournal checks the journal of a real match: it holds no SteamID and
// no name, and gives the match back. It returns the size of the journal.
func checkJournal(t *testing.T, m *Match) int {
	t.Helper()
	anon, seats := Anonymize(m)
	if len(seats) != len(m.Players) {
		t.Fatalf("%d seats of %d players", len(seats), len(m.Players))
	}
	b, err := EncodeJournal(anon)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeJournal(b)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(back)
	if want, _ := json.Marshal(anon); !bytes.Equal(raw, want) {
		t.Error("the journal does not give the match back")
	}
	for _, p := range m.Players {
		for _, secret := range []string{strconv.FormatUint(p.SteamID64, 10), `"` + p.Name + `"`} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Errorf("the journal holds %s", secret)
			}
		}
	}
	for _, team := range m.Teams {
		if team.Name != "" && bytes.Contains(raw, []byte(`"`+team.Name+`"`)) {
			t.Errorf("the journal holds the team %s", team.Name)
		}
	}
	return len(b)
}
