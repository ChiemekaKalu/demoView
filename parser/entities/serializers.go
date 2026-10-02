// Package entities decodes a demo's entities: the classes from
// DEM_ClassInfo, the field layout of each from DEM_SendTables, and the
// entity state that svc_PacketEntities messages build.
//
// A serializer is an ordered list of fields. Structs embedded in a class are
// flattened into it, so most fields are plain values; Field.SendNode says
// which embedded struct a field came from. Pointers, components and arrays
// nest: an entity field path's first index picks a field of the class's
// serializer, and each further index steps into Field.Elem or
// Field.Serializer, as the field's Decoder.Kind says.
//
// State holds the entities. To read a field, resolve its name once with
// Class.Ref, then read it from any entity of that class with Entity.Get.
// A player pawn's x coordinate, for example, is the WorldCoord of its
// "CBodyComponent.m_skeletonInstance.m_vecOrigin.m_cellX" and "...m_vecX";
// its yaw is element 1 of m_angEyeAngles, in degrees counterclockwise from
// +x; and State.ByHandle of its m_hController is its controller.
package entities

import (
	"encoding/binary"
	"fmt"
	"iter"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// Field is one field of a serializer. Serializers that list the same field
// share one *Field, so treat it as read-only.
type Field struct {
	Name          string // var name, such as "m_iHealth"
	Type          string // var type, such as "CNetworkUtlVectorBase< CHandle< CBaseEntity > >"
	SendNode      string // the embedded struct it came from, such as "m_skeletonInstance.m_vecOrigin", or ""
	Encoder       string // var encoder, such as "coord", or ""
	VarSerializer string // newer demos only: a var serializer, such as "minusone", or ""

	BitCount    int // 0 if not given
	LowValue    float32
	HighValue   float32
	EncodeFlags int32

	Decoder Decoder

	// Elem is an array's element. It repeats the array's name, encoder and
	// parameters, with the element's type.
	Elem *Field
	// Serializer is the nested serializer of a Pointer or Table, or of an
	// array of them.
	Serializer *Serializer
	// Polymorphic lists the other serializers a polymorphic pointer may
	// hold. Entity data picks one with a UBitVar after the presence bit: 0
	// for Serializer, or i for Polymorphic[i-1].
	Polymorphic []*Serializer

	quant quantizer // for QuantizedFloat and its vectors
}

// FullName returns the field's name prefixed with its send node, such as
// "m_skeletonInstance.m_vecOrigin.m_cellX".
func (f *Field) FullName() string {
	if f.SendNode == "" {
		return f.Name
	}
	return f.SendNode + "." + f.Name
}

// classified reports whether f and its elements all have a decoder.
func (f *Field) classified() bool {
	for ; f != nil; f = f.Elem {
		if f.Decoder.Kind == Unknown {
			return false
		}
	}
	return true
}

// Serializer is the field layout of an entity class or a nested type.
type Serializer struct {
	Name    string
	Version int
	Fields  []*Field
}

// FieldIndex returns the index of the field with the given full name, or -1.
func (s *Serializer) FieldIndex(name string) int {
	for i, f := range s.Fields {
		if f.FullName() == name {
			return i
		}
	}
	return -1
}

type serializerKey struct {
	name    string
	version int
}

// Serializers is the set of serializers from one DEM_SendTables.
type Serializers struct {
	list   []*Serializer // in message order
	fields []*Field      // in message order
	byKey  map[serializerKey]*Serializer
	byName map[string]*Serializer // highest version
}

// Lookup returns the serializer with the given name, or nil. A class's
// serializer has the class's network name. If there are several versions,
// it returns the highest; in the fixture demos only body components have
// more than one.
func (s *Serializers) Lookup(name string) *Serializer { return s.byName[name] }

// Get returns the serializer with the given name and version, or nil.
func (s *Serializers) Get(name string, version int) *Serializer {
	return s.byKey[serializerKey{name, version}]
}

// Len returns the number of serializers.
func (s *Serializers) Len() int { return len(s.list) }

// All yields every serializer in message order.
func (s *Serializers) All() iter.Seq[*Serializer] {
	return func(yield func(*Serializer) bool) {
		for _, ser := range s.list {
			if !yield(ser) {
				return
			}
		}
	}
}

// Unclassified returns the fields, in message order, that have no decoder
// themselves or in their elements. Entity data can't be read past one.
func (s *Serializers) Unclassified() []*Field {
	var out []*Field
	for _, f := range s.fields {
		if !f.classified() {
			out = append(out, f)
		}
	}
	return out
}

// DecodeSendTables returns the serializers in a DEM_SendTables, whose data
// is a varint length and then a CSVCMsg_FlattenedSerializer of that length.
// Malformed data gives an error wrapping demo.ErrBadDemo.
func DecodeSendTables(m *pb.CDemoSendTables) (*Serializers, error) {
	data := m.GetData()
	n, k := binary.Uvarint(data)
	if k <= 0 || n != uint64(len(data)-k) {
		return nil, fmt.Errorf("%w: send tables: length prefix doesn't match %d bytes of data", demo.ErrBadDemo, len(data))
	}
	var fs pb.CSVCMsg_FlattenedSerializer
	if err := proto.Unmarshal(data[k:], &fs); err != nil {
		return nil, fmt.Errorf("%w: send tables: %v", demo.ErrBadDemo, err)
	}
	return NewSerializers(&fs)
}

// NewSerializers builds the serializers in m. A symbol, field or serializer
// reference that doesn't resolve, or a repeated serializer, gives an error
// wrapping demo.ErrBadDemo. A field whose decoder can't be chosen is not an
// error; Unclassified lists it.
func NewSerializers(m *pb.CSVCMsg_FlattenedSerializer) (*Serializers, error) {
	b := builder{syms: m.GetSymbols()}
	s := &Serializers{
		byKey:  make(map[serializerKey]*Serializer, len(m.GetSerializers())),
		byName: make(map[string]*Serializer, len(m.GetSerializers())),
	}
	for i, ps := range m.GetSerializers() {
		ser := &Serializer{Name: b.sym(ps.SerializerNameSym, "name"), Version: int(ps.GetSerializerVersion())}
		key := serializerKey{ser.Name, ser.Version}
		switch {
		case b.err != nil:
			return nil, badf("serializer %d: %v", i, b.err)
		case s.byKey[key] != nil:
			return nil, badf("serializer %d: %s version %d is repeated", i, ser.Name, ser.Version)
		}
		s.byKey[key] = ser
		if prev := s.byName[ser.Name]; prev == nil || ser.Version > prev.Version {
			s.byName[ser.Name] = ser
		}
		s.list = append(s.list, ser)
	}

	s.fields = make([]*Field, len(m.GetFields()))
	for i, pf := range m.GetFields() {
		if s.fields[i] = b.field(s, pf); b.err != nil {
			return nil, badf("field %d: %v", i, b.err)
		}
	}

	for i, ps := range m.GetSerializers() {
		ser := s.list[i]
		ser.Fields = make([]*Field, len(ps.GetFieldsIndex()))
		for j, fi := range ps.GetFieldsIndex() {
			if fi < 0 || int(fi) >= len(s.fields) {
				return nil, badf("serializer %s/%d: field index %d of %d fields", ser.Name, ser.Version, fi, len(s.fields))
			}
			ser.Fields[j] = s.fields[fi]
		}
	}
	return s, nil
}

// builder resolves a FlattenedSerializer's references. Its first error
// sticks, and later calls return zero values.
type builder struct {
	syms []string
	err  error
}

// sym returns symbol *i, which must be given. what names it for errors.
func (b *builder) sym(i *int32, what string) string {
	switch {
	case b.err != nil:
	case i == nil:
		b.err = fmt.Errorf("no %s", what)
	case *i < 0 || int(*i) >= len(b.syms):
		b.err = fmt.Errorf("%s: symbol %d of %d", what, *i, len(b.syms))
	default:
		return b.syms[*i]
	}
	return ""
}

// optSym returns symbol *i, or "" if i is nil.
func (b *builder) optSym(i *int32, what string) string {
	if i == nil {
		return ""
	}
	return b.sym(i, what)
}

// field builds pf, resolving its serializers in s.
func (b *builder) field(s *Serializers, pf *pb.ProtoFlattenedSerializerFieldT) *Field {
	f := &Field{
		Name:          b.sym(pf.VarNameSym, "name"),
		Type:          b.sym(pf.VarTypeSym, "type"),
		SendNode:      b.optSym(pf.SendNodeSym, "send node"),
		Encoder:       b.optSym(pf.VarEncoderSym, "encoder"),
		VarSerializer: b.optSym(pf.VarSerializerSym, "var serializer"),
		BitCount:      int(pf.GetBitCount()),
		LowValue:      pf.GetLowValue(),
		HighValue:     pf.GetHighValue(),
		EncodeFlags:   pf.GetEncodeFlags(),
	}
	if pf.FieldSerializerNameSym != nil {
		f.Serializer = b.serializer(s, pf.FieldSerializerNameSym, pf.GetFieldSerializerVersion())
	}
	for _, pt := range pf.GetPolymorphicTypes() {
		f.Polymorphic = append(f.Polymorphic, b.serializer(s, pt.PolymorphicFieldSerializerNameSym, pt.GetPolymorphicFieldSerializerVersion()))
	}
	if b.err != nil {
		return nil
	}
	classify(f, false)
	return f
}

// serializer returns the serializer in s named by symbol *name.
func (b *builder) serializer(s *Serializers, name *int32, version int32) *Serializer {
	n := b.sym(name, "serializer name")
	if b.err != nil {
		return nil
	}
	ser := s.Get(n, int(version))
	if ser == nil {
		b.err = fmt.Errorf("no serializer %s version %d", n, version)
	}
	return ser
}

// badf returns an error wrapping demo.ErrBadDemo about a FlattenedSerializer.
func badf(format string, args ...any) error {
	return fmt.Errorf("%w: flattened serializer: %s", demo.ErrBadDemo, fmt.Sprintf(format, args...))
}
