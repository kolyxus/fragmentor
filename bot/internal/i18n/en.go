package i18n

var en = Messages{
	StartJoined: `Hi! I'm Fragmentor, an AI coach for CS2.

I read the demos of your FACEIT matches and explain where you lose rounds and what to practice this week.

I'm still in development. You're on the beta waitlist: I'll message you here as soon as access opens.

What I can do right now: /help`,

	StartAlready: `You're already on the beta waitlist. I'll message you here as soon as access opens.`,

	Help: `Right now I can do just a little:
/start — join the beta waitlist
/help — this help

Coming soon: linking your FACEIT nickname and breakdowns of your matches.

Data and privacy: https://fragmentor.tech/privacy.html`,

	Unknown: `For now I only understand /start and /help. Match breakdowns will arrive with the beta.`,

	Failure: `Something went wrong. Please try again in a minute.`,

	CommandStart: "Join the beta waitlist",
	CommandHelp:  "What the bot can do",

	Description: `Fragmentor is an AI coach for CS2. It reads the demos of your FACEIT matches and explains where you lose rounds and what to practice this week: your three biggest mistakes with numbers and one drill, compared with players at your level.

The bot is in development. Start it to join the beta waitlist.`,

	ShortDescription: "AI coach for CS2. Reads the demos of your FACEIT matches and explains where you lose rounds.",
}
