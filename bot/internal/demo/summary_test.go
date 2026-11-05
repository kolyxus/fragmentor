package demo

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The builder is tested on made-up matches: teams of three are enough.

func human(id uint64, side Side) *playerRef {
	return &playerRef{ID: id, Name: fmt.Sprintf("p%d", id), Side: side}
}

func bot(side Side) *playerRef { return &playerRef{Name: "BOT", Side: side, Bot: true} }

// sides returns the players of team A (1, 2, 3) on sideA and of team B
// (4, 5, 6) on the other side.
func sides(sideA Side) []playerRef {
	sideB := T
	if sideA == T {
		sideB = CT
	}
	var out []playerRef
	for id := uint64(1); id <= 6; id++ {
		s := sideA
		if id > 3 {
			s = sideB
		}
		out = append(out, *human(id, s))
	}
	return out
}

func end(winner Side, players []playerRef) roundEndEvent {
	return roundEndEvent{Winner: winner, Reason: "t_eliminated", Players: players}
}

func playerByID(t *testing.T, m Match, id uint64) Player {
	t.Helper()
	for _, p := range m.Players {
		if p.SteamID64 == id {
			return p
		}
	}
	t.Fatalf("no player %d in %+v", id, m.Players)
	return Player{}
}

// game returns the game's own score for b's rounds, as if it agreed.
func game(b *builder) gameScore {
	if len(b.rounds) == 0 {
		return gameScore{}
	}
	ct := b.rounds[len(b.rounds)-1].ctTeam
	return gameScore{CT: b.score[ct], T: b.score[1-ct], Rounds: len(b.rounds)}
}

func TestBuilderKillsDeathsAssists(t *testing.T) {
	b := newBuilder()
	ct1, ct2 := human(1, CT), human(2, CT)
	t1, t2 := human(4, T), human(5, T)
	b.kill(killEvent{Killer: ct1, Victim: t1, Assister: ct2, Headshot: true})
	b.kill(killEvent{Killer: ct1, Victim: t2})
	b.kill(killEvent{Killer: t2, Victim: ct2, Assister: t1}) // t2 died already; events are what they are
	// A teamkill and a suicide give no kill; the victim dies all the same.
	b.kill(killEvent{Killer: ct1, Victim: ct2, Headshot: true})
	b.kill(killEvent{Killer: t1, Victim: t1})
	// The world: the bomb, a fall.
	b.kill(killEvent{Victim: t2})
	// An assist on a teammate's death does not count.
	b.kill(killEvent{Killer: t1, Victim: ct1, Assister: ct2})
	b.roundEnd(end(T, []playerRef{*ct1, *ct2, *t1, *t2}))
	m := b.build(24, game(b))

	for _, tc := range []struct {
		id                         uint64
		kills, deaths, assists, hs int
	}{
		{1, 2, 1, 0, 1},
		{2, 0, 2, 1, 0},
		{4, 1, 2, 1, 0}, // killed and the suicide
		{5, 1, 2, 0, 0}, // killed and the world
	} {
		p := playerByID(t, m, tc.id)
		if p.Kills != tc.kills || p.Deaths != tc.deaths || p.Assists != tc.assists || p.Headshots != tc.hs {
			t.Errorf("player %d: K/D/A %d/%d/%d HS %d, want %d/%d/%d HS %d",
				tc.id, p.Kills, p.Deaths, p.Assists, p.Headshots, tc.kills, tc.deaths, tc.assists, tc.hs)
		}
	}
}

func TestBuilderDamage(t *testing.T) {
	b := newBuilder()
	ct1, ct2, t1 := human(1, CT), human(2, CT), human(4, T)
	b.hurt(hurtEvent{Attacker: ct1, Victim: t1, Damage: 27})
	b.hurt(hurtEvent{Attacker: ct1, Victim: t1, Damage: 73})
	b.hurt(hurtEvent{Attacker: ct1, Victim: ct2, Damage: 40}) // a teammate
	b.hurt(hurtEvent{Attacker: ct1, Victim: ct1, Damage: 15}) // own grenade
	b.hurt(hurtEvent{Attacker: ct1, Victim: bot(T), Damage: 50})
	b.hurt(hurtEvent{Attacker: bot(T), Victim: ct1, Damage: 30})
	b.hurt(hurtEvent{Victim: ct2, Damage: 10}) // the world
	b.roundEnd(end(CT, []playerRef{*ct1, *ct2, *t1}))
	m := b.build(24, game(b))

	if got := playerByID(t, m, 1).Damage; got != 150 {
		t.Errorf("damage of player 1 = %d, want 150: enemies and an enemy bot only", got)
	}
	if got := playerByID(t, m, 2).Damage; got != 0 {
		t.Errorf("damage of player 2 = %d, want 0", got)
	}
}

func TestBuilderBotsAreNotPlayers(t *testing.T) {
	b := newBuilder()
	ct1, t1 := human(1, CT), human(4, T)
	b.kill(killEvent{Killer: bot(T), Victim: ct1})
	b.kill(killEvent{Killer: ct1, Victim: bot(T), Assister: bot(CT)})
	b.kill(killEvent{Killer: t1, Victim: bot(CT), Headshot: true})
	b.roundEnd(end(CT, []playerRef{*ct1, *t1}))
	m := b.build(24, game(b))

	if len(m.Players) != 2 {
		t.Fatalf("players = %+v, want the two humans", m.Players)
	}
	if p := playerByID(t, m, 1); p.Kills != 1 || p.Deaths != 1 {
		t.Errorf("player 1: K/D %d/%d, want 1/1: a bot's kill is a death, a killed bot a kill", p.Kills, p.Deaths)
	}
	if p := playerByID(t, m, 4); p.Kills != 1 || p.Headshots != 1 {
		t.Errorf("player 4: kills %d HS %d, want 1 and 1", p.Kills, p.Headshots)
	}
}

func TestBuilderResetForgetsTheWarmup(t *testing.T) {
	b := newBuilder()
	b.kill(killEvent{Killer: human(1, CT), Victim: human(4, T)})
	b.hurt(hurtEvent{Attacker: human(1, CT), Victim: human(4, T), Damage: 100})
	b.roundEnd(end(CT, sides(CT)))
	b.warnf("before the start")

	b.reset()
	b.kill(killEvent{Killer: human(4, CT), Victim: human(1, T)})
	b.roundEnd(end(CT, sides(T)))
	m := b.build(24, game(b))

	if len(m.Rounds) != 1 || m.Rounds[0].Number != 1 {
		t.Errorf("rounds = %+v, want the one after the start", m.Rounds)
	}
	if p := playerByID(t, m, 1); p.Kills != 0 || p.Deaths != 1 || p.Damage != 0 {
		t.Errorf("player 1 = %+v, want only the death after the start", p)
	}
	if len(m.Warnings) != 0 {
		t.Errorf("warnings = %q, want none", m.Warnings)
	}
	// Team B played the first round after the start as CT, so it is team 0.
	if p := playerByID(t, m, 4); p.Team != 0 || p.StartSide != CT {
		t.Errorf("player 4: team %d, start side %s; want 0, CT", p.Team, p.StartSide)
	}
}

func opposite(s Side) Side {
	if s == CT {
		return T
	}
	return CT
}

// TestBuilderScoreAcrossSidesAndOvertime plays MR12 with an overtime of MR3:
// team A starts as CT, the sides switch after round 12 and in the middle of
// the overtime, after round 27, but not when the overtime starts.
func TestBuilderScoreAcrossSidesAndOvertime(t *testing.T) {
	b := newBuilder()
	aSide := func(n int) Side {
		if n <= 12 || n >= 28 {
			return CT
		}
		return T
	}
	// Team A wins rounds 1-8 and 13-16, team B the rest of regulation:
	// 12:12. Team A wins the overtime 4:0, from round 25 to 28.
	for n := 1; n <= 28; n++ {
		side := aSide(n)
		winner := side
		if aWins := n <= 8 || (n >= 13 && n <= 16) || n >= 25; !aWins {
			winner = opposite(side)
		}
		e := end(winner, sides(side))
		e.Tick = n * 100
		b.roundEnd(e)
	}
	m := b.build(24, game(b))

	wantA := Team{StartSide: CT, Score: 16, FirstHalf: 8, SecondHalf: 4, Overtime: 4}
	wantB := Team{StartSide: T, Score: 12, FirstHalf: 4, SecondHalf: 8, Overtime: 0}
	if m.Teams[0] != wantA || m.Teams[1] != wantB {
		t.Errorf("teams = %+v, want %+v and %+v", m.Teams, wantA, wantB)
	}
	if len(m.Rounds) != 28 {
		t.Fatalf("%d rounds, want 28", len(m.Rounds))
	}
	for _, tc := range []struct {
		n, winner int
		side      Side
		score     [2]int
	}{
		{1, 0, CT, [2]int{1, 0}},
		{9, 1, T, [2]int{8, 1}},
		{13, 0, T, [2]int{9, 4}},
		{17, 1, CT, [2]int{12, 5}},
		{24, 1, CT, [2]int{12, 12}},
		{25, 0, T, [2]int{13, 12}},
		{28, 0, CT, [2]int{16, 12}},
	} {
		r := m.Rounds[tc.n-1]
		if r.Number != tc.n || r.Winner != tc.winner || r.WinnerSide != tc.side || r.Score != tc.score || r.EndTick != tc.n*100 {
			t.Errorf("round %d = %+v, want winner %d (%s), score %v", tc.n, r, tc.winner, tc.side, tc.score)
		}
	}
	for id := uint64(1); id <= 6; id++ {
		p := playerByID(t, m, id)
		wantTeam, wantSide := 0, CT
		if id > 3 {
			wantTeam, wantSide = 1, T
		}
		if p.Team != wantTeam || p.StartSide != wantSide {
			t.Errorf("player %d: team %d (%s), want %d (%s)", id, p.Team, p.StartSide, wantTeam, wantSide)
		}
	}
	if len(m.Warnings) != 0 {
		t.Errorf("warnings = %q, want none", m.Warnings)
	}
}

// A player who joins in the second half, when team B plays CT, is in team B.
func TestBuilderPlayerJoinsLater(t *testing.T) {
	b := newBuilder()
	for n := 1; n <= 12; n++ {
		b.roundEnd(end(CT, sides(CT)))
	}
	late := human(7, CT) // team A is T now
	b.kill(killEvent{Killer: late, Victim: human(1, T)})
	b.roundEnd(end(CT, append(sides(T), *late)))
	m := b.build(24, game(b))

	if p := playerByID(t, m, 7); p.Team != 1 || p.StartSide != T || p.Kills != 1 {
		t.Errorf("late player = %+v, want team 1, which started as T, with a kill", p)
	}
	if got := m.Rounds[12].Winner; got != 1 {
		t.Errorf("round 13 won by team %d, want 1: CT in the second half", got)
	}
}

func TestBuilderComparesWithTheGame(t *testing.T) {
	b := newBuilder()
	b.roundEnd(end(CT, sides(CT)))
	b.roundEnd(end(T, sides(CT)))
	m := b.build(24, gameScore{CT: 1, T: 1, Rounds: 2})
	if len(m.Warnings) != 0 || m.Suspicious {
		t.Errorf("warnings = %q, suspicious %t; want none", m.Warnings, m.Suspicious)
	}

	// The game played a round more than the demo showed, a round we lost.
	b = newBuilder()
	b.roundEnd(end(CT, sides(CT)))
	m = b.build(24, gameScore{CT: 1, T: 1, Rounds: 2})
	if !m.Suspicious || len(m.Warnings) != 2 ||
		!strings.Contains(m.Warnings[0], "1 rounds were played since the start, the game counted 2") ||
		!strings.Contains(m.Warnings[1], "the score 1:0 (CT:T) differs from the game's 1:1") {
		t.Errorf("suspicious %t, warnings = %q; want the rounds and the score", m.Suspicious, m.Warnings)
	}
}

func TestBuilderRoundWithoutWinner(t *testing.T) {
	b := newBuilder()
	b.roundEnd(roundEndEvent{Tick: 640, Players: sides(CT)})
	b.roundEnd(end(T, sides(CT)))
	m := b.build(24, gameScore{T: 1, Rounds: 2})
	if len(m.Rounds) != 1 || m.Rounds[0].Number != 1 || m.Teams[1].Score != 1 {
		t.Errorf("rounds = %+v, teams = %+v; want one round won by team 1", m.Rounds, m.Teams)
	}
	// The round is not lost silently: the summary is suspicious.
	if !m.Suspicious || len(m.Warnings) != 2 ||
		!strings.Contains(m.Warnings[0], "without a winner at tick 640") ||
		!strings.Contains(m.Warnings[1], "2 rounds were played since the start, the score adds up to 1") {
		t.Errorf("suspicious %t, warnings = %q", m.Suspicious, m.Warnings)
	}
}

func TestBuilderTeamNamesFollowSides(t *testing.T) {
	b := newBuilder()
	e := end(CT, sides(CT))
	e.Names = map[Side]string{CT: "team_a", T: "team_b"}
	b.roundEnd(e)
	e = end(CT, sides(T))
	e.Names = map[Side]string{CT: "team_b", T: ""}
	b.roundEnd(e)
	m := b.build(24, game(b))
	if m.Teams[0].Name != "team_a" || m.Teams[1].Name != "team_b" {
		t.Errorf("names = %q, %q; want team_a, team_b", m.Teams[0].Name, m.Teams[1].Name)
	}
}

func TestBuilderPlayersOrder(t *testing.T) {
	b := newBuilder()
	b.kill(killEvent{Killer: human(5, T), Victim: human(1, CT)})
	b.kill(killEvent{Killer: human(2, CT), Victim: human(4, T)})
	b.kill(killEvent{Killer: human(2, CT), Victim: human(6, T)})
	b.kill(killEvent{Killer: human(3, CT), Victim: human(5, T)})
	b.roundEnd(end(CT, sides(CT)))
	m := b.build(24, game(b))
	var got []uint64
	for _, p := range m.Players {
		got = append(got, p.SteamID64)
	}
	if want := []uint64{2, 3, 1, 5, 4, 6}; !slices.Equal(got, want) {
		t.Errorf("players = %v, want %v: by team, then by kills", got, want)
	}
}

func TestBuilderSkipsNobody(t *testing.T) {
	b := newBuilder()
	// A kill without a victim and hits without players must not panic.
	b.kill(killEvent{Killer: human(1, CT)})
	b.hurt(hurtEvent{Attacker: human(1, CT), Damage: 10})
	b.hurt(hurtEvent{Victim: human(4, T), Damage: 10})
	// A spectator who never played is not a player of the match.
	b.see(&playerRef{ID: 9, Name: "spec"})
	b.roundEnd(end(CT, sides(CT)))
	m := b.build(24, game(b))
	if len(m.Players) != 6 {
		t.Errorf("players = %+v, want the six who played", m.Players)
	}
}
