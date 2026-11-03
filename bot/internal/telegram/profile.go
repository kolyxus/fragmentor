package telegram

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/i18n"
)

// profileLangs maps Bot API language codes to bot languages. The empty code
// sets the default for every user language without its own texts, so English
// covers "en" and all the rest.
var profileLangs = []struct {
	code string
	lang i18n.Lang
}{
	{"", i18n.EN},
	{"ru", i18n.RU},
}

// syncProfile registers the command menu, the description and the short
// description in every language. It reads what Telegram has first and sets
// only what differs: the bot restarts often, and needless setMy* calls may
// run into flood limits. It returns what was changed.
func (b *Bot) syncProfile(ctx context.Context) (changed []string, err error) {
	for _, p := range profileLangs {
		texts := i18n.For(p.lang)
		label := p.code
		if label == "" {
			label = "default"
		}

		commands := []models.BotCommand{
			{Command: "start", Description: texts.CommandStart},
			{Command: "link", Description: texts.CommandLink},
			{Command: "matches", Description: texts.CommandMatches},
			{Command: "help", Description: texts.CommandHelp},
		}
		haveCommands, err := b.api.GetMyCommands(ctx, &bot.GetMyCommandsParams{LanguageCode: p.code})
		if err != nil {
			return changed, fmt.Errorf("get commands (%s): %w", label, err)
		}
		if !slices.Equal(haveCommands, commands) {
			if _, err := b.api.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: commands, LanguageCode: p.code}); err != nil {
				return changed, fmt.Errorf("set commands (%s): %w", label, err)
			}
			changed = append(changed, "commands ("+label+")")
		}

		haveDescription, err := b.api.GetMyDescription(ctx, &bot.GetMyDescriptionParams{LanguageCode: p.code})
		if err != nil {
			return changed, fmt.Errorf("get description (%s): %w", label, err)
		}
		if haveDescription.Description != texts.Description {
			if _, err := b.api.SetMyDescription(ctx, &bot.SetMyDescriptionParams{Description: texts.Description, LanguageCode: p.code}); err != nil {
				return changed, fmt.Errorf("set description (%s): %w", label, err)
			}
			changed = append(changed, "description ("+label+")")
		}

		haveShort, err := b.api.GetMyShortDescription(ctx, &bot.GetMyShortDescriptionParams{LanguageCode: p.code})
		if err != nil {
			return changed, fmt.Errorf("get short description (%s): %w", label, err)
		}
		if haveShort.ShortDescription != texts.ShortDescription {
			if _, err := b.api.SetMyShortDescription(ctx, &bot.SetMyShortDescriptionParams{ShortDescription: texts.ShortDescription, LanguageCode: p.code}); err != nil {
				return changed, fmt.Errorf("set short description (%s): %w", label, err)
			}
			changed = append(changed, "short description ("+label+")")
		}
	}
	return changed, nil
}
