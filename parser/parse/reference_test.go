package parse

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/diff"
	"github.com/ChiemekaKalu/demoview/parser/match"
)

// reference is a local demo with an oracle export of its match.json.
type reference struct {
	name, demo string
	match      []byte
}

// TestReferenceDiff parses each local demo that has an oracle export in
// testdata/reference/<name>/match.json, and diffs the result against it.
// Subtests are named by map, then by file, so the results read per map.
// Neither demos nor exports are in git, so it skips without them, and under
// -short.
func TestReferenceDiff(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	paths, _ := filepath.Glob(filepath.Join("..", "testdata", "demos", "*.dem"))
	byMap := map[string][]reference{}
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".dem")
		ref, err := os.ReadFile(filepath.Join("..", "testdata", "reference", name, "match.json"))
		if err != nil {
			continue
		}
		var head struct {
			Match struct {
				Map string `json:"map"`
			} `json:"match"`
		}
		if err := json.Unmarshal(ref, &head); err != nil {
			t.Fatalf("%s: reference match.json: %v", name, err)
		}
		byMap[head.Match.Map] = append(byMap[head.Match.Map], reference{name, p, ref})
	}
	if len(byMap) == 0 {
		t.Skip("no demos in testdata/demos with an export in testdata/reference")
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
