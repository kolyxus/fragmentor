package faceit

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrNotNickname means the text is neither a FACEIT nickname nor a link to a
// FACEIT profile.
var ErrNotNickname = errors.New("faceit: not a nickname or a profile link")

const (
	// FACEIT asks for at least three characters; older accounts may have
	// fewer. The upper bound only stops nonsense.
	minNicknameLen = 2
	maxNicknameLen = 32
)

// langRe matches the language part of a FACEIT link: en, ru, pt-br.
var langRe = regexp.MustCompile(`^(?i)[a-z]{2}(-[a-z]{2})?$`)

// ParseNickname returns the FACEIT nickname from what a user typed: the
// nickname itself, possibly with an @ in front, or a link to the profile
// (see ParseProfileLink). The letter case is kept: FACEIT looks nicknames up
// with the case as is.
func ParseNickname(input string) (string, error) {
	s := strings.TrimSpace(input)
	if nick, ok := ParseProfileLink(s); ok {
		return nick, nil
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "faceit.com") || strings.Contains(s, "/") {
		return "", ErrNotNickname
	}
	s = strings.TrimPrefix(s, "@")
	if !validNickname(s) {
		return "", ErrNotNickname
	}
	return s, nil
}

// ParseProfileLink returns the nickname from a link to a FACEIT profile, with
// or without https:// and www., with or without the language part and with
// anything after the nickname:
//
//	https://www.faceit.com/ru/players/s1mple
//	faceit.com/en/players/s1mple/stats/cs2
func ParseProfileLink(input string) (string, bool) {
	s := strings.TrimSpace(input)
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	if host := strings.ToLower(u.Hostname()); host != "faceit.com" && host != "www.faceit.com" {
		return "", false
	}
	segments := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(segments) > 0 && langRe.MatchString(segments[0]) {
		segments = segments[1:]
	}
	if len(segments) < 2 || (segments[0] != "players" && segments[0] != "players-modal") {
		return "", false
	}
	nick, err := url.PathUnescape(segments[1])
	if err != nil || !validNickname(nick) {
		return "", false
	}
	return nick, true
}

// validNickname accepts letters, digits, "-", "_" and ".". FACEIT does not
// publish its rules, so the check only keeps out what cannot be a nickname:
// spaces, URL syntax and the like.
func validNickname(s string) bool {
	n := utf8.RuneCountInString(s)
	if n < minNicknameLen || n > maxNicknameLen {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}
