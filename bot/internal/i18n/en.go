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

	HelpDemo: `Match analysis: send me the link to the demo of your match from your browser's downloads, and I'll analyze it and save the stats. How to get the link: the button below.`,

	DemoClosed: `Demo analysis opens with the beta.`,

	DemoHowToButton: "How to get the link",

	DemoHowTo: `How to send a demo for analysis:
1. Open your match on faceit.com and press "Watch demo". The browser starts downloading a .dem.zst file; you can cancel the download right away.
2. Open your browser's downloads: Ctrl+J.
3. Right-click the link under the file name and choose "Copy link address".
4. Send me the link straight away: FACEIT gives it for only 5 minutes.`,

	DemoBadLink: `That doesn't look like a link to a FACEIT demo. I need the link from your browser's downloads; how to get it: the button below.`,

	DemoUnsigned: `FACEIT won't give the demo by this link: it has no signature. I need the link from your browser's downloads; how to get it: the button below.`,

	DemoExpired: `The link has expired: FACEIT gives it for only a few minutes. Download the demo again and send me the fresh link right away.`,

	DemoNoAccount: `To analyze a match, link your FACEIT account first: /link`,

	DemoNotFound: `FACEIT doesn't know this match. Check the link.`,

	DemoNotFinished: `This match isn't over on FACEIT yet.`,

	DemoMultiMap: `For now I only analyze matches of a single map.`,

	DemoNotYours: `%s didn't play in this match. I analyze the matches of the linked account: /link`,

	DemoDuplicate: `This demo is already in the queue.`,

	DemoTooMany: `You can have at most %d demos in the queue at once. Send the next one when I've analyzed these.`,

	DemoQueueTooLong: `The queue is long right now: your demo would be number %d, and I'd get to it in about %d min, but the link won't last that long. Send a fresh link in about %d min.`,

	DemoAlready: `This match is already analyzed: %s, %d:%d.`,

	DemoQueued:    `Your demo is in the queue, place %d. I'll start the analysis %s.`,
	DemoSoon:      "in under a minute",
	DemoInMinutes: "in about %d min",

	DemoRunning: `Analyzing the demo. It usually takes under a minute.`,

	DemoDone: `Done: %s, %d:%d. I've analyzed the demo and saved the match stats. The written breakdown with your mistakes and a drill comes in the next version of the bot.`,

	DemoFailExpired: `The link expired while the demo waited in the queue. Download the demo again and send me a fresh link.`,

	DemoFailRejected: `FACEIT wouldn't give the demo by this link; it has most likely expired. Download the demo again and send me the fresh link right away.`,

	DemoFailGone: `FACEIT no longer keeps this demo.`,

	DemoFailDownload: `I couldn't download the demo. Send a fresh link a bit later.`,

	DemoFailTooLarge: `The demo is too large, I can't analyze it.`,

	DemoFailBroken: `I couldn't analyze the demo: the file is broken or it isn't from this match.`,

	DemoFailFaceit: `FACEIT didn't respond, so I couldn't check the match. Send a fresh link in a couple of minutes.`,

	DemoFailNotLinked: `Your FACEIT account was unlinked, so the analysis is cancelled. Link it again: /link`,

	DemoFailNotYours: `The account linked now didn't play in this match, so the analysis is cancelled.`,

	DemoFailed: `I couldn't analyze the demo. Please try again later.`,

	CommandStart:   "Join the beta waitlist",
	CommandLink:    "Link your FACEIT nickname",
	CommandMatches: "Your latest matches",
	CommandHelp:    "What the bot can do",

	Description: `Fragmentor is an AI coach for CS2. It reads the demos of your FACEIT matches and explains where you lose rounds and what to practice this week: your three biggest mistakes with numbers and one drill, compared with players at your level.

The bot is in development. Start it to join the beta waitlist.`,

	ShortDescription: "AI coach for CS2. Reads the demos of your FACEIT matches and explains where you lose rounds.",
}
