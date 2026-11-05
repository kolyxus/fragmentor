package metrics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// TestComputeTestdata computes the metrics of the demos in bot/testdata and
// checks what holds for any match: the totals as FACEIT counts them are
// the summary's, which go run ./cmd/parsedemo -faceit checks against
// FACEIT, and the coaching metrics stay within their bounds. The demos are
// not in git: the test skips without them, and with -short. They are parsed
// in parallel, as each takes seconds.
func TestComputeTestdata(t *testing.T) {
	if testing.Short() {
		t.Skip("parsing real demos takes seconds")
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "*.dem.zst"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no demos in bot/testdata")
	}
	for _, file := range files {
		t.Run(filepath.Base(file)[:10], func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			m, err := demo.ParseFile(ctx, file, demo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			checkMatch(t, m, Compute(m))
		})
	}
}

func checkMatch(t *testing.T, m *demo.Match, ps []Player) {
	t.Helper()
	if len(ps) != len(m.Players) {
		t.Fatalf("%d players, the match has %d", len(ps), len(m.Players))
	}
	// What the log says of each player, to bound the coaching metrics.
	started := make(map[uint64]int) // rounds started alive
	deaths := make(map[uint64]int)  // deaths by enemies, a bot under the player's control included
	for _, r := range m.Rounds {
		for _, a := range r.Alive {
			if !a.Bot {
				started[a.ID]++
			}
		}
	}
	for _, k := range m.Kills {
		if duel(k) && !coach(k.Victim).bot {
			deaths[coach(k.Victim).id]++
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
		checkShootingAndGrenades(t, i, p.Match)
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
		if s.OpeningWins > s.OpeningAttempts || s.TradeKills > s.Kills || s.TradedDeaths+s.UntradedDeaths != deaths[p.SteamID64] {
			t.Errorf("player %d: opening %d of %d, trade kills %d of %d kills, deaths traded %d and not %d of %d by enemies",
				i, s.OpeningWins, s.OpeningAttempts, s.TradeKills, s.Kills, s.TradedDeaths, s.UntradedDeaths, deaths[p.SteamID64])
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

// checkShootingAndGrenades checks the bounds of the shooting and grenade
// metrics of player i.
func checkShootingAndGrenades(t *testing.T, i int, s Stats) {
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
	if s.RifleKillDuels+s.SniperKillDuels > s.Kills || s.RifleKillTime < 0 || s.RifleKillTime > 10 || s.SniperKillTime < 0 || s.SniperKillTime > 10 {
		t.Errorf("player %d: %d rifle duels of %v s, %d of sniper rifles of %v s, %d kills", i,
			s.RifleKillDuels, s.RifleKillTime, s.SniperKillDuels, s.SniperKillTime, s.Kills)
	}
	// A flash blinds for seconds, not for minutes.
	if s.EnemiesFlashed > 0 && (s.EnemyBlindTime < MinFlash.Seconds() || s.EnemyBlindTime > 10) {
		t.Errorf("player %d: %d enemies blinded for %v s on average", i, s.EnemiesFlashed, s.EnemyBlindTime)
	}
	// Four grenades at most, two flashbangs among them: $1300 at most.
	if s.GrenadeDamage > s.Damage || s.DeathsWithGrenades > s.Deaths || s.UnusedGrenadeValue > 1300*s.DeathsWithGrenades ||
		(s.DeathsWithGrenades > 0) != (s.UnusedGrenadeValue > 0) {
		t.Errorf("player %d: grenade damage %d of %d, %d of %d deaths with grenades worth $%d", i,
			s.GrenadeDamage, s.Damage, s.DeathsWithGrenades, s.Deaths, s.UnusedGrenadeValue)
	}
}
