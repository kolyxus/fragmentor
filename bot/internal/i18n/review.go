package i18n

// ReviewTexts holds the texts of the template review of internal/review: plain
// text, with the numbers formatted by the review. Texts with % are fmt
// formats; the comment says what goes into them.
type ReviewTexts struct {
	// The head of the review.
	Header     string // %[1]s map, %[2]d:%[3]d score with the player's team first, %[4]s Win, Loss or Draw
	Win        string
	Loss       string
	Draw       string
	Scoreboard string // %[1]d kills, %[2]d deaths, %[3]d assists, %[4]s ADR, %[5]s KAST
	Suspicious string // the summary of the demo may be wrong

	// The parts of the review.
	Mistakes   string // the heading of the mistakes
	NoMistakes string // a match without a mistake to tell
	Strength   string // %s: the title of the strength
	Task       string // %s: the title of the drill
	Progress   string // %[1]s the title of its metric, %[2]d events of %[3]s, the opportunities with their noun

	// What a finding is against its base. %[1]s and the rest are numbers the
	// review formats.
	Share    string // %[1]s events of %[2]s, the opportunities with their noun; %[3]s the share of the lobby, %[4]s expected, %[5]s more
	PerRound string // %[1]s events in %[2]s, the rounds with their noun; %[3]s the lobby's a round, %[4]s expected, %[5]s more
	Spot     string // %[1]s place, %[2]s side, %[3]d untraded of %[4]s, the deaths there with their noun; %[5]s the lobby's, %[6]s more
	Habit    string // the past matches show the same
	With     string // %s: the findings nested with it, made of WithItem
	WithItem string // %[1]s the short title of a finding, %[2]s its events

	// A moment of the evidence.
	Moment       string // %[1]d round, %[2]s clock, %[3]s what happened, %[4]d the tick for demo_gototick
	MomentNoGoto string // %[1]d round, %[2]s clock, %[3]s what happened
	Bomb         string // %s: the clock of the bomb once it was planted
	Death        string // %[1]s where the player stood, %[2]s where his killer did
	Kill         string // %[1]s where the player stood, %[2]s where his victim did
	Teammate     string // %s meters to the nearest live teammate
	Alive        string // %[1]d alive on the player's side, %[2]d on the enemy's
	Left         string // %[1]s the grenades left, %[2]d their worth in dollars
	TeammateDied string // %s: where the teammate died
	NoShot       string // the player saw the killer of his teammate and did not shoot
	Missed       string // he shot at the killer and missed
	Hit          string // he hit the killer and did not kill him
	Blinded      string // %[1]d enemies and %[2]d teammates a flashbang blinded
	BlindedNone  string // a flashbang that blinded nobody
	OnTheMove    string // %[1]d of %[2]d shots on the move, %[3]s the gun
	FirstMiss    string // %s: the rifle whose first shot of the fight missed
	Kills        string // %d: the kills of the round
	Clutch       string // %d: the enemies of a 1vX clutch won
	GrenadeHit   string // %[1]s HE or Fire, %[2]d damage, %[3]d enemies hurt
	HE           string
	Fire         string

	// Rounds is the forms of the noun of rounds after a number: one, few,
	// many, see Plural.
	Rounds [3]string
	// Rules is the texts of each rule of internal/findings, by its ID.
	Rules map[string]RuleText
	// Grenades names the grenades, by their names in demo.Kill.Grenades.
	Grenades map[string]string
	// Sides names the sides a player died on: "T" and "CT".
	Sides map[string]string
}

// RuleText is the texts of a rule of the findings.
type RuleText struct {
	Title string // the finding
	Short string // the finding in the list of those nested with another
	// Of is the forms of the noun of the opportunities after a number, see
	// Plural: deaths, shots, flashbangs. A rule of events a round has none.
	Of [3]string
	// Variants titles the variants of the finding, by the variant.
	Variants map[string]string
}

// Plural returns the form of a noun for the number n: forms are the forms
// for one, a few and many, as Russian has them: смерть, смерти, смертей (1,
// 2–4, 5–20, and so on by the last digits). English needs one and many: the
// second form is for the rest.
func Plural(l Lang, n int, forms [3]string) string {
	if n < 0 {
		n = -n
	}
	if l != RU {
		if n == 1 {
			return forms[0]
		}
		return forms[1]
	}
	switch last, two := n%10, n%100; {
	case last == 1 && two != 11:
		return forms[0]
	case last >= 2 && last <= 4 && (two < 12 || two > 14):
		return forms[1]
	}
	return forms[2]
}
