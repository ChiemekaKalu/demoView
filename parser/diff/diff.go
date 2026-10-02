// Package diff compares our parse output with a reference, normally the
// oracle's (tools/oracle), under the spec's Validation rules: events match
// exactly on tick, attacker, victim and weapon, and positions within 1 game
// unit. It reads both sides as JSON into its own minimal types, so it does
// not depend on the parser's Go types.
package diff

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Report lists every difference found, in the order checked.
type Report struct {
	Mismatches []Mismatch
}

// Mismatch is one difference. Path names the field, e.g.
// "kills[12].weapon" or "scoreboard[76561198000000000].adr".
type Mismatch struct {
	Path string
	Ours string
	Ref  string
}

// OK reports whether the two sides agree.
func (r Report) OK() bool { return len(r.Mismatches) == 0 }

// maxShown caps String's output; the count is always complete.
const maxShown = 40

// String is a compact human-readable summary: "OK", or the number of
// mismatches followed by one line each.
func (r Report) String() string {
	if r.OK() {
		return "OK"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d mismatch", len(r.Mismatches))
	if len(r.Mismatches) != 1 {
		b.WriteString("es")
	}
	for i, m := range r.Mismatches {
		if i == maxShown {
			fmt.Fprintf(&b, "\n  ... and %d more", len(r.Mismatches)-maxShown)
			break
		}
		fmt.Fprintf(&b, "\n  %s: ours %s, reference %s", m.Path, m.Ours, m.Ref)
	}
	return b.String()
}

func (r *Report) add(path string, ours, ref any) {
	r.Mismatches = append(r.Mismatches, Mismatch{Path: path, Ours: show(ours), Ref: show(ref)})
}

// check adds a mismatch when ours != ref.
func check[T comparable](r *Report, path string, ours, ref T) {
	if ours != ref {
		r.add(path, ours, ref)
	}
}

func show(v any) string {
	switch v := v.(type) {
	case string:
		return fmt.Sprintf("%q", v)
	case nil:
		return "null"
	}
	return fmt.Sprint(v)
}

// matchFile is the part of match.json the diff reads.
type matchFile struct {
	Match struct {
		Map        string `json:"map"`
		TickRate   int    `json:"tick_rate"`
		DemoSHA256 string `json:"demo_sha256"`
	} `json:"match"`
	Players []struct {
		Idx     int    `json:"idx"`
		SteamID string `json:"steam_id"`
		Name    string `json:"name"`
		Team    string `json:"team"`
	} `json:"players"`
	Rounds []struct {
		N             int    `json:"n"`
		StartTick     int    `json:"start_tick"`
		FreezeEndTick int    `json:"freeze_end_tick"`
		EndTick       int    `json:"end_tick"`
		Winner        string `json:"winner"`
		Reason        string `json:"reason"`
		SideA         string `json:"side_a"`
	} `json:"rounds"`
	Kills []struct {
		Round    int    `json:"round"`
		Tick     int    `json:"tick"`
		Attacker *int   `json:"attacker"`
		Victim   int    `json:"victim"`
		Assister *int   `json:"assister"`
		Weapon   string `json:"weapon"`
		Headshot bool   `json:"headshot"`
	} `json:"kills"`
	Scoreboard []struct {
		Player  int     `json:"player"`
		Kills   int     `json:"kills"`
		Deaths  int     `json:"deaths"`
		Assists int     `json:"assists"`
		ADR     float64 `json:"adr"`
		HSPct   float64 `json:"hs_pct"`
	} `json:"scoreboard"`

	keys []string // player key by idx
}

func decodeMatch(data []byte, side string) (*matchFile, error) {
	var m matchFile
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("diff: %s match.json: %w", side, err)
	}
	maxIdx := -1
	for _, p := range m.Players {
		maxIdx = max(maxIdx, p.Idx)
	}
	m.keys = make([]string, maxIdx+1)
	for _, p := range m.Players {
		if p.Idx >= 0 {
			m.keys[p.Idx] = playerKey(p.SteamID, p.Name)
		}
	}
	return &m, nil
}

// playerKey identifies a player across the two files: the Steam id, or the
// name for bots, whose Steam id is "0".
func playerKey(steamID, name string) string {
	if steamID == "" || steamID == "0" {
		return "bot:" + name
	}
	return steamID
}

// key is the player key for idx, "-" for none, or a note when idx is not a
// player in m.
func (m *matchFile) key(idx *int) string {
	switch {
	case idx == nil:
		return "-"
	case *idx < 0 || *idx >= len(m.keys) || m.keys[*idx] == "":
		return fmt.Sprintf("<idx %d>", *idx)
	}
	return m.keys[*idx]
}

// Compare diffs our match.json against a reference match.json.
//
//   - match: the same map, tick rate and demo hash
//   - players: paired by Steam id (bots by name), on the same team
//   - rounds: paired by n, with exact ticks, winner, reason and side_a
//   - kills: the same count and, in order, exact round, tick, attacker,
//     victim and weapon; headshot and assister are compared too
//   - scoreboard: paired by player; kills, deaths, assists and hs_pct exact,
//     adr within 1% (the v0 rule in plan.md; exact once entity decoding lands)
//
// An error means one side is not valid JSON.
func Compare(ours, ref []byte) (Report, error) {
	a, err := decodeMatch(ours, "our")
	if err != nil {
		return Report{}, err
	}
	b, err := decodeMatch(ref, "reference")
	if err != nil {
		return Report{}, err
	}
	var r Report
	check(&r, "match.map", a.Match.Map, b.Match.Map)
	check(&r, "match.tick_rate", a.Match.TickRate, b.Match.TickRate)
	check(&r, "match.demo_sha256", a.Match.DemoSHA256, b.Match.DemoSHA256)
	comparePlayers(&r, a, b)
	compareRounds(&r, a, b)
	compareKills(&r, a, b)
	compareScoreboard(&r, a, b)
	return r, nil
}

func comparePlayers(r *Report, a, b *matchFile) {
	check(r, "players.count", len(a.Players), len(b.Players))
	theirs := map[string]string{} // key -> team
	for _, p := range b.Players {
		theirs[playerKey(p.SteamID, p.Name)] = p.Team
	}
	seen := map[string]bool{}
	for _, p := range a.Players {
		k := playerKey(p.SteamID, p.Name)
		seen[k] = true
		team, ok := theirs[k]
		if !ok {
			r.add(fmt.Sprintf("players[%s]", k), p.Name, "absent")
			continue
		}
		check(r, fmt.Sprintf("players[%s].team", k), p.Team, team)
	}
	for _, p := range b.Players {
		if k := playerKey(p.SteamID, p.Name); !seen[k] {
			r.add(fmt.Sprintf("players[%s]", k), "absent", p.Name)
		}
	}
}

func compareRounds(r *Report, a, b *matchFile) {
	check(r, "rounds.count", len(a.Rounds), len(b.Rounds))
	byN := map[int]int{}
	for i, rd := range b.Rounds {
		byN[rd.N] = i
	}
	seen := map[int]bool{}
	for _, x := range a.Rounds {
		seen[x.N] = true
		i, ok := byN[x.N]
		if !ok {
			r.add(fmt.Sprintf("rounds[n=%d]", x.N), "present", "absent")
			continue
		}
		y := b.Rounds[i]
		p := fmt.Sprintf("rounds[n=%d].", x.N)
		check(r, p+"start_tick", x.StartTick, y.StartTick)
		check(r, p+"freeze_end_tick", x.FreezeEndTick, y.FreezeEndTick)
		check(r, p+"end_tick", x.EndTick, y.EndTick)
		check(r, p+"winner", x.Winner, y.Winner)
		check(r, p+"reason", x.Reason, y.Reason)
		check(r, p+"side_a", x.SideA, y.SideA)
	}
	for _, y := range b.Rounds {
		if !seen[y.N] {
			r.add(fmt.Sprintf("rounds[n=%d]", y.N), "absent", "present")
		}
	}
}

func compareKills(r *Report, a, b *matchFile) {
	check(r, "kills.count", len(a.Kills), len(b.Kills))
	for i := range min(len(a.Kills), len(b.Kills)) {
		x, y := a.Kills[i], b.Kills[i]
		p := fmt.Sprintf("kills[%d].", i)
		check(r, p+"round", x.Round, y.Round)
		check(r, p+"tick", x.Tick, y.Tick)
		check(r, p+"attacker", a.key(x.Attacker), b.key(y.Attacker))
		check(r, p+"victim", a.key(&x.Victim), b.key(&y.Victim))
		check(r, p+"weapon", x.Weapon, y.Weapon)
		check(r, p+"headshot", x.Headshot, y.Headshot)
		check(r, p+"assister", a.key(x.Assister), b.key(y.Assister))
	}
}

// adrClose applies the v0 ADR tolerance: 1% relative, with a 0.05 floor for
// the rounding to one decimal.
func adrClose(ours, ref float64) bool {
	return math.Abs(ours-ref) <= max(0.01*math.Abs(ref), 0.05)
}

func compareScoreboard(r *Report, a, b *matchFile) {
	theirs := map[string]int{}
	for i, row := range b.Scoreboard {
		theirs[b.key(&row.Player)] = i
	}
	seen := map[string]bool{}
	for _, x := range a.Scoreboard {
		k := a.key(&x.Player)
		seen[k] = true
		i, ok := theirs[k]
		if !ok {
			r.add(fmt.Sprintf("scoreboard[%s]", k), "present", "absent")
			continue
		}
		y := b.Scoreboard[i]
		p := fmt.Sprintf("scoreboard[%s].", k)
		check(r, p+"kills", x.Kills, y.Kills)
		check(r, p+"deaths", x.Deaths, y.Deaths)
		check(r, p+"assists", x.Assists, y.Assists)
		check(r, p+"hs_pct", x.HSPct, y.HSPct)
		if !adrClose(x.ADR, y.ADR) {
			r.add(p+"adr", x.ADR, y.ADR)
		}
	}
	for _, y := range b.Scoreboard {
		if k := b.key(&y.Player); !seen[k] {
			r.add(fmt.Sprintf("scoreboard[%s]", k), "absent", "present")
		}
	}
}
