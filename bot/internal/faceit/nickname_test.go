package faceit

import (
	"errors"
	"strings"
	"testing"
)

func TestParseNickname(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"kolyx", "kolyx"},
		{"  KoLyX  ", "KoLyX"},
		{"@kolyx", "kolyx"},
		{"s1mple", "s1mple"},
		{"-NiKo_", "-NiKo_"},
		{"dev.one", "dev.one"},
		{"ab", "ab"},
		{"Тренер", "Тренер"},
		{"https://www.faceit.com/ru/players/kolyx", "kolyx"},
		{"https://www.faceit.com/en/players/kolyx/stats/cs2", "kolyx"},
		{"https://faceit.com/players/kolyx", "kolyx"},
		{"http://www.faceit.com/en/players/kolyx", "kolyx"},
		{"www.faceit.com/pt-br/players/KoLyX/", "KoLyX"},
		{"faceit.com/ru/players/kolyx?tab=cs2#top", "kolyx"},
		{"https://WWW.FACEIT.COM/RU/players/kolyx", "kolyx"},
		{"https://www.faceit.com/en/players-modal/kolyx", "kolyx"},
		{"https://www.faceit.com/ru/players/%D0%A2%D1%80%D0%B5%D0%BD%D0%B5%D1%80", "Тренер"},
	}
	for _, tt := range tests {
		got, err := ParseNickname(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseNickname(%q) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestParseNicknameRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"a",
		"@",
		"two words",
		"kolyx\nkolyx",
		strings.Repeat("x", maxNicknameLen+1),
		"nick?x=1",
		"a/b",
		"<script>",
		"https://steamcommunity.com/id/kolyx",
		"https://www.faceit.com/ru/cs2/room/1-00000000-0000-4000-8000-000000000004",
		"https://www.faceit.com/ru/players/",
		"https://www.faceit.com",
		"faceit.com",
		"https://evilfaceit.com/ru/players/kolyx",
		"https://faceit.com.evil.example/ru/players/kolyx",
		"https://www.faceit.com/ru/players/two%20words",
		"ftp://faceit.com/players/kolyx",
		"javascript:alert(1)",
	} {
		if got, err := ParseNickname(in); !errors.Is(err, ErrNotNickname) {
			t.Errorf("ParseNickname(%q) = %q, %v, want ErrNotNickname", in, got, err)
		}
	}
}

func TestParseProfileLinkOnlyTakesLinks(t *testing.T) {
	for _, in := range []string{"kolyx", "@kolyx", "players/kolyx"} {
		if nick, ok := ParseProfileLink(in); ok {
			t.Errorf("ParseProfileLink(%q) = %q, want no link", in, nick)
		}
	}
	if nick, ok := ParseProfileLink("Глянь мой профиль https://faceit.com/ru/players/kolyx"); ok {
		t.Errorf("ParseProfileLink() took %q out of a sentence, want only a bare link", nick)
	}
}
