package extract

import (
	"math"
	"strconv"

	"github.com/ChiemekaKalu/demoview/parser/entities"
)

// controllerRefs are the CCSPlayerController fields the Recorder reads.
type controllerRefs struct{ steamID, name, team, pawn entities.Ref }

// pawnRefs are the CCSPlayerPawn fields it reads.
type pawnRefs struct {
	pos                      position
	eyes, health, controller entities.Ref
}

// pose is a player at one sample.
type pose struct {
	x, y, yaw int16
	hp        uint8
}

// track is one player's columns, one value per sample from sample first
// on. While the player is dead, or has no pawn, the columns repeat their
// last pose with hp 0.
type track struct {
	key       string // playerKey
	first     int
	x, y, yaw []int16
	hp        []uint8

	cur  pose   // this sample's pose
	seen uint32 // the sample stamp cur is from
}

// at returns the pose at sample i, which must be one of the track's.
func (t *track) at(i int) pose {
	i -= t.first
	return pose{t.x[i], t.y[i], t.yaw[i], t.hp[i]}
}

// players are the tracks, by player.
type players struct {
	tracks []*track
	byKey  map[string]*track
	ctls   []controllerCache // by controller entity index
}

// controllerCache remembers which track a controller entity's player has,
// so a sample builds no keys.
type controllerCache struct {
	ent     *entities.Entity
	steamID uint64
	name    string // for a bot
	track   *track
}

func (p *players) init() { p.byKey = map[string]*track{} }

// sampleController takes the pose of controller c's pawn, if c plays on T
// or CT and has one.
func (r *Recorder) sampleController(c *entities.Entity, s *entities.State) {
	if team := get(c, r.ctl.team).Uint(); team != 2 && team != 3 {
		return
	}
	pawn := s.ByHandle(uint32(get(c, r.ctl.pawn).Uint()))
	if pawn == nil || pawn.Class != r.pawn {
		return
	}
	t := r.track(c)
	x, y := r.pawnRef.pos.at(pawn)
	t.cur = pose{x, y, yaw(get(pawn, r.pawnRef.eyes).Vector()[1]), health(get(pawn, r.pawnRef.health).Int())}
	t.seen = r.stamp
}

// track returns the track of controller c's player, making it if needed.
func (r *Recorder) track(c *entities.Entity) *track {
	id := get(c, r.ctl.steamID).Uint()
	var name string
	if id == 0 {
		name = get(c, r.ctl.name).Str()
	}
	if c.Index >= len(r.ctls) {
		r.ctls = append(r.ctls, make([]controllerCache, c.Index+1-len(r.ctls))...)
	}
	cc := &r.ctls[c.Index]
	if cc.ent != c || cc.steamID != id || cc.name != name {
		*cc = controllerCache{c, id, name, r.trackOf(playerKey(id, name))}
	}
	return cc.track
}

func (r *Recorder) trackOf(key string) *track {
	t := r.byKey[key]
	if t == nil {
		t = &track{key: key, first: r.n}
		r.byKey[key] = t
		r.tracks = append(r.tracks, t)
	}
	return t
}

// endPlayerSample appends this sample's pose to every track.
func (r *Recorder) endPlayerSample() {
	for _, t := range r.tracks {
		p := t.cur
		if t.seen != r.stamp {
			p.hp = 0
		}
		if last := len(t.hp) - 1; p.hp == 0 && last >= 0 {
			p = pose{t.x[last], t.y[last], t.yaw[last], 0}
		}
		t.x, t.y, t.yaw, t.hp = append(t.x, p.x), append(t.y, p.y), append(t.yaw, p.yaw), append(t.hp, p.hp)
	}
}

// playerKey identifies a player as match logic does: by SteamID64, or by
// name for a bot.
func playerKey(steamID uint64, name string) string {
	if steamID != 0 {
		return "steam:" + strconv.FormatUint(steamID, 10)
	}
	return "bot:" + name
}

// yaw returns a yaw in degrees, such as m_angEyeAngles', in whole degrees
// in [0, 360), counterclockwise from +x.
func yaw(deg float32) int16 {
	d := math.Mod(float64(deg), 360)
	if d < 0 {
		d += 360
	}
	if d = math.Round(d); d >= 360 {
		d -= 360
	}
	return int16(d)
}

func health(hp int64) uint8 { return uint8(max(0, min(100, hp))) }
