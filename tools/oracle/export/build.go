package export

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// reasons maps the game's round end reasons to the schema's.
var reasons = map[events.RoundEndReason]string{
	events.RoundEndReasonTargetBombed:        "bomb_exploded",
	events.RoundEndReasonBombDefused:         "bomb_defused",
	events.RoundEndReasonCTWin:               "elimination",
	events.RoundEndReasonTerroristsWin:       "elimination",
	events.RoundEndReasonTargetSaved:         "time_expired",
	events.RoundEndReasonTerroristsSurrender: "surrender",
	events.RoundEndReasonCTSurrender:         "surrender",
}

func sideName(t common.Team) string {
	if t == common.TeamCounterTerrorists {
		return "CT"
	}
	return "T"
}

// build assembles match.json and the round files from what was collected.
func (x *exporter) build(sha string, tickRate int) (*Result, error) {
	sides, err := x.teamASides()
	if err != nil {
		return nil, err
	}
	letters := x.teamLetters(sides)

	// Players: team A first, then by Steam id, so idx is stable.
	var keys []string
	for k := range letters {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		pa, pb := x.players[a], x.players[b]
		if letters[a] != letters[b] {
			if letters[a] == "A" {
				return -1
			}
			return 1
		}
		if pa.steamID != pb.steamID {
			if pa.steamID < pb.steamID {
				return -1
			}
			return 1
		}
		if pa.name < pb.name {
			return -1
		}
		if pa.name > pb.name {
			return 1
		}
		return 0
	})
	idx := map[string]int{}
	m := Match{
		SchemaVersion: SchemaVersion,
		ParserVersion: Version,
		Match:         MatchInfo{Map: x.mapName, TickRate: tickRate, DemoSHA256: sha},
		Players:       []Player{},
		Rounds:        []Round{},
		Kills:         []Kill{},
		Scoreboard:    []ScoreRow{},
	}
	for i, k := range keys {
		idx[k] = i
		p := x.players[k]
		m.Players = append(m.Players, Player{Idx: i, SteamID: strconv.FormatUint(p.steamID, 10), Name: p.name, Team: letters[k]})
	}

	for i, r := range x.rounds {
		reason, ok := reasons[r.reason]
		if !ok {
			return nil, fmt.Errorf("round %d ended for reason %d, which the schema has no name for", r.n, r.reason)
		}
		if r.winner != common.TeamTerrorists && r.winner != common.TeamCounterTerrorists {
			return nil, fmt.Errorf("round %d has no winner", r.n)
		}
		winner := "B"
		if r.winner == sides[i] {
			winner = "A"
		}
		m.Rounds = append(m.Rounds, Round{
			N: r.n, StartTick: r.start, FreezeEndTick: r.freezeEnd, EndTick: r.end,
			Winner: winner, Reason: reason, SideA: sideName(sides[i]),
		})
	}

	// Kills, sorted by tick. Kills between a round's end and the next
	// round's start already belong to the round that ended.
	slices.SortStableFunc(x.kills, func(a, b *killRec) int { return a.tick - b.tick })
	killIndex := map[*killRec]int{}
	for i, k := range x.kills {
		killIndex[k] = i
		out := Kill{Round: k.round.n, Tick: k.tick, Victim: idx[k.victim], Weapon: k.weapon, Headshot: k.headshot}
		if out.Weapon == "" {
			out.Weapon = "unknown"
			x.stats.UnknownWeapons++
		}
		if k.attacker != "" {
			a := idx[k.attacker]
			out.Attacker = &a
		}
		// An assist counts only against the enemy; flash assists included.
		if k.assister != "" && letters[k.assister] != letters[k.victim] {
			a := idx[k.assister]
			out.Assister = &a
		}
		m.Kills = append(m.Kills, out)
	}
	m.Scoreboard = x.scoreboard(m, len(keys), letters, keys)

	res := &Result{Match: m}
	for _, r := range x.rounds {
		res.Rounds = append(res.Rounds, x.roundFile(r, idx, killIndex))
		// Its samples are done with, and they outweigh the round file.
		if r.rec != nil {
			r.rec.frames, r.rec.prev = nil, nil
		}
	}
	res.Stats = x.stats
	return res, nil
}

// teamASides returns the side team A played in each round. Team A is whoever
// was on CT at the start of round 1. In later rounds A's side is the side
// most of A's players are on, since sides swap at half and in overtime.
func (x *exporter) teamASides() ([]common.Team, error) {
	first := x.rounds[0].rec
	if first == nil || len(first.frames) == 0 {
		return nil, fmt.Errorf("round 1 has no samples to tell the teams apart")
	}
	teamA := map[string]bool{}
	for _, s := range first.frames[0].players {
		if s.side == common.TeamCounterTerrorists {
			teamA[s.key] = true
		}
	}
	sides := make([]common.Team, len(x.rounds))
	sides[0] = common.TeamCounterTerrorists
	for i := 1; i < len(x.rounds); i++ {
		sides[i] = sides[i-1]
		rec := x.rounds[i].rec
		if rec == nil || len(rec.frames) == 0 {
			continue
		}
		ct, t := 0, 0
		for _, s := range rec.frames[0].players {
			if !teamA[s.key] {
				continue
			}
			switch s.side {
			case common.TeamCounterTerrorists:
				ct++
			case common.TeamTerrorists:
				t++
			}
		}
		if ct > t {
			sides[i] = common.TeamCounterTerrorists
		} else if t > ct {
			sides[i] = common.TeamTerrorists
		}
	}
	return sides, nil
}

// teamLetters assigns A or B to everyone who played a live round: A if, in
// the first round they appear alive, they are on team A's side.
func (x *exporter) teamLetters(sides []common.Team) map[string]string {
	letters := map[string]string{}
	assign := func(key string, side common.Team, round int) {
		if key == "" || letters[key] != "" {
			return
		}
		if side != common.TeamTerrorists && side != common.TeamCounterTerrorists {
			return
		}
		if side == sides[round] {
			letters[key] = "A"
		} else {
			letters[key] = "B"
		}
	}
	for i, r := range x.rounds {
		if r.rec == nil {
			continue
		}
		for _, f := range r.rec.frames {
			for _, s := range f.players {
				if s.alive {
					assign(s.key, s.side, i)
				}
			}
		}
		// Players in this round's kills who were never sampled alive.
		for _, k := range x.kills {
			if k.round == r {
				assign(k.victim, k.victimSide, i)
				assign(k.attacker, k.attackerSide, i)
			}
		}
	}
	return letters
}

// scoreboard applies the plan's scoreboard rules: enemy kills only, every
// death, enemy assists including flash assists, headshot percentage of
// kills on a 0-100 scale, and ADR from the game's damage counter over the
// rounds played.
func (x *exporter) scoreboard(m Match, n int, letters map[string]string, keys []string) []ScoreRow {
	rows := make([]ScoreRow, n)
	hs := make([]int, n)
	for i := range rows {
		rows[i].Player = i
	}
	for _, k := range m.Kills {
		rows[k.Victim].Deaths++
		if k.Assister != nil {
			rows[*k.Assister].Assists++
		}
		if k.Attacker == nil || *k.Attacker == k.Victim || m.Players[*k.Attacker].Team == m.Players[k.Victim].Team {
			continue
		}
		rows[*k.Attacker].Kills++
		if k.Headshot {
			hs[*k.Attacker]++
		}
	}
	for i := range rows {
		if rows[i].Kills > 0 {
			rows[i].HSPct = round1(100 * float64(hs[i]) / float64(rows[i].Kills))
		}
		rows[i].ADR = round1(float64(x.damage[keys[i]]) / float64(len(m.Rounds)))
	}
	return rows
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// roundFile turns one round's samples into rounds/NN.json.
func (x *exporter) roundFile(r *roundRec, idx map[string]int, killIndex map[*killRec]int) RoundFile {
	rec := r.rec
	if rec == nil { // a round that never got a window; give it one empty frame
		rec = &recording{round: r, start: r.freezeEnd, step: 1}
	}
	s, step := rec.start, rec.step
	frameOf := func(tick int) int { return ceilDiv(tick-s, step) }

	// This round's kills and the frame each lands on.
	type roundKill struct {
		k      *killRec
		frame  int
		before bool // before the window opened, so at frame 0
	}
	var kills []roundKill
	frames := len(rec.frames)
	for _, k := range x.kills {
		if k.round != r {
			continue
		}
		f := frameOf(k.tick)
		if f < 0 {
			x.stats.KillFramesBefore++
			f = 0
		}
		kills = append(kills, roundKill{k, f, k.tick < s})
		frames = max(frames, f+1)
	}
	frames = max(frames, 1)
	snaps := rec.frames
	for len(snaps) < frames { // hold the last state for frames past the window
		last := &snapshot{}
		if len(snaps) > 0 {
			last = snaps[len(snaps)-1]
		}
		snaps = append(snaps, last)
	}

	rf := RoundFile{
		SchemaVersion: SchemaVersion, Round: r.n, Hz: x.hz, StartTick: s, Frames: frames,
		Players: []Track{}, Grenades: []Grenade{}, Events: []Event{},
	}

	aliveIn := func(key string) bool {
		for _, f := range snaps {
			if p, ok := f.player(key); ok && p.alive {
				return true
			}
		}
		return false
	}

	// Each victim's first death in the window. A death before it, such as a
	// kick before round 1, only holds for a player who never shows up alive
	// in the window: one who does came back.
	deaths := map[string]roundKill{}
	for _, rk := range kills {
		if _, seen := deaths[rk.k.victim]; !seen && !rk.before {
			deaths[rk.k.victim] = rk
		}
	}
	for _, rk := range kills {
		if _, seen := deaths[rk.k.victim]; !seen && rk.before && !aliveIn(rk.k.victim) {
			deaths[rk.k.victim] = rk
		}
	}

	// Tracks for everyone who was alive in this round or died in it.
	var tracked []string
	for k := range idx {
		if _, died := deaths[k]; died || aliveIn(k) {
			tracked = append(tracked, k)
		}
	}
	slices.SortFunc(tracked, func(a, b string) int { return idx[a] - idx[b] })
	for _, k := range tracked {
		rf.Players = append(rf.Players, track(k, idx[k], snaps, deaths[k].k, deaths[k].frame))
	}

	// Grenades.
	last := frames - 1
	clamp := func(f, lo int) int { return min(max(f, lo), last) }
	for _, g := range rec.nades {
		thrower, ok := idx[g.thrower]
		if !ok {
			continue
		}
		throwF := clamp(frameOf(g.throwTick), 0)
		det := g.detTick
		if det < 0 {
			det = g.destroyTick
		}
		detF := last
		if det >= 0 {
			detF = clamp(frameOf(det), throwF)
		}
		endF := detF
		fire := g.typ == "molotov" || g.typ == "incendiary"
		switch g.typ {
		case "smoke", "decoy", "molotov", "incendiary":
			switch {
			case g.endTick >= 0:
				endF = clamp(frameOf(g.endTick), detF)
			case fire && g.fire == 0:
				x.stats.MolotovsWithoutFire++ // no fire, e.g. landed in a smoke
			case fire:
				endF = last // still burning when the window closed
			case g.destroyTick >= 0:
				// Removed with no end seen, such as a decoy that never went off.
				endF = clamp(frameOf(g.destroyTick), detF)
			default:
				endF = last // still active when the window closed
			}
		}
		rf.Grenades = append(rf.Grenades, Grenade{
			Type: g.typ, Thrower: thrower, ThrowFrame: throwF, DetonateFrame: detF, EndFrame: endF,
			Path: grenadePath(g, snaps, throwF, detF, s, step),
		})
	}

	// Kill events, in frame order.
	slices.SortStableFunc(kills, func(a, b roundKill) int { return a.frame - b.frame })
	for _, rk := range kills {
		rf.Events = append(rf.Events, Event{Frame: rk.frame, Type: "kill", Kill: killIndex[rk.k]})
	}
	return rf
}

// track builds one player's columns. Frames where the player is missing hold
// their nearest known state. From their death frame on they hold the spot
// and facing they died with, at hp 0; a player who drops to hp 0 any other
// way (a disconnect) holds from there too.
func track(key string, idx int, snaps []*snapshot, death *killRec, deathFrame int) Track {
	n := len(snaps)
	t := Track{Idx: idx, X: make([]int32, n), Y: make([]int32, n), Yaw: make([]int16, n), HP: make([]int16, n)}
	var dead playerSample // where and how the player died
	if death != nil {
		dead = playerSample{x: int32(death.deathX), y: int32(death.deathY), yaw: int16(death.deathYaw)}
	}

	first := -1
	for f, s := range snaps {
		if _, ok := s.player(key); ok {
			first = f
			break
		}
	}
	var cur playerSample
	if first >= 0 {
		cur, _ = snaps[first].player(key)
	} else if death != nil {
		cur = dead
	}
	for f, s := range snaps {
		hp := cur.hp
		if p, ok := s.player(key); ok {
			cur, hp = p, p.hp
		} else if f > first {
			hp = 0 // gone: left the server
		}
		if !cur.alive {
			hp = 0
		}
		t.X[f], t.Y[f], t.Yaw[f], t.HP[f] = cur.x, cur.y, cur.yaw, hp
	}

	if death != nil {
		for f := deathFrame; f < n; f++ {
			t.X[f], t.Y[f], t.Yaw[f], t.HP[f] = dead.x, dead.y, dead.yaw, 0
		}
		// Alive until the kill's frame, by definition of that frame.
		for f := 0; f < deathFrame; f++ {
			t.HP[f] = max(t.HP[f], 1)
		}
	}
	// Once dead, stay dead and still. Before a kill's frame hp is at least 1,
	// so this only ever holds from the death frame on.
	for f := 1; f < n; f++ {
		if t.HP[f-1] == 0 {
			t.X[f], t.Y[f], t.Yaw[f], t.HP[f] = t.X[f-1], t.Y[f-1], t.Yaw[f-1], 0
		}
	}
	return t
}

// grenadePath has one point per frame from throw to detonation: the
// projectile's position where it was sampled, its detonation point once it
// has gone off, and otherwise the last known point.
func grenadePath(g *grenadeRec, snaps []*snapshot, throwF, detF, start, step int) [][2]int {
	path := make([][2]int, 0, detF-throwF+1)
	last := g.throwPos
	for f := throwF; f <= detF; f++ {
		tick := start + f*step
		switch pos, ok := snaps[f].nade(g.uid); {
		case ok:
			last = pos
		case g.detTick >= 0 && tick >= g.detTick:
			last = g.detPos
		case g.destroyTick >= 0 && tick >= g.destroyTick:
			last = g.destroyPos
		}
		path = append(path, last)
	}
	return path
}
