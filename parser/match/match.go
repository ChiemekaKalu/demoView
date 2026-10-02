// Package match defines match.json, the per-match summary the parser writes.
// The types mirror the schema in the spec, so their JSON names and nesting
// are a contract: changing them needs a schema_version bump.
package match

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// SchemaVersion is the schema_version these types describe.
const SchemaVersion = 1

// FileName is the name of the summary file in a parse output directory.
const FileName = "match.json"

// File is the match.json document.
type File struct {
	SchemaVersion int        `json:"schema_version"`
	ParserVersion string     `json:"parser_version"`
	Match         Info       `json:"match"`
	Players       []Player   `json:"players"`
	Rounds        []Round    `json:"rounds"`
	Kills         []Kill     `json:"kills"`
	Scoreboard    []ScoreRow `json:"scoreboard"`
}

// Info describes the match as a whole.
type Info struct {
	Map        string `json:"map"`
	TickRate   int    `json:"tick_rate"`
	DemoSHA256 string `json:"demo_sha256"` // hex SHA-256 of the demo file
}

// Player is one participant. Other records refer to players by Idx.
type Player struct {
	Idx     int    `json:"idx"`
	SteamID string `json:"steam_id"`
	Name    string `json:"name"`
	Team    string `json:"team"` // "A" or "B" for the whole match
}

// Round is one played round.
type Round struct {
	N             int    `json:"n"`
	StartTick     int    `json:"start_tick"`
	FreezeEndTick int    `json:"freeze_end_tick"`
	EndTick       int    `json:"end_tick"`
	Winner        string `json:"winner"` // "A" or "B"
	Reason        string `json:"reason"`
	SideA         string `json:"side_a"` // side team A played: "CT" or "T"
}

// Kill is one death. Attacker, Victim and Assister are player indexes.
type Kill struct {
	Round    int    `json:"round"`
	Tick     int    `json:"tick"`
	Attacker *int   `json:"attacker"` // nil for a world death (fall, bomb, disconnect); Victim for a suicide
	Victim   int    `json:"victim"`
	Assister *int   `json:"assister"` // nil when nobody assisted
	Weapon   string `json:"weapon"`
	Headshot bool   `json:"headshot"`
}

// ScoreRow is one player's line on the final scoreboard.
type ScoreRow struct {
	Player  int     `json:"player"`
	Kills   int     `json:"kills"`
	Deaths  int     `json:"deaths"`
	Assists int     `json:"assists"`
	ADR     float64 `json:"adr"`
	HSPct   float64 `json:"hs_pct"`
}

// Write encodes f to w as indented JSON. Nil slices are written as empty
// arrays, never null.
func Write(w io.Writer, f File) error {
	f.Players = nonNil(f.Players)
	f.Rounds = nonNil(f.Rounds)
	f.Kills = nonNil(f.Kills)
	f.Scoreboard = nonNil(f.Scoreboard)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(f)
}

// WriteFile writes f to dir/match.json, creating dir if needed.
func WriteFile(dir string, f File) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := os.Create(filepath.Join(dir, FileName))
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
