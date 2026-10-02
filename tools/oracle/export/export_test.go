package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

func TestYawDegrees(t *testing.T) {
	for in, want := range map[float32]int{0: 0, 90: 90, 359.6: 0, 360: 0, -90: 270, -0.4: 0, 180.5: 181, 721: 1} {
		if got := yawDegrees(in); got != want {
			t.Errorf("yawDegrees(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestCeilDiv(t *testing.T) {
	for _, c := range [][3]int{{0, 4, 0}, {1, 4, 1}, {4, 4, 1}, {5, 4, 2}, {-1, 4, 0}, {-4, 4, -1}, {-5, 4, -1}} {
		if got := ceilDiv(c[0], c[1]); got != c[2] {
			t.Errorf("ceilDiv(%d, %d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
}

func TestRoundFileName(t *testing.T) {
	for n, want := range map[int]string{1: "01.json", 9: "09.json", 10: "10.json", 100: "100.json"} {
		if got := RoundFileName(n); got != want {
			t.Errorf("RoundFileName(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestReasonsUseSchemaNames(t *testing.T) {
	allowed := map[string]bool{"elimination": true, "bomb_exploded": true, "bomb_defused": true, "time_expired": true, "surrender": true}
	for r, name := range reasons {
		if !allowed[name] {
			t.Errorf("reason %d maps to %q, not a schema reason", r, name)
		}
	}
	if reasons[events.RoundEndReasonTargetSaved] != "time_expired" {
		t.Error("TargetSaved should be time_expired")
	}
}

// known are facts about the fixture demos, checked when they are present.
var known = map[string]struct {
	mapName string
	rounds  int
	scoreA  int
}{
	"faceit_nuke_2026.dem":     {"de_nuke", 19, 13},
	"valve_mm_mirage_2024.dem": {"de_mirage", 10, 2},
}

// TestFixtureDemos exports whatever demos are in parser/testdata/demos.
// They are not in git, so it skips when there are none.
func TestFixtureDemos(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	paths, _ := filepath.Glob(filepath.Join("..", "..", "..", "parser", "testdata", "demos", "*.dem"))
	if len(paths) == 0 {
		t.Skip("no demos in parser/testdata/demos")
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			f, err := os.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			res, err := Run(f, DefaultHz)
			if err != nil {
				t.Fatal(err)
			}
			m := res.Match
			if !strings.HasPrefix(m.Match.Map, "de_") || m.Match.TickRate != 64 || len(m.Match.DemoSHA256) != 64 {
				t.Errorf("match info = %+v", m.Match)
			}
			if len(res.Rounds) != len(m.Rounds) {
				t.Fatalf("%d round files for %d rounds", len(res.Rounds), len(m.Rounds))
			}
			if k, ok := known[filepath.Base(p)]; ok {
				scoreA := 0
				for _, r := range m.Rounds {
					if r.Winner == "A" {
						scoreA++
					}
				}
				if m.Match.Map != k.mapName || len(m.Rounds) != k.rounds || scoreA != k.scoreA {
					t.Errorf("map %s, %d rounds, team A won %d; want %s, %d, %d", m.Match.Map, len(m.Rounds), scoreA, k.mapName, k.rounds, k.scoreA)
				}
			}
			if m.Rounds[0].SideA != "CT" {
				t.Error("team A must start on CT")
			}

			events := 0
			for _, rf := range res.Rounds {
				for _, tr := range rf.Players {
					for _, n := range []int{len(tr.X), len(tr.Y), len(tr.Yaw), len(tr.HP)} {
						if n != rf.Frames {
							t.Fatalf("round %d player %d: column of %d, want %d frames", rf.Round, tr.Idx, n, rf.Frames)
						}
					}
				}
				for _, g := range rf.Grenades {
					if len(g.Path) != g.DetonateFrame-g.ThrowFrame+1 {
						t.Errorf("round %d: %s path has %d points for frames %d..%d", rf.Round, g.Type, len(g.Path), g.ThrowFrame, g.DetonateFrame)
					}
				}
				events += len(rf.Events)
			}
			if events != len(m.Kills) {
				t.Errorf("%d kill events for %d kills", events, len(m.Kills))
			}
			if res.Stats.UnknownWeapons != 0 || res.Stats.UnknownGrenades != 0 {
				t.Errorf("stats = %+v", res.Stats)
			}
		})
	}
}
