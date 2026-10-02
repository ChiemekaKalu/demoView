package parse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/diff"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/roundfile"
)

// reference is a local demo with an oracle export of its match.json, in
// dir.
type reference struct {
	name, demo, dir string
	match           []byte
	mapName         string
}

// references returns the local demos that have an oracle export in
// testdata/reference/<name>. Neither demos nor exports are in git, so it
// skips the test without them, and under -short.
func references(t *testing.T) []reference {
	t.Helper()
	if testing.Short() {
		t.Skip("slow")
	}
	paths, _ := filepath.Glob(filepath.Join("..", "testdata", "demos", "*.dem"))
	var refs []reference
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".dem")
		dir := filepath.Join("..", "testdata", "reference", name)
		m, err := os.ReadFile(filepath.Join(dir, match.FileName))
		if err != nil {
			continue
		}
		var head struct {
			Match struct {
				Map string `json:"map"`
			} `json:"match"`
		}
		if err := json.Unmarshal(m, &head); err != nil {
			t.Fatalf("%s: reference match.json: %v", name, err)
		}
		refs = append(refs, reference{name, p, dir, m, head.Match.Map})
	}
	if len(refs) == 0 {
		t.Skip("no demos in testdata/demos with an export in testdata/reference")
	}
	return refs
}

// TestReferenceDiff parses each local demo that has an oracle export and
// diffs its match.json against the export's. Subtests are named by map,
// then by file, so the results read per map.
func TestReferenceDiff(t *testing.T) {
	byMap := map[string][]reference{}
	for _, r := range references(t) {
		byMap[r.mapName] = append(byMap[r.mapName], r)
	}
	for _, m := range slices.Sorted(maps.Keys(byMap)) {
		t.Run(m, func(t *testing.T) {
			t.Parallel()
			for _, r := range byMap[m] {
				t.Run(r.name, func(t *testing.T) {
					t.Parallel()
					diffReference(t, r)
				})
			}
		})
	}
}

func diffReference(t *testing.T, r reference) {
	f, err := os.Open(r.demo)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := Run(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	var ours bytes.Buffer
	if err := match.Write(&ours, got); err != nil {
		t.Fatal(err)
	}
	report, err := diff.Compare(ours.Bytes(), r.match)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Errorf("against the oracle: %s", report)
	}
}

// TestReferenceRounds parses each local demo that has oracle round files
// with round files on, and diffs every round file against the oracle's with
// diff.CompareRound. Player columns and events must match exactly within
// the tolerances. So must grenades, except where the oracle departs from
// the grenade conventions in plan.md (see oracleDeviation); those are
// counted and logged.
func TestReferenceRounds(t *testing.T) {
	for _, r := range references(t) {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			files, _ := filepath.Glob(filepath.Join(r.dir, roundfile.Dir, "*.json"))
			if len(files) == 0 {
				t.Skip("no oracle round files")
			}
			f, err := os.Open(r.demo)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			out, _, err := Parse(context.Background(), f, Options{Rounds: true})
			if err != nil {
				t.Fatal(err)
			}
			if out.RoundsErr != nil {
				t.Fatal(out.RoundsErr)
			}
			if len(out.Rounds) != len(files) {
				t.Errorf("%d round files, the oracle has %d", len(out.Rounds), len(files))
			}
			var ourMatch bytes.Buffer
			if err := match.Write(&ourMatch, out.Match); err != nil {
				t.Fatal(err)
			}
			deviations := map[string]int{}
			for _, rf := range out.Rounds {
				ref, err := os.ReadFile(filepath.Join(r.dir, roundfile.Dir, roundfile.Name(rf.Round)))
				if err != nil {
					t.Errorf("round %d: %v", rf.Round, err)
					continue
				}
				var ours bytes.Buffer
				if err := roundfile.Write(&ours, rf); err != nil {
					t.Fatal(err)
				}
				report, err := diff.CompareRound(ourMatch.Bytes(), r.match, ours.Bytes(), ref)
				if err != nil {
					t.Fatal(err)
				}
				var refFile oracleRound
				if err := json.Unmarshal(ref, &refFile); err != nil {
					t.Fatal(err)
				}
				for _, m := range report.Mismatches {
					if kind := oracleDeviation(m, rf, refFile); kind != "" {
						deviations[kind]++
					} else {
						t.Errorf("%s: ours %s, oracle %s", m.Path, m.Ours, m.Ref)
					}
				}
			}
			t.Logf("%d rounds; oracle deviations from the grenade conventions: %v", len(out.Rounds), deviations)
		})
	}
}

// oracleRound is the part of an oracle round file oracleDeviation reads.
type oracleRound struct {
	Frames   int `json:"frames"`
	Grenades []struct {
		Type     string   `json:"type"`
		EndFrame int      `json:"end_frame"`
		Path     [][2]int `json:"path"`
	} `json:"grenades"`
}

var grenadeField = regexp.MustCompile(`^round\[\d+\]\.grenades\[(\d+)\]\.(end_frame|path\[(\d+)\])$`)

// oracleDeviation names the known way the oracle departs from the grenade
// conventions that m shows, or returns "" if m is a real difference:
//
//   - "fire end": a molotov or incendiary ends when its fire goes out
//     (inferno_expire), but the oracle ends it later, when the inferno
//     entity is removed.
//   - "fire path [0,0]": the oracle's last path point of a molotov or
//     incendiary, at its detonation, is [0, 0], read from a projectile
//     already removed; ours is where the fire started.
//   - "decoy end": a decoy removed before it ever went off ends there, as a
//     molotov that never ignites does; the oracle runs it to the round's
//     last frame.
func oracleDeviation(m diff.Mismatch, ours roundfile.File, ref oracleRound) string {
	sub := grenadeField.FindStringSubmatch(m.Path)
	if sub == nil {
		return ""
	}
	i, _ := strconv.Atoi(sub[1])
	if i >= len(ours.Grenades) || i >= len(ref.Grenades) {
		return ""
	}
	g, rg := ours.Grenades[i], ref.Grenades[i]
	fire := g.Type == roundfile.Molotov || g.Type == roundfile.Incendiary
	switch {
	case sub[2] == "end_frame" && fire && g.EndFrame < rg.EndFrame:
		return "fire end"
	case sub[2] == "end_frame" && g.Type == roundfile.Decoy && g.EndFrame == g.DetonateFrame && rg.EndFrame == ref.Frames-1:
		return "decoy end"
	case sub[3] != "" && fire:
		k, _ := strconv.Atoi(sub[3])
		only := fmt.Sprintf("(%d, %d), 1 of %d off by more than %d unit", g.Path[k][0], g.Path[k][1], len(g.Path), diff.PositionTolerance)
		if k == len(rg.Path)-1 && rg.Path[k] == [2]int{0, 0} && m.Ours == strconv.Quote(only) {
			return "fire path [0,0]"
		}
	}
	return ""
}
