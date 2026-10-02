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

// snapshot is the state at one tick. A match holds one per frame until its
// round files are built, so it stays small: slices rather than maps, and
// player keys shared with the player records.
type snapshot struct {
	players []keyedSample
	nades   []nadeSample // projectiles in flight
}

type keyedSample struct {
	key string
	playerSample
}

type playerSample struct {
	x, y    int32
	yaw, hp int16
	side    common.Team
	alive   bool
}

type nadeSample struct {
	uid int64
	pos [2]int
}

// player returns the sample of the player with key, if the snapshot has one.
func (s *snapshot) player(key string) (playerSample, bool) {
	for _, p := range s.players {
		if p.key == key {
			return p.playerSample, true
		}
	}
	return playerSample{}, false
}

// nade returns the position of the projectile with uid, if it was sampled.
func (s *snapshot) nade(uid int64) ([2]int, bool) {
	for _, n := range s.nades {
		if n.uid == uid {
			return n.pos, true
		}
	}
	return [2]int{}, false
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
	playing := x.gs.Participants().Playing()
	s := &snapshot{players: make([]keyedSample, 0, len(playing))}
	for _, p := range playing {
		if p.PlayerPawnEntity() == nil {
			continue
		}
		pos := p.Position()
		s.players = append(s.players, keyedSample{x.seePlayer(p), playerSample{
			x:     int32(roundInt(pos.X)),
			y:     int32(roundInt(pos.Y)),
			yaw:   int16(yawDegrees(p.ViewDirectionX())),
			hp:    int16(p.Health()),
			side:  p.Team,
			alive: p.IsAlive(),
		}})
	}
	if len(r.byUID) > 0 {
		for _, g := range x.gs.GrenadeProjectiles() {
			if _, ok := r.byUID[g.UniqueID()]; ok {
				pos := g.Position()
				s.nades = append(s.nades, nadeSample{g.UniqueID(), [2]int{roundInt(pos.X), roundInt(pos.Y)}})
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
