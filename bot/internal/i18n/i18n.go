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

// Messages holds the texts of one language.
type Messages struct {
	// Chat replies, plain text without parse mode.
	StartJoined  string // first /start: welcome and waitlist confirmation
	StartAlready string // /start from a user already on the waitlist
	Help         string
	Unknown      string // any message the bot does not understand
	Failure      string // internal error, the user should retry

	// Bot profile, registered through the Bot API at startup.
	CommandStart     string // /start in the command menu
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
