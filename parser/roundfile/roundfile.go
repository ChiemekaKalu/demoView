// Package roundfile defines rounds/NN.json, the per-round replay the parser
// writes next to match.json. Like package match, its JSON names and nesting
// are a contract: changing them needs a schema_version bump, which
// match.SchemaVersion holds for both files.
//
// Values are columns, so a reader indexes frame i of every column directly.
// Frame i is tick StartTick + i*tick_rate/Hz.
package roundfile

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Dir is the directory, under a parse output directory, that holds the
// round files.
const Dir = "rounds"

// Grenade types.
const (
	Smoke      = "smoke"
	Flash      = "flash"
	HE         = "he"
	Molotov    = "molotov"
	Incendiary = "incendiary"
	Decoy      = "decoy"
)

// EventKill is the type of a kill event, the only event type in v1.
const EventKill = "kill"

// File is one rounds/NN.json document.
type File struct {
	SchemaVersion int       `json:"schema_version"`
	Round         int       `json:"round"` // match.json rounds[].n
	Hz            int       `json:"hz"`
	StartTick     int       `json:"start_tick"` // the round's freeze_end_tick
	Frames        int       `json:"frames"`     // the length of every column
	Players       []Player  `json:"players"`
	Grenades      []Grenade `json:"grenades"`
	Events        []Event   `json:"events"`
}

// Player is one player's columns. A dead player holds the position and yaw
// of their death, with HP 0.
type Player struct {
	Idx int     `json:"idx"` // match.json players[].idx
	X   []int16 `json:"x"`   // game units
	Y   []int16 `json:"y"`
	Yaw []int16 `json:"yaw"` // degrees in [0, 360), counterclockwise from +x
	HP  []int16 `json:"hp"`  // 0 to 100; 0 is dead
}

// Grenade is one thrown grenade. Path has one point per frame from
// ThrowFrame to DetonateFrame, inclusive.
type Grenade struct {
	Type          string     `json:"type"`
	Thrower       int        `json:"thrower"` // match.json players[].idx
	ThrowFrame    int        `json:"throw_frame"`
	DetonateFrame int        `json:"detonate_frame"`
	EndFrame      int        `json:"end_frame"`
	Path          [][2]int16 `json:"path"` // [x, y] in game units
}

// Event is something that happens at one frame.
type Event struct {
	Frame int    `json:"frame"`
	Type  string `json:"type"` // EventKill
	Kill  int    `json:"kill"` // index into match.json kills
}

// Name returns the file name of round n: "NN.json", at least two digits.
func Name(n int) string { return fmt.Sprintf("%02d.json", n) }

// Write encodes f to w as compact JSON. Nil slices are written as empty
// arrays, never null.
func Write(w io.Writer, f File) error {
	f.Players = nonNil(f.Players)
	f.Grenades = nonNil(f.Grenades)
	f.Events = nonNil(f.Events)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(f)
}

// WriteFile writes f to dir/rounds/NN.json, creating the directories if
// needed.
func WriteFile(dir string, f File) error {
	dir = filepath.Join(dir, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := os.Create(filepath.Join(dir, Name(f.Round)))
	if err != nil {
		return err
	}
	err = Write(out, f)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
