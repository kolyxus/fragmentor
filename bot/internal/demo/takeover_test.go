package demo

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs/msg"
)

// takeoverFile is testdata/takeover.json: raw player_death and player_hurt
// events of FACEIT demos in which a dead player took over a bot, with what
// the game state answered about them. Its field "about" tells where they
// come from. A player is his user ID on the server, a bot "bot <user ID>".
type takeoverFile struct {
	Events []struct {
		What string `json:"what"`
		Map  string `json:"map"`
		Tick int    `json:"tick"`
		Name string `json:"name"`
		// Data is the fields of the raw event that name players: user IDs
		// (short) and pawn handles (long), as the demo has them.
		Data map[string]struct {
			Short *int32 `json:"short"`
			Long  *int32 `json:"long"`
		} `json:"data"`
		// Library is the players the library named in its event, by role;
		// Pawns, whose pawn each handle was; Controls, which bot each
		// player controlled; Want, whose bodies the event names, by role.
		Library  map[string]string `json:"library"`
		Pawns    map[string]string `json:"pawns"`
		Controls map[string]string `json:"controls"`
		Want     map[string]string `json:"want"`
	} `json:"events"`
}

// fakePawns answers what the game state of the demo answered.
type fakePawns struct {
	handles    map[uint64]*common.Player
	controlled map[*common.Player]*common.Player // a player -> the bot he controls
}

func (f fakePawns) byPawn(handle uint64) *common.Player { return f.handles[handle] }

func (f fakePawns) controls(player, bot *common.Player) bool { return f.controlled[player] == bot }

// TestTakeoverEvents feeds deathBodies and hurtBodies the raw events of
// testdata/takeover.json. The fields that name players are named as the
// demos name them, so a wrong name in the parser, like attacker_pawn, fails
// here and not only against FACEIT in make verify-demos.
func TestTakeoverEvents(t *testing.T) {
	b, err := os.ReadFile("testdata/takeover.json")
	if err != nil {
		t.Fatal(err)
	}
	var f takeoverFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Events) < 8 {
		t.Fatalf("%d events in the fixture, want 8", len(f.Events))
	}
	for _, e := range f.Events {
		t.Run(fmt.Sprintf("%s@%d", e.Map, e.Tick), func(t *testing.T) {
			players := make(map[string]*common.Player)
			labels := make(map[*common.Player]string)
			player := func(label string) *common.Player {
				if label == "" {
					return nil
				}
				if p := players[label]; p != nil {
					return p
				}
				p := &common.Player{}
				id, bot := strings.CutPrefix(label, "bot ")
				n, err := strconv.Atoi(id)
				if err != nil {
					t.Fatalf("player %q", label)
				}
				p.UserID, p.IsBot = n, bot
				if !bot {
					p.SteamID64 = 76561190000000100 + uint64(n)
				}
				players[label], labels[p] = p, label
				return p
			}
			fake := fakePawns{handles: make(map[uint64]*common.Player), controlled: make(map[*common.Player]*common.Player)}
			for h, label := range e.Pawns {
				n, err := strconv.ParseInt(h, 10, 32)
				if err != nil {
					t.Fatalf("handle %q", h)
				}
				// As the parser reads a handle: GetValLong is an int32.
				fake.handles[uint64(int32(n))] = player(label)
			}
			for p, bot := range e.Controls {
				fake.controlled[player(p)] = player(bot)
			}
			data := make(map[string]*msg.CMsgSource1LegacyGameEventKeyT)
			for key, v := range e.Data {
				data[key] = &msg.CMsgSource1LegacyGameEventKeyT{ValShort: v.Short, ValLong: v.Long}
			}

			c := &collector{pawns: fake}
			got := make(map[string]*common.Player)
			lib := make(map[string]*common.Player)
			for role, label := range e.Library {
				lib[role] = player(label)
			}
			switch e.Name {
			case "player_death":
				k := &events.Kill{Killer: lib["killer"], Victim: lib["victim"], Assister: lib["assister"]}
				got["killer"], got["victim"], got["assister"] = c.deathBodies(k, data)
			case "player_hurt":
				h := &events.PlayerHurt{Attacker: lib["attacker"], Player: lib["victim"]}
				got["attacker"], got["victim"] = c.hurtBodies(h, data)
			default:
				t.Fatalf("event %q", e.Name)
			}
			for role, body := range got {
				if labels[body] != e.Want[role] {
					t.Errorf("%s: the %s is %q, want %q", e.What, role, labels[body], e.Want[role])
					continue
				}
				// A bot that a player controls has him as its controller.
				var want uint64
				for p, bot := range fake.controlled {
					if bot == body {
						want = p.SteamID64
					}
				}
				if r := c.bodyRef(body, lib[role]); r != nil && r.Controller != want {
					t.Errorf("%s: the %s has the controller %d, want %d", e.What, role, r.Controller, want)
				}
			}
		})
	}
}
