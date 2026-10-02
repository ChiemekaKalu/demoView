package matchlogic

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/eventlog"
)

// logLine is one narrative log record, as written.
type logLine struct {
	Tick  int            `json:"tick"`
	Round int            `json:"round"`
	Type  string         `json:"type"`
	Basis []int          `json:"basis"`
	D     map[string]any `json:"d"`
}

// tellLog builds l with a narrative log and returns its facts and derived
// records.
func tellLog(t *testing.T, l *testLog, sites []BombSite) (Result, []logLine, []logLine) {
	t.Helper()
	spool := eventlog.NewSpool(eventlog.DefaultLimit)
	res := Build(&l.Log, Options{Map: "de_test", TickRate: 64, Log: spool, BombSites: sites})
	lg, err := spool.Finalize(eventlog.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	return res, readLog(t, lg.Facts), readLog(t, lg.Derived)
}

func readLog(t *testing.T, ndjson []byte) []logLine {
	t.Helper()
	var out []logLine
	for line := range strings.Lines(string(ndjson)) {
		var l logLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("%v: %s", err, line)
		}
		out = append(out, l)
	}
	return out
}

func ofType(lines []logLine, typ string) []logLine {
	var out []logLine
	for _, l := range lines {
		if l.Type == typ {
			out = append(out, l)
		}
	}
	return out
}

func idxOf(res Result, name string) float64 {
	for _, p := range res.Players {
		if p.Name == name {
			return float64(p.Idx)
		}
	}
	return -1
}

// A defused bomb's timeline: the plant with its site from the entity stage,
// each defuse attempt with its kit, and the outcome, citing those facts.
func TestStoryBombTimeline(t *testing.T) {
	l, a, b, c, d := twoVsTwo()
	l.ev(10, BeginNewMatch).spawn(10, a, b, c, d).ev(100, RoundFreezeEnd)
	l.bomb(150, BombPlanted, c).kill(250, a, c).kill(260, b, d)
	l.add(Entry{Kind: BombBeginDefuse, Tick: 300, Player: b, Attacker: NoPlayer, Assister: NoPlayer})
	l.add(Entry{Kind: BombBeginDefuse, Tick: 350, Player: a, Attacker: NoPlayer, Assister: NoPlayer, Kit: true})
	l.bomb(400, BombDefused, a).ev(500, WinPanelMatch)
	res, facts, derived := tellLog(t, l, []BombSite{{Tick: 151, Site: "B"}})

	plant := ofType(facts, "bomb.plant")
	if len(plant) != 1 || plant[0].Tick != 150 || plant[0].Round != 1 || plant[0].D["site"] != "B" || plant[0].D["player"] != idxOf(res, "c") {
		t.Errorf("bomb.plant facts %+v", plant)
	}
	starts := ofType(facts, "bomb.defuse_start")
	if len(starts) != 2 || starts[0].D["kit"] != false || starts[1].D["kit"] != true {
		t.Errorf("bomb.defuse_start facts %+v", starts)
	}

	bomb := ofType(derived, "round.bomb")
	if len(bomb) != 1 {
		t.Fatalf("%d round.bomb records, want 1", len(bomb))
	}
	want := map[string]any{
		"planter": idxOf(res, "c"), "plant_tick": 150.0, "site": "B",
		"defuses": []any{
			map[string]any{"player": idxOf(res, "b"), "tick": 300.0, "kit": false},
			map[string]any{"player": idxOf(res, "a"), "tick": 350.0, "kit": true},
		},
		"outcome": "defused", "outcome_tick": 400.0,
	}
	if !reflect.DeepEqual(bomb[0].D, want) {
		t.Errorf("round.bomb d\n got %v\nwant %v", bomb[0].D, want)
	}
	if len(bomb[0].Basis) != 4 { // the plant, two defuse attempts and the defuse
		t.Errorf("round.bomb basis %v, want 4 facts", bomb[0].Basis)
	}

	end := ofType(derived, "round.end")
	if len(end) != 1 || end[0].D["reason"] != "bomb_defused" || end[0].D["method"] != "bomb" || end[0].D["winner"] != "A" {
		t.Errorf("round.end %+v", end)
	}
	if score := ofType(derived, "round.score"); len(score) != 1 || score[0].D["a"] != 1.0 || score[0].D["b"] != 0.0 {
		t.Errorf("round.score %+v", score)
	}
}

// Without the entity stage there is no site, and a bomb that neither
// explodes nor is defused has outcome none.
func TestStoryBombWithoutSite(t *testing.T) {
	l, a, b, c, d := twoVsTwo()
	l.ev(10, BeginNewMatch).spawn(10, a, b, c, d).ev(100, RoundFreezeEnd)
	l.bomb(150, BombPlanted, c).kill(250, c, a).kill(260, d, b).ev(500, WinPanelMatch)
	_, facts, derived := tellLog(t, l, nil)
	if p := ofType(facts, "bomb.plant"); len(p) != 1 || p[0].D["site"] != nil {
		t.Errorf("bomb.plant %+v, want no site", p)
	}
	bomb := ofType(derived, "round.bomb")
	if len(bomb) != 1 || bomb[0].D["site"] != nil || bomb[0].D["outcome"] != "none" || bomb[0].D["outcome_tick"] != nil {
		t.Errorf("round.bomb %+v", bomb)
	}
}

// The derived rounds agree with match.json, and every live death is a fact
// of its round.
func TestStoryAgreesWithTheMatch(t *testing.T) {
	l, a, b, c, d := twoVsTwo()
	l.ev(5, PlayerDeath) // warmup, outside the match
	l.ev(10, BeginNewMatch).spawn(10, a, b, c, d).ev(100, RoundFreezeEnd)
	l.kill(200, c, a).kill(300, d, b)
	l.ev(748, RoundOfficiallyEnded).spawn(748, a, b, c, d).ev(1000, RoundFreezeEnd)
	l.kill(1100, a, c).kill(1200, a, d).ev(1300, WinPanelMatch)
	res, facts, derived := tellLog(t, l, nil)

	begins, ends := ofType(derived, "round.begin"), ofType(derived, "round.end")
	if len(begins) != len(res.Rounds) || len(ends) != len(res.Rounds) {
		t.Fatalf("%d begins and %d ends for %d rounds", len(begins), len(ends), len(res.Rounds))
	}
	for i, r := range res.Rounds {
		if begins[i].D["start_tick"] != float64(r.StartTick) || ends[i].Tick != r.EndTick || ends[i].D["winner"] != r.Winner {
			t.Errorf("round %d: begin %v, end %+v; match.json %+v", r.N, begins[i].D, ends[i], r)
		}
	}
	var live int
	for _, f := range ofType(facts, "combat.death") {
		if f.Round > 0 {
			live++
		}
	}
	if live != len(res.Kills) {
		t.Errorf("%d live combat.death facts, %d kills", live, len(res.Kills))
	}
	if w := ofType(derived, "match.live_window"); len(w) != 1 || w[0].D["start_tick"] != 10.0 || w[0].D["end_tick"] != 1300.0 {
		t.Errorf("match.live_window %+v", w)
	}
}
