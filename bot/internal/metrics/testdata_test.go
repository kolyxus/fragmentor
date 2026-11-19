package metrics

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
	"github.com/kolyxus/fragmentor/bot/internal/testdemos"
)

// TestComputeTestdata computes the metrics of the demos in bot/testdata (see
// internal/testdemos) and checks what holds for any match: the totals as
// FACEIT counts them are the summary's, which go run ./cmd/parsedemo -faceit
// checks against FACEIT, the coaching metrics stay within their bounds, and
// the events in their context add up to the metrics. The demos are parsed in
// parallel, as each takes seconds.
func TestComputeTestdata(t *testing.T) {
	for _, file := range testdemos.Files(t) {
		t.Run(testdemos.Name(file), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			m, err := demo.ParseFile(ctx, file, demo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			a := Analyze(m)
			checkMatch(t, m, a.Players)
			checkAnalysis(t, m, a)
			// The journal keeps the anonymous match, which the metrics are
			// counted again from: they are the same, the seats for the IDs.
			anon, seats := demo.Anonymize(m)
			if again := fromSeats(Analyze(anon), seats); !reflect.DeepEqual(again, a) {
				t.Errorf("the metrics of the anonymous match differ from those of the match: %s", diffAnalysis(again, a))
			}
		})
	}
}

// diffAnalysis names the first field in which a and b differ, and the first
// element of it.
func diffAnalysis(a, b *Analysis) string {
	va, vb := reflect.ValueOf(*a), reflect.ValueOf(*b)
	for i := range va.NumField() {
		fa, fb := va.Field(i), vb.Field(i)
		if reflect.DeepEqual(fa.Interface(), fb.Interface()) {
			continue
		}
		name := va.Type().Field(i).Name
		if fa.Kind() != reflect.Slice {
			return fmt.Sprintf("%s: %v and %v", name, fa.Interface(), fb.Interface())
		}
		if fa.Len() != fb.Len() {
			return fmt.Sprintf("%s: %d and %d", name, fa.Len(), fb.Len())
		}
		for j := range fa.Len() {
			if !reflect.DeepEqual(fa.Index(j).Interface(), fb.Index(j).Interface()) {
				return fmt.Sprintf("%s[%d]: %+v and %+v", name, j, fa.Index(j).Interface(), fb.Index(j).Interface())
			}
		}
	}
	return "nothing"
}

// fromSeats puts the SteamIDs of seats, seats[i] for seat i+1, in place of
// the seats in a, the analysis of an anonymous match: see demo.Anonymize.
func fromSeats(a *Analysis, seats []uint64) *Analysis {
	id := func(seat uint64) uint64 {
		if seat == 0 || seat > uint64(len(seats)) {
			return seat
		}
		return seats[seat-1]
	}
	for i := range a.Players {
		a.Players[i].SteamID64 = id(a.Players[i].SteamID64)
	}
	for i := range a.Rounds {
		kills := make(map[uint64]int, len(a.Rounds[i].Kills))
		for seat, n := range a.Rounds[i].Kills {
			kills[id(seat)] = n
		}
		a.Rounds[i].Kills = kills
	}
	for i := range a.Deaths {
		d := &a.Deaths[i]
		d.Victim, d.Killer, d.Assister = id(d.Victim), id(d.Killer), id(d.Assister)
	}
	for i := range a.Throws {
		a.Throws[i].Thrower = id(a.Throws[i].Thrower)
	}
	for i := range a.Flashbangs {
		a.Flashbangs[i].Thrower = id(a.Flashbangs[i].Thrower)
	}
	for i := range a.Chances {
		a.Chances[i].Player = id(a.Chances[i].Player)
	}
	for i := range a.Clutches {
		a.Clutches[i].Player = id(a.Clutches[i].Player)
	}
	for i := range a.RoundShots {
		a.RoundShots[i].Player = id(a.RoundShots[i].Player)
	}
	for i := range a.Fights {
		a.Fights[i].Player = id(a.Fights[i].Player)
	}
	for i := range a.GrenadeHits {
		a.GrenadeHits[i].Player = id(a.GrenadeHits[i].Player)
	}
	return a
}

func checkMatch(t *testing.T, m *demo.Match, ps []Player) {
	t.Helper()
	if len(ps) != len(m.Players) {
		t.Fatalf("%d players, the match has %d", len(ps), len(m.Players))
	}
	// What the log says of each player, to bound the coaching metrics: by
	// the coaching rules, a bot under the player's control is the player.
	started := make(map[uint64]int) // rounds started alive
	coached := make(map[uint64]*coachTotals)
	of := func(w who) *coachTotals {
		if w.bot {
			return &coachTotals{} // a bot no player controls: nobody
		}
		if coached[w.id] == nil {
			coached[w.id] = &coachTotals{}
		}
		return coached[w.id]
	}
	for _, r := range m.Rounds {
		for _, a := range r.Alive {
			if !a.Bot {
				started[a.ID]++
			}
		}
	}
	for _, k := range m.Kills {
		if !duel(k) {
			continue
		}
		of(coach(k.Victim)).deaths++
		if !computerBot(k.Victim) {
			of(coach(*k.Killer)).kills++
		}
	}
	for _, h := range m.Hits {
		if enemies(h.Attacker, &h.Victim) {
			of(coach(*h.Attacker)).damage += h.Damage
		}
	}
	botRounds := 0
	for _, r := range m.Rounds {
		for _, a := range r.Alive {
			if a.Bot {
				botRounds++
				break
			}
		}
	}

	var openingWins, openingLosses, clutches int
	var total Stats // of all players, for what any match has
	for i, p := range ps {
		total = total.plus(p.Match)
		own := of(who{id: p.SteamID64})
		checkShootingAndGrenades(t, i, p.Match, *own)
		want := m.Players[i]
		s := p.Match
		if p.SteamID64 != want.SteamID64 || p.Team != want.Team {
			t.Fatalf("player %d is %d of team %d, the match has %d of team %d", i, p.SteamID64, p.Team, want.SteamID64, want.Team)
		}
		if s.Kills != want.Kills || s.Deaths != want.Deaths || s.Assists != want.Assists || s.HeadshotKills != want.Headshots || s.Damage != want.Damage {
			t.Errorf("player %d: K/D/A %d/%d/%d HS %d damage %d, the summary %d/%d/%d HS %d damage %d", i,
				s.Kills, s.Deaths, s.Assists, s.HeadshotKills, s.Damage, want.Kills, want.Deaths, want.Assists, want.Headshots, want.Damage)
		}
		if s.Rounds != len(m.Rounds) || p.T.Rounds == 0 || p.CT.Rounds == 0 || !near(s.ADR, float64(want.Damage)/float64(len(m.Rounds))) {
			t.Errorf("player %d: rounds %d (T %d, CT %d) of %d, ADR %v", i, s.Rounds, p.T.Rounds, p.CT.Rounds, len(m.Rounds), s.ADR)
		}
		if s.RoundsPlayed < 1 || s.RoundsPlayed > started[p.SteamID64] || s.KASTRounds > s.RoundsPlayed || s.KAST <= 0 || s.KAST > 1 {
			t.Errorf("player %d: KAST %d of %d rounds played, started %d", i, s.KASTRounds, s.RoundsPlayed, started[p.SteamID64])
		}
		if s.BulletHits == 0 || s.HeadHits > s.BulletHits {
			t.Errorf("player %d: %d of %d bullet hits in the head", i, s.HeadHits, s.BulletHits)
		}
		if s.OpeningWins > s.OpeningAttempts || s.TradeKills > own.kills || s.TradedDeaths+s.UntradedDeaths != own.deaths {
			t.Errorf("player %d: opening %d of %d, trade kills %d of %d kills, deaths traded %d and not %d of %d by enemies",
				i, s.OpeningWins, s.OpeningAttempts, s.TradeKills, own.kills, s.TradedDeaths, s.UntradedDeaths, own.deaths)
		}
		multi := 0
		for n, rounds := range s.MultiKills {
			multi += (n + 2) * rounds
		}
		if multi > s.Kills {
			t.Errorf("player %d: multikills %v make more than %d kills", i, s.MultiKills, s.Kills)
		}
		for x, c := range s.Clutches {
			if c.Wins > c.Attempts {
				t.Errorf("player %d: 1v%d won %d of %d", i, x+1, c.Wins, c.Attempts)
			}
			clutches += c.Attempts
		}
		openingWins += s.OpeningWins
		openingLosses += s.OpeningAttempts - s.OpeningWins
	}
	// One opening duel a round at most, a winner and a loser each, unless
	// a bot no player controls is in it.
	if openingWins > len(m.Rounds) || openingLosses > len(m.Rounds) || max(openingWins-openingLosses, openingLosses-openingWins) > botRounds {
		t.Errorf("%d opening duels won and %d lost in %d rounds", openingWins, openingLosses, len(m.Rounds))
	}
	// Two clutches a round at most: one a side.
	if clutches > 2*len(m.Rounds) {
		t.Errorf("%d clutches in %d rounds", clutches, len(m.Rounds))
	}
	// Any match has shots in sight, fights, duels, flashes and the like.
	if total.AccuracyShots == 0 || total.FirstShotFights == 0 || total.MovingBase == 0 || total.RifleKillDuels == 0 ||
		total.RifleHits == 0 || total.PistolHits == 0 || total.EnemiesFlashed == 0 || total.GrenadeDamage == 0 || total.DeathsWithGrenades == 0 {
		t.Errorf("the match counts no shooting or grenades: %+v", total)
	}
}

// coachTotals is what the log says a player did by the coaching rules: his
// kills of enemies, bots no player controls aside, his deaths by enemies and
// his damage to enemies, a bot under his control being him.
type coachTotals struct{ kills, deaths, damage int }

// checkShootingAndGrenades checks the bounds of the shooting and grenade
// metrics of player i, own being his totals by the coaching rules.
func checkShootingAndGrenades(t *testing.T, i int, s Stats, own coachTotals) {
	t.Helper()
	within := func(rate float64) bool { return rate >= 0 && rate <= 1 }
	if s.AccuracyHits > s.AccuracyShots || s.FirstShotHits > s.FirstShotFights || s.MovingShots > s.MovingBase ||
		!within(s.Accuracy) || !within(s.FirstShotAccuracy) || !within(s.MovingShotRate) {
		t.Errorf("player %d: accuracy %d of %d, first shot %d of %d, on the move %d of %d", i,
			s.AccuracyHits, s.AccuracyShots, s.FirstShotHits, s.FirstShotFights, s.MovingShots, s.MovingBase)
	}
	if s.RifleHeadHits > s.RifleHits || s.PistolHeadHits > s.PistolHits || s.RifleHits+s.PistolHits > s.BulletHits ||
		s.RifleHeadHits+s.PistolHeadHits > s.HeadHits {
		t.Errorf("player %d: rifles %d of %d in the head, pistols %d of %d, all bullets %d of %d", i,
			s.RifleHeadHits, s.RifleHits, s.PistolHeadHits, s.PistolHits, s.HeadHits, s.BulletHits)
	}
	// A duel is a kill: a pause of KillTimeGap at most between its hits,
	// and a rifle needs no more than a magazine.
	if s.RifleKillDuels+s.SniperKillDuels > own.kills || s.RifleKillTime < 0 || s.RifleKillTime > 10 || s.SniperKillTime < 0 || s.SniperKillTime > 10 {
		t.Errorf("player %d: %d rifle duels of %v s, %d of sniper rifles of %v s, %d kills", i,
			s.RifleKillDuels, s.RifleKillTime, s.SniperKillDuels, s.SniperKillTime, own.kills)
	}
	// A flash blinds for seconds, not for minutes.
	if s.EnemiesFlashed > 0 && (s.EnemyBlindTime < MinFlash.Seconds() || s.EnemyBlindTime > 10) {
		t.Errorf("player %d: %d enemies blinded for %v s on average", i, s.EnemiesFlashed, s.EnemyBlindTime)
	}
	// Four grenades at most, two flashbangs among them: $1300 at most.
	if s.GrenadeDamage > own.damage || s.DeathsWithGrenades > own.deaths || s.UnusedGrenadeValue > 1300*s.DeathsWithGrenades ||
		(s.DeathsWithGrenades > 0) != (s.UnusedGrenadeValue > 0) {
		t.Errorf("player %d: grenade damage %d of %d, %d of %d deaths with grenades worth $%d", i,
			s.GrenadeDamage, own.damage, s.DeathsWithGrenades, own.deaths, s.UnusedGrenadeValue)
	}
}

// checkAnalysis checks the events of a real match in their context against
// the metrics they are counted from, and their bounds.
func checkAnalysis(t *testing.T, m *demo.Match, a *Analysis) {
	t.Helper()
	if len(a.Rounds) != len(m.Rounds) {
		t.Fatalf("%d rounds in the analysis, %d in the match", len(a.Rounds), len(m.Rounds))
	}
	for i, r := range a.Rounds {
		pistol := r.Number == 1 || r.Number == 13
		for team := range 2 {
			if pistol != (r.Buy[team] == BuyPistol) || r.Buy[team] == "" || r.Equipment[team] <= 0 {
				t.Errorf("round %d: team %d bought %q for $%d", i+1, team, r.Buy[team], r.Equipment[team])
			}
		}
	}

	type sums struct {
		deaths, untraded, lone, advantage, dry, chances, missed, flashes, empty, team, bad, enemies int
		shots, moving, fights, firstHits, grenadeDamage, clutches, clutchWins                       int
	}
	got := make(map[uint64]*sums)
	for _, p := range a.Players {
		got[p.SteamID64] = &sums{}
	}
	var lone, advantage, dry int
	for i, d := range a.Deaths {
		s := got[d.Victim]
		if s == nil {
			t.Fatalf("death %d of a stranger: %+v", i, d)
		}
		s.deaths++
		if !d.Traded {
			s.untraded++
		}
		for _, b := range []struct {
			on  bool
			sum *int
			all *int
		}{{d.Lone, &s.lone, &lone}, {d.Advantage, &s.advantage, &advantage}, {d.Dry, &s.dry, &dry}} {
			if b.on {
				*b.sum++
				*b.all++
			}
		}
		k := m.Kills[d.Kill]
		r := m.Rounds[d.Round-1]
		bad := k.Tick != d.Tick || k.Round != d.Round || d.Elapsed < 0 || d.Clock < 0 || d.Clock > m.RoundTime ||
			d.Planted && (d.Clock > m.BombTime || d.Tick < r.BombPlantTick) || d.TeammateDistance < -1 ||
			d.Lone && (d.Traded || d.TeammateDistance <= LoneDistance) || d.Dry && (d.Traded || d.DamageBefore > 0) ||
			d.Excused && d.Buy != BuyEco || d.Opening && i > 0 && a.Deaths[i-1].Round == d.Round
		// The victim was alive, and has a teammate alive exactly when one
		// stands near enough to be measured.
		if !k.AfterRoundEnd && k.Tick >= r.FreezeEndTick {
			bad = bad || d.AliveOwn < 1 || d.AliveEnemy < 1 || (d.AliveOwn > 1) != (d.TeammateDistance >= 0)
		}
		if bad {
			t.Errorf("death %d = %+v", i, d)
		}
	}
	if lone == 0 || advantage == 0 || dry == 0 || len(a.Chances) == 0 || len(a.Flashbangs) == 0 {
		t.Errorf("%d lone, %d advantage and %d dry deaths, %d trade chances, %d flashbangs", lone, advantage, dry, len(a.Chances), len(a.Flashbangs))
	}
	missed := 0
	for i, ch := range a.Chances {
		d := a.Deaths[ch.Death]
		s := got[ch.Player]
		if s == nil || ch.Player == d.Victim || ch.Missed == d.Traded || ch.Traded && ch.Missed || d.AfterRoundEnd {
			t.Errorf("chance %d = %+v of death %+v", i, ch, d)
			continue
		}
		s.chances++
		if ch.Missed {
			s.missed++
			missed++
		}
	}
	if missed == 0 {
		t.Error("no trade missed in a match")
	}
	for i, fb := range a.Flashbangs {
		s := got[fb.Thrower]
		if s == nil || fb.Empty != (fb.Enemies == 0) || fb.TeamFlash != (fb.Teammates > 0) {
			t.Errorf("flashbang %d = %+v", i, fb)
			continue
		}
		s.flashes++
		s.enemies += fb.Enemies
		if fb.Empty {
			s.empty++
		}
		if fb.TeamFlash {
			s.team++
		}
		if fb.Empty || fb.TeamFlash {
			s.bad++
		}
	}
	// The events of the shooting, the grenade damage and the clutches: each
	// at a moment of its round, and adding up to the metrics below.
	if a.TickRate != m.TickRate {
		t.Errorf("tick rate %v, the match has %v", a.TickRate, m.TickRate)
	}
	moment := func(round, tick int, clock float64, planted bool) bool {
		if round < 1 || round > len(m.Rounds) {
			return false
		}
		r := m.Rounds[round-1]
		limit := m.RoundTime
		if planted {
			limit = m.BombTime
		}
		return tick >= r.FreezeEndTick && clock >= 0 && clock <= limit && planted == (r.BombPlantTick > 0 && tick >= r.BombPlantTick)
	}
	for i, rs := range a.RoundShots {
		s := got[rs.Player]
		if s == nil || rs.Shots < 1 || rs.Moving > rs.Shots || rs.Weapon == "" || !moment(rs.Round, rs.Tick, rs.Clock, rs.Planted) ||
			i > 0 && a.RoundShots[i-1].Round == rs.Round && a.RoundShots[i-1].Player == rs.Player {
			t.Errorf("round shots %d = %+v", i, rs)
			continue
		}
		s.shots += rs.Shots
		s.moving += rs.Moving
	}
	for i, f := range a.Fights {
		s := got[f.Player]
		if s == nil || f.Weapon == "" || !moment(f.Round, f.Tick, f.Clock, f.Planted) || i > 0 && a.Fights[i-1].Round > f.Round {
			t.Errorf("fight %d = %+v", i, f)
			continue
		}
		s.fights++
		if f.Hit {
			s.firstHits++
		}
	}
	for i, g := range a.GrenadeHits {
		s := got[g.Player]
		// A grenade hurts five enemies at most; fire may burn after the round.
		if s == nil || g.Damage < 1 || g.Enemies < 1 || g.Enemies > 5 || g.Round < 1 || g.Round > len(m.Rounds) || g.Clock < 0 {
			t.Errorf("grenade hit %d = %+v", i, g)
			continue
		}
		s.grenadeDamage += g.Damage
	}
	for i, cl := range a.Clutches {
		s := got[cl.Player]
		if s == nil || cl.Enemies < 1 || cl.Enemies > 5 || !moment(cl.Round, cl.Tick, cl.Clock, cl.Planted) {
			t.Errorf("clutch %d = %+v", i, cl)
			continue
		}
		s.clutches++
		if cl.Won {
			s.clutchWins++
		}
	}
	thrown := make(map[uint64]int)
	for i, th := range a.Throws {
		r := m.Rounds[th.Round-1]
		// A grenade may be thrown in the freeze time, before the clock runs.
		if got[th.Thrower] == nil || th.Weapon == "" || th.Tick >= r.FreezeEndTick && !moment(th.Round, th.Tick, th.Clock, th.Planted) ||
			i > 0 && a.Throws[i-1].Tick > th.Tick {
			t.Errorf("throw %d = %+v", i, th)
			continue
		}
		thrown[th.Thrower]++
	}
	flashAssists := make(map[uint64]int)
	for i, d := range a.Deaths {
		if d.FlashAssist && d.Assister == 0 || d.Assister != 0 && (got[d.Assister] == nil || d.Assister == d.Victim || d.Assister == d.Killer) {
			t.Errorf("death %d: the assist of %d, by a flash %t: %+v", i, d.Assister, d.FlashAssist, d)
		}
		if d.FlashAssist {
			flashAssists[d.Assister]++
		}
	}
	// The assists of the log by the coaching rules, a bot under the
	// player's control being him, as the assists of the deaths are.
	assists := make(map[uint64]int)
	for _, k := range m.Kills {
		if enemies(k.Assister, &k.Victim) && !coach(*k.Assister).bot {
			assists[coach(*k.Assister).id]++
		}
	}
	if len(a.RoundShots) == 0 || len(a.Fights) == 0 || len(a.GrenadeHits) == 0 || len(a.Clutches) == 0 {
		t.Errorf("%d round shots, %d fights, %d grenade hits, %d clutches", len(a.RoundShots), len(a.Fights), len(a.GrenadeHits), len(a.Clutches))
	}
	for i, p := range a.Players {
		st, s := p.Match, got[p.SteamID64]
		want := sums{
			deaths: st.TradedDeaths + st.UntradedDeaths, untraded: st.UntradedDeaths,
			lone: st.LoneDeaths, advantage: st.AdvantageDeaths, dry: st.DryDeaths,
			chances: st.TradeChances, missed: st.MissedTrades,
			flashes: st.FlashesThrown, empty: st.EmptyFlashes, team: st.TeamFlashes, bad: st.BadFlashes,
			enemies: s.enemies,
			shots:   st.MovingBase, moving: st.MovingShots, fights: st.FirstShotFights, firstHits: st.FirstShotHits,
			grenadeDamage: st.GrenadeDamage,
		}
		for _, cl := range st.Clutches {
			want.clutches += cl.Attempts
			want.clutchWins += cl.Wins
		}
		if n := st.FlashesThrown + st.SmokesThrown + st.HEGrenadesThrown + st.FireGrenadesThrown + st.DecoysThrown; thrown[p.SteamID64] != n {
			t.Errorf("player %d: %d throws, the metrics %d grenades thrown", i, thrown[p.SteamID64], n)
		}
		// An assist is a flash assist or not, and FACEIT counts both, but
		// not those through a bot: those of the log bound the flash assists.
		if flashAssists[p.SteamID64] > assists[p.SteamID64] || st.Assists > assists[p.SteamID64] {
			t.Errorf("player %d: %d flash assists and %d as FACEIT counts them of %d assists", i,
				flashAssists[p.SteamID64], st.Assists, assists[p.SteamID64])
		}
		if *s != want {
			t.Errorf("player %d: the events add up to %+v, the metrics %+v", i, *s, want)
		}
		// A blinding of an unknown flashbang counts in the metrics only.
		if s.enemies > st.EnemiesFlashed || st.TradeChances > st.TeammateDeaths {
			t.Errorf("player %d: flashbangs blinded %d enemies, the metrics %d; %d chances to trade of %d deaths of teammates",
				i, s.enemies, st.EnemiesFlashed, st.TradeChances, st.TeammateDeaths)
		}
	}
}
