package matchlogic

import (
	"slices"
	"testing"
)

func TestSegmentRounds(t *testing.T) {
	tests := []struct {
		name   string
		build  func() *testLog
		rounds []string
		kills  int
		notes  []string
	}{
		{
			// A newer demo: no round_start or round_end. A knife round and
			// a post-match death fall outside the match.
			name: "without round_end",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.round(100, 200, a, b, c, d).kill(300, a, c, weapon("knife")).ev(400, RoundOfficiallyEnded)
				l.round(500, 1000, a, b, c, d) // begin_new_match follows the restart
				l.Entries = slices.Insert(l.Entries, len(l.Entries)-1, entry(501, BeginNewMatch))
				l.kill(1100, c, a).kill(1200, d, b) // CT eliminated
				l.ev(1648, RoundOfficiallyEnded).round(1648, 2000, a, b, c, d)
				l.kill(2100, a, c).kill(2200, b, d).world(2300, b) // b leaves after the round: still round 2
				l.ev(2648, RoundOfficiallyEnded).round(2648, 3000, a, b, c, d)
				l.bomb(3100, BombPlanted, c).kill(3200, a, c).kill(3300, b, d).bomb(3400, BombDefused, a)
				l.ev(3500, WinPanelMatch).world(3600, a)
				return l
			},
			rounds: []string{
				"1:501-1000-1200 B CT elimination",
				"2:1648-2000-2200 A CT elimination",
				"3:2648-3000-3400 A CT bomb_defused",
			},
			kills: 7,
		},
		{
			// An older demo: round_start and round_end fire. start_tick is
			// round_start's.
			name: "with round_end",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.ev(10, RoundPrestart).ev(11, RoundStart).ev(11, BeginNewMatch).spawn(11, a, b, c, d).ev(500, RoundFreezeEnd)
				l.kill(600, c, a).kill(700, d, b).roundEnd(700, T, 9)
				l.ev(1148, RoundOfficiallyEnded).ev(1148, RoundPrestart).ev(1149, RoundStart).spawn(1149, a, b, c, d).ev(1500, RoundFreezeEnd)
				l.bomb(1600, BombPlanted, c).bomb(4160, BombExploded, c).roundEnd(4160, T, 1)
				l.ev(4300, WinPanelMatch)
				return l
			},
			rounds: []string{
				"1:11-500-700 B CT elimination",
				"2:1149-1500-4160 B CT bomb_exploded",
			},
			kills: 2,
		},
		{
			// A tournament GOTV demo: round_prestart fires only before the
			// match, so round_officially_ended splits the rounds.
			name: "no round_prestart in play",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.ev(5, RoundPrestart).ev(10, BeginNewMatch).spawn(10, a, b, c, d).ev(100, RoundFreezeEnd)
				l.kill(200, c, a).kill(300, d, b)
				l.ev(748, RoundOfficiallyEnded).spawn(748, a, b, c, d).ev(1000, RoundFreezeEnd)
				l.kill(1100, a, c).kill(1200, a, d)
				l.ev(1648, RoundOfficiallyEnded).spawn(1648, a, b, c, d).ev(2000, RoundFreezeEnd)
				l.kill(2100, a, c).kill(2200, b, d)
				l.ev(2300, WinPanelMatch)
				return l
			},
			rounds: []string{
				"1:10-100-300 B CT elimination",
				"2:748-1000-1200 A CT elimination",
				"3:1648-2000-2200 A CT elimination",
			},
			kills: 6,
		},
		{
			// Tournament GOTV again, with the round 1 spawns before
			// begin_new_match: round 1 still holds them, so its CTs are
			// alive and eliminated rather than the round timing out. Its
			// start is clamped to the first tick (0 here, the default).
			name: "round 1 spawns before begin_new_match",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.spawn(-1, a, b, c, d).ev(-1, BeginNewMatch).ev(100, RoundFreezeEnd)
				l.kill(200, c, a).kill(300, d, b)
				l.ev(748, RoundOfficiallyEnded).spawn(748, a, b, c, d).ev(1000, RoundFreezeEnd)
				l.kill(1100, a, c).kill(1200, a, d)
				l.ev(1300, WinPanelMatch)
				return l
			},
			rounds: []string{
				"1:0-100-300 B CT elimination",
				"2:748-1000-1200 A CT elimination",
			},
			kills: 4,
		},
		{
			// A tournament recording that starts in round 1's freeze time:
			// no begin_new_match, no spawns for round 1, and only
			// cs_pre_restart, a few ticks before each restart. Round 1's
			// players are round 2's, and a round starts at its spawns.
			name: "recorded from round 1, cs_pre_restart only",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.ev(100, RoundFreezeEnd).kill(200, c, a).kill(300, d, b)
				l.ev(729, PreRestart).spawn(748, a, b, c, d).ev(1000, RoundFreezeEnd)
				l.kill(1100, a, c).kill(1200, a, d)
				l.ev(1629, PreRestart).spawn(1648, a, b, c, d).ev(2000, RoundFreezeEnd)
				l.kill(2100, a, c).kill(2200, b, d)
				l.ev(2300, WinPanelMatch)
				return l
			},
			rounds: []string{
				"1:0-100-300 B CT elimination",
				"2:748-1000-1200 A CT elimination",
				"3:1648-2000-2200 A CT elimination",
			},
			kills: 6,
			notes: []string{"no begin_new_match before a round: the match starts at tick 0"},
		},
		{
			// A recording that starts after round 1's freeze end, with
			// deaths before the first restart: that is round 1, with no
			// freeze end of its own, and round 2's players.
			name: "recorded from the middle of round 1",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.kill(200, c, a).kill(300, d, b)
				l.ev(729, PreRestart).ev(748, RoundOfficiallyEnded).spawn(748, a, b, c, d).ev(1000, RoundFreezeEnd)
				l.kill(1100, a, c).kill(1200, a, d)
				l.ev(1629, PreRestart).ev(1648, RoundOfficiallyEnded).spawn(1648, a, b, c, d).ev(2000, RoundFreezeEnd)
				l.kill(2100, a, c).kill(2200, b, d)
				l.ev(2300, WinPanelMatch)
				return l
			},
			rounds: []string{
				"1:0-300-300 B CT elimination",
				"2:748-1000-1200 A CT elimination",
				"3:1648-2000-2200 A CT elimination",
			},
			kills: 6,
			notes: []string{"no begin_new_match before a round: the match starts at tick 0"},
		},
		{
			// A recording that starts with a restore: everyone dies on the
			// tick of the restart. Those deaths are part of the restart,
			// not a round, so the match starts at it.
			name: "restore at the start",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.world(5, a).world(5, b).world(5, c).world(5, d)
				l.ev(5, PreRestart).ev(5, RoundOfficiallyEnded).spawn(5, a, b, c, d).ev(100, RoundFreezeEnd)
				l.kill(200, c, a).kill(300, d, b)
				l.ev(729, PreRestart).ev(748, RoundOfficiallyEnded).spawn(748, a, b, c, d).ev(1000, RoundFreezeEnd)
				l.kill(1100, a, c).kill(1200, a, d)
				l.ev(1300, WinPanelMatch)
				return l
			},
			rounds: []string{
				"1:5-100-300 B CT elimination",
				"2:748-1000-1200 A CT elimination",
			},
			kills: 4,
			notes: []string{"no begin_new_match before a round: the match starts at tick 5"},
		},
		{
			// A recording of a first half whose only begin_new_match comes
			// at its end, as the match restarts, with no win panel. The
			// rounds before it are still the match.
			name: "begin_new_match at the end",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.ev(100, RoundFreezeEnd).kill(200, c, a).kill(300, d, b)
				l.ev(748, RoundOfficiallyEnded).ev(748, RoundPrestart).spawn(748, a, b, c, d).ev(1000, RoundFreezeEnd)
				l.kill(1100, a, c).kill(1200, a, d)
				l.ev(1648, RoundOfficiallyEnded).ev(1648, RoundPrestart).spawn(1648, a, b, c, d).ev(1649, BeginNewMatch)
				return l
			},
			rounds: []string{
				"1:0-100-300 B CT elimination",
				"2:748-1000-1200 A CT elimination",
			},
			kills: 4,
			notes: []string{
				"no cs_win_panel_match: the match runs to the end of the demo",
				"no begin_new_match before a round: the match starts at tick 0",
				"restart at tick 1648 has no freeze end or round end, and no round follows it; left out",
			},
		},
		{
			// A recording cut off in freeze time, after a restart: a death
			// there belongs to no round played, so it is left out.
			name: "cut off after a restart",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.ev(10, BeginNewMatch).spawn(10, a, b, c, d).ev(100, RoundFreezeEnd)
				l.kill(200, c, a).kill(300, d, b)
				l.ev(748, RoundOfficiallyEnded).spawn(748, a, b, c, d).ev(1000, RoundFreezeEnd)
				l.kill(1100, a, c).kill(1200, a, d)
				l.ev(1648, RoundOfficiallyEnded).spawn(1648, a, b, c, d).world(1700, b)
				return l
			},
			rounds: []string{
				"1:10-100-300 B CT elimination",
				"2:748-1000-1200 A CT elimination",
			},
			kills: 4,
			notes: []string{
				"no cs_win_panel_match: the match runs to the end of the demo",
				"restart at tick 1648 has no freeze end or round end, and no round follows it; left out",
				"death at tick 1700 is outside every round or has no listed victim; left out",
			},
		},
		{
			// A restart with nothing in it joins the round before it.
			name: "empty restart",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.ev(0, BeginNewMatch).round(0, 100, a, b, c, d).kill(200, c, a).kill(300, d, b)
				l.ev(748, RoundOfficiallyEnded).ev(748, RoundPrestart).spawn(748, a, b, c, d)
				l.ev(800, RoundPrestart).spawn(800, a, b, c, d).ev(1000, RoundFreezeEnd)
				l.kill(1100, a, c).kill(1200, a, d)
				l.ev(1300, WinPanelMatch)
				return l
			},
			rounds: []string{
				"1:0-100-300 B CT elimination",
				"2:800-1000-1200 A CT elimination",
			},
			kills: 4,
			notes: []string{"restart at tick 748 has no freeze end or round end; folded into round 1"},
		},
		{
			// A surrender vote in freeze time ends a round with no freeze
			// end: freeze_end_tick = end_tick.
			name: "surrender in freeze time",
			build: func() *testLog {
				l, a, b, c, d := twoVsTwo()
				l.ev(0, BeginNewMatch).round(0, 100, a, b, c, d).kill(200, c, a).kill(300, d, b)
				l.ev(748, RoundOfficiallyEnded).ev(748, RoundPrestart).spawn(748, a, b, c, d)
				l.surrender(900, CT).ev(1093, WinPanelMatch)
				return l
			},
			rounds: []string{
				"1:0-100-300 B CT elimination",
				"2:748-900-900 B CT surrender",
			},
			kills: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := mustBuild(t, tt.build(), tt.notes...)
			if got := roundsText(res); !slices.Equal(got, tt.rounds) {
				t.Errorf("rounds\n got %q\nwant %q", got, tt.rounds)
			}
			if len(res.Kills) != tt.kills {
				t.Errorf("%d kills, want %d", len(res.Kills), tt.kills)
			}
		})
	}
}

func TestLiveWindow(t *testing.T) {
	tests := []struct {
		name       string
		kinds      []Kind
		start, end int
		notes      int
	}{
		{"both", []Kind{BeginNewMatch, PlayerDeath, WinPanelMatch, PlayerDeath}, 0, 2, 0},
		{"last begin_new_match", []Kind{BeginNewMatch, PlayerDeath, BeginNewMatch, PlayerDeath, WinPanelMatch}, 2, 4, 0},
		// A new match started after the end doesn't hide the one played.
		{"restart after the end", []Kind{BeginNewMatch, PlayerDeath, WinPanelMatch, BeginNewMatch, PlayerDeath}, 0, 2, 0},
		{"no win panel", []Kind{PlayerDeath, BeginNewMatch, PlayerDeath}, 1, 3, 1},
		{"no begin_new_match", []Kind{PlayerDeath, WinPanelMatch}, 0, 1, 1},
		{"no begin_new_match, a restart before the first round", []Kind{PlayerSpawn, RoundPrestart, RoundFreezeEnd, PlayerDeath, WinPanelMatch}, 1, 4, 1},
		// A death before that restart: the recording starts mid-round.
		{"no begin_new_match, a death before the first restart", []Kind{PlayerDeath, RoundPrestart, RoundFreezeEnd, PlayerDeath, WinPanelMatch}, 0, 4, 1},
		// A match restarted at the end of the demo doesn't hide the one played.
		{"begin_new_match after the last round", []Kind{RoundFreezeEnd, PlayerDeath, BeginNewMatch}, 0, 3, 2},
		{"begin_new_match before and after the rounds", []Kind{BeginNewMatch, RoundFreezeEnd, PlayerDeath, BeginNewMatch, WinPanelMatch}, 0, 4, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var l testLog
			for i, k := range tt.kinds {
				l.ev(int32(i), k)
			}
			var n notes
			w := liveWindow(&l.Log, &n)
			if w.start != tt.start || w.end != tt.end || len(n) != tt.notes {
				t.Errorf("window [%d, %d) with %d notes, want [%d, %d) with %d", w.start, w.end, len(n), tt.start, tt.end, tt.notes)
			}
		})
	}
}
