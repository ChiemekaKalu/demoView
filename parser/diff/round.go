package diff

import (
	"encoding/json"
	"fmt"
	"math"
)

// roundFile is the part of rounds/NN.json the diff reads. Columns decode
// as float64 so an implementation that writes fractional values still
// compares.
type roundFile struct {
	Round     int `json:"round"`
	Hz        int `json:"hz"`
	StartTick int `json:"start_tick"`
	Frames    int `json:"frames"`
	Players   []struct {
		Idx int       `json:"idx"`
		X   []float64 `json:"x"`
		Y   []float64 `json:"y"`
		Yaw []float64 `json:"yaw"`
		HP  []float64 `json:"hp"`
	} `json:"players"`
	Grenades []struct {
		Type          string       `json:"type"`
		Thrower       int          `json:"thrower"`
		ThrowFrame    int          `json:"throw_frame"`
		DetonateFrame int          `json:"detonate_frame"`
		EndFrame      int          `json:"end_frame"`
		Path          [][2]float64 `json:"path"`
	} `json:"grenades"`
	Events []struct {
		Frame int    `json:"frame"`
		Type  string `json:"type"`
		Kill  *int   `json:"kill"`
	} `json:"events"`
}

// PositionTolerance is how far apart two positions may be, in game units,
// and still match (the spec's Validation rules).
const PositionTolerance = 1

// YawTolerance is how far apart two view directions may be, in degrees.
const YawTolerance = 1

// CompareRound diffs one of our round files against the reference's for the
// same round. ourMatch and refMatch are the two match.json files, which pair
// the players by Steam id, since idx may differ between the two sides.
//
//   - header: the same round, hz, start_tick and frame count
//   - players: the same set; per frame, x and y within PositionTolerance,
//     yaw within YawTolerance (around the circle), hp exact. Each column
//     reports its first bad frame and how many frames are off.
//   - grenades: the same count and, in order, type, thrower, the three
//     frames, and every path point within PositionTolerance
//   - events: the same count and, in order, frame, type and kill index
func CompareRound(ourMatch, refMatch, ours, ref []byte) (Report, error) {
	am, err := decodeMatch(ourMatch, "our")
	if err != nil {
		return Report{}, err
	}
	bm, err := decodeMatch(refMatch, "reference")
	if err != nil {
		return Report{}, err
	}
	var a, b roundFile
	if err := json.Unmarshal(ours, &a); err != nil {
		return Report{}, fmt.Errorf("diff: our round file: %w", err)
	}
	if err := json.Unmarshal(ref, &b); err != nil {
		return Report{}, fmt.Errorf("diff: reference round file: %w", err)
	}

	var r Report
	p := fmt.Sprintf("round[%d].", b.Round)
	check(&r, p+"round", a.Round, b.Round)
	check(&r, p+"hz", a.Hz, b.Hz)
	check(&r, p+"start_tick", a.StartTick, b.StartTick)
	check(&r, p+"frames", a.Frames, b.Frames)

	// Players, paired by key.
	theirs := map[string]int{}
	for i, t := range b.Players {
		theirs[bm.key(&t.Idx)] = i
	}
	seen := map[string]bool{}
	for _, x := range a.Players {
		k := am.key(&x.Idx)
		seen[k] = true
		i, ok := theirs[k]
		if !ok {
			r.add(fmt.Sprintf("%splayers[%s]", p, k), "present", "absent")
			continue
		}
		y := b.Players[i]
		pp := fmt.Sprintf("%splayers[%s].", p, k)
		compareColumns(&r, pp+"xy", x.X, x.Y, y.X, y.Y)
		compareColumn(&r, pp+"yaw", x.Yaw, y.Yaw, func(u, v float64) bool { return yawDelta(u, v) <= YawTolerance })
		compareColumn(&r, pp+"hp", x.HP, y.HP, func(u, v float64) bool { return u == v })
	}
	for _, y := range b.Players {
		if k := bm.key(&y.Idx); !seen[k] {
			r.add(fmt.Sprintf("%splayers[%s]", p, k), "absent", "present")
		}
	}

	// Grenades, in order.
	check(&r, p+"grenades.count", len(a.Grenades), len(b.Grenades))
	for i := range min(len(a.Grenades), len(b.Grenades)) {
		x, y := a.Grenades[i], b.Grenades[i]
		gp := fmt.Sprintf("%sgrenades[%d].", p, i)
		check(&r, gp+"type", x.Type, y.Type)
		check(&r, gp+"thrower", am.key(&x.Thrower), bm.key(&y.Thrower))
		check(&r, gp+"throw_frame", x.ThrowFrame, y.ThrowFrame)
		check(&r, gp+"detonate_frame", x.DetonateFrame, y.DetonateFrame)
		check(&r, gp+"end_frame", x.EndFrame, y.EndFrame)
		var xs, ys, xr, yr []float64
		for _, pt := range x.Path {
			xs, ys = append(xs, pt[0]), append(ys, pt[1])
		}
		for _, pt := range y.Path {
			xr, yr = append(xr, pt[0]), append(yr, pt[1])
		}
		compareColumns(&r, gp+"path", xs, ys, xr, yr)
	}

	// Events, in order.
	check(&r, p+"events.count", len(a.Events), len(b.Events))
	for i := range min(len(a.Events), len(b.Events)) {
		x, y := a.Events[i], b.Events[i]
		ep := fmt.Sprintf("%sevents[%d].", p, i)
		check(&r, ep+"frame", x.Frame, y.Frame)
		check(&r, ep+"type", x.Type, y.Type)
		check(&r, ep+"kill", intOrNull(x.Kill), intOrNull(y.Kill))
	}
	return r, nil
}

// compareColumns checks two x/y columns point by point within
// PositionTolerance, reporting the first bad point and how many are off.
func compareColumns(r *Report, path string, ax, ay, bx, by []float64) {
	n := min(len(ax), len(ay))
	m := min(len(bx), len(by))
	if n != m {
		r.add(path+".length", n, m)
	}
	first, bad := -1, 0
	for i := range min(n, m) {
		if math.Abs(ax[i]-bx[i]) > PositionTolerance || math.Abs(ay[i]-by[i]) > PositionTolerance {
			if first < 0 {
				first = i
			}
			bad++
		}
	}
	if bad > 0 {
		r.add(fmt.Sprintf("%s[%d]", path, first),
			fmt.Sprintf("(%v, %v), %d of %d off by more than %d unit", ax[first], ay[first], bad, min(n, m), PositionTolerance),
			fmt.Sprintf("(%v, %v)", bx[first], by[first]))
	}
}

// compareColumn checks one column value by value with same, reporting the
// first bad value and how many are off.
func compareColumn(r *Report, path string, a, b []float64, same func(ours, ref float64) bool) {
	if len(a) != len(b) {
		r.add(path+".length", len(a), len(b))
	}
	first, bad := -1, 0
	for i := range min(len(a), len(b)) {
		if !same(a[i], b[i]) {
			if first < 0 {
				first = i
			}
			bad++
		}
	}
	if bad > 0 {
		r.add(fmt.Sprintf("%s[%d]", path, first), fmt.Sprintf("%v, %d of %d off", a[first], bad, min(len(a), len(b))), b[first])
	}
}

// yawDelta is the angle between two yaws in degrees, the short way round.
func yawDelta(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	return min(d, 360-d)
}

// intOrNull shows an optional index: its value, or "null".
func intOrNull(p *int) string {
	if p == nil {
		return "null"
	}
	return fmt.Sprint(*p)
}
