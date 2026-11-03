package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAnonymizer(t *testing.T) {
	const (
		playerID = "1a2b3c4d-0000-4111-8222-123456789abc"
		matchID  = "1-9f8e7d6c-1111-4222-8333-abcdefabcdef"
		steamID  = "76561198012345678"
	)
	const avatar = "https://distribution.faceit-cdn.net/images/5e6f7a8b-2222-4333-8444-feedfacecafe.jpg"
	player := decode(t, `{
		"player_id": "`+playerID+`", "nickname": "RealNick", "avatar": "`+avatar+`",
		"faceit_url": "https://www.faceit.com/{lang}/players/RealNick",
		"steam_id_64": "`+steamID+`", "new_steam_id": "[U:1:52079950]",
		"platforms": {"steam": "STEAM_0:0:26039975"}, "friends_ids": ["`+playerID+`"],
		"activated_at": "2017-05-01T10:00:00Z", "country": "ru",
		"games": {"cs2": {"game_player_id": "`+steamID+`", "game_player_name": "SteamName", "skill_level": 8}}
	}`)
	match := decode(t, `{
		"match_id": "`+matchID+`",
		"demo_url": ["https://demos/cs2/`+matchID+`-1-1.dem.zst"],
		"teams": {"faction1": {"faction_id": "`+playerID+`", "leader": "`+playerID+`", "name": "team_RealNick",
			"roster": [{"player_id": "`+playerID+`", "nickname": "RealNick", "game_player_id": "`+steamID+`", "avatar": "`+avatar+`"},
				{"player_id": "`+playerID+`", "avatar": "https://avatars.steamstatic.com/0123456789abcdef0123456789abcdef01234567_full.jpg"}]}},
		"voting": {"map": {"pick": ["de_mirage"], "entities": [{"name": "Mirage", "guid": "de_mirage"}]}}
	}`)

	a := newAnonymizer()
	p := encode(t, a.value("", player))
	m := encode(t, a.value("", match))

	for _, real := range []string{"RealNick", "SteamName", playerID, "9f8e7d6c", steamID, "52079950", "26039975", "5e6f7a8b", "steamstatic", "2017-05-01"} {
		if strings.Contains(p+m, real) {
			t.Errorf("%q is still there:\n%s\n%s", real, p, m)
		}
	}
	for _, want := range []string{
		`"player_id":"00000000-0000-4000-8000-000000000001"`,
		`"nickname":"player01"`,
		`"avatar":"https://distribution.faceit-cdn.net/images/avatar-0001.jpg"`,
		`"faceit_url":"https://www.faceit.com/{lang}/players/player01"`,
		`"steam_id_64":"76561190000000001"`,
		`"game_player_name":"player02"`,
		`"friends_ids":[]`,
		`"country":"ru"`,
		`"skill_level":8`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("player misses %s:\n%s", want, p)
		}
	}
	// The same real values get the same fakes in another file.
	for _, want := range []string{
		`"faction_id":"00000000-0000-4000-8000-000000000001"`,
		`"name":"team_player01"`,
		`"nickname":"player01"`,
		`"game_player_id":"76561190000000001"`,
		`"avatar":"https://distribution.faceit-cdn.net/images/avatar-0001.jpg"`,
		`"avatar":""`,
		`-1-1.dem.zst`,
		`"name":"Mirage"`,
		`"pick":["de_mirage"]`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("match misses %s:\n%s", want, m)
		}
	}
	if !strings.Contains(m, `"match_id":"1-00000000-0000-4000-8000-000000000002"`) {
		t.Errorf("match ID not replaced as expected:\n%s", m)
	}

	for name, data := range map[string]any{"player": player, "match": match} {
		if err := a.check(name, data); err != nil {
			t.Errorf("check() on anonymized %s: %v", name, err)
		}
	}
}

func TestAnonymizerCheckCatchesLeaks(t *testing.T) {
	a := newAnonymizer()
	a.value("", decode(t, `{"nickname": "RealNick", "player_id": "1a2b3c4d-0000-4111-8222-123456789abc"}`))

	for name, leak := range map[string]string{
		"nickname in an unknown field":    `{"comment": "realnick"}`,
		"nickname in a link":              `{"link": "https://www.faceit.com/en/players/RealNick/stats"}`,
		"ID inside a string":              `{"chat": "match-1a2b3c4d-0000-4111-8222-123456789ABC"}`,
		"nickname in an array of strings": `{"parameters": ["REALNICK"]}`,
	} {
		err := a.check(name, decode(t, leak))
		if err == nil {
			t.Errorf("%s: check() passed", name)
			continue
		}
		if strings.Contains(strings.ToLower(err.Error()), "realnick") || strings.Contains(err.Error(), "1a2b3c4d") {
			t.Errorf("%s: the error shows the real value: %v", name, err)
		}
	}
	// A real name that is a part of a fake ID is not a leak.
	b := newAnonymizer()
	b.nickname("4000")
	if err := b.check("ids", decode(t, `{"id": "00000000-0000-4000-8000-000000000001"}`)); err != nil {
		t.Errorf("check() on a fake ID: %v", err)
	}
}
