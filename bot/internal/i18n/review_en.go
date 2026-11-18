package i18n

var reviewEN = ReviewTexts{
	Header:     "%[1]s, %[2]d:%[3]d, %[4]s",
	Win:        "a win",
	Loss:       "a loss",
	Draw:       "a draw",
	Scoreboard: "K/D/A %[1]d/%[2]d/%[3]d · ADR %[4]s · KAST %[5]s",
	Suspicious: "The summary of this demo may be wrong: its rounds do not add up to the score. Check the numbers below.",

	Mistakes:   "Mistakes:",
	NoMistakes: "No clear mistakes in this match: in nothing are you worse than the lobby by more than chance would explain.",
	Strength:   "Strength: %s",
	Task:       "Drill of the week: %s",
	Progress:   "The next reviews will show your progress in “%[1]s”: %[2]d of %[3]s in this match.",

	Share:    "%[1]s of %[2]s; the lobby's share (%[3]s) would give %[4]s — %[5]s more.",
	PerRound: "%[1]s in %[2]s; the lobby's rate (%[3]s a round) would give %[4]s — %[5]s more.",
	Spot:     "%[1]s (%[2]s) — %[3]d of %[4]s there untraded; the others average %[5]s in their worst place — %[6]s more for you.",
	Habit:    "This keeps happening: your past matches show the same.",
	With:     "Related findings: %s.",
	WithItem: "%[1]s: %[2]s",

	Moment:       "round %[1]d, %[2]s: %[3]s — demo_gototick %[4]d",
	MomentNoGoto: "round %[1]d, %[2]s: %[3]s",
	Bomb:         "bomb %s",
	Death:        "%[1]s ← %[2]s",
	Kill:         "%[1]s → %[2]s",
	Teammate:     "nearest teammate %s m away",
	Alive:        "%[1]dv%[2]d",
	Left:         "left %[1]s ($%[2]d)",
	TeammateDied: "your teammate died: %s",
	NoShot:       "you saw the killer and did not shoot",
	Missed:       "you shot and missed",
	Hit:          "you hit him but did not kill",
	Blinded:      "enemies blinded: %[1]d, teammates: %[2]d",
	BlindedNone:  "blinded no one",
	OnTheMove:    "%[1]d of %[2]d on the move (%[3]s)",
	FirstMiss:    "the first shot missed (%s)",
	Kills:        "%d kills",
	Clutch:       "1v%d",
	GrenadeHit:   "%[1]s: %[2]d damage to %[3]d enemies",
	HE:           "HE grenade",
	Fire:         "fire",

	Rounds: [3]string{"round", "rounds", "rounds"},
	Rules: map[string]RuleText{
		"untraded_death": {
			Title: "Untraded deaths", Short: "untraded",
			Of: [3]string{"death", "deaths", "deaths"},
		},
		"lone_death": {
			Title: "Deaths alone", Short: "alone",
			Of: [3]string{"death", "deaths", "deaths"},
		},
		"opening_loss": {
			Title: "Opening duels lost", Short: "opening duels",
			Of: [3]string{"opening duel", "opening duels", "opening duels"},
		},
		"advantage_death": {
			Title: "Deaths with more of your team alive", Short: "with more of you alive",
			Of: [3]string{"death", "deaths", "deaths"},
		},
		"dry_death": {
			Title: "Deaths without damage, untraded", Short: "without damage",
			Of: [3]string{"death", "deaths", "deaths"},
		},
		"unused_grenades": {
			Title: "Deaths with grenades unused", Short: "with grenades",
			Of: [3]string{"death", "deaths", "deaths"},
		},
		"bad_flashes": {
			Title: "Flashes that missed or blinded teammates", Short: "flashes",
			Of: [3]string{"flashbang", "flashbangs", "flashbangs"},
			Variants: map[string]string{
				"empty": "Flashes that blinded no enemy",
				"team":  "Flashes that blinded your teammates",
			},
		},
		"moving_shots": {
			Title: "Shots on the move", Short: "on the move",
			Of: [3]string{"shot at an enemy in sight", "shots at an enemy in sight", "shots at an enemy in sight"},
		},
		"first_shot": {
			Title: "Fights opened with a miss", Short: "first bullet",
			Of: [3]string{"fight", "fights", "fights"},
		},
		"missed_trade": {
			Title: "Missed trades", Short: "missed trades",
			Of: [3]string{"chance to trade", "chances to trade", "chances to trade"},
			Variants: map[string]string{
				"no_shot": "Missed trades: you saw the killer and did not shoot",
				"no_kill": "Missed trades: you shot but did not kill",
			},
		},
		"death_spot": {
			Title: "The same death spot", Short: "in one place",
			Of: [3]string{"death", "deaths", "deaths"},
		},
		"opening_wins": {
			Title: "Opening duels won", Short: "opening duels",
			Of: [3]string{"opening duel", "opening duels", "opening duels"},
		},
		"multikills": {
			Title: "Rounds with three kills or more", Short: "multikills",
			Of: [3]string{"round", "rounds", "rounds"},
		},
		"clutches": {
			Title: "Clutches won (a 1vX counts as X)", Short: "clutches",
			Of: [3]string{"round", "rounds", "rounds"},
		},
		"useful_grenades": {
			Title: "Useful grenades", Short: "grenades",
			Of: [3]string{"round", "rounds", "rounds"},
			Variants: map[string]string{
				"flashes": "Enemies blinded and flash assists",
				"damage":  "Grenade damage",
			},
		},
		"trade_kills": {
			Title: "Trades for your teammates", Short: "trades",
			Of: [3]string{"teammate's death", "teammates' deaths", "teammates' deaths"},
		},
	},
	Grenades: map[string]string{
		"Flashbang":          "a flashbang",
		"HE Grenade":         "an HE grenade",
		"Smoke Grenade":      "a smoke",
		"Molotov":            "a molotov",
		"Incendiary Grenade": "an incendiary",
		"Decoy Grenade":      "a decoy",
	},
	Sides: map[string]string{"T": "on T", "CT": "on CT"},
}
