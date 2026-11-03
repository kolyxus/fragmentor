package i18n

var en = Messages{
	StartJoined: `Hi! I'm Fragmentor, an AI coach for CS2.

I read the demos of your FACEIT matches and explain where you lose rounds and what to practice this week.

I'm still in development. You're on the beta waitlist: I'll message you here as soon as access opens.

What I can do right now: /help`,

	StartAlready: `You're already on the beta waitlist. I'll message you here as soon as access opens.`,

	Help: `What I can do right now:
/link — link your FACEIT nickname
/matches — your latest matches
/start — join the beta waitlist
/help — this help

Coming soon: breakdowns of your matches.

Data and privacy: https://fragmentor.tech/privacy.html`,

	Unknown: `I understand /link, /matches, /start and /help. Match breakdowns will arrive with the beta.`,

	Failure: `Something went wrong. Please try again in a minute.`,

	FaceitDown: `FACEIT isn't responding right now. Please try again in a couple of minutes.`,

	LinkAsk: `Send me your FACEIT nickname or a link to your profile, for example: s1mple or https://www.faceit.com/en/players/s1mple`,

	LinkBadInput: `That doesn't look like a FACEIT nickname. Send a nickname, like s1mple, or a profile link like https://www.faceit.com/en/players/s1mple`,

	LinkNotFound: `I couldn't find %s on FACEIT. Check the nickname and send it again.`,

	LinkNoCS2: `The FACEIT account %s has no CS2 profile. If that was a typo, send another nickname.`,

	LinkAlready: `%s is already linked. Your latest matches: /matches`,

	LinkCard: `Is this you?

%s
Level %d · %d ELO`,

	LinkCardReplace: `%s is linked now, I'll replace it with this one.`,

	LinkYes: "Yes, that's me",
	LinkNo:  "No",

	LinkDone: `Done, %s is linked. Your latest matches: /matches`,

	LinkDeclined: `OK, I won't link it. Send another nickname or a profile link.`,

	LinkGone: `I can't find this account on FACEIT anymore. Send the nickname again: /link`,

	MatchesNoAccount: `Link your FACEIT account first: /link`,

	MatchesEmpty: `%s has no finished CS2 matches on FACEIT yet.`,

	MatchesHeader: `Latest matches of %s:`,

	MatchStats:      "K/D/A %d/%d/%d · ADR %d · HS %d%%",
	MatchTimeLayout: "Jan 2 15:04",

	TimezoneButton: "🕓 Time zone",
	TimezoneAsk:    `Tap the time your clock shows now`,
	TimezoneSaved:  `Time zone saved.`,

	CommandStart:   "Join the beta waitlist",
	CommandLink:    "Link your FACEIT nickname",
	CommandMatches: "Your latest matches",
	CommandHelp:    "What the bot can do",

	Description: `Fragmentor is an AI coach for CS2. It reads the demos of your FACEIT matches and explains where you lose rounds and what to practice this week: your three biggest mistakes with numbers and one drill, compared with players at your level.

The bot is in development. Start it to join the beta waitlist.`,

	ShortDescription: "AI coach for CS2. Reads the demos of your FACEIT matches and explains where you lose rounds.",
}
