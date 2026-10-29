package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// utf8BOM is the byte order mark some Windows editors put at the start of a file.
const utf8BOM = "\xef\xbb\xbf"

// readDotEnv reads and parses a .env file.
func readDotEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseDotEnv(data)
}

// parseDotEnv parses KEY=VALUE lines. It skips blank lines and # comments,
// accepts an "export " prefix, strips matching single or double quotes and
// cuts an inline " #" comment from unquoted values. Quoted values are taken
// literally, without escapes. A UTF-8 BOM and CRLF line endings, which Windows
// editors tend to add, are ignored.
//
// Errors carry the line number only: the line itself may hold a secret.
func parseDotEnv(data []byte) (map[string]string, error) {
	text := strings.TrimPrefix(string(data), utf8BOM)
	vars := make(map[string]string)
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimSpace(rest)
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", i+1)
		}
		key = strings.TrimSpace(key)
		if !validKey(key) {
			return nil, fmt.Errorf("line %d: invalid variable name", i+1)
		}
		v, err := parseValue(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		vars[key] = v
	}
	return vars, nil
}

func parseValue(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if q := v[0]; q == '"' || q == '\'' {
		end := strings.IndexByte(v[1:], q)
		if end < 0 {
			return "", errors.New("unterminated quoted value")
		}
		if rest := strings.TrimSpace(v[end+2:]); rest != "" && !strings.HasPrefix(rest, "#") {
			return "", errors.New("unexpected text after quoted value")
		}
		return v[1 : end+1], nil
	}
	for i := 1; i < len(v); i++ {
		if v[i] == '#' && (v[i-1] == ' ' || v[i-1] == '\t') {
			v = v[:i]
			break
		}
	}
	return strings.TrimSpace(v), nil
}

// validKey reports whether key is a shell-style variable name.
func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
