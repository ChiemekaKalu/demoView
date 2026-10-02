package matchlogic

import (
	"slices"
	"strconv"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/match"
)

// In these tests a and b play CT and c and d play T. Freeze time ends at
// 1000, so the round timer (1.92 minutes, kept as 115 s) runs out at
// 1000 + 115*64 = 8360.

func TestInferOutcome(t *testing.T) {
	tests := []struct {
		name   string
		events func(l *testLog, a, b, c, d int)
		want   string // round 1: "end winner_side reason"
		notes  []string
	}{
		{"last CT dies", func(l *testLog, a, b, c, d int) {
			l.kill(1100, c, a).kill(1200, d, b)
		}, "1200 T elimination", nil},
		{"last T dies", func(l *testLog, a, b, c, d int) {
			l.kill(1100, a, c).kill(1200, a, d)
		}, "1200 CT elimination", nil},
		{"Ts dead after the plant, then defused", func(l *testLog, a, b, c, d int) {
			l.bomb(1100, BombPlanted, c).kill(1200, a, c).kill(1300, a, d).bomb(1500, BombDefused, b)
		}, "1500 CT bomb_defused", nil},
		{"Ts dead after the plant, then it explodes", func(l *testLog, a, b, c, d int) {
			l.bomb(1100, BombPlanted, c).kill(1200, a, c).kill(1300, a, d).ev(3660, BombExploded)
		}, "3660 T bomb_exploded", nil},
		{"CTs dead after the plant", func(l *testLog, a, b, c, d int) {
			l.bomb(1100, BombPlanted, c).kill(1200, c, a).kill(1300, c, b)
		}, "1300 T elimination", nil},
		{"time runs out", func(l *testLog, a, b, c, d int) {
			l.kill(1100, a, c).kill(9000, a, d) // the second kill is after the timer
		}, "8360 CT time_expired", nil},
		{"plant beats the timer", func(l *testLog, a, b, c, d int) {
			l.bomb(8300, BombPlanted, c).ev(10860, BombExploded)
		}, "10860 T bomb_exploded", nil},
		{"last CT disconnects", func(l *testLog, a, b, c, d int) {
			l.kill(1100, c, a).disconnect(1200, b)
		}, "1200 T elimination", nil},
		{"last CT leaves with a world death", func(l *testLog, a, b, c, d int) {
			l.kill(1100, c, a).disconnect(1200, b).world(1200, b)
		}, "1200 T elimination", nil},
		{"Ts surrender", func(l *testLog, a, b, c, d int) {
			l.kill(1100, a, c).surrender(1200, T)
		}, "1200 CT surrender", nil},
		{"nothing decides the last round", func(l *testLog, a, b, c, d int) {
			l.kill(1100, a, c)
		}, "5000 CT time_expired", []string{"round 1: nothing decided it; guessed CT time_expired at tick 5000"}},
		{"nothing decides, with a plant", func(l *testLog, a, b, c, d int) {
			l.bomb(1100, BombPlanted, c)
		}, "5000 T bomb_exploded", []string{"round 1: nothing decided it; guessed T bomb_exploded at tick 5000"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, a, b, c, d := twoVsTwo()
			l.ev(0, BeginNewMatch).round(0, 1000, a, b, c, d)
			tt.events(l, a, b, c, d)
			last := l.Entries[len(l.Entries)-1].Tick
			l.ev(max(last+100, 5000), WinPanelMatch)
			res := mustBuild(t, l, tt.notes...)
			if got := outcomeText(res, 0); got != tt.want {
				t.Errorf("round 1 = %q, want %q", got, tt.want)
			}
		})
	}
}

// outcomeText renders round r as "end winner_side reason". Team A is on CT
// in round 1 and the tests have no halftime, so A won as CT.
func outcomeText(res Result, r int) string {
	rd := res.Rounds[r]
	side := "T"
	if (rd.Winner == "A") == (rd.SideA == "CT") {
		side = "CT"
	}
	return strconv.Itoa(rd.EndTick) + " " + side + " " + rd.Reason
}

// A round nothing decided ends the restart delay before the next restart.
func TestGuessOutcomeBeforeRestart(t *testing.T) {
	l, a, b, c, d := twoVsTwo()
	l.ev(0, BeginNewMatch).round(0, 1000, a, b, c, d).kill(1100, a, c)
	l.ev(5000, RoundOfficiallyEnded).round(5000, 6000, a, b, c, d).kill(6100, a, c).kill(6200, b, d)
	l.ev(6500, WinPanelMatch)
	res := mustBuild(t, l, "round 1: nothing decided it; guessed CT time_expired at tick 4552")
	if got := outcomeText(res, 0); got != "4552 CT time_expired" {
		t.Errorf("round 1 = %q, want 4552 (7 s before the restart) CT time_expired", got)
	}
}

func TestRoundEndReasons(t *testing.T) {
	tests := []struct {
		winner Side
		reason int
		want   string
		notes  []string
	}{
		{T, 1, "2000 T bomb_exploded", nil},
		{CT, 7, "2000 CT bomb_defused", nil},
		{CT, 8, "2000 CT elimination", nil},
		{T, 9, "2000 T elimination", nil},
		{CT, 12, "2000 CT time_expired", nil},
		{CT, 17, "2000 CT surrender", nil},
		{T, 18, "2000 T surrender", nil},
		// An unknown code takes the inferred reason.
		{T, 99, "2000 T elimination", []string{"round 1: round_end reason 99 is not mapped; using elimination"}},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.reason), func(t *testing.T) {
			// The kills decide the round for T at 2000, so inference agrees
			// with round_end only for code 9 and 99; the others note it.
			l, a, b, c, d := twoVsTwo()
			l.ev(0, BeginNewMatch).round(0, 1000, a, b, c, d)
			l.kill(1900, c, a).kill(2000, d, b).roundEnd(2000, tt.winner, tt.reason).ev(2200, WinPanelMatch)
			res := Build(&l.Log, Options{Map: "de_test", TickRate: 64})
			if got := outcomeText(res, 0); got != tt.want {
				t.Errorf("round 1 = %q, want %q", got, tt.want)
			}
			for _, n := range tt.notes {
				if !slices.Contains(res.Notes, n) {
					t.Errorf("notes %q lack %q", res.Notes, n)
				}
			}
			disagree := "round 1: round_end says " + tt.want[5:] + " at tick 2000, inference says T elimination at tick 2000"
			if hasDisagreement := slices.Contains(res.Notes, disagree); hasDisagreement != (tt.want[5:] != "T elimination") {
				t.Errorf("notes %q: disagreement noted = %v", res.Notes, hasDisagreement)
			}
		})
	}
}

func TestRoundTimeTicks(t *testing.T) {
	tests := []struct {
		name    string
		cvars   [][2]string
		mapName string
		want    int32
		notes   int
	}{
		{"defuse map", [][2]string{{"mp_roundtime", "2.5"}, {"mp_roundtime_defuse", "1.92"}}, "de_x", 115 * 64, 0},
		{"other map", [][2]string{{"mp_roundtime", "2.5"}, {"mp_roundtime_defuse", "1.92"}}, "cs_x", 150 * 64, 0},
		{"defuse unset", [][2]string{{"mp_roundtime", "2.5"}, {"mp_roundtime_defuse", "0"}}, "de_x", 150 * 64, 0},
		{"latest value", [][2]string{{"mp_roundtime_defuse", "2"}, {"mp_roundtime_defuse", "1.75"}}, "de_x", 105 * 64, 0},
		{"none", nil, "de_x", 115 * 64, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var l testLog
			for _, cv := range tt.cvars {
				l.cvar(0, cv[0], cv[1])
			}
			var n notes
			if got := roundTimeTicks(&l.Log, len(l.Entries), tt.mapName, 64, &n); got != tt.want || len(n) != tt.notes {
				t.Errorf("roundTimeTicks = %d with %d notes, want %d with %d", got, len(n), tt.want, tt.notes)
			}
		})
	}
}

func TestCheckFinish(t *testing.T) {
	mr12 := schedule{maxRounds: 24, otRounds: 6, halftime: true}
	tests := []struct {
		name    string
		winners string // round winners in order
		last    string // the last round's reason
		noted   bool
	}{
		{"won in regulation", "AAAAAAAAAAAAA", reasonElimination, false},
		{"a draw", "AAAAAAAAAAAABBBBBBBBBBBB", reasonElimination, false},
		{"surrendered", "BBBBB", reasonSurrender, false},
		{"stopped early", "AABBB", reasonElimination, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rounds []match.Round
			for _, w := range tt.winners {
				rounds = append(rounds, match.Round{Winner: string(w), Reason: reasonElimination})
			}
			rounds[len(rounds)-1].Reason = tt.last
			var n notes
			checkFinish(rounds, mr12, &n)
			if got := len(n) > 0; got != tt.noted {
				t.Errorf("noted = %v (%q), want %v", got, n, tt.noted)
			}
		})
	}
}
