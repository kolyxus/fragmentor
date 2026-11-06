package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/kolyxus/fragmentor/bot/internal/analysis"
	"github.com/kolyxus/fragmentor/bot/internal/faceit"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/storage"
)

// The button under the texts about demo links carries "demo:howto".
const (
	demoButtonPrefix = "demo:"
	demoButtonHowTo  = demoButtonPrefix + "howto"
)

// demoLinkIn finds a word that looks like a link to a FACEIT demo in a
// message: one with faceit-cdn, backblaze or .dem in it, without the quotes,
// brackets and punctuation around. ok is false if the message has none: it
// is not about a demo.
func demoLinkIn(text string) (string, bool) {
	for _, word := range strings.Fields(text) {
		word = strings.TrimRight(strings.TrimLeft(word, `<([{"'«`), `>)]}"'».,;:!?`)
		lower := strings.ToLower(word)
		if strings.Contains(lower, "faceit-cdn") || strings.Contains(lower, "backblaze") || strings.Contains(lower, ".dem") {
			return word, true
		}
	}
	return "", false
}

// demoLink handles a message with a demo link: it checks the link and puts
// the demo in the queue of analyses. The status message of an accepted demo
// is drawn by the status loop, like every later change of the job.
func (b *Bot) demoLink(ctx context.Context, msg *models.Message, raw string) error {
	lang := i18n.FromTelegram(msg.From.LanguageCode)
	texts := i18n.For(lang)
	if !b.admins[msg.From.ID] {
		return b.reply(ctx, msg, texts.DemoClosed)
	}
	link, err := faceit.ParseDemoLink(raw)
	switch {
	case errors.Is(err, faceit.ErrUnsignedLink):
		return b.send(ctx, msg.Chat.ID, texts.DemoUnsigned, howToButton(texts))
	case err != nil:
		return b.send(ctx, msg.Chat.ID, texts.DemoBadLink, howToButton(texts))
	}

	b.typing(ctx, msg.Chat.ID)
	out, err := b.jobs.Submit(ctx, analysis.Request{TelegramID: msg.From.ID, Language: string(lang), Link: link})
	switch {
	case errors.Is(err, analysis.ErrFaceit):
		return errors.Join(fmt.Errorf("submit demo: %w", err), b.reply(ctx, msg, texts.FaceitDown))
	case err != nil:
		return errors.Join(fmt.Errorf("submit demo: %w", err), b.reply(ctx, msg, texts.Failure))
	}
	if out.Verdict == analysis.Accepted {
		b.log.Info("demo accepted", "job", out.Job.ID, "lang", lang)
		b.status.JobsChanged(out.Job.ID)
		return nil
	}
	text, markup := verdictText(texts, out)
	return b.send(ctx, msg.Chat.ID, text, markup)
}

// verdictText is the answer to a demo link that added no job.
func verdictText(texts i18n.Messages, out analysis.Outcome) (string, models.ReplyMarkup) {
	switch out.Verdict {
	case analysis.Analyzed:
		own, opp := score(out.Result)
		return fmt.Sprintf(texts.DemoAlready, faceit.MapName(out.Result.Map), own, opp), nil
	case analysis.Duplicate:
		return texts.DemoDuplicate, nil
	case analysis.TooMany:
		return fmt.Sprintf(texts.DemoTooMany, analysis.MaxActiveJobs), nil
	case analysis.NotLinked:
		return texts.DemoNoAccount, nil
	case analysis.LinkExpired:
		return texts.DemoExpired, howToButton(texts)
	case analysis.QueueTooLong:
		return fmt.Sprintf(texts.DemoQueueTooLong, out.Position, minutes(out.Wait), minutes(out.RetryIn)), nil
	case analysis.MatchNotFound:
		return texts.DemoNotFound, nil
	case analysis.MatchNotFinished:
		return texts.DemoNotFinished, nil
	case analysis.MultiMap:
		return texts.DemoMultiMap, nil
	case analysis.NotInMatch:
		return fmt.Sprintf(texts.DemoNotYours, out.Account.Nickname), nil
	}
	return texts.Failure, nil
}

// statusText is the status message of a job, in the language of its link;
// "" for a job done whose match is gone.
func statusText(v analysis.View) (string, models.ReplyMarkup) {
	texts := i18n.For(i18n.Lang(v.Job.Language))
	switch v.Job.Status {
	case storage.JobQueued:
		when := texts.DemoSoon
		if v.Wait >= time.Minute {
			when = fmt.Sprintf(texts.DemoInMinutes, minutes(v.Wait))
		}
		return fmt.Sprintf(texts.DemoQueued, v.Position, when), nil
	case storage.JobRunning:
		return texts.DemoRunning, nil
	case storage.JobDone:
		if !v.HasResult {
			return "", nil
		}
		own, opp := score(v.Result)
		return fmt.Sprintf(texts.DemoDone, faceit.MapName(v.Result.Map), own, opp), nil
	}
	return failText(texts, v.Job.Reason)
}

// failText tells why a job failed, with the button that tells how to get
// a link when a fresh one will do.
func failText(texts i18n.Messages, reason string) (string, models.ReplyMarkup) {
	switch reason {
	case analysis.ReasonLinkExpired:
		return texts.DemoFailExpired, howToButton(texts)
	case analysis.ReasonLinkRejected:
		return texts.DemoFailRejected, howToButton(texts)
	case analysis.ReasonDownload:
		return texts.DemoFailDownload, howToButton(texts)
	case analysis.ReasonDemoGone:
		return texts.DemoFailGone, nil
	case analysis.ReasonTooLarge:
		return texts.DemoFailTooLarge, nil
	case analysis.ReasonBadDemo:
		return texts.DemoFailBroken, nil
	case analysis.ReasonFaceit:
		return texts.DemoFailFaceit, nil
	case analysis.ReasonNotLinked:
		return texts.DemoFailNotLinked, nil
	case analysis.ReasonNotInMatch:
		return texts.DemoFailNotYours, nil
	}
	return texts.DemoFailed, nil
}

// score is the score of a parsed match, the user's team first; in the order
// of the teams if the user is not kept with the match.
func score(r storage.MatchResult) (own, opp int) {
	if r.Team == 1 {
		return r.Score[1], r.Score[0]
	}
	return r.Score[0], r.Score[1]
}

// minutes is d in whole minutes, rounded up, at least 1.
func minutes(d time.Duration) int {
	return max(int((d+time.Minute-1)/time.Minute), 1)
}

// howToButton is the button that tells how to get a demo link.
func howToButton(texts i18n.Messages) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: texts.DemoHowToButton, CallbackData: demoButtonHowTo},
	}}}
}

// demoButton handles the button "how to get the link": the instructions
// come as a new message, under the one with the button.
func (b *Bot) demoButton(ctx context.Context, cq *models.CallbackQuery) error {
	answerErr := b.answer(ctx, cq)
	if cq.Data != demoButtonHowTo {
		b.log.Warn("demo button with bad data ignored")
		return answerErr
	}
	texts := i18n.For(i18n.FromTelegram(cq.From.LanguageCode))
	return errors.Join(answerErr, b.sendToButtonChat(ctx, cq, texts.DemoHowTo))
}
