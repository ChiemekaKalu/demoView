package entities_test

import (
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// flat builds a CSVCMsg_FlattenedSerializer, adding symbols as they are
// used.
type flat struct {
	msg  pb.CSVCMsg_FlattenedSerializer
	syms map[string]int32
}

// field describes a field for flat.field. Zero values are left unset.
type field struct {
	name, typ, node, encoder, varSer string
	bits, flags                      int32
	low, high                        float32
	ser                              string
	serVersion                       int32
	poly                             []string // version 0 each
}

func (b *flat) sym(s string) *int32 {
	if b.syms == nil {
		b.syms = map[string]int32{}
	}
	i, ok := b.syms[s]
	if !ok {
		i = int32(len(b.msg.Symbols))
		b.msg.Symbols = append(b.msg.Symbols, s)
		b.syms[s] = i
	}
	return proto.Int32(i)
}

func (b *flat) optSym(s string) *int32 {
	if s == "" {
		return nil
	}
	return b.sym(s)
}

// field adds f and returns its index.
func (b *flat) field(f field) int32 {
	pf := &pb.ProtoFlattenedSerializerFieldT{
		VarNameSym:       b.sym(f.name),
		VarTypeSym:       b.sym(f.typ),
		SendNodeSym:      b.optSym(f.node),
		VarEncoderSym:    b.optSym(f.encoder),
		VarSerializerSym: b.optSym(f.varSer),
	}
	if f.bits != 0 {
		pf.BitCount = proto.Int32(f.bits)
	}
	if f.flags != 0 {
		pf.EncodeFlags = proto.Int32(f.flags)
	}
	if f.low != 0 {
		pf.LowValue = proto.Float32(f.low)
	}
	if f.high != 0 {
		pf.HighValue = proto.Float32(f.high)
	}
	if f.ser != "" {
		pf.FieldSerializerNameSym = b.sym(f.ser)
		pf.FieldSerializerVersion = proto.Int32(f.serVersion)
	}
	for _, p := range f.poly {
		pf.PolymorphicTypes = append(pf.PolymorphicTypes, &pb.ProtoFlattenedSerializerFieldTPolymorphicFieldT{
			PolymorphicFieldSerializerNameSym: b.sym(p),
			PolymorphicFieldSerializerVersion: proto.Int32(0),
		})
	}
	b.msg.Fields = append(b.msg.Fields, pf)
	return int32(len(b.msg.Fields) - 1)
}

// serializer adds a serializer with the fields at the given indexes.
func (b *flat) serializer(name string, version int32, fields ...int32) {
	b.msg.Serializers = append(b.msg.Serializers, &pb.ProtoFlattenedSerializerT{
		SerializerNameSym: b.sym(name),
		SerializerVersion: proto.Int32(version),
		FieldsIndex:       fields,
	})
}

func (b *flat) build(t *testing.T) *entities.Serializers {
	t.Helper()
	s, err := entities.NewSerializers(&b.msg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSerializers(t *testing.T) {
	var b flat
	health := b.field(field{name: "m_iHealth", typ: "int32"})
	body := b.field(field{name: "CBodyComponent", typ: "CBodyComponent", ser: "Body", serVersion: 1})
	cellX := b.field(field{name: "m_cellX", typ: "uint16", node: "m_skeletonInstance.m_vecOrigin"})
	b.serializer("Pawn", 0, health, body)
	b.serializer("Other", 0, health)
	b.serializer("Body", 1, cellX)
	b.serializer("Body", 0)
	s := b.build(t)

	var names []string
	for ser := range s.All() {
		names = append(names, ser.Name)
	}
	if want := []string{"Pawn", "Other", "Body", "Body"}; s.Len() != 4 || !slices.Equal(names, want) {
		t.Errorf("Len %d, All %v; want 4, %v", s.Len(), names, want)
	}

	pawn, other := s.Lookup("Pawn"), s.Lookup("Other")
	if pawn.Fields[0] != other.Fields[0] {
		t.Error("serializers listing the same field don't share it")
	}
	if got := s.Lookup("Body"); got == nil || got.Version != 1 {
		t.Errorf("Lookup(Body) = %v, want version 1", got)
	}
	if got := s.Get("Body", 0); got == nil || got.Version != 0 || len(got.Fields) != 0 {
		t.Errorf("Get(Body, 0) = %v", got)
	}
	if s.Get("Body", 2) != nil || s.Lookup("Nope") != nil {
		t.Error("found a serializer that isn't there")
	}

	f := pawn.Fields[1]
	if f.Decoder.Kind != entities.Pointer || f.Serializer != s.Get("Body", 1) {
		t.Errorf("CBodyComponent is %s of %v, want a pointer to Body/1", f.Decoder, f.Serializer)
	}
	cell := f.Serializer.Fields[0]
	if got := cell.FullName(); got != "m_skeletonInstance.m_vecOrigin.m_cellX" {
		t.Errorf("FullName = %q", got)
	}
	if got := f.Serializer.FieldIndex("m_skeletonInstance.m_vecOrigin.m_cellX"); got != 0 {
		t.Errorf("FieldIndex(full name) = %d, want 0", got)
	}
	if got := f.Serializer.FieldIndex("m_cellX"); got != -1 {
		t.Errorf("FieldIndex(bare name) = %d, want -1", got)
	}
	if got := pawn.FieldIndex("m_iHealth"); got != 0 {
		t.Errorf("FieldIndex(m_iHealth) = %d, want 0", got)
	}
	if u := s.Unclassified(); len(u) != 0 {
		t.Errorf("Unclassified = %v, want none", u)
	}
}

func TestUnclassified(t *testing.T) {
	var b flat
	b.serializer("S", 0,
		b.field(field{name: "a", typ: "int32"}),
		b.field(field{name: "b", typ: "CTransform"}),
		b.field(field{name: "c", typ: "CNetworkUtlVectorBase< CTransform >"}),
		b.field(field{name: "d", typ: "float32", encoder: "mystery"}),
	)
	var got []string
	for _, f := range b.build(t).Unclassified() {
		got = append(got, f.Name)
	}
	if want := []string{"b", "c", "d"}; !slices.Equal(got, want) {
		t.Errorf("Unclassified = %v, want %v", got, want)
	}
}

func TestNewSerializersErrors(t *testing.T) {
	tests := []struct {
		name  string
		build func(b *flat)
		want  string
	}{
		{"name symbol out of range", func(b *flat) {
			b.serializer("S", 0, b.field(field{name: "m_x", typ: "int32"}))
			b.msg.Fields[0].VarNameSym = proto.Int32(99)
		}, "field 0: name: symbol 99 of"},
		{"no type", func(b *flat) {
			b.serializer("S", 0, b.field(field{name: "m_x", typ: "int32"}))
			b.msg.Fields[0].VarTypeSym = nil
		}, "field 0: no type"},
		{"negative encoder symbol", func(b *flat) {
			b.serializer("S", 0, b.field(field{name: "m_x", typ: "int32", encoder: "coord"}))
			b.msg.Fields[0].VarEncoderSym = proto.Int32(-1)
		}, "field 0: encoder: symbol -1 of"},
		{"no serializer name", func(b *flat) {
			b.serializer("S", 0)
			b.msg.Serializers[0].SerializerNameSym = nil
		}, "serializer 0: no name"},
		{"repeated serializer", func(b *flat) {
			b.serializer("S", 3)
			b.serializer("S", 3)
		}, "serializer 1: S version 3 is repeated"},
		{"field index out of range", func(b *flat) {
			b.serializer("S", 0, b.field(field{name: "m_x", typ: "int32"}), 1)
		}, "serializer S/0: field index 1 of 1 fields"},
		{"missing field serializer", func(b *flat) {
			b.serializer("S", 0, b.field(field{name: "m_p", typ: "CThing*", ser: "Thing", serVersion: 2}))
			b.serializer("Thing", 0)
		}, "field 0: no serializer Thing version 2"},
		{"missing polymorphic serializer", func(b *flat) {
			b.serializer("S", 0, b.field(field{name: "m_p", typ: "CThing*", ser: "S", poly: []string{"Gone"}}))
		}, "field 0: no serializer Gone version 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b flat
			tc.build(&b)
			_, err := entities.NewSerializers(&b.msg)
			if !errors.Is(err, demo.ErrBadDemo) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want ErrBadDemo with %q", err, tc.want)
			}
		})
	}
}

func TestDecodeSendTables(t *testing.T) {
	var b flat
	b.serializer("S", 0, b.field(field{name: "m_x", typ: "int32"}))
	body, err := proto.Marshal(&b.msg)
	if err != nil {
		t.Fatal(err)
	}
	prefixed := func(n int, body []byte) []byte {
		return append(binary.AppendUvarint(nil, uint64(n)), body...)
	}

	s, err := entities.DecodeSendTables(&pb.CDemoSendTables{Data: prefixed(len(body), body)})
	if err != nil {
		t.Fatal(err)
	}
	if ser := s.Lookup("S"); ser == nil || len(ser.Fields) != 1 || ser.Fields[0].Name != "m_x" {
		t.Errorf("Lookup(S) = %v", ser)
	}

	for name, data := range map[string][]byte{
		"empty":        nil,
		"short prefix": prefixed(len(body)-1, body),
		"long prefix":  prefixed(len(body)+1, body),
		"bad varint":   {0xFF},
		"bad protobuf": prefixed(2, []byte{0x0A, 0x05}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := entities.DecodeSendTables(&pb.CDemoSendTables{Data: data})
			if !errors.Is(err, demo.ErrBadDemo) {
				t.Fatalf("got %v, want ErrBadDemo", err)
			}
		})
	}
}
