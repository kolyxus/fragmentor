package demo

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// The journal of a match is what the bot keeps of its demo, which it never
// keeps: the summary and the events of the Match, every metric and finding
// can be counted again from. It holds no names and no SteamIDs: Anonymize
// replaces the players by their seats, 1 to the number of players, and the
// database keeps who sat where apart from the journal (docs/DECISIONS.md,
// 2026-11-09, the storage).

// JournalVersion is the version of what a journal holds. Bump it whenever
// the parser or the events change what a Match holds: a match whose journal
// is of an older version needs its demo again, a fresh link.
// 2: the killer, the assister and the attacker are bodies, a bot that a
// player controlled is the bot, and the place of the killer is his body's.
const JournalVersion = 2

// maxJournal bounds a decoded journal: the largest of bot/testdata is 1.2 MiB.
const maxJournal = 64 << 20

// ErrBadJournal means a journal cannot be read.
var ErrBadJournal = errors.New("demo: bad journal")

// Anonymize returns a copy of m in which every player is his seat: the
// SteamID64 of the player i of m.Players, and of his refs in the events, is
// i+1. Names of players and teams are gone. Someone in the events who is no
// player of m gets a seat after the players. Bots keep their IDs, which tell
// nobody apart beyond the match. seats[i] is the SteamID64 of seat i+1.
func Anonymize(m *Match) (anon *Match, seats []uint64) {
	seatOf := make(map[uint64]uint64, len(m.Players))
	for i, p := range m.Players {
		seatOf[p.SteamID64] = uint64(i + 1)
		seats = append(seats, p.SteamID64)
	}
	next := uint64(len(m.Players))
	seat := func(id uint64) uint64 {
		if id == 0 {
			return 0
		}
		s, ok := seatOf[id]
		if !ok {
			next++
			s = next
			seatOf[id] = s
		}
		return s
	}
	ref := func(r Ref) Ref {
		if !r.Bot {
			r.ID = seat(r.ID)
		}
		r.Controller = seat(r.Controller)
		return r
	}
	refp := func(r *Ref) *Ref {
		if r == nil {
			return nil
		}
		c := ref(*r)
		return &c
	}

	out := *m
	out.Teams[0].Name, out.Teams[1].Name = "", ""
	out.Warnings = slices.Clone(m.Warnings)
	out.Players = slices.Clone(m.Players)
	for i := range out.Players {
		out.Players[i].SteamID64 = uint64(i + 1)
		out.Players[i].Name = ""
	}
	out.Rounds = slices.Clone(m.Rounds)
	for i := range out.Rounds {
		r := &out.Rounds[i]
		r.Alive = slices.Clone(r.Alive)
		for j := range r.Alive {
			r.Alive[j] = ref(r.Alive[j])
		}
		r.Equipment = slices.Clone(r.Equipment)
		for j := range r.Equipment {
			r.Equipment[j].Body = ref(r.Equipment[j].Body)
		}
	}
	out.Kills = slices.Clone(m.Kills)
	for i := range out.Kills {
		k := &out.Kills[i]
		k.Killer, k.Victim, k.Assister = refp(k.Killer), ref(k.Victim), refp(k.Assister)
		k.Grenades = slices.Clone(k.Grenades)
		k.Positions = slices.Clone(k.Positions)
		for j := range k.Positions {
			k.Positions[j].Body = ref(k.Positions[j].Body)
		}
	}
	out.Hits = slices.Clone(m.Hits)
	for i := range out.Hits {
		h := &out.Hits[i]
		h.Attacker, h.Victim = refp(h.Attacker), ref(h.Victim)
	}
	out.Shots = slices.Clone(m.Shots)
	for i := range out.Shots {
		out.Shots[i].Shooter = ref(out.Shots[i].Shooter)
	}
	out.Sightings = slices.Clone(m.Sightings)
	for i := range out.Sightings {
		s := &out.Sightings[i]
		s.Spotter, s.Target = ref(s.Spotter), ref(s.Target)
	}
	out.Grenades = slices.Clone(m.Grenades)
	for i := range out.Grenades {
		out.Grenades[i].Thrower = ref(out.Grenades[i].Thrower)
	}
	out.Flashes = slices.Clone(m.Flashes)
	for i := range out.Flashes {
		f := &out.Flashes[i]
		f.Thrower, f.Victim = refp(f.Thrower), ref(f.Victim)
	}
	return &out, seats
}

// journalWindow is the window of the zstd encoder of journals. The JSON of a
// journal of bot/testdata is up to 1.2 MiB and fits in it; the default window
// of 8 MiB compresses it no better, and its encoder holds 17 MiB for good,
// this one 5 MiB.
const journalWindow = 2 << 20

var (
	journalEncoder = sync.OnceValues(func() (*zstd.Encoder, error) {
		return zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(journalWindow))
	})
	journalDecoder = sync.OnceValues(func() (*zstd.Decoder, error) {
		return zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxJournal))
	})
)

// EncodeJournal returns the journal of m: its JSON compressed with zstd. m
// should be anonymous, see Anonymize: the journal keeps whatever m holds.
func EncodeJournal(m *Match) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("demo: encode journal: %w", err)
	}
	enc, err := journalEncoder()
	if err != nil {
		return nil, fmt.Errorf("demo: encode journal: %w", err)
	}
	return enc.EncodeAll(b, nil), nil
}

// DecodeJournal reads a journal of EncodeJournal. A journal that is no zstd,
// decodes to more than maxJournal or is no JSON of a Match fails with
// ErrBadJournal.
func DecodeJournal(b []byte) (*Match, error) {
	dec, err := journalDecoder()
	if err != nil {
		return nil, fmt.Errorf("demo: decode journal: %w", err)
	}
	raw, err := dec.DecodeAll(b, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadJournal, err)
	}
	var m Match
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadJournal, err)
	}
	return &m, nil
}
