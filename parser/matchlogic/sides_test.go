package matchlogic

import (
	"maps"
	"slices"
	"strconv"
	"testing"
)

func TestScheduleSwapAfter(t *testing.T) {
	mr12 := schedule{maxRounds: 24, otRounds: 6, halftime: true}
	for n, want := range map[int]bool{
		11: false, 12: true, 13: false, 23: false,
		24: false, // into overtime without a swap
		27: true, 30: false, 33: true, 36: false, 39: true,
	} {
		if got := mr12.swapAfter(n); got != want {
			t.Errorf("MR12: swapAfter(%d) = %v, want %v", n, got, want)
		}
	}
	noHalf := schedule{maxRounds: 24, otRounds: 6, halftime: false}
	if noHalf.swapAfter(12) {
		t.Error("mp_halftime 0: swapAfter(12) = true")
	}
}

// halves builds a four-round 2v2 match with a halftime after round 2. a and
// b start on CT and win both their CT rounds; c and d win theirs after the
// swap.
type halves struct {
	teamEvents bool     // player_team events, with silent swaps at the half
	swapEarly  bool     // the swap's player_team events come before round 2 ends
	phaseEnd   bool     // announce_phase_end at the half
	cvars      []string // name, value pairs
}

func (h halves) build() *testLog {
	l := &testLog{}
	a, b := l.human("a", T), l.human("b", T) // final sides, after the swap
	c, d := l.human("c", CT), l.human("d", CT)
	l.cvar(0, "mp_roundtime_defuse", "1.92")
	for i := 0; i+1 < len(h.cvars); i += 2 {
		l.cvar(0, h.cvars[i], h.cvars[i+1])
	}
	if h.teamEvents {
		l.team(5, a, NoSide, CT).team(5, b, NoSide, CT).team(5, c, NoSide, T).team(5, d, NoSide, T)
	}
	swap := func(tick int32) {
		l.team(tick, a, CT, T).team(tick, b, CT, T).team(tick, c, T, CT).team(tick, d, T, CT)
	}
	tick := int32(10)
	for r := range 4 {
		l.ev(tick, RoundPrestart)
		if r == 0 {
			l.ev(tick, BeginNewMatch)
		}
		ct1, ct2, t1, t2 := a, b, c, d
		if r >= 2 {
			ct1, ct2, t1, t2 = c, d, a, b
		}
		if r == 2 && h.teamEvents && !h.swapEarly {
			swap(tick)
		}
		l.spawn(tick, a, b, c, d).ev(tick+100, RoundFreezeEnd)
		l.kill(tick+200, ct1, t1, headshot).kill(tick+300, ct2, t2)
		if r == 1 && h.phaseEnd {
			l.ev(tick+350, AnnouncePhaseEnd)
		}
		if r == 1 && h.teamEvents && h.swapEarly {
			swap(tick + 400)
		}
		tick += 1000
		if r < 3 {
			l.ev(tick, RoundOfficiallyEnded)
		}
	}
	return l.ev(tick, WinPanelMatch)
}

func TestAssignSidesAtHalftime(t *testing.T) {
	full := []string{"mp_maxrounds", "4", "mp_overtime_maxrounds", "6", "mp_halftime", "1"}
	tests := []struct {
		name  string
		h     halves
		notes []string
	}{
		{"player_team events", halves{teamEvents: true, phaseEnd: true, cvars: full}, nil},
		{"player_team events before the round ends", halves{teamEvents: true, swapEarly: true, phaseEnd: true, cvars: full}, nil},
		{"player_team events, no cvars", halves{teamEvents: true, phaseEnd: true}, []string{
			"halftime after round 2 is true by announce_phase_end but false by the cvars (mp_maxrounds 24, mp_overtime_maxrounds 6, mp_halftime true)",
		}},
		{"end-of-match sides and announce_phase_end", halves{phaseEnd: true, cvars: full}, nil},
		{"end-of-match sides and the cvars", halves{cvars: full}, nil},
		{"end-of-match sides and some cvars", halves{cvars: full[:2]}, []string{
			"no announce_phase_end; halftimes follow mp_maxrounds 4, mp_overtime_maxrounds 6, mp_halftime true, assumed for mp_overtime_maxrounds, mp_halftime",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := mustBuild(t, tt.h.build(), tt.notes...)
			if got, want := roundsText(res), []string{
				"1:10-110-310 A CT elimination",
				"2:1010-1110-1310 A CT elimination",
				"3:2010-2110-2310 B T elimination",
				"4:3010-3110-3310 B T elimination",
			}; !slices.Equal(got, want) {
				t.Errorf("rounds\n got %q\nwant %q", got, want)
			}
			if got, want := teamsOf(res), map[string]string{"a": "A", "b": "A", "c": "B", "d": "B"}; !maps.Equal(got, want) {
				t.Errorf("teams %v, want %v", got, want)
			}
		})
	}
}

// teamsOf maps each player's name to their team.
func teamsOf(res Result) map[string]string {
	out := map[string]string{}
	for _, p := range res.Players {
		out[p.Name] = p.Team
	}
	return out
}

func TestAssignSidesOvertime(t *testing.T) {
	// mp_maxrounds 2 and MR1 halves of overtime (mp_overtime_maxrounds 2):
	// sides swap after rounds 1 and 3, not entering overtime after round 2.
	l := &testLog{}
	a, b := l.human("a", CT), l.human("b", T)
	l.cvar(0, "mp_maxrounds", "2").cvar(0, "mp_overtime_maxrounds", "2").cvar(0, "mp_halftime", "1").cvar(0, "mp_roundtime", "2")
	l.ev(0, BeginNewMatch)
	for r := range 4 {
		tick := int32(1000 * r)
		l.ev(tick, RoundPrestart).spawn(tick, a, b).ev(tick+100, RoundFreezeEnd).kill(tick+200, a, b)
	}
	l.ev(3500, WinPanelMatch)
	res := mustBuild(t, l)
	var sides []string
	for _, r := range res.Rounds {
		sides = append(sides, r.SideA)
	}
	// a finishes on CT in round 4; back through the swaps after 3 and 1.
	if want := []string{"CT", "T", "T", "CT"}; !slices.Equal(sides, want) {
		t.Errorf("side_a by round %v, want %v", sides, want)
	}
}

func TestAssignSidesEdgeCases(t *testing.T) {
	t.Run("player_team disagrees with the end of the match", func(t *testing.T) {
		l, a, b, c, d := twoVsTwo()
		l.team(5, a, NoSide, T) // wrong, and a finishes on CT
		l.ev(10, BeginNewMatch).round(10, 100, a, b, c, d).kill(200, a, c).kill(300, b, d).ev(400, WinPanelMatch)
		res := mustBuild(t, l, "a ends on T by player_team but on CT by the end-of-match message; using the latter")
		if got := teamsOf(res); got["a"] != "A" {
			t.Errorf("a is on team %q, want A", got["a"])
		}
	})

	t.Run("a bot nobody placed", func(t *testing.T) {
		l, a, b, c, d := twoVsTwo()
		bot := l.bot("Bot", NoSide)
		l.ev(10, BeginNewMatch).round(10, 100, a, b, c, d, bot)
		l.kill(200, bot, c).kill(300, bot, d).ev(400, WinPanelMatch)
		res := mustBuild(t, l, "Bot has no known side; put on team A, opposite the players they fought (0 A, 2 B)")
		if got := teamsOf(res); got["Bot"] != "A" {
			t.Errorf("bot on team %q, want A", got["Bot"])
		}
		if res.Players[0].Name != "Bot" || res.Players[0].SteamID != "0" {
			t.Errorf("players[0] = %+v, want the bot, with steam_id 0", res.Players[0])
		}
		if res.Rounds[0].Winner != "A" || res.Rounds[0].Reason != "elimination" {
			t.Errorf("round 1 = %+v, want an A elimination", res.Rounds[0])
		}
	})

	t.Run("a player joins late", func(t *testing.T) {
		l, a, b, c, d := twoVsTwo()
		late := l.human("late", NoSide)
		l.ev(10, BeginNewMatch).round(10, 100, a, b, c, d).kill(200, a, c).kill(300, b, d)
		l.ev(1000, RoundOfficiallyEnded).ev(1000, RoundPrestart).team(1050, late, NoSide, T).spawn(1050, a, b, c, d, late)
		l.ev(1100, RoundFreezeEnd).kill(1200, late, a).kill(1300, late, b).ev(1400, WinPanelMatch)
		res := mustBuild(t, l)
		if got := teamsOf(res); got["late"] != "B" {
			t.Errorf("late joiner on team %q, want B", got["late"])
		}
		if got := roundsText(res)[1]; got != "2:1000-1100-1300 B CT elimination" {
			t.Errorf("round 2 = %q", got)
		}
	})

	t.Run("a team kill is noted", func(t *testing.T) {
		l, a, b, c, d := twoVsTwo()
		l.ev(10, BeginNewMatch).round(10, 100, a, b, c, d).kill(150, a, b).kill(200, a, c).kill(300, a, d).ev(400, WinPanelMatch)
		mustBuild(t, l, "1 kills between players on the same side (team kills, or wrong sides)")
	})
}

func TestListPlayersOrder(t *testing.T) {
	l := &testLog{}
	// Added out of order: by team, then SteamID64 (bots are 0), then name.
	z := l.human("z", T)
	y := l.human("y", CT)
	bot2, bot1 := l.bot("Zed", CT), l.bot("Abe", CT)
	x := l.human("x", T)
	l.cvar(0, "mp_roundtime", "2")
	l.gotv()                     // never plays
	l.human("spectator", NoSide) // never spawns
	l.ev(0, BeginNewMatch).round(0, 100, z, y, bot2, bot1, x).kill(200, y, z).kill(300, y, x).ev(400, WinPanelMatch)
	res := mustBuild(t, l)
	var got []string
	for _, p := range res.Players {
		got = append(got, p.Team+":"+p.Name+":"+p.SteamID+":"+strconv.Itoa(p.Idx))
	}
	want := []string{"A:Abe:0:0", "A:Zed:0:1", "A:y:1001:2", "B:z:1000:3", "B:x:1004:4"}
	if !slices.Equal(got, want) {
		t.Errorf("players\n got %q\nwant %q", got, want)
	}
}
