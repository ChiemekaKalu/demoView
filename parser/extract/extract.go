// Package extract keeps what round files show while a demo's entities
// stream past, and slices it into round files once match logic has found
// the rounds.
//
// A Recorder samples the entity state once per tick into compact columns:
// each player's position, yaw and health (players.go), and each grenade
// projectile's path (grenades.go). Game events give the grenade moments
// that entities don't show, such as a smoke's end. After the walk, Rounds
// (rounds.go) cuts the columns into one roundfile.File per round.
package extract

import (
	"fmt"
	"math"

	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/roundfile"
)

// Classes and fields the Recorder reads.
const (
	controllerClass = "CCSPlayerController"
	pawnClass       = "CCSPlayerPawn"
	origin          = "CBodyComponent.m_skeletonInstance.m_vecOrigin."
)

// projectileClasses are the grenade projectile classes and their grenade
// types. A CMolotovProjectile is an incendiary when m_bIsIncGrenade is set.
var projectileClasses = []struct{ class, typ string }{
	{"CSmokeGrenadeProjectile", roundfile.Smoke},
	{"CFlashbangProjectile", roundfile.Flash},
	{"CHEGrenadeProjectile", roundfile.HE},
	{"CMolotovProjectile", roundfile.Molotov},
	{"CDecoyProjectile", roundfile.Decoy},
}

// Recorder samples entity state for round files. Call Sample for every
// tick in order and Event for every game event; then Rounds.
type Recorder struct {
	byClass []role // [class id]
	ctl     controllerRefs
	pawn    *entities.Class
	pawnRef pawnRefs

	base  int32  // tick of sample 0
	n     int    // samples taken
	stamp uint32 // the current sample, counting from 1

	players
	grenades
}

// role is what the Recorder does with an entity of a class.
type role struct {
	controller bool
	projectile *projectileRefs // nil if not a grenade projectile
}

// New returns a Recorder for the given classes. A class or field it reads
// that the classes lack gives an error wrapping entities.ErrUnknown.
func New(classes *entities.Classes) (*Recorder, error) {
	r := &Recorder{byClass: make([]role, classes.Len())}
	var b refBuilder
	ctl := b.class(classes, controllerClass)
	r.pawn = b.class(classes, pawnClass)
	r.ctl = controllerRefs{
		steamID: b.ref(ctl, "m_steamID"),
		name:    b.ref(ctl, "m_iszPlayerName"),
		team:    b.ref(ctl, "m_iTeamNum"),
		pawn:    b.ref(ctl, "m_hPlayerPawn"),
	}
	r.pawnRef = pawnRefs{
		pos:        b.position(r.pawn),
		eyes:       b.ref(r.pawn, "m_angEyeAngles"),
		health:     b.ref(r.pawn, "m_iHealth"),
		controller: b.ref(r.pawn, "m_hController"),
	}
	for _, pc := range projectileClasses {
		// A build that lacks a projectile class has none of those grenades.
		c := classes.ByName(pc.class)
		if c == nil {
			continue
		}
		p := &projectileRefs{typ: pc.typ, pos: b.position(c), thrower: b.ref(c, "m_hThrower")}
		if pc.typ == roundfile.Molotov {
			p.incendiary = b.ref(c, "m_bIsIncGrenade")
		}
		r.byClass[c.ID].projectile = p
	}
	if b.err != nil {
		return nil, b.err
	}
	r.byClass[ctl.ID].controller = true
	r.players.init()
	r.grenades.init()
	return r, nil
}

// Sample records the state as of tick, which must be one more than the
// last sample's.
func (r *Recorder) Sample(tick int32, s *entities.State) {
	if r.n == 0 {
		r.base = tick
	}
	r.stamp++
	for e := range s.All() {
		switch role := r.byClass[e.Class.ID]; {
		case role.controller:
			r.sampleController(e, s)
		case role.projectile != nil:
			r.sampleProjectile(e, role.projectile, s, tick)
		}
	}
	r.endPlayerSample()
	r.endGrenadeSample(tick)
	r.n++
}

// Next returns the tick the next Sample is for, or ok = false before the
// first.
func (r *Recorder) Next() (tick int32, ok bool) { return r.base + int32(r.n), r.n > 0 }

// Event takes the game events that time grenades.
func (r *Recorder) Event(e events.Event) { r.grenadeEvent(e) }

// position is the refs for an entity's x and y.
type position struct{ cellX, cellY, vecX, vecY entities.Ref }

// at returns e's rounded x and y in game units.
func (p position) at(e *entities.Entity) (x, y int16) {
	return coord(get(e, p.cellX), get(e, p.vecX)), coord(get(e, p.cellY), get(e, p.vecY))
}

func coord(cell, offset entities.Value) int16 {
	return int16(max(math.MinInt16, min(math.MaxInt16, math.Round(entities.WorldCoord(cell, offset)))))
}

// get returns e's value of r, or the zero Value if it has none.
func get(e *entities.Entity, r entities.Ref) entities.Value {
	v, _ := e.Get(r)
	return v
}

// refBuilder resolves classes and refs, keeping the first error.
type refBuilder struct{ err error }

func (b *refBuilder) class(classes *entities.Classes, name string) *entities.Class {
	c := classes.ByName(name)
	if c == nil && b.err == nil {
		b.err = fmt.Errorf("%w: extract: no class %s", entities.ErrUnknown, name)
	}
	return c
}

func (b *refBuilder) ref(c *entities.Class, name string) entities.Ref {
	if c == nil || b.err != nil {
		return entities.Ref{}
	}
	r, err := c.Ref(name)
	if err != nil {
		b.err = fmt.Errorf("%w: extract: %v", entities.ErrUnknown, err)
	}
	return r
}

func (b *refBuilder) position(c *entities.Class) position {
	return position{b.ref(c, origin+"m_cellX"), b.ref(c, origin+"m_cellY"), b.ref(c, origin+"m_vecX"), b.ref(c, origin+"m_vecY")}
}
