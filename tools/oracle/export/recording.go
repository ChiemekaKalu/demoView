package export

import (
	"fmt"
	"math"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// recording samples one round's window: from its freeze end until the next
// round starts (or the demo ends), one frame every step ticks.
type recording struct {
	round *roundRec
	start int // tick of frame 0
	step  int // ticks per frame
	next  int // tick of the next frame to fill

	frames []*snapshot // frame i is the state at tick start + i*step
	prev   *snapshot   // state at the last FrameDone

	nades []*grenadeRec
	byUID map[int64]*grenadeRec
}

// snapshot is the state at one tick.
type snapshot struct {
	players map[string]playerSample // by player key
	nades   map[int64][2]int        // projectile position by unique id
}

type playerSample struct {
	x, y, yaw, hp int
	side          common.Team
	alive         bool
}

type grenadeRec struct {
	uid       int64
	entityID  int
	typ       string // schema type
	thrower   string // player key
	throwTick int
	throwPos  [2]int

	detTick, destroyTick, endTick int // -1 until seen
	detPos, destroyPos            [2]int
	infernoUID                    int64 // the fire a molotov started, or 0
}

func (x *exporter) startRecording(r *roundRec) {
	x.closeRecording(x.tick())
	tickRate := int(math.Round(x.p.TickRate()))
	if tickRate <= 0 || tickRate%x.hz != 0 {
		if x.err == nil {
			x.err = fmt.Errorf("tick rate %d is not a multiple of %d Hz", tickRate, x.hz)
		}
		return
	}
	t := x.tick()
	x.rec = &recording{round: r, start: t, step: tickRate / x.hz, next: t, byUID: map[int64]*grenadeRec{}}
	r.rec = x.rec
}

// closeRecording ends the open window at tick end, exclusive. Frames still
// owed before end get the last state seen, which is the state at their tick:
// nothing has changed since.
func (x *exporter) closeRecording(end int) {
	r := x.rec
	if r == nil {
		return
	}
	for r.prev != nil && r.next < end {
		r.frames = append(r.frames, r.prev)
		r.next += r.step
	}
	x.rec = nil
}

// sample fills every frame whose tick has been reached. A frame takes the
// state at the latest tick at or before its own, so a frame between two
// demo ticks takes the earlier one.
func (r *recording) sample(x *exporter, t int) {
	cur := x.snapshot(r)
	for r.next <= t {
		s := cur
		if r.next < t && r.prev != nil {
			s = r.prev
		}
		r.frames = append(r.frames, s)
		r.next += r.step
	}
	r.prev = cur
}

func (x *exporter) snapshot(r *recording) *snapshot {
	s := &snapshot{players: make(map[string]playerSample, 10)}
	for _, p := range x.gs.Participants().Playing() {
		if p.PlayerPawnEntity() == nil {
			continue
		}
		pos := p.Position()
		s.players[x.seePlayer(p)] = playerSample{
			x:     roundInt(pos.X),
			y:     roundInt(pos.Y),
			yaw:   yawDegrees(p.ViewDirectionX()),
			hp:    p.Health(),
			side:  p.Team,
			alive: p.IsAlive(),
		}
	}
	if len(r.byUID) > 0 {
		s.nades = map[int64][2]int{}
		for _, g := range x.gs.GrenadeProjectiles() {
			if _, ok := r.byUID[g.UniqueID()]; ok {
				pos := g.Position()
				s.nades[g.UniqueID()] = [2]int{roundInt(pos.X), roundInt(pos.Y)}
			}
		}
	}
	return s
}

// grenadeTypes maps demoinfocs equipment to the schema's grenade types.
var grenadeTypes = map[common.EquipmentType]string{
	common.EqSmoke:      "smoke",
	common.EqFlash:      "flash",
	common.EqHE:         "he",
	common.EqMolotov:    "molotov",
	common.EqIncendiary: "incendiary",
	common.EqDecoy:      "decoy",
}

func (x *exporter) registerGrenades() {
	x.p.RegisterEventHandler(func(e events.GrenadeProjectileThrow) {
		g := e.Projectile
		if x.rec == nil || g == nil || g.Thrower == nil || g.Entity == nil {
			return
		}
		typ := ""
		if g.WeaponInstance != nil {
			typ = grenadeTypes[g.WeaponInstance.Type]
		}
		if typ == "" {
			x.stats.UnknownGrenades++
			return
		}
		pos := g.Position()
		gr := &grenadeRec{
			uid:       g.UniqueID(),
			entityID:  g.Entity.ID(),
			typ:       typ,
			thrower:   x.seePlayer(g.Thrower),
			throwTick: x.tick(),
			throwPos:  [2]int{roundInt(pos.X), roundInt(pos.Y)},
			detTick:   -1, destroyTick: -1, endTick: -1,
		}
		x.rec.nades = append(x.rec.nades, gr)
		x.rec.byUID[gr.uid] = gr
	})
	x.p.RegisterEventHandler(func(e events.GrenadeProjectileDestroy) {
		if x.rec == nil || e.Projectile == nil {
			return
		}
		if gr := x.rec.byUID[e.Projectile.UniqueID()]; gr != nil && gr.destroyTick < 0 {
			pos := e.Projectile.Position()
			gr.destroyTick = x.tick()
			gr.destroyPos = [2]int{roundInt(pos.X), roundInt(pos.Y)}
		}
	})

	// Detonation, matched to the projectile by entity id.
	detonate := func(e events.GrenadeEvent, typ string) {
		if gr := x.findGrenade(e.GrenadeEntityID, typ, func(g *grenadeRec) bool { return g.detTick < 0 }); gr != nil {
			gr.detTick = x.tick()
			gr.detPos = [2]int{roundInt(e.Position.X), roundInt(e.Position.Y)}
		}
	}
	expire := func(e events.GrenadeEvent, typ string) {
		if gr := x.findGrenade(e.GrenadeEntityID, typ, func(g *grenadeRec) bool { return g.endTick < 0 }); gr != nil {
			gr.endTick = x.tick()
		}
	}
	x.p.RegisterEventHandler(func(e events.FlashExplode) { detonate(e.GrenadeEvent, "flash") })
	x.p.RegisterEventHandler(func(e events.HeExplode) { detonate(e.GrenadeEvent, "he") })
	x.p.RegisterEventHandler(func(e events.SmokeStart) { detonate(e.GrenadeEvent, "smoke") })
	x.p.RegisterEventHandler(func(e events.SmokeExpired) { expire(e.GrenadeEvent, "smoke") })
	x.p.RegisterEventHandler(func(e events.DecoyStart) { detonate(e.GrenadeEvent, "decoy") })
	x.p.RegisterEventHandler(func(e events.DecoyExpired) { expire(e.GrenadeEvent, "decoy") })

	// A molotov's fire is a separate entity. Match it to the thrower's
	// molotov that landed (or is landing) at the same moment.
	x.p.RegisterEventHandler(func(e events.InfernoStart) {
		if x.rec == nil || e.Inferno == nil || e.Inferno.Thrower() == nil {
			return
		}
		thrower, t := playerKey(e.Inferno.Thrower()), x.tick()
		var best *grenadeRec
		for _, g := range x.rec.nades {
			if (g.typ == "molotov" || g.typ == "incendiary") && g.thrower == thrower && g.infernoUID == 0 &&
				g.throwTick <= t && (g.destroyTick < 0 || t-g.destroyTick <= 16) {
				best = g // the most recent throw wins
			}
		}
		if best != nil {
			best.infernoUID = e.Inferno.UniqueID()
			if best.detTick < 0 {
				best.detTick = t
			}
		}
	})
	x.p.RegisterEventHandler(func(e events.InfernoExpired) {
		if x.rec == nil || e.Inferno == nil {
			return
		}
		for _, g := range x.rec.nades {
			if g.infernoUID == e.Inferno.UniqueID() && g.endTick < 0 {
				g.endTick = x.tick()
			}
		}
	})
}

// findGrenade returns the most recent grenade in the open window with the
// entity id and type that satisfies ok.
func (x *exporter) findGrenade(entityID int, typ string, ok func(*grenadeRec) bool) *grenadeRec {
	if x.rec == nil {
		return nil
	}
	for i := len(x.rec.nades) - 1; i >= 0; i-- {
		g := x.rec.nades[i]
		if g.entityID == entityID && g.typ == typ && ok(g) {
			return g
		}
	}
	return nil
}
