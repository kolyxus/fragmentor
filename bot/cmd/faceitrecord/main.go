// Command faceitrecord records FACEIT Data API responses as test fixtures for
// internal/faceit.
//
// It takes FACEIT_API_KEY the way the bot does, from the environment or .env,
// and never prints it. Before writing, it replaces nicknames, player, team and
// match IDs and SteamIDs with fake ones, the same way in every file, and drops
// avatars and friend lists. Then it checks that none of the real values is
// left, so the fixtures can be committed.
//
// Run from bot/:
//
//	go run ./cmd/faceitrecord -nickname <nickname> [-match <match_id>]...
//
// It records the player, a player search, the CS2 match history, the details
// and stats of the latest matches and of every -match, the player's stats in
// the last 10 matches, and two error responses. It also prints a few probes of the API behaviour, see
// docs/notes/faceit-data-api.md.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/config"
)

const (
	baseURL = "https://open.faceit.com/data/v4"
	// pause between requests keeps the tool far below any rate limit.
	pause = 300 * time.Millisecond
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	nickname := flag.String("nickname", "", "FACEIT nickname to record (required)")
	out := flag.String("out", filepath.Join("internal", "faceit", "testdata"), "directory for the fixtures")
	envPath := flag.String("env", ".env", "file with FACEIT_API_KEY")
	recent := flag.Int("recent", 5, "how many matches of the history to record with details and stats")
	var matches multiFlag
	flag.Var(&matches, "match", "another match ID to record, may repeat")
	flag.Parse()
	if *nickname == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*envPath, *nickname, *out, *recent, matches); err != nil {
		fmt.Fprintln(os.Stderr, "faceitrecord:", err)
		os.Exit(1)
	}
}

// fixture is a recorded response waiting to be anonymized and written.
type fixture struct {
	name    string // file name; for match files the match ID is added later
	matchID string // original match ID for files under matches/
	status  int
	data    any
}

type recorder struct {
	key         string
	client      *http.Client
	fixtures    []*fixture
	seenHeaders map[string]bool
}

func run(envPath, nickname, out string, recent int, extraMatches []string) error {
	cfg, err := config.Load(envPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	r := &recorder{key: cfg.FaceitAPIKey, client: &http.Client{Timeout: 20 * time.Second}, seenHeaders: map[string]bool{}}
	ctx := context.Background()

	player, status, err := r.get(ctx, r.key, "/players", url.Values{"nickname": {nickname}})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("player lookup: status %d", status)
	}
	r.save("player.json", "", status, player)
	playerObj, _ := player.(map[string]any)
	playerID, _ := playerObj["player_id"].(string)
	if playerID == "" {
		return errors.New("player lookup: no player_id in the response")
	}

	fmt.Println("probes:")
	for _, variant := range []string{strings.ToUpper(nickname), strings.ToUpper(nickname[:1]) + nickname[1:]} {
		if variant == nickname {
			continue
		}
		data, status, err := r.get(ctx, r.key, "/players", url.Values{"nickname": {variant}})
		if err != nil {
			return err
		}
		fmt.Printf("  lookup with other letter case: status %d\n", status)
		if status == http.StatusNotFound && !r.has("error_not_found.json") {
			r.save("error_not_found.json", "", status, data)
		}
	}
	_, status, err = r.get(ctx, r.key, "/players", url.Values{"nickname": {nickname}, "game": {"cs2"}})
	if err != nil {
		return err
	}
	fmt.Printf("  lookup with game=cs2: status %d\n", status)

	search, status, err := r.get(ctx, r.key, "/search/players", url.Values{"nickname": {nickname}, "game": {"cs2"}, "limit": {"5"}})
	if err != nil {
		return err
	}
	r.save("search_players.json", "", status, search)
	upperSearch, status, err := r.get(ctx, r.key, "/search/players", url.Values{"nickname": {strings.ToUpper(nickname)}, "game": {"cs2"}, "limit": {"5"}})
	if err != nil {
		return err
	}
	fmt.Printf("  search with upper case: status %d, finds the player: %t\n", status, containsPlayer(upperSearch, playerID))

	historyPath := "/players/" + url.PathEscape(playerID) + "/history"
	history, status, err := r.get(ctx, r.key, historyPath, url.Values{"game": {"cs2"}, "limit": {"10"}})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("history: status %d", status)
	}
	r.save("history.json", "", status, history)
	fmt.Printf("  history by default: %s\n", describeHistory(history))

	old := strconv.FormatInt(time.Now().AddDate(0, 0, -45).Unix(), 10)
	for _, probe := range []struct {
		label string
		query url.Values
	}{
		{"from=0", url.Values{"from": {"0"}}},
		{"from=1", url.Values{"from": {"1"}}},
		{"to=45 days ago", url.Values{"to": {old}}},
		{"from=1, to=45 days ago", url.Values{"from": {"1"}, "to": {old}}},
		{"to=45 days ago, limit=100", url.Values{"to": {old}, "limit": {"100"}}},
		{"limit=100", url.Values{"limit": {"100"}}},
	} {
		q := probe.query
		q.Set("game", "cs2")
		if !q.Has("limit") {
			q.Set("limit", "10")
		}
		data, status, err := r.get(ctx, r.key, historyPath, q)
		if err != nil {
			return err
		}
		fmt.Printf("  history with %s: status %d, %s\n", probe.label, status, describeHistory(data))
	}

	var ids []string
	historyObj, _ := history.(map[string]any)
	if items, ok := historyObj["items"].([]any); ok {
		for _, it := range items[:min(recent, len(items))] {
			item, _ := it.(map[string]any)
			if id, ok := item["match_id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	for _, id := range extraMatches {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		match, status, err := r.get(ctx, r.key, "/matches/"+url.PathEscape(id), nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			fmt.Printf("  match details: status %d, skipped\n", status)
			continue
		}
		r.save(".json", id, status, match)
		stats, status, err := r.get(ctx, r.key, "/matches/"+url.PathEscape(id)+"/stats", nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			fmt.Printf("  match stats: status %d, skipped\n", status)
			continue
		}
		r.save("_stats.json", id, status, stats)
	}

	playerStats, status, err := r.get(ctx, r.key, "/players/"+url.PathEscape(playerID)+"/games/cs2/stats", url.Values{"limit": {"10"}})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("player match stats: status %d", status)
	}
	r.save("player_match_stats.json", "", status, playerStats)

	if !r.has("error_not_found.json") {
		data, status, err := r.get(ctx, r.key, "/matches/1-00000000-0000-0000-0000-000000000000", nil)
		if err != nil {
			return err
		}
		r.save("error_not_found.json", "", status, data)
	}
	data, status, err := r.get(ctx, "not-a-real-key", "/players/"+url.PathEscape(playerID), nil)
	if err != nil {
		return err
	}
	r.save("error_unauthorized.json", "", status, data)

	return r.write(out)
}

// get calls the API and decodes the JSON body. Errors never carry the key:
// it travels only in the request header.
func (r *recorder) get(ctx context.Context, key, path string, query url.Values) (any, int, error) {
	time.Sleep(pause)
	u := baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		// The URL in the error may hold the nickname, keep it out.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	for name, values := range resp.Header {
		lower := strings.ToLower(name)
		if (strings.Contains(lower, "rate") || strings.Contains(lower, "limit") || strings.Contains(lower, "retry")) && !r.seenHeaders[lower] {
			r.seenHeaders[lower] = true
			fmt.Printf("  header %s: %s\n", name, strings.Join(values, ", "))
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		fmt.Printf("  note: status %d with an empty body\n", resp.StatusCode)
		return nil, resp.StatusCode, nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var data any
	if err := dec.Decode(&data); err != nil {
		fmt.Printf("  note: status %d, the body is not JSON (%s, %d bytes)\n", resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
		return nil, resp.StatusCode, nil
	}
	return data, resp.StatusCode, nil
}

func (r *recorder) save(name, matchID string, status int, data any) {
	r.fixtures = append(r.fixtures, &fixture{name: name, matchID: matchID, status: status, data: data})
}

func (r *recorder) has(name string) bool {
	return slices.ContainsFunc(r.fixtures, func(f *fixture) bool { return f.name == name })
}

// write anonymizes every fixture, checks that no real value is left and only
// then writes the files.
func (r *recorder) write(out string) error {
	a := newAnonymizer()
	for _, f := range r.fixtures {
		f.data = a.value("", f.data)
		if f.matchID != "" {
			f.name = filepath.Join("matches", a.text(f.matchID)+f.name)
		}
	}
	encoded := make([][]byte, len(r.fixtures))
	for i, f := range r.fixtures {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(f.data); err != nil {
			return fmt.Errorf("encode %s: %w", f.name, err)
		}
		encoded[i] = buf.Bytes()
		if err := a.check(f.name, f.data); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(out, "matches"), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", out, err)
	}
	fmt.Println("fixtures:")
	for i, f := range r.fixtures {
		path := filepath.Join(out, f.name)
		if err := os.WriteFile(path, encoded[i], 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Printf("  %-60s status %d, %d bytes\n", filepath.ToSlash(path), f.status, len(encoded[i]))
	}
	return nil
}

func containsPlayer(search any, playerID string) bool {
	m, _ := search.(map[string]any)
	items, _ := m["items"].([]any)
	for _, it := range items {
		if p, ok := it.(map[string]any); ok && p["player_id"] == playerID {
			return true
		}
	}
	return false
}

// describeHistory reports the time range and size of a history response
// without any identifiers.
func describeHistory(data any) string {
	m, _ := data.(map[string]any)
	items, _ := m["items"].([]any)
	var statuses []string
	var oldest int64
	for _, it := range items {
		item, _ := it.(map[string]any)
		if s, ok := item["status"].(string); ok && !slices.Contains(statuses, s) {
			statuses = append(statuses, s)
		}
		if n, ok := item["finished_at"].(json.Number); ok {
			if v, err := n.Int64(); err == nil && (oldest == 0 || v < oldest) {
				oldest = v
			}
		}
	}
	day := func(key string) string {
		n, ok := m[key].(json.Number)
		if !ok {
			return "absent"
		}
		v, err := n.Int64()
		if err != nil {
			return n.String()
		}
		if v > 1e11 { // milliseconds
			v /= 1000
		}
		return fmt.Sprintf("%s (%s)", n, time.Unix(v, 0).UTC().Format("2006-01-02"))
	}
	oldestDay := "-"
	if oldest > 0 {
		oldestDay = time.Unix(oldest, 0).UTC().Format("2006-01-02")
	}
	return fmt.Sprintf("from=%s to=%s items=%d statuses=%v oldest=%s", day("from"), day("to"), len(items), statuses, oldestDay)
}

var (
	uuidRe  = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	steamRe = regexp.MustCompile(`7656119\d{10}`)
)

// anonymizer replaces identifying values with fake ones. The same real value
// always gets the same fake one, so the files keep referring to each other.
// Fakes are numbered in the order the values are met; objects are walked in
// sorted key order, so a new recording numbers them the same way.
type anonymizer struct {
	ids, steamIDs, nicknames, avatars map[string]string
	// secretIDs and secretNames map every replaced real value, lower-cased,
	// to its fake one for check. IDs are long and distinctive, so check
	// looks for them inside strings; names may be short common words, so
	// check compares them with whole strings and link segments.
	secretIDs, secretNames map[string]string
}

func newAnonymizer() *anonymizer {
	return &anonymizer{
		ids:         map[string]string{},
		steamIDs:    map[string]string{},
		nicknames:   map[string]string{},
		avatars:     map[string]string{},
		secretIDs:   map[string]string{},
		secretNames: map[string]string{},
	}
}

func (a *anonymizer) id(s string) string {
	key := strings.ToLower(s)
	if fake, ok := a.ids[key]; ok {
		return fake
	}
	fake := fmt.Sprintf("00000000-0000-4000-8000-%012d", len(a.ids)+1)
	a.ids[key] = fake
	a.secretIDs[key] = fake
	return fake
}

func (a *anonymizer) steamID(s string) string {
	if fake, ok := a.steamIDs[s]; ok {
		return fake
	}
	fake := fmt.Sprintf("7656119%010d", len(a.steamIDs)+1)
	a.steamIDs[s] = fake
	a.secretIDs[s] = fake
	return fake
}

// avatar replaces the link to a picture on the FACEIT CDN with a fake one on
// the same host, so the client's host check still has something real to
// test; any other link goes. Fakes have their own numbering, so adding
// avatars did not renumber the IDs of earlier recordings.
func (a *anonymizer) avatar(s string) string {
	u, err := url.Parse(s)
	if s == "" || err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".faceit-cdn.net") {
		return ""
	}
	key := strings.ToLower(s)
	if fake, ok := a.avatars[key]; ok {
		return fake
	}
	fake := fmt.Sprintf("https://%s/images/avatar-%04d.jpg", strings.ToLower(u.Hostname()), len(a.avatars)+1)
	a.avatars[key] = fake
	a.secretIDs[key] = fake
	for _, id := range uuidRe.FindAllString(s, -1) {
		a.secretIDs[strings.ToLower(id)] = fake
	}
	return fake
}

// nickname maps player nicknames and the team names FACEIT builds from them,
// "team_<nickname>".
func (a *anonymizer) nickname(s string) string {
	if s == "" {
		return s
	}
	if rest, ok := strings.CutPrefix(s, "team_"); ok && rest != "" {
		return "team_" + a.nickname(rest)
	}
	if fake, ok := a.nicknames[s]; ok {
		return fake
	}
	fake := fmt.Sprintf("player%02d", len(a.nicknames)+1)
	a.nicknames[s] = fake
	a.secretNames[strings.ToLower(s)] = fake
	return fake
}

// value returns v, found under key, with identifying values replaced.
func (a *anonymizer) value(key string, v any) any {
	switch key {
	case "avatar":
		if s, ok := v.(string); ok {
			return a.avatar(s)
		}
	case "cover_image", "cover_featured_image":
		if _, ok := v.(string); ok {
			return ""
		}
	case "friends_ids":
		return []any{}
	case "activated_at":
		if _, ok := v.(string); ok {
			return "2020-01-01T00:00:00Z"
		}
	case "nickname", "Nickname", "game_player_name", "steam_nickname", "Team":
		if s, ok := v.(string); ok {
			return a.nickname(s)
		}
	case "new_steam_id":
		if s, ok := v.(string); ok && s != "" {
			a.secretIDs[strings.ToLower(s)] = "[U:1:0]"
			return "[U:1:0]"
		}
	case "platforms":
		if m, ok := v.(map[string]any); ok {
			for k, pv := range m {
				if s, ok := pv.(string); ok && s != "" {
					a.secretIDs[strings.ToLower(s)] = "redacted"
				}
				m[k] = "redacted"
			}
			return m
		}
	case "faceit_url":
		if s, ok := v.(string); ok {
			return a.link(s)
		}
	}
	switch v := v.(type) {
	case map[string]any:
		// Teams of a match: "name" is the team name. Elsewhere "name" is a
		// map or a server location and stays.
		_, isFaction := v["faction_id"]
		for _, k := range sortedKeys(v) {
			if k == "name" && isFaction {
				if s, ok := v[k].(string); ok {
					v[k] = a.nickname(s)
					continue
				}
			}
			v[k] = a.value(k, v[k])
		}
		return v
	case []any:
		for i := range v {
			v[i] = a.value(key, v[i])
		}
		return v
	case string:
		return a.text(v)
	}
	return v
}

// text replaces the UUIDs and SteamIDs inside s.
func (a *anonymizer) text(s string) string {
	s = uuidRe.ReplaceAllStringFunc(s, a.id)
	return steamRe.ReplaceAllStringFunc(s, a.steamID)
}

// link replaces the IDs in a FACEIT link and the nickname after /players/.
func (a *anonymizer) link(s string) string {
	parts := strings.Split(a.text(s), "/")
	for i := 1; i < len(parts); i++ {
		if parts[i-1] == "players" && parts[i] != "" {
			parts[i] = a.nickname(parts[i])
		}
	}
	return strings.Join(parts, "/")
}

// fakeRe matches the fake IDs and SteamIDs, which check skips: a real name
// may happen to be a part of them.
var fakeRe = regexp.MustCompile(`00000000-0000-4000-8000-\d{12}|76561190\d{9}`)

// check fails if a real value replaced anywhere is still found in a string
// of data, whatever the letter case. It names the fake value and where it
// was found, not the real value.
func (a *anonymizer) check(name string, data any) error {
	return a.checkValue(name, "", data)
}

func (a *anonymizer) checkValue(name, path string, v any) error {
	switch v := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(v) {
			if err := a.checkValue(name, path+"."+k, v[k]); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range v {
			if err := a.checkValue(name, fmt.Sprintf("%s[%d]", path, i), item); err != nil {
				return err
			}
		}
	case string:
		lower := strings.ToLower(v)
		leak := func(fake string) error {
			return fmt.Errorf("%s: the real value behind %q is still in %s, nothing written", name, fake, path)
		}
		stripped := fakeRe.ReplaceAllString(lower, "")
		for orig, fake := range a.secretIDs {
			if len(orig) >= 3 && strings.Contains(stripped, orig) {
				return leak(fake)
			}
		}
		// Names shorter than FACEIT allows, Steam names like "1", would match
		// ordinary values such as best_of.
		if fake, ok := a.secretNames[lower]; ok && len(lower) >= 3 {
			return leak(fake)
		}
		for _, segment := range strings.Split(lower, "/") {
			if fake, ok := a.secretNames[segment]; ok && len(segment) >= 3 {
				return leak(fake)
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
