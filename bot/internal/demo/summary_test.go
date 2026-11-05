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

// endRound plays a round as the demo does: the freeze time ends with the
// players of the round's end alive, then the round ends.
func endRound(b *builder, e roundEndEvent) {
	b.freezeTimeEnd(e.Tick, e.Players)
	b.roundEnd(e)
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
	endRound(b, end(T, []playerRef{*ct1, *ct2, *t1, *t2}))
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
	endRound(b, end(CT, []playerRef{*ct1, *ct2, *t1}))
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
	endRound(b, end(CT, []playerRef{*ct1, *t1}))
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
	endRound(b, end(CT, sides(CT)))
	b.warnf("before the start")

	b.reset()
	b.kill(killEvent{Killer: human(4, CT), Victim: human(1, T)})
	endRound(b, end(CT, sides(T)))
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
		endRound(b, e)
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
		endRound(b, end(CT, sides(CT)))
	}
	late := human(7, CT) // team A is T now
	b.kill(killEvent{Killer: late, Victim: human(1, T)})
	endRound(b, end(CT, append(sides(T), *late)))
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
	endRound(b, end(CT, sides(CT)))
	endRound(b, end(T, sides(CT)))
	m := b.build(24, gameScore{CT: 1, T: 1, Rounds: 2})
	if len(m.Warnings) != 0 || m.Suspicious {
		t.Errorf("warnings = %q, suspicious %t; want none", m.Warnings, m.Suspicious)
	}

	// The game played a round more than the demo showed, a round we lost.
	b = newBuilder()
	endRound(b, end(CT, sides(CT)))
	m = b.build(24, gameScore{CT: 1, T: 1, Rounds: 2})
	if !m.Suspicious || len(m.Warnings) != 2 ||
		!strings.Contains(m.Warnings[0], "1 rounds were played since the start, the game counted 2") ||
		!strings.Contains(m.Warnings[1], "the score 1:0 (CT:T) differs from the game's 1:1") {
		t.Errorf("suspicious %t, warnings = %q; want the rounds and the score", m.Suspicious, m.Warnings)
	}
}

func TestBuilderRoundWithoutWinner(t *testing.T) {
	b := newBuilder()
	endRound(b, roundEndEvent{Tick: 640, Players: sides(CT)})
	endRound(b, end(T, sides(CT)))
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
	endRound(b, e)
	e = end(CT, sides(T))
	e.Names = map[Side]string{CT: "team_b", T: ""}
	endRound(b, e)
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
	endRound(b, end(CT, sides(CT)))
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
	endRound(b, end(CT, sides(CT)))
	m := b.build(24, game(b))
	if len(m.Players) != 6 {
		t.Errorf("players = %+v, want the six who played", m.Players)
	}
}

func TestBuilderLogsEvents(t *testing.T) {
	b := newBuilder()
	ct1, t1 := human(1, CT), human(4, T)
	// The warmup is forgotten, events and all.
	b.kill(killEvent{Tick: 5, Killer: ct1, Victim: t1})
	b.reset()

	b.hurt(hurtEvent{Tick: 10, Attacker: ct1, Victim: t1, Damage: 27, HitGroup: HitHead, Weapon: "AK-47", Class: ClassRifle})
	b.kill(killEvent{Tick: 11, Killer: ct1, Victim: t1, Assister: human(2, CT), FlashAssist: true, Headshot: true, Weapon: "AK-47", Class: ClassRifle})
	b.kill(killEvent{Tick: 12, Killer: ct1, FlashAssist: true}) // nobody died: not logged
	endRound(b, roundEndEvent{Tick: 20, Winner: CT, Players: sides(CT)})
	// After the end of round 1 until round 2 starts: the bomb, a late kill.
	b.kill(killEvent{Tick: 21, Victim: human(5, T), Weapon: "C4", Class: ClassBomb})
	b.roundStart(25)
	b.kill(killEvent{Tick: 30, Killer: human(6, T), Victim: human(6, T), Weapon: "World", Class: ClassWorld, Disconnect: true})
	b.kill(killEvent{Tick: 31, Killer: t1, Victim: ct1, FlashAssist: true, Weapon: "Glock-18", Class: ClassPistol})
	b.leftServer(len(b.kills) - 1)
	b.leftServer(99) // out of the log: ignored
	endRound(b, roundEndEvent{Tick: 40, Winner: T, Players: sides(CT)})
	m := b.build(24, game(b))

	type got struct {
		tick, round   int
		after, flash  bool
		head, discon  bool
		killer, vicID uint64
	}
	var kills []got
	for _, k := range m.Kills {
		var killer uint64
		if k.Killer != nil {
			killer = k.Killer.ID
		}
		kills = append(kills, got{k.Tick, k.Round, k.AfterRoundEnd, k.FlashAssist, k.Headshot, k.Disconnect, killer, k.Victim.ID})
	}
	want := []got{
		{11, 1, false, true, true, false, 1, 4},
		{21, 1, true, false, false, false, 0, 5},
		{30, 2, false, false, false, true, 6, 6},
		{31, 2, false, false, false, true, 4, 1}, // marked by leftServer; a flash assist needs an assister
	}
	if !slices.Equal(kills, want) {
		t.Errorf("kills =\n%+v\nwant\n%+v", kills, want)
	}
	if len(m.Hits) != 1 {
		t.Fatalf("hits = %+v, want the one after the start", m.Hits)
	}
	if h := m.Hits[0]; h.Round != 1 || h.Tick != 10 || h.Damage != 27 || h.HitGroup != HitHead || h.Class != ClassRifle || h.Attacker.ID != 1 || h.Victim.ID != 4 {
		t.Errorf("hit = %+v", h)
	}
	if k := m.Kills[0]; k.Assister == nil || k.Assister.ID != 2 || k.Assister.Side != CT || k.Weapon != "AK-47" {
		t.Errorf("kill = %+v, want the assister and the weapon", k)
	}
}

func TestBuilderAlive(t *testing.T) {
	b := newBuilder()
	b.freezeTimeEnd(100, append(sides(CT), *bot(T)))
	endRound(b, roundEndEvent{Tick: 900, Winner: CT, Players: sides(CT)})
	// The demo lacks the start of round 2: the end of its freeze time starts it.
	b.kill(killEvent{Tick: 950, Killer: human(1, CT), Victim: human(4, T)})
	b.freezeTimeEnd(1000, sides(CT))
	b.kill(killEvent{Tick: 1010, Killer: human(1, CT), Victim: human(5, T)})
	b.roundEnd(roundEndEvent{Tick: 1900, Winner: CT, Players: sides(CT)})
	m := b.build(24, game(b))

	// endRound noted the players of the end again at tick 900; the
	// roster at 100 had a bot.
	if r := m.Rounds[0]; r.FreezeEndTick != 900 || len(r.Alive) != 6 {
		t.Errorf("round 1: freeze end %d, alive %+v", r.FreezeEndTick, r.Alive)
	}
	if r := m.Rounds[1]; r.FreezeEndTick != 1000 || len(r.Alive) != 6 || r.Alive[0] != (Ref{ID: 1, Side: CT}) {
		t.Errorf("round 2: freeze end %d, alive %+v", r.FreezeEndTick, r.Alive)
	}
	if got := []int{m.Kills[0].Round, m.Kills[1].Round}; !slices.Equal(got, []int{1, 2}) || !m.Kills[0].AfterRoundEnd || m.Kills[1].AfterRoundEnd {
		t.Errorf("kills = %+v, want one after round 1, one in round 2", m.Kills)
	}
	if len(m.Warnings) != 0 {
		t.Errorf("warnings = %q", m.Warnings)
	}

	// A round whose freeze time never ended: who was alive is unknown.
	b = newBuilder()
	b.roundEnd(roundEndEvent{Tick: 900, Winner: CT, Players: sides(CT)})
	m = b.build(24, game(b))
	if !m.Suspicious || len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "round 1 has no end of its freeze time") {
		t.Errorf("suspicious %t, warnings %q", m.Suspicious, m.Warnings)
	}
}

func TestRefExport(t *testing.T) {
	var none *playerRef
	if none.export() != nil {
		t.Error("nil exports to a ref")
	}
	p := playerRef{ID: 12, Name: "BOT Eli", Side: T, Bot: true, Controller: 76561190000000001}
	if got := *p.export(); got != (Ref{ID: 12, Bot: true, Side: T, Controller: 76561190000000001}) {
		t.Errorf("export = %+v", got)
	}
}

func TestRoundSide(t *testing.T) {
	for _, tc := range []struct {
		winner     int
		winnerSide Side
		team       int
		want       Side
	}{
		{0, CT, 0, CT}, {0, CT, 1, T},
		{0, T, 0, T}, {0, T, 1, CT},
		{1, CT, 1, CT}, {1, T, 0, CT},
	} {
		r := Round{Winner: tc.winner, WinnerSide: tc.winnerSide}
		if got := r.Side(tc.team); got != tc.want {
			t.Errorf("round won by %d on %s: team %d played %s, want %s", tc.winner, tc.winnerSide, tc.team, got, tc.want)
		}
	}
}

func TestBuilderSightings(t *testing.T) {
	b := newBuilder()
	ct1, ct2, t1, t2 := human(1, CT), human(2, CT), human(4, T), human(5, T)
	b.freezeTimeEnd(100, sides(CT))
	b.spotted(110, t1, []playerRef{*ct1, *ct2, *t2}) // the flag of a teammate does not count
	b.spotted(120, t1, []playerRef{*ct2})            // ct1 lost sight of t1
	b.spotted(130, ct1, []playerRef{*t1})
	b.kill(killEvent{Tick: 140, Killer: ct2, Victim: t1}) // t1 neither sees nor is seen any more
	b.spotted(150, t1, []playerRef{*ct2})                 // the flag of a dead body
	b.spotted(150, ct2, []playerRef{*t1, *t2})            // t1 is dead, t2 sees ct2
	b.spotted(155, ct1, []playerRef{*t2})
	// ct1 leaves the server and is back: a disconnect is no death.
	b.kill(killEvent{Tick: 160, Killer: ct1, Victim: ct1, Disconnect: true, Weapon: "World", Class: ClassWorld})
	b.spotted(170, ct1, []playerRef{*t2})
	b.roundEnd(roundEndEvent{Tick: 200, Winner: CT, Players: sides(CT)})
	b.spotted(210, ct2, []playerRef{*t2}) // no change: still in sight
	b.roundStart(300)                     // the sightings of round 1 end, the dead live
	b.freezeTimeEnd(400, sides(CT))
	b.spotted(410, t1, []playerRef{*ct1}) // never lost: ends with the demo
	b.roundEnd(roundEndEvent{Tick: 500, Winner: CT, Players: sides(CT)})
	m := b.build(24, game(b))

	ref := func(p *playerRef) Ref { return *p.export() }
	want := []Sighting{
		{Round: 1, Spotter: ref(ct1), Target: ref(t1), From: 110, To: 120},
		{Round: 1, Spotter: ref(ct2), Target: ref(t1), From: 110, To: 140},
		{Round: 1, Spotter: ref(t1), Target: ref(ct1), From: 130, To: 140},
		{Round: 1, Spotter: ref(t2), Target: ref(ct2), From: 150, To: 300},
		{Round: 1, Spotter: ref(t2), Target: ref(ct1), From: 155, To: 160},
		{Round: 1, Spotter: ref(t2), Target: ref(ct1), From: 170, To: 300},
		{Round: 2, Spotter: ref(ct1), Target: ref(t1), From: 410, To: 500},
	}
	if !slices.Equal(m.Sightings, want) {
		t.Errorf("sightings =\n%+v\nwant\n%+v", m.Sightings, want)
	}
}

func TestBuilderShotsAndFlashes(t *testing.T) {
	b := newBuilder()
	b.shot(shotEvent{Tick: 5, Shooter: human(1, CT), Weapon: "AK-47", Class: ClassRifle})
	b.flash(flashEvent{Tick: 6, Thrower: human(4, T), Victim: human(1, CT), Duration: 2})
	b.reset() // the warmup is forgotten
	b.freezeTimeEnd(100, sides(CT))
	b.shot(shotEvent{Tick: 110, Shooter: human(1, CT), Weapon: "M4A4", Class: ClassRifle, Speed: 50, MaxSpeed: 225})
	b.shot(shotEvent{Tick: 111, Weapon: "M4A4", Class: ClassRifle}) // nobody fired: not logged
	b.flash(flashEvent{Tick: 120, Thrower: human(4, T), Victim: human(1, CT), Duration: 2.5})
	b.flash(flashEvent{Tick: 121, Thrower: human(4, T), Duration: 3}) // nobody blinded: not logged
	b.roundEnd(roundEndEvent{Tick: 200, Winner: CT, Players: sides(CT)})
	b.shot(shotEvent{Tick: 210, Shooter: human(5, T), Weapon: "Glock-18", Class: ClassPistol, Speed: 3, MaxSpeed: 240})
	b.flash(flashEvent{Tick: 220, Victim: bot(T), Duration: 1}) // the thrower is unknown
	m := b.build(24, game(b))

	wantShots := []Shot{
		{Tick: 110, Round: 1, Shooter: Ref{ID: 1, Side: CT}, Weapon: "M4A4", Class: ClassRifle, Speed: 50, MaxSpeed: 225},
		{Tick: 210, Round: 1, AfterRoundEnd: true, Shooter: Ref{ID: 5, Side: T}, Weapon: "Glock-18", Class: ClassPistol, Speed: 3, MaxSpeed: 240},
	}
	if !slices.Equal(m.Shots, wantShots) {
		t.Errorf("shots =\n%+v\nwant\n%+v", m.Shots, wantShots)
	}
	if len(m.Flashes) != 2 {
		t.Fatalf("flashes = %+v, want two", m.Flashes)
	}
	if f := m.Flashes[0]; f.Tick != 120 || f.Round != 1 || f.AfterRoundEnd || f.Thrower == nil || *f.Thrower != (Ref{ID: 4, Side: T}) ||
		f.Victim != (Ref{ID: 1, Side: CT}) || f.Duration != 2.5 {
		t.Errorf("flash 1 = %+v", f)
	}
	if f := m.Flashes[1]; f.Tick != 220 || f.Round != 1 || !f.AfterRoundEnd || f.Thrower != nil || f.Victim != (Ref{Bot: true, Side: T}) || f.Duration != 1 {
		t.Errorf("flash 2 = %+v", f)
	}
}

func TestBuilderKillGrenades(t *testing.T) {
	b := newBuilder()
	b.kill(killEvent{Tick: 10, Killer: human(4, T), Victim: human(1, CT), Grenades: []string{Flashbang, Flashbang, HEGrenade}})
	b.kill(killEvent{Tick: 11, Killer: human(1, CT), Victim: human(5, T)})
	endRound(b, end(CT, sides(CT)))
	m := b.build(24, game(b))
	if got := m.Kills[0].Grenades; !slices.Equal(got, []string{Flashbang, Flashbang, HEGrenade}) {
		t.Errorf("grenades of the first victim = %q", got)
	}
	if got := m.Kills[1].Grenades; got != nil {
		t.Errorf("grenades of the second victim = %q, want none", got)
	}
}
