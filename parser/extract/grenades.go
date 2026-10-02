package extract

import (
	"math"

	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/roundfile"
)

// projectileRefs are the fields of one grenade projectile class.
type projectileRefs struct {
	typ        string // roundfile type
	pos        position
	thrower    entities.Ref
	incendiary entities.Ref // CMolotovProjectile only
}

// grenade is one projectile. Ticks are -1 until known.
type grenade struct {
	typ     string
	handle  uint32
	thrower *track // nil until known

	throw    int32 // its first sample
	detonate int32 // its detonation event, or its first sample without it if none came
	end      int32 // the event that ends its effect
	gone     int32 // its first sample without it
	ignited  bool  // a molotov whose fire started

	seen   uint32   // the sample stamp it was last in
	pos    [2]int16 // where it was then
	det    [2]int16 // where its detonation event put it, if detSet
	detSet bool

	path [][2]int16 // one point per sample from throw to detonate
}

// grenades are the projectiles seen so far.
type grenades struct {
	list     []*grenade
	byIndex  map[int]*grenade // the last projectile at each entity index
	live     []*grenade       // in the last sample
	flying   []*grenade       // still taking path points
	infernos map[int]*grenade // ignited molotovs, by their inferno's entity index
}

func (g *grenades) init() {
	g.byIndex = map[int]*grenade{}
	g.infernos = map[int]*grenade{}
}

// sampleProjectile takes projectile e of class p.
func (r *Recorder) sampleProjectile(e *entities.Entity, p *projectileRefs, s *entities.State, tick int32) {
	g := r.byIndex[e.Index]
	if g == nil || g.handle != e.Handle() || g.gone >= 0 {
		g = r.newGrenade(p.typ, e.Index, e.Handle(), tick)
	}
	g.seen = r.stamp
	if g.thrower == nil {
		g.thrower = r.thrower(get(e, p.thrower), s)
	}
	if g.detonate >= 0 && g.detonate < tick {
		return
	}
	g.pos[0], g.pos[1] = p.pos.at(e)
	if p.typ == roundfile.Molotov {
		g.typ = roundfile.Molotov
		if get(e, p.incendiary).Bool() {
			g.typ = roundfile.Incendiary
		}
	}
}

// newGrenade starts a projectile of type typ, first seen at tick at entity
// index index.
func (r *Recorder) newGrenade(typ string, index int, handle uint32, tick int32) *grenade {
	g := &grenade{typ: typ, handle: handle, throw: tick, detonate: -1, end: -1, gone: -1}
	r.list = append(r.list, g)
	r.byIndex[index] = g
	r.live = append(r.live, g)
	r.flying = append(r.flying, g)
	return g
}

// thrower returns the track of the player whose pawn has handle h, or nil.
func (r *Recorder) thrower(h entities.Value, s *entities.State) *track {
	pawn := s.ByHandle(uint32(h.Uint()))
	if pawn == nil || pawn.Class != r.pawn {
		return nil
	}
	c := s.ByHandle(uint32(get(pawn, r.pawnRef.controller).Uint()))
	if c == nil || !r.byClass[c.Class.ID].controller {
		return nil
	}
	return r.track(c)
}

// endGrenadeSample notes the projectiles that are gone, and adds this
// sample's point to the path of each one still flying.
func (r *Recorder) endGrenadeSample(tick int32) {
	live := r.live[:0]
	for _, g := range r.live {
		if g.seen == r.stamp {
			live = append(live, g)
		} else {
			g.gone = tick
		}
	}
	clear(r.live[len(live):])
	r.live = live

	flying := r.flying[:0]
	for _, g := range r.flying {
		switch {
		case g.detonate >= 0 && g.detonate < tick:
			continue // detonated after its point for that tick was taken
		case g.detonate < 0 && g.gone >= 0:
			g.detonate = tick // gone without detonating
		}
		g.path = append(g.path, g.point(r.stamp))
		if g.detonate < 0 {
			flying = append(flying, g)
		}
	}
	clear(r.flying[len(flying):])
	r.flying = flying
}

// point returns g's path point for the sample with the given stamp: where
// it is, or once it is gone, where it detonated or was last.
func (g *grenade) point(stamp uint32) [2]int16 {
	if g.seen != stamp && g.detSet {
		return g.det
	}
	return g.pos
}

// Events that name a projectile by entityid: the detonations, and the ends
// of the effects that outlast them.
var (
	detonations = map[string]string{
		"smokegrenade_detonate": roundfile.Smoke,
		"flashbang_detonate":    roundfile.Flash,
		"hegrenade_detonate":    roundfile.HE,
		"decoy_started":         roundfile.Decoy,
	}
	effectEnds = map[string]string{
		"smokegrenade_expired": roundfile.Smoke,
		"decoy_detonate":       roundfile.Decoy,
	}
)

// grenadeEvent applies a game event that times a grenade.
func (r *Recorder) grenadeEvent(e events.Event) {
	if typ, ok := detonations[e.Name]; ok {
		if g := r.named(e, typ); g != nil && g.detonate < 0 {
			g.detonated(e)
			if typ == roundfile.Flash || typ == roundfile.HE {
				g.end = e.Tick
			}
		}
		return
	}
	if typ, ok := effectEnds[e.Name]; ok {
		if g := r.named(e, typ); g != nil && g.end < 0 {
			g.end = e.Tick
			if g.detonate < 0 {
				g.detonated(e)
			}
		}
		return
	}
	switch e.Name {
	case "inferno_startburn":
		if g := r.landedFire(e); g != nil {
			g.detonated(e)
			g.ignited = true
			id, _ := e.Int("entityid")
			r.infernos[id] = g
		}
	case "inferno_expire":
		id, _ := e.Int("entityid")
		if g := r.infernos[id]; g != nil {
			g.end = e.Tick
			delete(r.infernos, id)
		}
	}
}

// named returns the projectile of type typ that event e names by its
// entityid, or nil.
func (r *Recorder) named(e events.Event, typ string) *grenade {
	id, _ := e.Int("entityid")
	if g := r.byIndex[id]; g != nil && g.typ == typ {
		return g
	}
	return nil
}

// landedFire returns the flying molotov or incendiary nearest to where
// inferno_startburn event e starts a fire, or nil.
func (r *Recorder) landedFire(e events.Event) *grenade {
	x, y := eventPos(e)
	var best *grenade
	bestDist := math.Inf(1)
	for _, g := range r.flying {
		if g.detonate >= 0 || g.typ != roundfile.Molotov && g.typ != roundfile.Incendiary {
			continue
		}
		if d := math.Hypot(float64(g.pos[0]-x), float64(g.pos[1]-y)); d < bestDist {
			best, bestDist = g, d
		}
	}
	return best
}

// detonated records detonation event e.
func (g *grenade) detonated(e events.Event) {
	g.detonate = e.Tick
	g.det[0], g.det[1] = eventPos(e)
	g.detSet = true
}

// eventPos returns an event's rounded x and y.
func eventPos(e events.Event) (x, y int16) {
	fx, _ := e.Float("x")
	fy, _ := e.Float("y")
	return int16(math.Round(float64(fx))), int16(math.Round(float64(fy)))
}

// effectEnd returns the tick g's effect ends, or -1 if the demo doesn't
// say, when it lasts to the end of its round. A flash or HE has no effect
// past its detonation, and nor does a molotov that never caught fire.
func (g *grenade) effectEnd() int32 {
	switch {
	case g.end >= 0:
		return g.end
	case g.typ == roundfile.Flash || g.typ == roundfile.HE:
		return g.detonate
	case g.typ == roundfile.Molotov || g.typ == roundfile.Incendiary:
		if !g.ignited {
			return g.detonate
		}
		return -1
	}
	return g.gone
}
