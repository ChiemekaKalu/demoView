package entities

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/stringtables"
)

// FuzzFieldPaths decodes arbitrary bytes as a field path list, on its own
// and through State.Apply, as the list of an update to entity 0, a Chain.
// Decoding must not panic, every path must hold 1 to maxPathDepth indices,
// and there can be no more paths than input bits. Apply must succeed or
// fail with ErrBadDemo, and may succeed only if the list decoded cleanly
// with no negative index. A list that did must decode the same again after
// BitWriter.FieldPaths re-encodes it. The seeds are lists, with their
// values, that apply cleanly to a Chain, plus no input and random bytes.
// The spec's exit check runs it for 10 minutes:
//
//	go test ./entities -run '^$' -fuzz '^FuzzFieldPaths$' -fuzztime 10m -fuzzminimizetime 1s
func FuzzFieldPaths(f *testing.F) {
	fx := newFuzzFixture(f)
	for i, s := range fieldPathSeeds() {
		var w BitWriter
		if s.ops != nil {
			s.ops(&w)
		} else {
			w.FieldPaths(s.paths...)
		}
		s.values(&w)
		paths, err := readFieldPaths(bitread.New(w.Data()), nil)
		if got := pathInts(paths); err != nil || !slices.EqualFunc(got, s.paths, slices.Equal) {
			f.Fatalf("seed %d: decoded %v, %v; want %v", i, got, err, s.paths)
		}
		if err := fx.state(f, nil).Apply(chainUpdate(w.Data()), nil); err != nil {
			f.Fatalf("seed %d: %v", i, err)
		}
		f.Add(w.Data())
	}
	f.Add([]byte{})
	rng := rand.New(rand.NewPCG(6, 2))
	random := make([]byte, 64)
	for i := range random {
		random[i] = byte(rng.Uint32())
	}
	f.Add(random)

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bitread.New(data)
		paths, err := readFieldPaths(r, nil)
		if len(paths) > 8*len(data) {
			t.Fatalf("%d paths from %d bits", len(paths), 8*len(data))
		}
		clean := err == nil
		for i, p := range paths {
			if p.last < 0 || p.last >= maxPathDepth {
				t.Fatalf("path %d: last index %d, want 0 to %d", i, p.last, maxPathDepth-1)
			}
			if slices.ContainsFunc(p.idx[:p.last+1], func(v int32) bool { return v < 0 }) {
				clean = false
			}
		}

		applyErr := fx.state(t, nil).Apply(chainUpdate(data), nil)
		if applyErr != nil && !errors.Is(applyErr, demo.ErrBadDemo) {
			t.Fatalf("Apply: %v, want ErrBadDemo", applyErr)
		}
		if applyErr == nil && !clean {
			t.Fatalf("Apply accepted paths %v, which decode with %v", pathInts(paths), err)
		}
		if !clean {
			return
		}

		want := pathInts(paths)
		var w BitWriter
		w.FieldPaths(want...)
		again, err := readFieldPaths(bitread.New(w.Data()), nil)
		if got := pathInts(again); err != nil || !slices.EqualFunc(got, want, slices.Equal) {
			t.Fatalf("re-encoded %v decodes as %v, %v", want, got, err)
		}
	})
}

// FuzzApply applies a fuzzed svc_PacketEntities to a state that holds a
// Chain, a Pawn and a Thing: its entity data and updated entry count,
// whether it is a delta, whether it is in the older format, with PVS bits
// and an index list, and an alternate baseline unless altEntity is
// negative. Apply must succeed or fail with ErrBadDemo, without a panic.
// Every entity must then be where State.Entity and State.ByHandle find it,
// of one of the classes, and reading any of its fields must not panic, nor
// succeed with another class's Ref. Then, after success or error, the
// setup message, a full update, must apply again and leave the same
// entities and values as on a new State, so an error leaves nothing behind
// that a full update keeps, such as a damaged cached baseline. The seeds
// are valid creates, updates, leaves and deletes, in both formats and with
// alternate baselines. The spec's exit check runs it for 10 minutes:
//
//	go test ./entities -run '^$' -fuzz '^FuzzApply$' -fuzztime 10m -fuzzminimizetime 1s
func FuzzApply(f *testing.F) {
	fx := newFuzzFixture(f)
	for i, s := range applySeeds() {
		m := packetEntities(s.data, s.entries, s.delta, s.legacy, s.altEntity, s.altEntry)
		if err := fx.state(f, fx.baselines).Apply(m, fx.baselines); err != nil {
			f.Fatalf("seed %d: %v", i, err)
		}
		f.Add(s.data, s.entries, s.delta, s.legacy, s.altEntity, s.altEntry)
	}

	f.Fuzz(func(t *testing.T, data []byte, entries int32, delta, legacy bool, altEntity, altEntry int32) {
		s := fx.state(t, fx.baselines)
		err := s.Apply(packetEntities(data, entries, delta, legacy, altEntity, altEntry), fx.baselines)
		if err != nil && !errors.Is(err, demo.ErrBadDemo) {
			t.Fatalf("Apply: %v, want ErrBadDemo", err)
		}
		fx.check(t, s)
		if again := s.Apply(fx.setup, fx.baselines); again != nil {
			t.Fatalf("the setup message after a message that gave %v: %v", err, again)
		}
		fx.same(t, s, fx.state(t, fx.baselines))
	})
}

// Class ids of the fuzz fixture, and the width of an id in entity data.
const (
	fuzzPawn = iota
	fuzzThing
	fuzzProp
	fuzzChain
	fuzzIDBits = 3
)

// fuzzFixture is the class setup the fuzz targets share. Fields, by index:
//
//	Pawn, as in the state tests:
//	  0 m_iHealth, 1 m_angEyeAngles (QAnglePrecise),
//	  2 CBodyComponent, a pointer to Body: 0 m_cellX, 1 m_vecX,
//	  3 m_hController, 4 m_vecList (uint8s), 5 m_arr (two int32s)
//	Thing: 0 m_nValue
//	Prop: one field of each decoder kind Pawn and Chain lack; see propFields
//	Chain: 0 m_nValue, 1 m_pNext (a Chain pointer), 2 m_vec (int32s),
//	  3 m_arr (two inline Chains), so paths of any depth can be valid
//
// The instancebaseline table holds, by entry: 0 the Pawn baseline, 1 the
// Prop baseline, 2 an alternate Thing baseline, 3 an alternate Prop one.
type fuzzFixture struct {
	classes   *Classes
	baselines *stringtables.Table
	setup     *pb.CSVCMsg_PacketEntities // creates Chain 0, Pawn 1 and Thing 2
	refs      [][]namedRef               // by class id
}

type namedRef struct {
	name string
	Ref
}

// fuzzField describes a field for newFuzzFixture. Zero values are left
// unset.
type fuzzField struct {
	name, typ, node, enc, ser string
	bits                      int32
	low, high                 float32
	poly                      []string
}

func newFuzzFixture(tb testing.TB) *fuzzFixture {
	tb.Helper()
	var m pb.CSVCMsg_FlattenedSerializer
	syms := map[string]int32{}
	sym := func(s string) *int32 {
		i, ok := syms[s]
		if !ok {
			i = int32(len(m.Symbols))
			m.Symbols = append(m.Symbols, s)
			syms[s] = i
		}
		return proto.Int32(i)
	}
	optSym := func(s string) *int32 {
		if s == "" {
			return nil
		}
		return sym(s)
	}
	serializer := func(name string, fields ...fuzzField) {
		ps := &pb.ProtoFlattenedSerializerT{SerializerNameSym: sym(name), SerializerVersion: proto.Int32(0)}
		for _, f := range fields {
			pf := &pb.ProtoFlattenedSerializerFieldT{
				VarNameSym:    sym(f.name),
				VarTypeSym:    sym(f.typ),
				SendNodeSym:   optSym(f.node),
				VarEncoderSym: optSym(f.enc),
			}
			if f.bits != 0 {
				pf.BitCount = proto.Int32(f.bits)
			}
			if f.low != 0 {
				pf.LowValue = proto.Float32(f.low)
			}
			if f.high != 0 {
				pf.HighValue = proto.Float32(f.high)
			}
			if f.ser != "" {
				pf.FieldSerializerNameSym = sym(f.ser)
				pf.FieldSerializerVersion = proto.Int32(0)
			}
			for _, p := range f.poly {
				pf.PolymorphicTypes = append(pf.PolymorphicTypes, &pb.ProtoFlattenedSerializerFieldTPolymorphicFieldT{
					PolymorphicFieldSerializerNameSym: sym(p),
					PolymorphicFieldSerializerVersion: proto.Int32(0),
				})
			}
			ps.FieldsIndex = append(ps.FieldsIndex, int32(len(m.Fields)))
			m.Fields = append(m.Fields, pf)
		}
		m.Serializers = append(m.Serializers, ps)
	}
	origin := "m_skeletonInstance.m_vecOrigin"
	serializer("Body",
		fuzzField{name: "m_cellX", typ: "uint16", node: origin},
		fuzzField{name: "m_vecX", typ: "float32", node: origin},
	)
	serializer("Item",
		fuzzField{name: "m_nCount", typ: "int32"},
		fuzzField{name: "m_szLabel", typ: "CUtlString"},
	)
	serializer("Pawn",
		fuzzField{name: "m_iHealth", typ: "int32"},
		fuzzField{name: "m_angEyeAngles", typ: "QAngle", enc: "qangle_precise"},
		fuzzField{name: "CBodyComponent", typ: "CBodyComponent", ser: "Body"},
		fuzzField{name: "m_hController", typ: "CHandle< CBasePlayerController >"},
		fuzzField{name: "m_vecList", typ: "CUtlVector< uint8 >"},
		fuzzField{name: "m_arr", typ: "int32[2]"},
	)
	serializer("Thing", fuzzField{name: "m_nValue", typ: "uint32"})
	serializer("Prop",
		fuzzField{name: "m_bFlag", typ: "bool"},
		fuzzField{name: "m_nBig", typ: "int64"},
		fuzzField{name: "m_nWide", typ: "uint64"},
		fuzzField{name: "m_nFixed", typ: "uint64", enc: "fixed64"},
		fuzzField{name: "m_nSequence", typ: "HSequence"},
		fuzzField{name: "m_hOwner", typ: "CHandle< CBaseEntity >"},
		fuzzField{name: "m_szName", typ: "CUtlString"},
		fuzzField{name: "m_blob", typ: "CUtlBinaryBlock"},
		fuzzField{name: "m_flRaw", typ: "float32"},
		fuzzField{name: "m_flQuant", typ: "float32", bits: 8, high: 256},
		fuzzField{name: "m_flCoord", typ: "float32", enc: "coord"},
		fuzzField{name: "m_flNormal", typ: "float32", enc: "normal"},
		fuzzField{name: "m_flSimulationTime", typ: "float32"},
		fuzzField{name: "m_vecPos", typ: "Vector", enc: "coord"},
		fuzzField{name: "m_vecDir", typ: "Vector", enc: "normal"},
		fuzzField{name: "m_angA", typ: "QAngle", enc: "qangle"},
		fuzzField{name: "m_angB", typ: "QAngle", enc: "qangle", bits: 10},
		fuzzField{name: "m_angC", typ: "QAngle", enc: "qangle", bits: 32},
		fuzzField{name: "m_angD", typ: "QAngle", enc: "qangle_pitch_yaw", bits: 12},
		fuzzField{name: "m_vec2", typ: "Vector2D"},
		fuzzField{name: "m_vecQ", typ: "Vector", bits: 10, low: -100, high: 100},
		fuzzField{name: "m_pPoly", typ: "CBodyComponent", ser: "Body", poly: []string{"Item"}},
		fuzzField{name: "m_items", typ: "CUtlVectorEmbeddedNetworkVar< Item >", ser: "Item"},
		fuzzField{name: "m_hHandles", typ: "CHandle< CBaseEntity >[4]"},
	)
	serializer("Chain",
		fuzzField{name: "m_nValue", typ: "int32"},
		fuzzField{name: "m_pNext", typ: "Chain*", ser: "Chain"},
		fuzzField{name: "m_vec", typ: "CUtlVector< int32 >"},
		fuzzField{name: "m_arr", typ: "Chain[2]", ser: "Chain"},
	)
	sers, err := NewSerializers(&m)
	if err != nil {
		tb.Fatal(err)
	}
	if u := sers.Unclassified(); len(u) != 0 {
		tb.Fatalf("fields with no decoder: %v", u)
	}
	var info pb.CDemoClassInfo
	for id, name := range []string{fuzzPawn: "Pawn", fuzzThing: "Thing", fuzzProp: "Prop", fuzzChain: "Chain"} {
		info.Classes = append(info.Classes, &pb.CDemoClassInfoClassT{ClassId: proto.Int32(int32(id)), NetworkName: proto.String(name)})
	}
	classes, err := NewClasses(&info, sers)
	if err != nil {
		tb.Fatal(err)
	}
	if classes.IDBits() != fuzzIDBits {
		tb.Fatalf("class ids of %d bits, want %d", classes.IDBits(), fuzzIDBits)
	}

	item := func(key string, fields func(w *BitWriter)) *pb.CDemoStringTablesItemsT {
		return &pb.CDemoStringTablesItemsT{Str: proto.String(key), Data: fuzzBits(fields)}
	}
	var ts stringtables.Tables
	ts.ApplySnapshot(&pb.CDemoStringTables{Tables: []*pb.CDemoStringTablesTableT{{
		TableName: proto.String(stringtables.InstanceBaseline),
		Items: []*pb.CDemoStringTablesItemsT{
			item("0", func(w *BitWriter) {
				w.FieldPaths([]int{0}, []int{2}, []int{2, 0})
				w.VarInt(100)
				w.Bit(true)
				w.VarUint(34)
			}),
			item("2", func(w *BitWriter) {
				w.FieldPaths([]int{0}, []int{6})
				w.Bit(true)
				w.Str("base")
			}),
			item("1:0", func(w *BitWriter) {
				w.FieldPaths([]int{0})
				w.VarUint(2)
			}),
			item("2:0", func(w *BitWriter) {
				w.FieldPaths([]int{21}, []int{21, 1})
				w.Bit(true)
				w.UBitVar(1) // an Item
				w.Str("alt")
			}),
		},
	}}})

	fx := &fuzzFixture{classes: classes, baselines: ts.Get(stringtables.InstanceBaseline)}
	data, n := fuzzEntities(setupEntries()...)
	fx.setup = packetEntities(data, n, false, false, -1, 0)
	for c := range classes.All() {
		refs := fuzzRefs(c)
		if len(refs) == 0 {
			tb.Fatalf("class %s has no refs", c.Name)
		}
		fx.refs = append(fx.refs, refs)
	}
	return fx
}

// fuzzRefs resolves the fields of class c down to 4 path indices, with
// array elements 0, 1 and 3.
func fuzzRefs(c *Class) []namedRef {
	var refs []namedRef
	add := func(name string) {
		if r, err := c.Ref(name); err == nil {
			refs = append(refs, namedRef{name, r})
		}
	}
	var walk func(s *Serializer, prefix string, depth int)
	walk = func(s *Serializer, prefix string, depth int) {
		if depth >= 4 {
			return
		}
		for _, f := range s.Fields {
			name := prefix + f.FullName()
			add(name)
			switch f.Decoder.Kind {
			case Pointer, Table:
				walk(f.Serializer, name+".", depth+1)
			case FixedArray, DynamicArray:
				for _, i := range []int{0, 1, 3} {
					elem := fmt.Sprintf("%s.%d", name, i)
					add(elem)
					if k := f.Elem.Decoder.Kind; k == Pointer || k == Table {
						walk(f.Elem.Serializer, elem+".", depth+2)
					}
				}
			}
		}
	}
	walk(c.Serializer, "", 0)
	return refs
}

// state returns a new State after the setup message.
func (fx *fuzzFixture) state(tb testing.TB, baselines *stringtables.Table) *State {
	tb.Helper()
	s := NewState(fx.classes)
	if err := s.Apply(fx.setup, baselines); err != nil {
		tb.Fatalf("setup message: %v", err)
	}
	return s
}

// check fails unless every entity of s is where State.Entity and
// State.ByHandle find it, and of one of the classes. It reads every field
// it has a Ref for, and fails if a Ref of another class reads anything.
func (fx *fuzzFixture) check(t *testing.T, s *State) {
	t.Helper()
	for e := range s.All() {
		if e.Class == nil || fx.classes.ByID(e.Class.ID) != e.Class {
			t.Fatalf("entity %d has class %v, not one of the classes", e.Index, e.Class)
		}
		if s.Entity(e.Index) != e || e.Serial >= 1<<serialBits || s.ByHandle(e.Handle()) != e {
			t.Fatalf("entity %d, serial %d, handle %#x: Entity or ByHandle doesn't find it", e.Index, e.Serial, e.Handle())
		}
		for _, r := range fx.refs[e.Class.ID] {
			e.Get(r.Ref)
		}
		other := fx.refs[(e.Class.ID+1)%len(fx.refs)][0]
		if v, ok := e.Get(other.Ref); ok {
			t.Fatalf("entity %d, a %s, read %v with a Ref to another class's %s", e.Index, e.Class.Name, v, other.name)
		}
	}
}

// same fails unless got and want hold the same entities, with the same
// value or lack of one for every Ref.
func (fx *fuzzFixture) same(t *testing.T, got, want *State) {
	t.Helper()
	g, w := slices.Collect(got.All()), slices.Collect(want.All())
	if len(g) != len(w) {
		t.Fatalf("%d entities, want %d", len(g), len(w))
	}
	for i, ge := range g {
		we := w[i]
		if ge.Index != we.Index || ge.Class != we.Class || ge.Serial != we.Serial {
			t.Fatalf("entity %d is a %s of serial %d, want entity %d, a %s of serial %d",
				ge.Index, ge.Class.Name, ge.Serial, we.Index, we.Class.Name, we.Serial)
		}
		for _, r := range fx.refs[ge.Class.ID] {
			gv, gok := ge.Get(r.Ref)
			wv, wok := we.Get(r.Ref)
			if gv != wv || gok != wok {
				t.Fatalf("entity %d %s: %+v, %v; want %+v, %v", ge.Index, r.name, gv, gok, wv, wok)
			}
		}
	}
}

// packetEntities returns an svc_PacketEntities. The newer format sends
// non_transmitted_entities; the older one sends PVS bits with each update
// and appends an index list to delta messages.
func packetEntities(data []byte, entries int32, delta, legacy bool, altEntity, altEntry int32) *pb.CSVCMsg_PacketEntities {
	m := &pb.CSVCMsg_PacketEntities{
		UpdatedEntries: proto.Int32(entries),
		LegacyIsDelta:  proto.Bool(delta),
		EntityData:     data,
	}
	if legacy {
		m.HasPvsVisBitsDeprecated = proto.Uint32(1)
	} else {
		m.NonTransmittedEntities = &pb.CSVCMsg_PacketEntitiesNonTransmittedEntitiesT{}
	}
	if altEntity >= 0 {
		m.AlternateBaselines = []*pb.CSVCMsg_PacketEntitiesAlternateBaselineT{{
			EntityIndex:   proto.Int32(altEntity),
			BaselineIndex: proto.Int32(altEntry),
		}}
	}
	return m
}

// chainUpdate returns a delta message that updates entity 0 with the field
// path list and values in data. Its first byte is the entry's index delta
// and command, so data starts on a byte boundary.
func chainUpdate(data []byte) *pb.CSVCMsg_PacketEntities {
	return packetEntities(append([]byte{0}, data...), 1, true, false, -1, 0)
}

// A fuzzEntry writes one entry of entity data: the index delta, the
// command, and what follows.
type fuzzEntry func(w *BitWriter)

func fuzzCreate(skip uint32, class, serial uint64, fields func(w *BitWriter)) fuzzEntry {
	return func(w *BitWriter) {
		w.UBitVar(skip)
		w.Bits(cmdCreate, 2)
		w.Bits(class, fuzzIDBits)
		w.Bits(serial, serialBits)
		w.VarUint(0)
		fields(w)
	}
}

func fuzzUpdate(skip uint32, fields func(w *BitWriter)) fuzzEntry {
	return func(w *BitWriter) {
		w.UBitVar(skip)
		w.Bits(cmdUpdate, 2)
		fields(w)
	}
}

func fuzzRemove(skip uint32, cmd uint64) fuzzEntry {
	return func(w *BitWriter) {
		w.UBitVar(skip)
		w.Bits(cmd, 2)
	}
}

// fuzzEntities returns the entity data and entry count of entries.
func fuzzEntities(entries ...fuzzEntry) ([]byte, int32) {
	var w BitWriter
	for _, e := range entries {
		e(&w)
	}
	return w.Data(), int32(len(entries))
}

func fuzzBits(write func(w *BitWriter)) []byte {
	var w BitWriter
	write(&w)
	return w.Data()
}

// setupEntries create Chain 0, Pawn 1 and Thing 2.
func setupEntries() []fuzzEntry {
	return []fuzzEntry{
		fuzzCreate(0, fuzzChain, 1, func(w *BitWriter) {
			w.FieldPaths([]int{0}, []int{1}, []int{1, 0}, []int{2}, []int{2, 0}, []int{3, 0, 0})
			w.VarInt(1)
			w.Bit(true)
			w.VarInt(2)
			w.VarUint(1)
			w.VarInt(3)
			w.VarInt(4)
		}),
		fuzzCreate(0, fuzzPawn, 2, func(w *BitWriter) {
			w.FieldPaths([]int{1}, []int{2, 1}, []int{3}, []int{4}, []int{4, 0}, []int{4, 1})
			w.Bits(0b010, 3)     // yaw only
			w.Bits(3<<18, 20)    // 270 - 180
			w.Float32(352)       // m_vecX
			w.VarUint(1<<14 | 2) // controller: serial 1, index 2
			w.VarUint(2)         // list length
			w.VarUint(7)
			w.VarUint(8)
		}),
		fuzzCreate(0, fuzzThing, 3, func(w *BitWriter) {
			w.FieldPaths([]int{0})
			w.VarUint(3)
		}),
	}
}

// propFields sets every field of a Prop.
func propFields(w *BitWriter) {
	var paths [][]int
	for i := range 22 {
		paths = append(paths, []int{i})
	}
	w.FieldPaths(append(paths, []int{21, 0}, []int{22}, []int{22, 0, 1}, []int{23, 3})...)
	coord := func(intPart, frac32 uint64) { // positive, with both parts
		w.Bits(0b011, 3)
		w.Bits(intPart-1, 14)
		w.Bits(frac32, 5)
	}
	w.Bit(true)            // m_bFlag
	w.VarInt(-5)           // m_nBig
	w.VarUint(1 << 40)     // m_nWide
	w.Bits(0xDEADBEEF, 32) // m_nFixed
	w.Bits(0x01234567, 32)
	w.VarUint(0)         // m_nSequence: -1
	w.VarUint(3<<14 | 1) // m_hOwner
	w.Str("prop")        // m_szName
	w.VarUint(3)         // m_blob
	w.Bits(0x030201, 24)
	w.Float32(1.5) // m_flRaw
	w.Bits(200, 8) // m_flQuant
	coord(12, 16)  // m_flCoord: 12.5
	w.Bit(false)   // m_flNormal: +1
	w.Bits(2047, 11)
	w.VarUint(640) // m_flSimulationTime: 10 s
	w.Bits(0, 2)   // m_vecPos: 0, 3.25, 0
	coord(3, 8)
	w.Bits(0, 2)
	w.Bits(0b01, 2) // m_vecDir: x only, then z negative
	w.Bit(false)
	w.Bits(1023, 11)
	w.Bit(true)
	w.Bits(0b010, 3) // m_angA: yaw only
	coord(90, 0)
	for range 3 { // m_angB
		w.Bits(256, 10)
	}
	for range 3 { // m_angC
		w.Float32(90)
	}
	w.Bits(1024, 12) // m_angD
	w.Bits(2048, 12)
	w.Float32(1) // m_vec2
	w.Float32(2)
	for range 3 { // m_vecQ
		w.Bits(512, 10)
	}
	w.Bit(true) // m_pPoly: an Item
	w.UBitVar(1)
	w.VarInt(2)  // m_pPoly.m_nCount
	w.VarUint(1) // m_items
	w.Str("x")   // m_items.0.m_szLabel
	w.VarUint(5) // m_hHandles.3
}

// pathSeed is a FuzzFieldPaths seed: a field path list, written by ops if
// it isn't nil or by BitWriter.FieldPaths, and then values that suit
// entity 0, a Chain.
type pathSeed struct {
	paths  [][]int
	ops    func(w *BitWriter)
	values func(w *BitWriter)
}

func fieldPathSeeds() []pathSeed {
	ints := func(vs ...int64) func(w *BitWriter) {
		return func(w *BitWriter) {
			for _, v := range vs {
				w.VarInt(v)
			}
		}
	}
	return []pathSeed{
		{values: func(*BitWriter) {}},
		{ // flat, and an array
			paths: [][]int{{0}, {2}, {2, 0}, {2, 1}},
			values: func(w *BitWriter) {
				w.VarInt(7)
				w.VarUint(2)
				ints(1, 2)(w)
			},
		},
		{ // pointers to the deepest path, and back up
			paths: [][]int{{1}, {1, 1}, {1, 1, 1}, {1, 1, 1, 1}, {1, 1, 1, 1, 1}, {1, 1, 1, 1, 1, 1}, {1, 1, 1, 1, 1, 1, 0}, {1, 1, 0}, {3, 1, 0}},
			values: func(w *BitWriter) {
				for range 6 {
					w.Bit(true)
				}
				ints(-3, 4, 5)(w)
			},
		},
		{ // inline structs, arrays and pointers mixed
			paths: [][]int{{0}, {3, 0, 0}, {3, 1, 1}, {3, 1, 1, 3, 0, 0}, {3, 1, 2}, {3, 1, 2, 0}, {2}},
			values: func(w *BitWriter) {
				ints(1, 2)(w)
				w.Bit(true)
				ints(3)(w)
				w.VarUint(1)
				ints(4)(w)
				w.VarUint(0)
			},
		},
		{ // the ops that real lists use most
			paths: [][]int{{0}, {1}, {1, 0}, {2}, {2, 0}, {2, 2}, {2, 7}},
			ops: func(w *BitWriter) {
				w.op(opPlusOne)
				w.op(opPlusOne)
				w.op(opPushOneLeftDeltaZeroRightZero)
				w.op(opPopAllButOnePlusOne)
				w.op(opPushOneLeftDeltaZeroRightZero)
				w.op(opPlusTwo)
				w.op(opPlusN)
				w.UBitVarFP(0)
				w.op(opFinish)
			},
			values: func(w *BitWriter) {
				ints(9)(w)
				w.Bit(true)
				ints(8)(w)
				w.VarUint(8)
				ints(1, 2, 3)(w)
			},
		},
	}
}

// pathInts returns the indices of each path.
func pathInts(paths []fieldPath) [][]int {
	var out [][]int
	for _, p := range paths {
		var ints []int
		for _, i := range p.idx[:p.last+1] {
			ints = append(ints, int(i))
		}
		out = append(out, ints)
	}
	return out
}

// applyArgs are FuzzApply's arguments.
type applyArgs struct {
	data                []byte
	entries             int32
	delta, legacy       bool
	altEntity, altEntry int32
}

// applySeeds are valid messages to apply after the setup message.
func applySeeds() []applyArgs {
	seed := func(delta bool, entries ...fuzzEntry) applyArgs {
		data, n := fuzzEntities(entries...)
		return applyArgs{data: data, entries: n, delta: delta, altEntity: -1}
	}
	empty := func(w *BitWriter) { w.FieldPaths() }

	// The older format: entity 0 is unchanged, entity 2 changes, and an
	// index list follows.
	var w BitWriter
	w.UBitVar(0)
	w.Bits(cmdUpdate, 2)
	w.Bits(0b01, 2)
	w.UBitVar(1)
	w.Bits(cmdUpdate, 2)
	w.Bits(0b10, 2)
	w.FieldPaths([]int{0})
	w.VarUint(6)
	w.UBitVar(2)
	w.UBitVar(40)
	w.UBitVar(300)
	legacy := applyArgs{data: w.Data(), entries: 2, delta: true, legacy: true, altEntity: -1}

	altThing := seed(true, fuzzUpdate(2, empty))
	altThing.altEntity, altThing.altEntry = 2, 2
	altProp := seed(true, fuzzCreate(5, fuzzProp, 6, func(w *BitWriter) {
		w.FieldPaths([]int{21, 0})
		w.VarInt(4)
	}))
	altProp.altEntity, altProp.altEntry = 5, 3

	return []applyArgs{
		seed(true),
		seed(false, setupEntries()...),
		seed(true,
			fuzzUpdate(0, func(w *BitWriter) { // Chain 0
				w.FieldPaths([]int{1, 1}, []int{1, 1, 0}, []int{3, 1, 1}, []int{3, 1, 1, 3, 0, 0}, []int{3, 1, 2}, []int{3, 1, 2, 0})
				w.Bit(true)
				w.VarInt(5)
				w.Bit(true)
				w.VarInt(7)
				w.VarUint(1)
				w.VarInt(6)
			}),
			fuzzUpdate(0, func(w *BitWriter) { // Pawn 1
				w.FieldPaths([]int{0}, []int{2, 1}, []int{4}, []int{5, 1})
				w.VarInt(73)
				w.Float32(-352)
				w.VarUint(1)
				w.VarInt(-3)
			}),
			fuzzUpdate(0, func(w *BitWriter) { // Thing 2
				w.FieldPaths([]int{0})
				w.VarUint(9)
			}),
			fuzzCreate(1, fuzzProp, 4, propFields),
		),
		seed(true, fuzzRemove(1, cmdLeave), fuzzRemove(0, cmdDelete)),
		legacy,
		altThing,
		altProp,
	}
}
