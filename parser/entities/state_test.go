package entities_test

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/stringtables"
)

// Classes for the State tests: a pawn (class 0) and a thing (class 1).
// Pawn fields, by index:
//
//	0 m_iHealth
//	1 m_angEyeAngles
//	2 CBodyComponent, a pointer to Body: 0 m_cellX, 1 m_vecX
//	3 m_hController
//	4 m_vecList, a vector of uint8
//	5 m_arr, two int32s
const (
	pawnClass  = 0
	thingClass = 1
	idBits     = 2
)

func testClasses(t *testing.T) *entities.Classes {
	t.Helper()
	var b flat
	b.serializer("Body", 0,
		b.field(field{name: "m_cellX", typ: "uint16", node: "m_skeletonInstance.m_vecOrigin"}),
		b.field(field{name: "m_vecX", typ: "float32", node: "m_skeletonInstance.m_vecOrigin"}),
	)
	b.serializer("Pawn", 0,
		b.field(field{name: "m_iHealth", typ: "int32"}),
		b.field(field{name: "m_angEyeAngles", typ: "QAngle", encoder: "qangle_precise"}),
		b.field(field{name: "CBodyComponent", typ: "CBodyComponent", ser: "Body"}),
		b.field(field{name: "m_hController", typ: "CHandle< CBasePlayerController >"}),
		b.field(field{name: "m_vecList", typ: "CUtlVector< uint8 >"}),
		b.field(field{name: "m_arr", typ: "int32[2]"}),
	)
	b.serializer("Thing", 0, b.field(field{name: "m_nValue", typ: "uint32"}))
	c, err := entities.NewClasses(classInfo(class{pawnClass, "Pawn"}, class{thingClass, "Thing"}), b.build(t))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// baselines returns an instancebaseline table with the given entries.
func baselines(entries ...*pb.CDemoStringTablesItemsT) *stringtables.Table {
	var ts stringtables.Tables
	ts.ApplySnapshot(&pb.CDemoStringTables{Tables: []*pb.CDemoStringTablesTableT{{
		TableName: proto.String(stringtables.InstanceBaseline),
		Items:     entries,
	}}})
	return ts.Get(stringtables.InstanceBaseline)
}

func item(key string, fields func(w *entities.BitWriter)) *pb.CDemoStringTablesItemsT {
	var w entities.BitWriter
	fields(&w)
	return &pb.CDemoStringTablesItemsT{Str: proto.String(key), Data: w.Data()}
}

// pawnBaseline gives a pawn 100 health and a body at cell 34.
func pawnBaseline(w *entities.BitWriter) {
	w.FieldPaths([]int{0}, []int{2}, []int{2, 0})
	w.VarInt(100)
	w.Bit(true)
	w.VarUint(34)
}

// An entry writes one entry of entity data: the index delta, the command,
// and what follows.
type entry func(w *entities.BitWriter)

func create(skip uint32, class, serial uint64, fields func(w *entities.BitWriter)) entry {
	return func(w *entities.BitWriter) {
		w.UBitVar(skip)
		w.Bits(2, 2)
		w.Bits(class, idBits)
		w.Bits(serial, 17)
		w.VarUint(0)
		fields(w)
	}
}

func update(skip uint32, fields func(w *entities.BitWriter)) entry {
	return func(w *entities.BitWriter) {
		w.UBitVar(skip)
		w.Bits(0, 2)
		fields(w)
	}
}

func remove(skip uint32, cmd uint64) entry {
	return func(w *entities.BitWriter) {
		w.UBitVar(skip)
		w.Bits(cmd, 2)
	}
}

// message returns an svc_PacketEntities of the newer kind, which sends
// non_transmitted_entities and appends nothing to its entries.
func message(delta bool, entries ...entry) *pb.CSVCMsg_PacketEntities {
	var w entities.BitWriter
	for _, e := range entries {
		e(&w)
	}
	return &pb.CSVCMsg_PacketEntities{
		UpdatedEntries:         proto.Int32(int32(len(entries))),
		LegacyIsDelta:          proto.Bool(delta),
		EntityData:             w.Data(),
		NonTransmittedEntities: &pb.CSVCMsg_PacketEntitiesNonTransmittedEntitiesT{},
	}
}

func ref(t *testing.T, c *entities.Class, name string) entities.Ref {
	t.Helper()
	r, err := c.Ref(name)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestState(t *testing.T) {
	classes := testClasses(t)
	pawn, thing := classes.ByID(pawnClass), classes.ByID(thingClass)
	health := ref(t, pawn, "m_iHealth")
	eyes := ref(t, pawn, "m_angEyeAngles")
	cellX := ref(t, pawn, "CBodyComponent.m_skeletonInstance.m_vecOrigin.m_cellX")
	vecX := ref(t, pawn, "CBodyComponent.m_skeletonInstance.m_vecOrigin.m_vecX")
	controller := ref(t, pawn, "m_hController")
	list := ref(t, pawn, "m_vecList")
	elem1 := ref(t, pawn, "m_vecList.1")
	value := ref(t, thing, "m_nValue")
	base := baselines(item("0", pawnBaseline))

	s := entities.NewState(classes)
	apply := func(m *pb.CSVCMsg_PacketEntities) {
		t.Helper()
		if err := s.Apply(m, base); err != nil {
			t.Fatal(err)
		}
	}
	get := func(e *entities.Entity, r entities.Ref) entities.Value {
		t.Helper()
		v, ok := e.Get(r)
		if !ok {
			t.Fatalf("entity %d has no %s", e.Index, r.Field().Name)
		}
		return v
	}

	// Entity 5 starts from the pawn baseline.
	apply(message(false, create(5, pawnClass, 7, func(w *entities.BitWriter) {
		w.FieldPaths([]int{1}, []int{2, 1}, []int{3}, []int{4}, []int{4, 0}, []int{4, 1})
		w.Bits(0b010, 3)     // yaw only
		w.Bits(3<<18, 20)    // 270 - 180
		w.Float32(352)       // m_vecX
		w.VarUint(1<<14 | 2) // controller: serial 1, index 2
		w.VarUint(2)         // list length
		w.VarUint(7)
		w.VarUint(8)
	})))
	e := s.Entity(5)
	if e == nil || e.Class != pawn || e.Serial != 7 {
		t.Fatalf("entity 5 is %+v", e)
	}
	if got := get(e, health).Int(); got != 100 {
		t.Errorf("health %d, want 100 from the baseline", got)
	}
	if got := get(e, eyes).Vector(); got != [4]float32{0, 90, 0} {
		t.Errorf("eye angles %v", got)
	}
	if got := entities.WorldCoord(get(e, cellX), get(e, vecX)); got != 1376 {
		t.Errorf("x %g, want 1376", got)
	}
	if got := get(e, controller).Uint(); got != 1<<14|2 {
		t.Errorf("controller %#x", got)
	}
	if got := get(e, elem1).Uint(); got != 8 {
		t.Errorf("m_vecList.1 = %d, want 8", got)
	}
	if h := e.Handle(); h != 7<<14|5 || s.ByHandle(h) != e || s.ByHandle(h+1<<14) != nil {
		t.Errorf("handle %#x doesn't find the entity, or finds it with the wrong serial", h)
	}

	// An update changes a field and shrinks the list, and entity 6 appears.
	apply(message(true,
		update(5, func(w *entities.BitWriter) {
			w.FieldPaths([]int{0}, []int{4})
			w.VarInt(73)
			w.VarUint(1)
		}),
		create(0, thingClass, 1, func(w *entities.BitWriter) {
			w.FieldPaths([]int{0})
			w.VarUint(3)
		}),
	))
	if got := get(e, health).Int(); got != 73 {
		t.Errorf("health %d after the update, want 73", got)
	}
	if got := get(e, list).Uint(); got != 1 {
		t.Errorf("list length %d, want 1", got)
	}
	if _, ok := e.Get(elem1); ok {
		t.Error("m_vecList.1 is still there after the list shrank")
	}
	if got := get(s.Entity(6), value).Uint(); got != 3 {
		t.Errorf("thing value %d, want 3", got)
	}
	if _, ok := s.Entity(6).Get(health); ok {
		t.Error("a pawn ref read a thing")
	}

	// Entity 5 leaves the PVS and 6 is deleted; both are gone.
	apply(message(true, remove(5, 1), remove(0, 3)))
	if s.Entity(5) != nil || s.Entity(6) != nil {
		t.Error("an entity that left or was deleted is still there")
	}

	// A non-delta message replaces everything.
	apply(message(true, create(1, thingClass, 2, func(w *entities.BitWriter) { w.FieldPaths() })))
	apply(message(false, create(9, thingClass, 3, func(w *entities.BitWriter) { w.FieldPaths() })))
	var indexes []int
	for e := range s.All() {
		indexes = append(indexes, e.Index)
	}
	if len(indexes) != 1 || indexes[0] != 9 {
		t.Errorf("entities %v after a full update, want [9]", indexes)
	}
}

func TestStateAlternateBaseline(t *testing.T) {
	classes := testClasses(t)
	value := ref(t, classes.ByID(thingClass), "m_nValue")
	thing := func(v uint64) func(w *entities.BitWriter) {
		return func(w *entities.BitWriter) {
			w.FieldPaths([]int{0})
			w.VarUint(v)
		}
	}
	// Entry 1 of the table is the thing's alternate baseline.
	base := baselines(item("1", thing(1)), item("1:0", thing(2)))
	s := entities.NewState(classes)
	alt := func(m *pb.CSVCMsg_PacketEntities) *pb.CSVCMsg_PacketEntities {
		m.AlternateBaselines = []*pb.CSVCMsg_PacketEntitiesAlternateBaselineT{{EntityIndex: proto.Int32(3), BaselineIndex: proto.Int32(1)}}
		return m
	}
	read := func() uint64 {
		v, _ := s.Entity(3).Get(value)
		return v.Uint()
	}
	steps := []struct {
		m    *pb.CSVCMsg_PacketEntities
		want uint64
	}{
		{message(false, create(3, thingClass, 1, func(w *entities.BitWriter) { w.FieldPaths() })), 1},
		{message(true, update(3, thing(5))), 5},
		// An update with an alternate baseline starts from it.
		{alt(message(true, update(3, func(w *entities.BitWriter) { w.FieldPaths() }))), 2},
		{alt(message(false, create(3, thingClass, 1, func(w *entities.BitWriter) { w.FieldPaths() }))), 2},
	}
	for i, step := range steps {
		if err := s.Apply(step.m, base); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if got := read(); got != step.want {
			t.Errorf("step %d: value %d, want %d", i, got, step.want)
		}
	}
}

func TestStateOlderFormat(t *testing.T) {
	classes := testClasses(t)
	value := ref(t, classes.ByID(thingClass), "m_nValue")
	thing := func(v uint64) func(w *entities.BitWriter) {
		return func(w *entities.BitWriter) {
			w.FieldPaths([]int{0})
			w.VarUint(v)
		}
	}
	s := entities.NewState(classes)
	if err := s.Apply(message(false, create(0, thingClass, 1, thing(4)), create(0, thingClass, 1, thing(5))), nil); err != nil {
		t.Fatal(err)
	}

	// Older demos send two more bits per update, with the low one set for
	// no change, and end delta messages with a list of entity indices.
	var w entities.BitWriter
	w.UBitVar(0) // entity 0
	w.Bits(0, 2)
	w.Bits(0b01, 2)
	w.UBitVar(0) // entity 1
	w.Bits(0, 2)
	w.Bits(0b10, 2)
	thing(6)(&w)
	w.UBitVar(2)
	w.UBitVar(40)
	w.UBitVar(300)
	m := &pb.CSVCMsg_PacketEntities{
		UpdatedEntries:          proto.Int32(2),
		LegacyIsDelta:           proto.Bool(true),
		HasPvsVisBitsDeprecated: proto.Uint32(1),
		EntityData:              w.Data(),
	}
	if err := s.Apply(m, nil); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint64{4, 6} {
		if v, _ := s.Entity(i).Get(value); v.Uint() != want {
			t.Errorf("entity %d: value %d, want %d", i, v.Uint(), want)
		}
	}
}

func TestStateErrors(t *testing.T) {
	classes := testClasses(t)
	empty := func(w *entities.BitWriter) { w.FieldPaths() }
	tests := []struct {
		name string
		m    *pb.CSVCMsg_PacketEntities
		want string
	}{
		{"update of nothing", message(true, update(4, empty)), "doesn't exist"},
		{"unknown class", message(false, create(0, 3, 1, empty)), "class id 3"},
		{"truncated", message(false, create(0, thingClass, 1, func(w *entities.BitWriter) {
			w.FieldPaths([]int{0})
		})), "read past end"},
		{"left over", func() *pb.CSVCMsg_PacketEntities {
			m := message(false, create(0, thingClass, 1, empty))
			m.EntityData = append(m.EntityData, 0xFF)
			return m
		}(), "left over"},
		{"bad path", message(false, create(0, thingClass, 1, func(w *entities.BitWriter) {
			w.FieldPaths([]int{1})
		})), "Thing has 1 fields"},
		{"alternate baseline of another class", func() *pb.CSVCMsg_PacketEntities {
			m := message(false, create(0, thingClass, 1, empty))
			m.AlternateBaselines = []*pb.CSVCMsg_PacketEntitiesAlternateBaselineT{{EntityIndex: proto.Int32(0), BaselineIndex: proto.Int32(0)}}
			return m
		}(), "isn't one of Thing's"},
	}
	base := baselines(item("0", pawnBaseline))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := entities.NewState(classes).Apply(tc.m, base)
			if !errors.Is(err, demo.ErrBadDemo) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want ErrBadDemo with %q", err, tc.want)
			}
		})
	}
}

func TestRef(t *testing.T) {
	classes := testClasses(t)
	pawn := classes.ByID(pawnClass)
	for _, name := range []string{"m_iHealth", "CBodyComponent", "CBodyComponent.m_skeletonInstance.m_vecOrigin.m_vecX", "m_vecList", "m_vecList.40", "m_arr.1"} {
		if r, err := pawn.Ref(name); err != nil || r.Field() == nil {
			t.Errorf("Ref(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", "m_iHealthy", "m_iHealth.0", "CBodyComponent.m_cellX", "m_arr", "m_arr.2", "m_vecList.x", "m_vecList.-1"} {
		if _, err := pawn.Ref(name); err == nil {
			t.Errorf("Ref(%q) resolved", name)
		}
	}

	// A null pointer holds no fields.
	s := entities.NewState(classes)
	if err := s.Apply(message(false, create(0, pawnClass, 1, func(w *entities.BitWriter) {
		w.FieldPaths([]int{2}, []int{5, 1})
		w.Bit(false)
		w.VarInt(-3)
	})), nil); err != nil {
		t.Fatal(err)
	}
	e := s.Entity(0)
	if _, ok := e.Get(ref(t, pawn, "CBodyComponent.m_skeletonInstance.m_vecOrigin.m_cellX")); ok {
		t.Error("read a field under a null pointer")
	}
	if v, ok := e.Get(ref(t, pawn, "m_arr.1")); !ok || v.Int() != -3 {
		t.Errorf("m_arr.1 = %d, %v; want -3", v.Int(), ok)
	}
	if _, ok := e.Get(ref(t, pawn, "m_vecList.0")); ok {
		t.Error("read an element of an empty list")
	}
}
