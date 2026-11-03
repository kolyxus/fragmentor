// Package i18n holds every user-facing text of the bot in Russian and English.
package i18n

import "strings"

// Lang is a language the bot speaks.
type Lang string

const (
	RU Lang = "ru"
	EN Lang = "en"
)

// Langs lists the supported languages.
var Langs = []Lang{RU, EN}

// FromTelegram picks the bot language for a Telegram language_code: Russian
// for "ru" and its regional variants, English for everything else, including
// an empty code.
func FromTelegram(languageCode string) Lang {
	code := strings.ToLower(strings.TrimSpace(languageCode))
	primary, _, _ := strings.Cut(strings.ReplaceAll(code, "_", "-"), "-")
	if primary == "ru" {
		return RU
	}
	return EN
}

// Messages holds the texts of one language. Texts with % are fmt formats;
// the comment says what goes into them.
type Messages struct {
	// Chat replies, plain text without parse mode.
	StartJoined  string // first /start: welcome and waitlist confirmation
	StartAlready string // /start from a user already on the waitlist
	Help         string
	Unknown      string // any message the bot does not understand
	Failure      string // internal error, the user should retry
	FaceitDown   string // FACEIT does not answer, the user should retry

	// /link: linking a FACEIT account.
	LinkAsk         string // /link without a nickname: asks for one
	LinkBadInput    string // neither a nickname nor a profile link
	LinkNotFound    string // %s: the nickname that was not found
	LinkNoCS2       string // %s: nickname of an account without CS2
	LinkAlready     string // %s: nickname of the account linked already
	LinkCard        string // "is this you?" card; %s nickname, %d level, %d ELO
	LinkCardReplace string // added to the card; %s: nickname of the account linked now
	LinkYes         string // card button
	LinkNo          string // card button
	LinkDone        string // %s: nickname of the account just linked
	LinkDeclined    string // after the No button
	LinkGone        string // the account vanished between the card and Yes

	// /matches: the latest matches of the linked account.
	MatchesNoAccount string // no FACEIT account linked yet
	MatchesEmpty     string // %s: nickname of an account without finished CS2 matches
	MatchesHeader    string // %s: nickname; one line per match follows
	MatchWin         string
	MatchLoss        string
	MatchNoMap       string // in place of the map when FACEIT did not give it
	MatchTimeLayout  string // Go time layout of the match start

	// Bot profile, registered through the Bot API at startup.
	CommandStart     string // /start in the command menu
	CommandLink      string // /link in the command menu
	CommandMatches   string // /matches in the command menu
	CommandHelp      string // /help in the command menu
	Description      string // shown in an empty chat with the bot
	ShortDescription string // shown on the bot profile and in shared links
}

// For returns the texts in l, English for an unknown language.
func For(l Lang) Messages {
	if l == RU {
		return ru
	}
	return en
}
