package i18n

import (
	"reflect"
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
	if For("de") != For(EN) {
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

func TestMessagesTranslated(t *testing.T) {
	ruTexts, enTexts := fields(For(RU)), fields(For(EN))
	for name, text := range ruTexts {
		if text == enTexts[name] {
			t.Errorf("%s is the same in ru and en", name)
		}
	}
}

// TestTelegramLimits checks the Bot API length limits, which count UTF-16
// code units.
func TestTelegramLimits(t *testing.T) {
	limits := map[string]int{
		"CommandStart":     256,
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

// fields returns the string fields of m by name.
func fields(m Messages) map[string]string {
	v := reflect.ValueOf(m)
	out := make(map[string]string, v.NumField())
	for i := range v.NumField() {
		out[v.Type().Field(i).Name] = v.Field(i).String()
	}
	return out
}
