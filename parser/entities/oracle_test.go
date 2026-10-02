package entities_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
)

// The oracle's export: match.json players, and per round, columns of
// integer x, y, yaw (degrees in [0, 360), counterclockwise from +x) and hp
// per player, one frame per tick.
type (
	oracleMatch struct {
		Players []struct {
			Idx     int    `json:"idx"`
			SteamID string `json:"steam_id"`
			Name    string `json:"name"`
		} `json:"players"`
		Kills []struct {
			Round  int `json:"round"`
			Tick   int `json:"tick"`
			Victim int `json:"victim"`
		} `json:"kills"`
	}
	oracleRound struct {
		Round     int `json:"round"`
		Hz        int `json:"hz"`
		StartTick int `json:"start_tick"`
		Frames    int `json:"frames"`
		Players   []struct {
			Idx int       `json:"idx"`
			X   []int     `json:"x"`
			Y   []int     `json:"y"`
			Yaw []float64 `json:"yaw"`
			HP  []int     `json:"hp"`
		} `json:"players"`
	}
)

// TestOracle decodes every entity of each demo that has an oracle export,
// and compares every player's pawn with the oracle at every round-file
// frame: x and y within 1 unit, yaw within 1 degree, and hp exactly.
// Positions and yaw are compared while the oracle has the player alive,
// since from death on it holds the pose at death.
func TestOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("decodes whole demos")
	}
	dirs, _ := filepath.Glob(filepath.Join("..", "testdata", "reference", "*"))
	if len(dirs) == 0 {
		t.Skip("no oracle exports in testdata/reference")
	}
	for _, dir := range dirs {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "testdata", "demos", name+".dem")
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			compareWithOracle(t, path, dir)
		})
	}
}

// sample is one round-file frame: its tick, and where its oracle values are.
type sample struct {
	tick  int
	round *oracleRound
	frame int
}

func compareWithOracle(t *testing.T, path, dir string) {
	var m oracleMatch
	readJSON(t, filepath.Join(dir, "match.json"), &m)
	files, _ := filepath.Glob(filepath.Join(dir, "rounds", "*.json"))
	var samples []sample
	for _, f := range files {
		r := new(oracleRound)
		readJSON(t, f, r)
		if r.Hz != 64 {
			t.Fatalf("%s: %d Hz, want 64", f, r.Hz)
		}
		for i := range r.Frames {
			samples = append(samples, sample{r.StartTick + i, r, i})
		}
	}
	slices.SortStableFunc(samples, func(a, b sample) int { return a.tick - b.tick })

	steamIDs := map[int]string{}
	names := map[int]string{}
	for _, p := range m.Players {
		steamIDs[p.Idx], names[p.Idx] = p.SteamID, p.Name
	}

	w := &walker{}
	var p *pawnReader
	var cmp comparison
	next := 0
	take := func(upTo int) {
		for ; next < len(samples) && samples[next].tick <= upTo; next++ {
			if p == nil {
				p = newPawnReader(t, w)
			}
			s := samples[next]
			pawns := p.byPlayer(w.state)
			for _, op := range s.round.Players {
				key := steamIDs[op.Idx]
				if key == "0" {
					key = "bot:" + names[op.Idx]
				}
				cmp.check(t, s, op.Idx, pawns[key], op.X[s.frame], op.Y[s.frame], op.Yaw[s.frame], op.HP[s.frame])
			}
		}
	}
	// A frame takes the state after the first demo frame at its tick, or,
	// if no demo frame has its tick, after the last one before it.
	w.before = func(f demo.Frame) { take(int(f.Tick) - 1) }
	w.after = func(f demo.Frame) { take(int(f.Tick)) }
	w.run(t, path)
	if next < len(samples) {
		t.Errorf("%d of %d frames are past the demo's end", len(samples)-next, len(samples))
	}
	t.Logf("%d svc_PacketEntities, all consumed exactly; %d frames of %d rounds", w.packets, len(samples), len(files))
	cmp.report(t)
}

// pawnReader reads players' pawns: controllers by Steam id (or "bot:" and
// the name), each with its m_hPlayerPawn.
type pawnReader struct {
	controller, pawn                       *entities.Class
	steamID, name, team, pawnHandle        entities.Ref
	cellX, cellY, vecX, vecY, eyes, health entities.Ref
}

func newPawnReader(t *testing.T, w *walker) *pawnReader {
	const (
		ctrl   = "CCSPlayerController"
		pawn   = "CCSPlayerPawn"
		origin = "CBodyComponent.m_skeletonInstance.m_vecOrigin."
	)
	return &pawnReader{
		controller: w.classes.ByName(ctrl),
		pawn:       w.classes.ByName(pawn),
		steamID:    w.ref(t, ctrl, "m_steamID"),
		name:       w.ref(t, ctrl, "m_iszPlayerName"),
		team:       w.ref(t, ctrl, "m_iTeamNum"),
		pawnHandle: w.ref(t, ctrl, "m_hPlayerPawn"),
		cellX:      w.ref(t, pawn, origin+"m_cellX"),
		cellY:      w.ref(t, pawn, origin+"m_cellY"),
		vecX:       w.ref(t, pawn, origin+"m_vecX"),
		vecY:       w.ref(t, pawn, origin+"m_vecY"),
		eyes:       w.ref(t, pawn, "m_angEyeAngles"),
		health:     w.ref(t, pawn, "m_iHealth"),
	}
}

// pawnState is what the comparison reads from a pawn.
type pawnState struct {
	x, y, yaw float64
	hp        int
}

// byPlayer returns the pawn of every controller on T or CT.
func (p *pawnReader) byPlayer(s *entities.State) map[string]pawnState {
	out := map[string]pawnState{}
	for e := range s.All() {
		if e.Class != p.controller {
			continue
		}
		if team, _ := e.Get(p.team); team.Uint() != 2 && team.Uint() != 3 {
			continue
		}
		h, _ := e.Get(p.pawnHandle)
		pawn := s.ByHandle(uint32(h.Uint()))
		if pawn == nil || pawn.Class != p.pawn {
			continue
		}
		id, _ := e.Get(p.steamID)
		key := strconv.FormatUint(id.Uint(), 10)
		if id.Uint() == 0 {
			name, _ := e.Get(p.name)
			key = "bot:" + name.Str()
		}
		get := func(r entities.Ref) entities.Value { v, _ := pawn.Get(r); return v }
		out[key] = pawnState{
			x:   entities.WorldCoord(get(p.cellX), get(p.vecX)),
			y:   entities.WorldCoord(get(p.cellY), get(p.vecY)),
			yaw: float64(get(p.eyes).Vector()[1]),
			hp:  int(get(p.health).Int()),
		}
	}
	return out
}

// comparison counts frames within tolerance, per column.
type comparison struct {
	n, bad [4]int // x, y, yaw, hp
	shown  int
}

var columns = [4]string{"x", "y", "yaw", "hp"}

func (c *comparison) check(t *testing.T, s sample, idx int, ours pawnState, x, y int, yaw float64, hp int) {
	var ok [4]bool
	alive := hp > 0
	if alive {
		ok[0] = math.Abs(ours.x-float64(x)) <= 1
		ok[1] = math.Abs(ours.y-float64(y)) <= 1
		d := math.Mod(math.Abs(yawDegrees(ours.yaw)-yaw), 360)
		ok[2] = min(d, 360-d) <= 1
	}
	ok[3] = ours.hp == hp
	for col := range columns {
		if col < 3 && !alive {
			continue
		}
		c.n[col]++
		if !ok[col] {
			c.bad[col]++
			if c.shown < 20 {
				c.shown++
				t.Logf("round %d frame %d (tick %d) player %d: %s: ours x %.2f y %.2f yaw %.2f hp %d; oracle x %d y %d yaw %g hp %d",
					s.round.Round, s.frame, s.tick, idx, columns[col], ours.x, ours.y, ours.yaw, ours.hp, x, y, yaw, hp)
			}
		}
	}
}

func (c *comparison) report(t *testing.T) {
	var line string
	for col, name := range columns {
		rate := 100.0
		if c.n[col] > 0 {
			rate = 100 * float64(c.n[col]-c.bad[col]) / float64(c.n[col])
		}
		line += fmt.Sprintf(" %s %.4f%% (%d of %d off)", name, rate, c.bad[col], c.n[col])
		if c.bad[col] > 0 {
			t.Errorf("%s: %d of %d frames out of tolerance", name, c.bad[col], c.n[col])
		}
	}
	t.Logf("within tolerance:%s", line)
}

// yawDegrees maps a yaw in degrees to [0, 360), as the oracle's integer
// yaw is before rounding.
func yawDegrees(yaw float64) float64 {
	return math.Mod(math.Mod(yaw, 360)+360, 360)
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
