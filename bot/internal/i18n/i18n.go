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
	MatchesHeader    string // %s: nickname; the matches follow
	MatchStats       string // the player's line in a match; %d kills, deaths, assists, ADR, headshot %
	MatchTimeLayout  string // Go time layout of the match start

	// Time zone, chosen with the button under /matches.
	TimezoneButton string // the button under the match list
	TimezoneAsk    string // above the buttons with the time in each zone
	TimezoneSaved  string // the zone is saved, but the match list is not shown

	// Demo links: the analysis of a match. Until the beta only for the
	// admins.
	HelpDemo         string // added to /help for the admins
	DemoClosed       string // a demo link from anyone else
	DemoHowToButton  string // the button under the help and the texts about links
	DemoHowTo        string // how to get the link, behind the button
	DemoBadLink      string // not a link to a FACEIT demo
	DemoUnsigned     string // a link to a FACEIT demo without the signature
	DemoExpired      string // the link had run out when it came
	DemoNoAccount    string // no FACEIT account linked
	DemoNotFound     string // FACEIT has no such match
	DemoNotFinished  string // the match is not over
	DemoMultiMap     string // a match of more than one map
	DemoNotYours     string // %s: the nickname linked, who did not play the match
	DemoDuplicate    string // the demo is in the queue already
	DemoTooMany      string // %d: how many demos a user may have in the queue
	DemoQueueTooLong string // %d place the demo would take, %d minutes to wait, %d minutes until a fresh link would last
	DemoAlready      string // %s map, %d:%d score of the user's team first: the match is parsed already
	DemoQueued       string // %d place in the queue, %s when the parse starts: DemoSoon or DemoInMinutes
	DemoSoon         string // the parse starts in under a minute
	DemoInMinutes    string // %d minutes until the parse starts
	DemoRunning      string // the parse is under way
	DemoDone         string // %s map, %d:%d score of the user's team first: the match is parsed

	// Why a parse failed.
	DemoFailExpired   string // the link ran out while the demo waited in the queue
	DemoFailRejected  string // the storage refused the link
	DemoFailGone      string // the storage no longer has the demo
	DemoFailDownload  string // the download failed
	DemoFailTooLarge  string // the demo is larger than the limit
	DemoFailBroken    string // the demo is broken or not of the match
	DemoFailFaceit    string // FACEIT did not answer
	DemoFailNotLinked string // the FACEIT account was unlinked meanwhile
	DemoFailNotYours  string // the account linked now did not play the match
	DemoFailed        string // anything else; the user should try later

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
