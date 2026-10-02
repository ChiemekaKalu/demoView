package export

// The JSON documents of schema v1 (schema/match.schema.json and
// schema/round.schema.json). They are declared here rather than imported
// from the parser, so the oracle shares no code with what it checks.

// SchemaVersion is the schema_version of every file the oracle writes.
const SchemaVersion = 1

// Match is match.json.
type Match struct {
	SchemaVersion int        `json:"schema_version"`
	ParserVersion string     `json:"parser_version"`
	Match         MatchInfo  `json:"match"`
	Players       []Player   `json:"players"`
	Rounds        []Round    `json:"rounds"`
	Kills         []Kill     `json:"kills"`
	Scoreboard    []ScoreRow `json:"scoreboard"`
}

// MatchInfo describes the match as a whole.
type MatchInfo struct {
	Map        string `json:"map"`
	TickRate   int    `json:"tick_rate"`
	DemoSHA256 string `json:"demo_sha256"`
}

// Player is one participant, referred to everywhere else by Idx.
type Player struct {
	Idx     int    `json:"idx"`
	SteamID string `json:"steam_id"`
	Name    string `json:"name"`
	Team    string `json:"team"` // "A" or "B" for the whole match
}

// Round is one live round.
type Round struct {
	N             int    `json:"n"`
	StartTick     int    `json:"start_tick"`
	FreezeEndTick int    `json:"freeze_end_tick"`
	EndTick       int    `json:"end_tick"`
	Winner        string `json:"winner"` // "A" or "B"
	Reason        string `json:"reason"`
	SideA         string `json:"side_a"` // "CT" or "T"
}

// Kill is one death. Attacker is nil for world, fall and bomb deaths.
type Kill struct {
	Round    int    `json:"round"`
	Tick     int    `json:"tick"`
	Attacker *int   `json:"attacker"`
	Victim   int    `json:"victim"`
	Assister *int   `json:"assister"`
	Weapon   string `json:"weapon"`
	Headshot bool   `json:"headshot"`
}

// ScoreRow is one player's final scoreboard line.
type ScoreRow struct {
	Player  int     `json:"player"`
	Kills   int     `json:"kills"`
	Deaths  int     `json:"deaths"`
	Assists int     `json:"assists"`
	ADR     float64 `json:"adr"`
	HSPct   float64 `json:"hs_pct"`
}

// RoundFile is rounds/NN.json: one round sampled at Hz, as columns.
type RoundFile struct {
	SchemaVersion int       `json:"schema_version"`
	Round         int       `json:"round"`
	Hz            int       `json:"hz"`
	StartTick     int       `json:"start_tick"`
	Frames        int       `json:"frames"`
	Players       []Track   `json:"players"`
	Grenades      []Grenade `json:"grenades"`
	Events        []Event   `json:"events"`
}

// Track is one player's columns, one value per frame.
type Track struct {
	Idx int     `json:"idx"`
	X   []int32 `json:"x"`
	Y   []int32 `json:"y"`
	Yaw []int16 `json:"yaw"` // integer degrees in [0, 360), 0 = +x, counterclockwise
	HP  []int16 `json:"hp"`  // 0 means dead
}

// Grenade is one thrown grenade.
type Grenade struct {
	Type          string   `json:"type"`
	Thrower       int      `json:"thrower"`
	ThrowFrame    int      `json:"throw_frame"`
	DetonateFrame int      `json:"detonate_frame"`
	EndFrame      int      `json:"end_frame"`
	Path          [][2]int `json:"path"` // one point per frame, throw to detonation
}

// Event is something to mark on the timeline. Only kills exist in v1.
type Event struct {
	Frame int    `json:"frame"`
	Type  string `json:"type"`
	Kill  int    `json:"kill"` // index into match.json kills
}
