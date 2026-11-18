package i18n

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestFromTelegram(t *testing.T) {
	tests := []struct {
		code string
		want Lang
	}{
		{"ru", RU},
		{"RU", RU},
		{"ru-RU", RU},
		{"ru_RU", RU},
		{" ru ", RU},
		{"en", EN},
		{"en-US", EN},
		{"uk", EN},
		{"be", EN},
		{"pt-br", EN},
		{"rus", EN},
		{"", EN},
	}
	for _, tt := range tests {
		if got := FromTelegram(tt.code); got != tt.want {
			t.Errorf("FromTelegram(%q) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestForUnknownLangFallsBackToEnglish(t *testing.T) {
	if !reflect.DeepEqual(For("de"), For(EN)) {
		t.Error(`For("de") differs from For(EN)`)
	}
}

func TestMessagesComplete(t *testing.T) {
	for _, l := range Langs {
		for name, text := range fields(For(l)) {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s.%s is empty", l, name)
			}
			if text != strings.TrimSpace(text) {
				t.Errorf("%s.%s has leading or trailing whitespace", l, name)
			}
			if strings.Contains(text, "\r") {
				t.Errorf("%s.%s contains a carriage return", l, name)
			}
		}
	}
}

// sameInBoth is the texts that are the same in both languages by nature:
// arrows between two places.
var sameInBoth = map[string]bool{"Review.Death": true, "Review.Kill": true}

func TestMessagesTranslated(t *testing.T) {
	ruTexts, enTexts := fields(For(RU)), fields(For(EN))
	for name, text := range ruTexts {
		if _, ok := enTexts[name]; !ok {
			t.Errorf("%s is in ru only", name)
			continue
		}
		if text == enTexts[name] && !sameInBoth[name] {
			t.Errorf("%s is the same in ru and en", name)
		}
	}
	for name := range enTexts {
		if _, ok := ruTexts[name]; !ok {
			t.Errorf("%s is in en only", name)
		}
	}
	for name := range sameInBoth {
		if ruTexts[name] != enTexts[name] {
			t.Errorf("%s differs in ru and en: take it out of sameInBoth", name)
		}
	}
}

// TestTelegramLimits checks the Bot API length limits, which count UTF-16
// code units.
func TestTelegramLimits(t *testing.T) {
	limits := map[string]int{
		"CommandStart":     256,
		"CommandLink":      256,
		"CommandMatches":   256,
		"CommandHelp":      256,
		"Description":      512,
		"ShortDescription": 120,
	}
	const messageLimit = 4096
	for _, l := range Langs {
		for name, text := range fields(For(l)) {
			limit, ok := limits[name]
			if !ok {
				limit = messageLimit
			}
			if n := len(utf16.Encode([]rune(text))); n > limit {
				t.Errorf("%s.%s is %d characters long, Telegram allows %d", l, name, n, limit)
			}
		}
	}
}

// verbRe matches a fmt verb such as %s, %d or %5.1f.
var verbRe = regexp.MustCompile(`%[-+# 0]*[0-9]*(?:\.[0-9]+)?[a-zA-Z%]`)

// TestFormatVerbsMatch checks that every format has the same verbs in the
// same order in both languages, so the same arguments fit both.
func TestFormatVerbsMatch(t *testing.T) {
	ruTexts, enTexts := fields(For(RU)), fields(For(EN))
	for name, ruText := range ruTexts {
		ruVerbs, enVerbs := verbRe.FindAllString(ruText, -1), verbRe.FindAllString(enTexts[name], -1)
		if !slices.Equal(ruVerbs, enVerbs) {
			t.Errorf("%s: ru has %q, en has %q", name, ruVerbs, enVerbs)
		}
	}
}

// fields returns the texts of m by name: the strings of its fields, of the
// fields of its structs, of its maps and of its arrays, like
// "Review.Rules[lone_death].Title".
func fields(m Messages) map[string]string {
	out := make(map[string]string)
	flatten(reflect.ValueOf(m), "", out)
	return out
}

func flatten(v reflect.Value, name string, out map[string]string) {
	switch v.Kind() {
	case reflect.String:
		out[name] = v.String()
	case reflect.Struct:
		for i := range v.NumField() {
			field := v.Type().Field(i).Name
			if name != "" {
				field = name + "." + field
			}
			flatten(v.Field(i), field, out)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			flatten(v.MapIndex(k), fmt.Sprintf("%s[%v]", name, k), out)
		}
	case reflect.Array, reflect.Slice:
		for i := range v.Len() {
			flatten(v.Index(i), fmt.Sprintf("%s[%d]", name, i), out)
		}
	}
}

func TestPlural(t *testing.T) {
	forms := [3]string{"смерть", "смерти", "смертей"}
	for n, want := range map[int]string{
		0: "смертей", 1: "смерть", 2: "смерти", 4: "смерти", 5: "смертей", 11: "смертей", 12: "смертей", 14: "смертей",
		21: "смерть", 22: "смерти", 25: "смертей", 101: "смерть", 111: "смертей", 112: "смертей", 122: "смерти", -3: "смерти",
	} {
		if got := Plural(RU, n, forms); got != want {
			t.Errorf("Plural(ru, %d) = %q, want %q", n, got, want)
		}
	}
	en := [3]string{"death", "deaths", "deaths"}
	for n, want := range map[int]string{0: "deaths", 1: "death", 2: "deaths", 21: "deaths"} {
		if got := Plural(EN, n, en); got != want {
			t.Errorf("Plural(en, %d) = %q, want %q", n, got, want)
		}
	}
}
