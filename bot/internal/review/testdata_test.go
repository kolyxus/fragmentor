package review

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/metrics"
	"github.com/kolyxus/fragmentor/bot/internal/testdemos"
)

// telegramLimit is the length of a message Telegram takes, in UTF-16 code
// units: the review goes into one with step 9.
const telegramLimit = 4096

// TestTestdata makes the facts and the review of every player of every
// demo in bot/testdata (see internal/testdemos), his other demos as his
// past, as cmd/parsedemo -findings does, and checks what holds for any of
// them: the places of the demos are in places.yaml, the facts hold no
// SteamID and no nickname, and the review in either language has no broken
// format and fits a message. The places of the demos of bot/testdata/maps/
// only go run ./cmd/parsedemo -places checks.
func TestTestdata(t *testing.T) {
	files := testdemos.Files(t)
	matches := make([]*demo.Match, len(files))
	analyses := make([]*metrics.Analysis, len(files))
	t.Run("parse", func(t *testing.T) {
		for i, file := range files {
			t.Run(testdemos.Name(file), func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				m, err := demo.ParseFile(ctx, file, demo.Options{})
				if err != nil {
					t.Fatal(err)
				}
				matches[i], analyses[i] = m, metrics.Analyze(m)
			})
		}
	})
	if slices.Contains(analyses, nil) {
		t.Fatal("not every demo was parsed")
	}
	c := content.Default()

	// The places of every kill are in the dictionary of their map.
	for i, m := range matches {
		places := c.Maps()[m.Map].Places
		for _, k := range m.Kills {
			for _, p := range []string{k.VictimPlace, k.KillerPlace} {
				if _, ok := places[p]; p != "" && !ok {
					t.Errorf("%s: the place %q of %s is not in places.yaml", testdemos.Name(files[i]), p, m.Map)
				}
			}
		}
	}

	first := make([][]findings.Report, len(analyses))
	for i, a := range analyses {
		first[i] = findings.Analyze(a, nil)
	}
	longest, largest := 0, 0
	for i, a := range analyses {
		past := make(map[uint64]findings.History)
		for j, other := range analyses {
			if j == i {
				continue
			}
			for k, p := range other.Players {
				h := past[p.SteamID64]
				h.Add(first[j][k])
				past[p.SteamID64] = h
			}
		}
		m := matches[i]
		var secrets []string
		for _, p := range m.Players {
			secrets = append(secrets, strconv.FormatUint(p.SteamID64, 10))
			if len([]rune(p.Name)) >= 5 {
				secrets = append(secrets, p.Name)
			}
		}
		for k, r := range findings.Analyze(a, past) {
			f := findings.BuildFacts(findings.FactsInput{Match: m, Analysis: a, Player: k, Report: r, Level: 5,
				Drills: func(rule findings.RuleID) []string { return c.DrillsFor(rule, m.Map) }})
			name := testdemos.Name(files[i]) + " player " + strconv.Itoa(k)
			b, err := json.Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			largest = max(largest, len(b))
			for _, s := range secrets {
				if strings.Contains(string(b), s) {
					t.Errorf("%s: the facts hold %q", name, s)
				}
			}
			if len(f.Review.Mistakes) > 0 && f.Review.Task == nil {
				t.Errorf("%s: mistakes and no task", name)
			}
			for _, lang := range i18n.Langs {
				text := Render(f, lang, c)
				// A broken format, a text missing for a rule or a variant, an
				// empty part of a moment.
				for _, bad := range []string{"%!", ": :", ":  ", " , ", ", —", "()", "[]"} {
					if strings.Contains(text, bad) {
						t.Errorf("%s, %s: %q in\n%s", name, lang, bad, text)
					}
				}
				n := len(utf16.Encode([]rune(text)))
				longest = max(longest, n)
				if n > telegramLimit {
					t.Errorf("%s, %s: the review is %d characters long, a message takes %d", name, lang, n, telegramLimit)
				}
			}
		}
	}
	t.Logf("the longest review is %d characters long, the largest facts %d KiB", longest, largest>>10)
}
