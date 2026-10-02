package extract

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/roundfile"
)

func newTestRecorder() *Recorder {
	r := &Recorder{}
	r.players.init()
	r.grenades.init()
	return r
}

// grenadeEventNames are the events eventDecoder knows, with ids from 1.
var grenadeEventNames = []string{
	"smokegrenade_detonate", "smokegrenade_expired", "flashbang_detonate", "hegrenade_detonate",
	"decoy_started", "decoy_detonate", "inferno_startburn", "inferno_expire",
}

// event returns grenade event name at tick, with keys entityid, x and y.
func event(t *testing.T, name string, tick int32, entity int, x, y float32) events.Event {
	t.Helper()
	var list pb.CMsgSource1LegacyGameEventList
	id := 0
	for i, n := range grenadeEventNames {
		d := &pb.CMsgSource1LegacyGameEventListDescriptorT{Eventid: proto.Int32(int32(i + 1)), Name: proto.String(n)}
		for _, k := range []struct {
			name string
			typ  events.KeyType
		}{{"entityid", events.TypeShort}, {"x", events.TypeFloat}, {"y", events.TypeFloat}} {
			d.Keys = append(d.Keys, &pb.CMsgSource1LegacyGameEventListKeyT{Name: proto.String(k.name), Type: proto.Int32(int32(k.typ))})
		}
		list.Descriptors = append(list.Descriptors, d)
		if n == name {
			id = i + 1
		}
	}
	var dec events.Decoder
	body, _ := proto.Marshal(&list)
	if err := dec.HandleList(body); err != nil {
		t.Fatal(err)
	}
	body, _ = proto.Marshal(&pb.CMsgSource1LegacyGameEvent{Eventid: proto.Int32(int32(id)), Keys: []*pb.CMsgSource1LegacyGameEventKeyT{
		{Type: proto.Int32(int32(events.TypeShort)), ValShort: proto.Int32(int32(entity))},
		{Type: proto.Int32(int32(events.TypeFloat)), ValFloat: proto.Float32(x)},
		{Type: proto.Int32(int32(events.TypeFloat)), ValFloat: proto.Float32(y)},
	}})
	e, err := dec.Decode(body, tick)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// sample ends the recorder's sample at tick with the given projectiles in
// it, at the given positions, as Sample does after reading the entities.
func sample(r *Recorder, tick int32, in map[*grenade][2]int16) {
	r.stamp++
	for g, pos := range in {
		g.seen, g.pos = r.stamp, pos
	}
	r.endGrenadeSample(tick)
}

func TestGrenadeTimes(t *testing.T) {
	r := newTestRecorder()
	p := func(x int16) [2]int16 { return [2]int16{x, x} }
	flash := r.newGrenade(roundfile.Flash, 1, 1, 10)
	smoke := r.newGrenade(roundfile.Smoke, 2, 2, 10)
	dud := r.newGrenade(roundfile.Molotov, 3, 3, 10)  // never catches fire
	fire := r.newGrenade(roundfile.Molotov, 4, 4, 10) // lands at 500, 500
	far := r.newGrenade(roundfile.Incendiary, 5, 5, 10)
	decoy := r.newGrenade(roundfile.Decoy, 6, 6, 10) // removed before it goes off
	he := r.newGrenade(roundfile.HE, 7, 7, 10)       // its detonation comes after its tick's sample

	all := func(x int16, gs ...*grenade) map[*grenade][2]int16 {
		m := map[*grenade][2]int16{}
		for _, g := range gs {
			m[g] = p(x)
		}
		return m
	}
	sample(r, 10, all(10, flash, smoke, dud, fire, far, decoy, he))
	sample(r, 11, all(11, flash, smoke, dud, fire, far, decoy, he))
	r.Event(event(t, "flashbang_detonate", 12, 1, 100, 200))
	r.Event(event(t, "smokegrenade_detonate", 12, 2, 0, 0))
	sample(r, 12, all(12, smoke, dud, fire, far, decoy, he)) // the flash is gone
	r.Event(event(t, "hegrenade_detonate", 12, 7, 0, 0))
	in := all(13, smoke, far, decoy, he)
	in[fire] = [2]int16{500, 500}
	sample(r, 13, in) // the dud is gone
	r.Event(event(t, "inferno_startburn", 14, 30, 505, 495))
	sample(r, 14, all(14, smoke, far, he)) // the fire's projectile and the decoy are gone
	r.Event(event(t, "inferno_expire", 20, 30, 0, 0))
	r.Event(event(t, "smokegrenade_expired", 20, 2, 0, 0))

	tests := []struct {
		name          string
		g             *grenade
		detonate, end int32
		path          [][2]int16
	}{
		{"flash", flash, 12, 12, [][2]int16{p(10), p(11), {100, 200}}},
		{"smoke", smoke, 12, 20, [][2]int16{p(10), p(11), p(12)}},
		{"dud molotov", dud, 13, 13, [][2]int16{p(10), p(11), p(12), p(12)}},
		{"molotov", fire, 14, 20, [][2]int16{p(10), p(11), p(12), {500, 500}, {505, 495}}},
		{"decoy", decoy, 14, 14, [][2]int16{p(10), p(11), p(12), p(13), p(13)}},
		{"he", he, 12, 12, [][2]int16{p(10), p(11), p(12)}},
	}
	for _, tc := range tests {
		if tc.g.detonate != tc.detonate || tc.g.effectEnd() != tc.end || !slices.Equal(tc.g.path, tc.path) {
			t.Errorf("%s: detonate %d, end %d, path %v; want %d, %d, %v", tc.name, tc.g.detonate, tc.g.effectEnd(), tc.g.path, tc.detonate, tc.end, tc.path)
		}
	}
	if far.detonate >= 0 || len(r.flying) != 1 || r.flying[0] != far {
		t.Errorf("the incendiary far from the fire: detonate %d; flying %d", far.detonate, len(r.flying))
	}
}

// A dead player, or one with no pawn, holds their last pose with hp 0.
func TestPlayerHold(t *testing.T) {
	r := newTestRecorder()
	tr := r.trackOf("steam:1")
	poses := []struct {
		seen bool
		cur  pose
	}{
		{true, pose{1, 2, 3, 100}},
		{true, pose{9, 9, 9, 0}},
		{false, pose{}},
		{true, pose{5, 6, 7, 90}},
	}
	for _, p := range poses {
		r.stamp++
		if p.seen {
			tr.cur, tr.seen = p.cur, r.stamp
		}
		r.endPlayerSample()
		r.n++
	}
	want := []pose{{1, 2, 3, 100}, {1, 2, 3, 0}, {1, 2, 3, 0}, {5, 6, 7, 90}}
	for i, w := range want {
		if got := tr.at(i); got != w {
			t.Errorf("sample %d: %+v, want %+v", i, got, w)
		}
	}
}

func TestYaw(t *testing.T) {
	for _, tc := range []struct {
		deg  float32
		want int16
	}{{0, 0}, {-90, 270}, {359.6, 0}, {-0.4, 0}, {180.5, 181}, {725, 5}} {
		if got := yaw(tc.deg); got != tc.want {
			t.Errorf("yaw(%g) = %d, want %d", tc.deg, got, tc.want)
		}
	}
}

// TestRounds slices a recording of ticks 100 to 160 into two rounds: round
// 1 from its freeze end at 110 to round 2's start at 130, and round 2 from
// 140 through the last tick.
func TestRounds(t *testing.T) {
	r := newTestRecorder()
	r.base, r.n = 100, 61
	a := r.trackOf("steam:7")
	a.first = 0
	for i := range r.n {
		a.x, a.y, a.yaw, a.hp = append(a.x, int16(100+i)), append(a.y, 1), append(a.yaw, 2), append(a.hp, 100)
	}
	bob := r.trackOf("bot:Bob") // from tick 115
	bob.first = 15
	for range r.n - bob.first {
		bob.x, bob.y, bob.yaw, bob.hp = append(bob.x, 5), append(bob.y, 6), append(bob.yaw, 7), append(bob.hp, 50)
	}
	stranger := r.trackOf("steam:99")

	thrown := func(by *track, throw, detonate int32, typ string) *grenade {
		g := &grenade{typ: typ, thrower: by, throw: throw, detonate: detonate, end: -1, gone: -1}
		for tick := throw; tick <= detonate; tick++ {
			g.path = append(g.path, [2]int16{int16(tick), 0})
		}
		r.list = append(r.list, g)
		return g
	}
	thrown(a, 129, 135, roundfile.HE)        // detonates past the round's end
	thrown(bob, 112, 115, roundfile.Flash)   // sorts first
	thrown(stranger, 115, 116, roundfile.HE) // by nobody in match.json
	thrown(a, 135, 137, roundfile.HE)        // in round 2's freeze time

	m := &match.File{
		Match:   match.Info{TickRate: 64},
		Players: []match.Player{{Idx: 0, SteamID: "7", Name: "A"}, {Idx: 1, SteamID: "0", Name: "Bob"}},
		Rounds:  []match.Round{{N: 1, StartTick: 100, FreezeEndTick: 110}, {N: 2, StartTick: 130, FreezeEndTick: 140}},
		Kills:   []match.Kill{{Round: 1, Tick: 105}, {Round: 1, Tick: 125}, {Round: 2, Tick: 150}},
	}
	files := r.Rounds(m, [][]int{{0, 1}, {0}}, 160)
	if len(files) != 2 {
		t.Fatalf("%d files", len(files))
	}
	r1, r2 := files[0], files[1]
	if r1.Round != 1 || r1.Hz != 64 || r1.StartTick != 110 || r1.Frames != 20 || r2.StartTick != 140 || r2.Frames != 21 {
		t.Errorf("headers %+v, %+v", r1, r2)
	}
	if len(r1.Players) != 2 || len(r2.Players) != 1 || r2.Players[0].Idx != 0 {
		t.Fatalf("players %d and %d", len(r1.Players), len(r2.Players))
	}
	if p := r1.Players[0]; p.X[0] != 110 || p.X[19] != 129 || p.HP[0] != 100 {
		t.Errorf("player 0 in round 1: x %d..%d, hp %d", p.X[0], p.X[19], p.HP[0])
	}
	// Bob has no samples before tick 115: dead in his first pose.
	if p := r1.Players[1]; p.HP[4] != 0 || p.X[4] != 5 || p.HP[5] != 50 {
		t.Errorf("Bob at frames 4 and 5: hp %d x %d, hp %d", p.HP[4], p.X[4], p.HP[5])
	}

	want := []roundfile.Grenade{
		{Type: roundfile.Flash, Thrower: 1, ThrowFrame: 2, DetonateFrame: 5, EndFrame: 5,
			Path: [][2]int16{{112, 0}, {113, 0}, {114, 0}, {115, 0}}},
		{Type: roundfile.HE, Thrower: 0, ThrowFrame: 19, DetonateFrame: 19, EndFrame: 19, Path: [][2]int16{{129, 0}}},
	}
	if !slices.EqualFunc(r1.Grenades, want, sameGrenade) || len(r2.Grenades) != 0 {
		t.Errorf("grenades\n %+v\n %+v\nwant %+v", r1.Grenades, r2.Grenades, want)
	}
	wantEvents := [][]roundfile.Event{
		{{Frame: 0, Type: "kill", Kill: 0}, {Frame: 15, Type: "kill", Kill: 1}},
		{{Frame: 10, Type: "kill", Kill: 2}},
	}
	for i, f := range files {
		if !slices.Equal(f.Events, wantEvents[i]) {
			t.Errorf("round %d events %+v, want %+v", f.Round, f.Events, wantEvents[i])
		}
	}

	// At 128 ticks a frame is two ticks.
	m.Match.TickRate = 128
	f := r.Rounds(m, [][]int{{0}, {0}}, 160)[0]
	if f.Hz != 64 || f.Frames != 10 || f.Players[0].X[1] != 112 || f.Events[1].Frame != 8 {
		t.Errorf("at 128 ticks: hz %d, %d frames, x[1] %d, kill frame %d", f.Hz, f.Frames, f.Players[0].X[1], f.Events[1].Frame)
	}
}

func sameGrenade(a, b roundfile.Grenade) bool {
	return a.Type == b.Type && a.Thrower == b.Thrower && a.ThrowFrame == b.ThrowFrame &&
		a.DetonateFrame == b.DetonateFrame && a.EndFrame == b.EndFrame && slices.Equal(a.Path, b.Path)
}
