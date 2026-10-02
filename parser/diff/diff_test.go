package diff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// baseMatch is a small valid match.json as a mutable map.
func baseMatch() map[string]any {
	return map[string]any{
		"schema_version": 1,
		"parser_version": "0.1.0",
		"match":          map[string]any{"map": "de_mirage", "tick_rate": 64, "demo_sha256": "ab"},
		"players": []any{
			map[string]any{"idx": 0, "steam_id": "100", "name": "a", "team": "A"},
			map[string]any{"idx": 1, "steam_id": "200", "name": "b", "team": "B"},
			map[string]any{"idx": 2, "steam_id": "0", "name": "Bot", "team": "B"},
		},
		"rounds": []any{
			map[string]any{"n": 1, "start_tick": 10, "freeze_end_tick": 20, "end_tick": 90, "winner": "A", "reason": "elimination", "side_a": "CT"},
			map[string]any{"n": 2, "start_tick": 100, "freeze_end_tick": 120, "end_tick": 190, "winner": "B", "reason": "bomb_exploded", "side_a": "CT"},
		},
		"kills": []any{
			map[string]any{"round": 1, "tick": 50, "attacker": 0, "victim": 1, "assister": nil, "weapon": "ak47", "headshot": true},
			map[string]any{"round": 1, "tick": 60, "attacker": 0, "victim": 2, "assister": nil, "weapon": "ak47", "headshot": false},
			map[string]any{"round": 2, "tick": 150, "attacker": nil, "victim": 0, "assister": nil, "weapon": "world", "headshot": false},
		},
		"scoreboard": []any{
			map[string]any{"player": 0, "kills": 2, "deaths": 1, "assists": 0, "adr": 100.0, "hs_pct": 50.0},
			map[string]any{"player": 1, "kills": 0, "deaths": 1, "assists": 0, "adr": 0.0, "hs_pct": 0.0},
			map[string]any{"player": 2, "kills": 0, "deaths": 1, "assists": 0, "adr": 0.0, "hs_pct": 0.0},
		},
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func at(m map[string]any, list string, i int) map[string]any {
	return m[list].([]any)[i].(map[string]any)
}

func paths(r Report) []string {
	var out []string
	for _, m := range r.Mismatches {
		out = append(out, m.Path)
	}
	return out
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		mod  func(m map[string]any)
		want []string // mismatch paths, in order
	}{
		{"identical", nil, nil},
		{"version differs, ignored", func(m map[string]any) { m["parser_version"] = "9" }, nil},
		{"idx order differs, same players", func(m map[string]any) {
			// Swap players 1 and 2 and every reference to them.
			at(m, "players", 1)["idx"], at(m, "players", 2)["idx"] = 2, 1
			at(m, "kills", 0)["victim"], at(m, "kills", 1)["victim"] = 2, 1
			at(m, "scoreboard", 1)["player"], at(m, "scoreboard", 2)["player"] = 2, 1
		}, nil},
		{"map", func(m map[string]any) { m["match"].(map[string]any)["map"] = "de_nuke" }, []string{"match.map"}},
		{"team", func(m map[string]any) { at(m, "players", 0)["team"] = "B" }, []string{"players[100].team"}},
		{"missing player", func(m map[string]any) {
			m["players"] = m["players"].([]any)[:2]
		}, []string{"players.count", "players[bot:Bot]", "kills[1].victim", "scoreboard[<idx 2>]", "scoreboard[bot:Bot]"}},
		{"round tick", func(m map[string]any) { at(m, "rounds", 1)["freeze_end_tick"] = 121 }, []string{"rounds[n=2].freeze_end_tick"}},
		{"round reason", func(m map[string]any) { at(m, "rounds", 0)["reason"] = "surrender" }, []string{"rounds[n=1].reason"}},
		{"kill tick", func(m map[string]any) { at(m, "kills", 1)["tick"] = 61 }, []string{"kills[1].tick"}},
		{"kill weapon", func(m map[string]any) { at(m, "kills", 0)["weapon"] = "m4a1" }, []string{"kills[0].weapon"}},
		{"null vs player attacker", func(m map[string]any) { at(m, "kills", 2)["attacker"] = 0 }, []string{"kills[2].attacker"}},
		{"headshot", func(m map[string]any) { at(m, "kills", 1)["headshot"] = true }, []string{"kills[1].headshot"}},
		{"assister", func(m map[string]any) { at(m, "kills", 0)["assister"] = 2 }, []string{"kills[0].assister"}},
		{"kill count", func(m map[string]any) { m["kills"] = m["kills"].([]any)[:2] }, []string{"kills.count"}},
		{"kills", func(m map[string]any) { at(m, "scoreboard", 0)["kills"] = 3 }, []string{"scoreboard[100].kills"}},
		{"hs_pct exact", func(m map[string]any) { at(m, "scoreboard", 0)["hs_pct"] = 50.1 }, []string{"scoreboard[100].hs_pct"}},
		{"adr within 1%", func(m map[string]any) { at(m, "scoreboard", 0)["adr"] = 100.9 }, nil},
		{"adr over 1%", func(m map[string]any) { at(m, "scoreboard", 0)["adr"] = 101.2 }, []string{"scoreboard[100].adr"}},
		{"adr zero within rounding", func(m map[string]any) { at(m, "scoreboard", 1)["adr"] = 0.05 }, nil},
	}
	ref := mustJSON(t, baseMatch())
	for _, tc := range tests {
		m := baseMatch()
		if tc.mod != nil {
			tc.mod(m)
		}
		r, err := Compare(mustJSON(t, m), ref)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := paths(r); !slices.Equal(got, tc.want) {
			t.Errorf("%s: mismatches %q, want %q\n%s", tc.name, got, tc.want, r)
		}
		if r.OK() != (len(tc.want) == 0) {
			t.Errorf("%s: OK() = %v", tc.name, r.OK())
		}
	}
}

func TestCompareInvalidJSON(t *testing.T) {
	if _, err := Compare([]byte("{"), mustJSON(t, baseMatch())); err == nil || !strings.Contains(err.Error(), "our match.json") {
		t.Errorf("err = %v", err)
	}
}

func TestReportString(t *testing.T) {
	if s := (Report{}).String(); s != "OK" {
		t.Errorf("empty report = %q", s)
	}
	r := Report{}
	r.add("kills[0].weapon", "ak47", "m4a1")
	if s := r.String(); s != "1 mismatch\n  kills[0].weapon: ours \"ak47\", reference \"m4a1\"" {
		t.Errorf("String() = %q", s)
	}
	for range 50 {
		r.add("x", 1, 2)
	}
	if s := r.String(); !strings.HasPrefix(s, "51 mismatches") || !strings.HasSuffix(s, "... and 11 more") {
		t.Errorf("long report:\n%s", s)
	}
}

func baseRound() map[string]any {
	return map[string]any{
		"schema_version": 1, "round": 1, "hz": 16, "start_tick": 20, "frames": 3,
		"players": []any{
			map[string]any{"idx": 0, "x": []any{0, 10, 20}, "y": []any{0, 0, 0}, "yaw": []any{0, 359, 90}, "hp": []any{100, 100, 100}},
			map[string]any{"idx": 1, "x": []any{5, 5, 5}, "y": []any{5, 5, 5}, "yaw": []any{180, 180, 180}, "hp": []any{100, 0, 0}},
		},
		"grenades": []any{
			map[string]any{"type": "flash", "thrower": 0, "throw_frame": 0, "detonate_frame": 1, "end_frame": 1, "path": []any{[]any{0, 0}, []any{50, 50}}},
		},
		"events": []any{map[string]any{"frame": 1, "type": "kill", "kill": 0}},
	}
}

func TestCompareRound(t *testing.T) {
	match := mustJSON(t, baseMatch())
	tests := []struct {
		name string
		mod  func(r map[string]any)
		want []string
	}{
		{"identical", nil, nil},
		{"within one unit", func(r map[string]any) {
			at(r, "players", 0)["x"] = []any{1, 9, 21}
		}, nil},
		{"two units off", func(r map[string]any) {
			at(r, "players", 0)["y"] = []any{0, 2, 3}
		}, []string{"round[1].players[100].xy[1]"}},
		{"yaw wraps around", func(r map[string]any) { at(r, "players", 0)["yaw"] = []any{359, 0, 91} }, nil},
		{"yaw off", func(r map[string]any) { at(r, "players", 0)["yaw"] = []any{0, 355, 90} }, []string{"round[1].players[100].yaw[1]"}},
		{"hp exact", func(r map[string]any) { at(r, "players", 1)["hp"] = []any{100, 1, 0} }, []string{"round[1].players[200].hp[1]"}},
		{"frames", func(r map[string]any) { r["frames"] = 4 }, []string{"round[1].frames"}},
		{"missing player", func(r map[string]any) { r["players"] = r["players"].([]any)[:1] }, []string{"round[1].players[200]"}},
		{"grenade path", func(r map[string]any) {
			at(r, "grenades", 0)["path"] = []any{[]any{0, 0}, []any{52, 50}}
		}, []string{"round[1].grenades[0].path[1]"}},
		{"grenade end", func(r map[string]any) { at(r, "grenades", 0)["end_frame"] = 2 }, []string{"round[1].grenades[0].end_frame"}},
		{"event frame", func(r map[string]any) { at(r, "events", 0)["frame"] = 2 }, []string{"round[1].events[0].frame"}},
	}
	ref := mustJSON(t, baseRound())
	for _, tc := range tests {
		rf := baseRound()
		if tc.mod != nil {
			tc.mod(rf)
		}
		r, err := CompareRound(match, match, mustJSON(t, rf), ref)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := paths(r); !slices.Equal(got, tc.want) {
			t.Errorf("%s: mismatches %q, want %q\n%s", tc.name, got, tc.want, r)
		}
	}
}

// TestSelfDiffReference diffs the oracle's exports in testdata/reference
// against themselves, which must find nothing. They are not in git, so it
// skips without them.
func TestSelfDiffReference(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join("..", "testdata", "reference", "*"))
	if len(dirs) == 0 {
		t.Skip("no exports in testdata/reference")
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			match, err := os.ReadFile(filepath.Join(dir, "match.json"))
			if err != nil {
				t.Skip("no match.json")
			}
			if r, err := Compare(match, match); err != nil || !r.OK() {
				t.Errorf("match.json: %v\n%s", err, r)
			}
			rounds, _ := filepath.Glob(filepath.Join(dir, "rounds", "*.json"))
			for _, p := range rounds {
				rf, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				if r, err := CompareRound(match, match, rf, rf); err != nil || !r.OK() {
					t.Errorf("%s: %v\n%s", filepath.Base(p), err, r)
				}
			}
			t.Logf("match.json and %d round files agree with themselves", len(rounds))
		})
	}
}
