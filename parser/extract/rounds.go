package extract

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/roundfile"
)

// Hz is the frame rate of round files: one frame per tick at the usual 64
// ticks a second.
const Hz = 64

// Rounds slices the recording into the round files of m, a built
// match.json. spawned lists each round's players by idx (match logic's
// Result.Spawned), and lastTick is the demo's last tick.
//
// Round n's file starts at its freeze_end_tick and runs to the next
// round's start_tick, or for the last round through lastTick. Its
// grenades are those thrown in that span, with their frames clamped to it,
// and its events are the kills match.json files under it, at
// ceil((tick - start_tick) / ticks per frame), clamped too.
func (r *Recorder) Rounds(m *match.File, spawned [][]int, lastTick int32) []roundfile.File {
	hz, step := Hz, 1
	switch rate := m.Match.TickRate; {
	case rate > 0 && rate%Hz == 0:
		step = rate / Hz
	case rate > 0:
		hz = rate
	}
	byIdx := make([]*track, len(m.Players))
	idxOf := map[*track]int{}
	for _, p := range m.Players {
		id, _ := strconv.ParseUint(p.SteamID, 10, 64)
		if t := r.byKey[playerKey(id, p.Name)]; t != nil && p.Idx >= 0 && p.Idx < len(byIdx) {
			byIdx[p.Idx], idxOf[t] = t, p.Idx
		}
	}
	throws := slices.Clone(r.list)
	slices.SortStableFunc(throws, func(a, b *grenade) int { return cmp.Compare(a.throw, b.throw) })

	files := make([]roundfile.File, len(m.Rounds))
	for i, rd := range m.Rounds {
		end := lastTick + 1
		if i+1 < len(m.Rounds) {
			end = int32(m.Rounds[i+1].StartTick)
		}
		s := span{start: int32(rd.FreezeEndTick), step: int32(step)}
		s.frames = max(1, ceilDiv(end-s.start, s.step))
		f := roundfile.File{
			SchemaVersion: match.SchemaVersion,
			Round:         rd.N,
			Hz:            hz,
			StartTick:     rd.FreezeEndTick,
			Frames:        int(s.frames),
		}
		if i < len(spawned) {
			for _, idx := range spawned[i] {
				if idx >= 0 && idx < len(byIdx) {
					f.Players = append(f.Players, r.columns(byIdx[idx], idx, s))
				}
			}
		}
		for _, g := range throws {
			idx, ok := idxOf[g.thrower]
			if ok && g.throw >= s.start && g.throw < end {
				f.Grenades = append(f.Grenades, g.inRound(idx, s))
			}
		}
		for k, kill := range m.Kills {
			if kill.Round == rd.N {
				f.Events = append(f.Events, roundfile.Event{Frame: int(s.frame(int32(kill.Tick))), Type: roundfile.EventKill, Kill: k})
			}
		}
		files[i] = f
	}
	return files
}

// span is the frames of one round file.
type span struct {
	start  int32 // tick of frame 0
	step   int32 // ticks per frame
	frames int32
}

func (s span) tick(frame int32) int32 { return s.start + frame*s.step }

// frame returns the first frame at or after tick, clamped to the span.
func (s span) frame(tick int32) int32 {
	return max(0, min(s.frames-1, ceilDiv(tick-s.start, s.step)))
}

// columns returns the round file columns of player idx, whose track is t.
// Frames outside the track are dead, in the track's nearest pose, and with
// no track at all, dead at 0, 0.
func (r *Recorder) columns(t *track, idx int, s span) roundfile.Player {
	p := roundfile.Player{
		Idx: idx,
		X:   make([]int16, s.frames),
		Y:   make([]int16, s.frames),
		Yaw: make([]int16, s.frames),
		HP:  make([]int16, s.frames),
	}
	if t == nil || len(t.hp) == 0 {
		return p
	}
	for i := range s.frames {
		sample := int(s.tick(i) - r.base)
		pose := t.at(max(t.first, min(t.first+len(t.hp)-1, sample)))
		if sample < t.first || sample >= t.first+len(t.hp) {
			pose.hp = 0
		}
		p.X[i], p.Y[i], p.Yaw[i], p.HP[i] = pose.x, pose.y, pose.yaw, int16(pose.hp)
	}
	return p
}

// inRound returns g as thrown by player idx in a round file over s.
func (g *grenade) inRound(idx int, s span) roundfile.Grenade {
	throw := s.frame(g.throw)
	det := s.frames - 1
	if g.detonate >= 0 {
		det = s.frame(g.detonate)
	}
	end := s.frames - 1
	if t := g.effectEnd(); t >= 0 {
		end = max(det, s.frame(t))
	}
	path := make([][2]int16, det-throw+1)
	for i := range path {
		j := int(s.tick(throw+int32(i)) - g.throw)
		path[i] = g.path[max(0, min(len(g.path)-1, j))]
	}
	return roundfile.Grenade{
		Type:          g.typ,
		Thrower:       idx,
		ThrowFrame:    int(throw),
		DetonateFrame: int(det),
		EndFrame:      int(end),
		Path:          path,
	}
}

// ceilDiv returns a/b rounded up, for b > 0.
func ceilDiv(a, b int32) int32 {
	q := a / b
	if a%b > 0 {
		q++
	}
	return q
}
