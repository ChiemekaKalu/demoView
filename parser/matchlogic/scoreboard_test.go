package matchlogic

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/match"
)

// scoreMatch is two rounds of 2v2 (a and b on CT, c and d on T) that cover
// the scoreboard rules. Players are listed a, b, c, d, so idx = participant.
func scoreMatch() *testLog {
	l, a, b, c, d := twoVsTwo()
	l.hurt(-50, c, a, 50, 50).kill(-40, c, a) // warmup: not counted
	l.ev(0, RoundPrestart).ev(0, BeginNewMatch).spawn(0, a, b, c, d).ev(100, RoundFreezeEnd)
	l.hurt(200, c, a, 35, 65)                                      // 65
	l.hurt(210, c, a, 0, 140)                                      // fatal: capped at the 35 left
	l.kill(210, c, a, headshot, flash(d))                          // d's flash assist counts
	l.hurt(220, b, b, 80, 20)                                      // self damage: not counted
	l.hurt(230, d, c, 90, 10)                                      // team damage: not counted
	l.hurt(240, b, c, 0, 95)                                       // fatal: capped at 90
	l.kill(240, b, c, weapon("weapon_m4a1"))                       // prefix stripped
	l.kill(250, d, b, assist(a))                                   // a is on b's side: no assist
	l.ev(1000, RoundOfficiallyEnded).round(1000, 1100, a, b, c, d) // spawns reset health
	l.kill(1200, b, b, weapon("hegrenade"))                        // suicide: a death, no kill
	l.kill(1250, c, d)                                             // team kill: a death, no kill
	l.hurt(1300, a, c, 43, 57)                                     // 57
	l.hurt(1310, a, c, 31, 11)                                     // dmg_health 11, health drops 12: counts 12
	l.hurt(1320, a, c, 0, 40)                                      // fatal: capped at 31
	l.kill(1320, a, c, headshot)
	l.ev(1400, WinPanelMatch).world(1500, a) // after the match: not counted
	return l
}

func TestScoreboard(t *testing.T) {
	res := mustBuild(t, scoreMatch(), "1 kills between players on the same side (team kills, or wrong sides)")
	want := []match.ScoreRow{
		{Player: 0, Kills: 1, Deaths: 1, Assists: 0, ADR: 50, HSPct: 100}, // a: 57+12+31 damage
		{Player: 1, Kills: 1, Deaths: 2, Assists: 0, ADR: 45, HSPct: 0},   // b: 90
		{Player: 2, Kills: 1, Deaths: 2, Assists: 0, ADR: 50, HSPct: 100}, // c: 65+35; the team kill doesn't count
		{Player: 3, Kills: 1, Deaths: 1, Assists: 1, ADR: 0, HSPct: 0},    // d: the flash assist
	}
	if !slices.Equal(res.Scoreboard, want) {
		t.Errorf("scoreboard\n got %+v\nwant %+v", res.Scoreboard, want)
	}
	if got, want := roundsText(res), []string{"1:0-100-250 B CT elimination", "2:1000-1100-1320 A CT elimination"}; !slices.Equal(got, want) {
		t.Errorf("rounds %q, want %q", got, want)
	}
}

func TestKills(t *testing.T) {
	res := mustBuild(t, scoreMatch(), "1 kills between players on the same side (team kills, or wrong sides)")
	var got []string
	for _, k := range res.Kills {
		got = append(got, fmt.Sprintf("r%d %d %s>%d +%s %s hs=%v", k.Round, k.Tick, idxText(k.Attacker), k.Victim, idxText(k.Assister), k.Weapon, k.Headshot))
	}
	want := []string{
		"r1 210 2>0 +3 ak47 hs=true",
		"r1 240 1>2 +- m4a1 hs=false",
		"r1 250 3>1 +- ak47 hs=false", // the same-side assister is dropped
		"r2 1200 1>1 +- hegrenade hs=false",
		"r2 1250 2>3 +- ak47 hs=false",
		"r2 1320 0>2 +- ak47 hs=true",
	}
	if !slices.Equal(got, want) {
		t.Errorf("kills\n got %q\nwant %q", got, want)
	}
}

func idxText(i *int) string {
	if i == nil {
		return "-"
	}
	return fmt.Sprint(*i)
}

func TestKillsWorldAndBetweenRounds(t *testing.T) {
	l, a, b, c, d := twoVsTwo()
	l.ev(0, BeginNewMatch).round(0, 100, a, b, c, d).kill(200, a, c).kill(300, b, d)
	l.world(500, c)                         // a disconnect after the round ended: round 1
	l.kill(600, c, c, weapon("planted_c4")) // the bomb: no attacker
	l.ev(1000, RoundOfficiallyEnded).round(1000, 1100, a, b, c, d).kill(1200, a, c).kill(1300, a, d).ev(1400, WinPanelMatch)
	res := mustBuild(t, l)
	for i, want := range []struct {
		round    int
		attacker string
	}{{1, "0"}, {1, "1"}, {1, "-"}, {1, "-"}, {2, "0"}, {2, "0"}} {
		k := res.Kills[i]
		if k.Round != want.round || idxText(k.Attacker) != want.attacker {
			t.Errorf("kills[%d] = round %d, attacker %s; want round %d, attacker %s", i, k.Round, idxText(k.Attacker), want.round, want.attacker)
		}
	}
	// World deaths count as deaths, never as kills.
	if row := res.Scoreboard[2]; row.Deaths != 4 || row.Kills != 0 {
		t.Errorf("c: %+v, want 4 deaths and no kills", row)
	}
}

func TestHeadshotPercent(t *testing.T) {
	l := &testLog{}
	a := l.human("a", CT)
	ts := []int{l.human("t1", T), l.human("t2", T), l.human("t3", T)}
	l.cvar(0, "mp_roundtime_defuse", "1.92")
	l.ev(0, BeginNewMatch).round(0, 100, append([]int{a}, ts...)...)
	l.kill(200, a, ts[0], headshot).kill(300, a, ts[1]).kill(400, a, ts[2]).ev(500, WinPanelMatch)
	res := mustBuild(t, l)
	if got := res.Scoreboard[0]; got.HSPct != 33.3 || got.Kills != 3 {
		t.Errorf("a: %+v, want 3 kills at 33.3%% headshots", got)
	}
	if got := res.Scoreboard[1]; got.HSPct != 0 || got.Kills != 0 {
		t.Errorf("t1: %+v, want no kills and hs_pct 0", got)
	}
}
