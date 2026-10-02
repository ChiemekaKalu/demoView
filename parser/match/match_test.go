package match

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// specExample is the match.json example from the spec, verbatim.
const specExample = `{
  "schema_version": 1,
  "parser_version": "0.1.0",
  "match": { "map": "de_mirage", "tick_rate": 64, "demo_sha256": "..." },
  "players": [
    { "idx": 0, "steam_id": "7656...", "name": "...", "team": "A" }
  ],
  "rounds": [
    { "n": 1, "start_tick": 0, "freeze_end_tick": 0, "end_tick": 0,
      "winner": "A", "reason": "bomb_defused", "side_a": "CT" }
  ],
  "kills": [
    { "round": 1, "tick": 0, "attacker": 0, "victim": 5, "assister": null,
      "weapon": "ak47", "headshot": true }
  ],
  "scoreboard": [
    { "player": 0, "kills": 0, "deaths": 0, "assists": 0, "adr": 0.0, "hs_pct": 0.0 }
  ]
}`

// TestSpecExample checks that the types round-trip the spec's example with no
// field missing, extra or renamed.
func TestSpecExample(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(specExample))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Write(&buf, f); err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(specExample), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip differs:\n got %s\nwant %s", buf.Bytes(), specExample)
	}
}

func TestWriteEmptyArraysAndNullablePlayers(t *testing.T) {
	two, five := 2, 5
	f := File{Kills: []Kill{{Attacker: &two, Assister: &five}, {}}}
	var buf bytes.Buffer
	if err := Write(&buf, f); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Players, Rounds, Scoreboard []any
		Kills                       []map[string]any
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Players == nil || got.Rounds == nil || got.Scoreboard == nil {
		t.Fatalf("nil slices must encode as [], got %s", buf.Bytes())
	}
	for _, c := range []struct {
		kill int
		key  string
		want any
	}{
		{0, "attacker", 2.0},
		{0, "assister", 5.0},
		{1, "attacker", nil},
		{1, "assister", nil},
	} {
		if v, ok := got.Kills[c.kill][c.key]; !ok || v != c.want {
			t.Errorf("kill %d %s = %v (present %v), want %v", c.kill, c.key, v, ok, c.want)
		}
	}
}

func TestWriteFileCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := WriteFile(dir, File{SchemaVersion: SchemaVersion}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid JSON: %s", b)
	}
}
