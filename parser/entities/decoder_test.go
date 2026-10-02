package entities_test

import (
	"fmt"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/entities"
)

func TestDecoders(t *testing.T) {
	tests := []struct {
		f    field
		want string // describe's output
	}{
		{field{typ: "bool"}, "Bool"},
		{field{typ: "int8"}, "VarInt32"},
		{field{typ: "int16"}, "VarInt32"},
		{field{typ: "int32"}, "VarInt32"},
		{field{typ: "int64"}, "VarInt64"},
		{field{typ: "uint8"}, "VarUint32"},
		{field{typ: "uint32"}, "VarUint32"},
		{field{typ: "uint64"}, "VarUint64"},
		{field{typ: "uint64", encoder: "fixed64"}, "Fixed64"},
		{field{typ: "int32", encoder: "fixed64"}, "Unknown"},
		{field{typ: "GameTick_t"}, "VarInt32"},
		{field{typ: "Color", varSer: "color"}, "VarUint32 [color]"},
		{field{typ: "MoveType_t"}, "VarUint64"},
		{field{typ: "DecalMode_t"}, "VarUint64"},
		{field{typ: "loadout_slot_t"}, "VarUint64"},
		{field{typ: "EGrenadeThrowState"}, "VarUint64"},
		{field{typ: "NewEnum_t", encoder: "coord"}, "Unknown"},
		{field{typ: "CNewThing"}, "Unknown"},

		{field{typ: "HSequence"}, "MinusOne"},
		{field{typ: "HSequence", varSer: "minusone"}, "MinusOne [minusone]"},
		{field{typ: "int32", varSer: "minusone"}, "MinusOne [minusone]"},
		{field{typ: "int32", varSer: "ClampHealth"}, "VarInt32 [ClampHealth]"},
		{field{typ: "int32", varSer: "mystery"}, "Unknown [mystery]"},
		{field{typ: "int32", varSer: "simulationTimeSerializer"}, "Unknown [simulationTimeSerializer]"},

		{field{typ: "CUtlString"}, "String"},
		{field{typ: "CUtlSymbolLarge"}, "String"},
		{field{typ: "CGlobalSymbol"}, "String"},
		{field{typ: "CUtlBinaryBlock"}, "Bytes"},
		{field{typ: "char[64]"}, "String"},
		{field{typ: "char"}, "Unknown"},
		{field{typ: "CHandle< CBaseEntity >", varSer: "ehandle"}, "Handle [ehandle]"},
		{field{typ: "CGameSceneNodeHandle"}, "Handle"},
		{field{typ: "CStrongHandle< InfoForResourceTypeCModel >"}, "VarUint64"},
		{field{typ: "CWeakHandle< CBaseEntity >"}, "Unknown"},

		{field{typ: "float32"}, "Float32"},
		{field{typ: "float32", bits: 32}, "Float32"},
		{field{typ: "GameTime_t", bits: 32}, "Float32"},
		{field{typ: "float32", bits: 10, low: -25, high: 25, flags: 2}, "QuantizedFloat bits=10 low=-25 high=25 flags=2"},
		{field{typ: "CNetworkedQuantizedFloat", bits: 15, high: 1024, flags: 1, varSer: "posx"}, "QuantizedFloat bits=15 low=0 high=1024 flags=1 [posx]"},
		{field{typ: "CNetworkedQuantizedFloat", bits: 32, varSer: "posx"}, "Float32 [posx]"},
		{field{typ: "float32", encoder: "coord"}, "Coord"},
		{field{typ: "float32", encoder: "normal"}, "Normal"},
		{field{typ: "float32", encoder: "simtime"}, "SimTime"},
		{field{typ: "float32", encoder: "runetime"}, "Unknown"},
		{field{name: "m_flSimulationTime", typ: "float32"}, "SimTime"},
		{field{name: "m_flAnimTime", typ: "float32", varSer: "animTimeSerializer"}, "SimTime [animTimeSerializer]"},
		{field{typ: "float32", varSer: "simulationTimeSerializer"}, "SimTime [simulationTimeSerializer]"},
		{field{typ: "float32", varSer: "minusone"}, "Unknown [minusone]"},

		{field{typ: "Vector", bits: 32}, "Vector(3 Float32)"},
		{field{typ: "Vector", encoder: "coord"}, "Vector(3 Coord)"},
		{field{typ: "VectorWS", encoder: "coord"}, "Vector(3 Coord)"},
		{field{typ: "Vector", bits: 18, low: -4096, high: 4096, flags: 4}, "Vector(3 QuantizedFloat) bits=18 low=-4096 high=4096 flags=4"},
		{field{typ: "Vector", encoder: "normal"}, "VectorNormal"},
		{field{typ: "Vector", encoder: "qangle"}, "Unknown"},
		{field{typ: "Vector2D", bits: 32}, "Vector(2 Float32)"},
		{field{typ: "Vector2D", encoder: "normal"}, "Unknown"},
		{field{typ: "Vector4D", bits: 32}, "Vector(4 Float32)"},
		{field{typ: "Quaternion", bits: 32}, "Vector(4 Float32)"},

		{field{typ: "QAngle", encoder: "qangle"}, "QAngleCoord"},
		{field{typ: "QAngle", encoder: "qangle", bits: 13}, "QAngleBits bits=13"},
		{field{typ: "QAngle", encoder: "qangle", bits: 32}, "QAngleFloat"},
		{field{typ: "QAngle", encoder: "qangle_precise"}, "QAnglePrecise"},
		{field{typ: "QAngle", encoder: "qangle_pitch_yaw", bits: 16}, "QAnglePitchYaw bits=16"},
		{field{typ: "QAngle", encoder: "qangle_pitch_yaw"}, "Unknown"},
		{field{typ: "QAngle"}, "Unknown"},

		{field{typ: "int32[64]"}, "FixedArray(64) of VarInt32"},
		{field{typ: "CHandle< CBaseEntity >[10]", varSer: "ehandle"}, "FixedArray(10) of Handle [ehandle]"},
		{field{typ: "Vector[8]", encoder: "coord"}, "FixedArray(8) of Vector(3 Coord)"},
		{field{typ: "CNetworkUtlVectorBase< CHandle< CBaseEntity > >"}, "DynamicArray of Handle"},
		{field{typ: "CNetworkUtlVectorBase< QAngle >", encoder: "qangle", bits: 13}, "DynamicArray of QAngleBits bits=13"},
		{field{typ: "CUtlVector< uint8 >"}, "DynamicArray of VarUint32"},
		{field{typ: "CNetworkUtlVectorBase< CTransform >"}, "DynamicArray of Unknown"},
		{field{typ: "CUtlVectorEmbeddedNetworkVar< Rec >", ser: "Nested"}, "DynamicArray of Table Nested/0"},
		{field{typ: "Rec[4]", ser: "Nested"}, "FixedArray(4) of Table Nested/0"},

		{field{typ: "CThing*", ser: "Nested"}, "Pointer Nested/0"},
		{field{typ: "CBodyComponent", ser: "Nested"}, "Pointer Nested/0"},
		{field{typ: "CRules*", ser: "Nested", poly: []string{"A", "B"}}, "Pointer Nested/0 |A/0 |B/0"},
		{field{typ: "CThing*"}, "Unknown"},

		{field{typ: "CTransform", bits: 32}, "Unknown"},
		{field{typ: "int32[x]"}, "Unknown"},
		{field{typ: "int32[0]"}, "Unknown"},
		{field{typ: "int32]"}, "Unknown"},
		{field{typ: "CHandle< CBaseEntity"}, "Unknown"},
	}
	for _, tc := range tests {
		name := fmt.Sprintf("%s/%s/%s/%d", tc.f.typ, tc.f.encoder, tc.f.varSer, tc.f.bits)
		t.Run(name, func(t *testing.T) {
			if tc.f.name == "" {
				tc.f.name = "m_x"
			}
			var b flat
			for _, s := range []string{"Nested", "A", "B"} {
				b.serializer(s, 0)
			}
			b.serializer("S", 0, b.field(tc.f))
			f := b.build(t).Lookup("S").Fields[0]
			if got := describe(f); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// describe returns f's decoder with the parameters it uses, its nested
// serializers, and its var serializer, recursing into array elements.
func describe(f *entities.Field) string {
	s := f.Decoder.String()
	switch d := f.Decoder; {
	case d.Kind == entities.QuantizedFloat || d.Kind == entities.Vector && d.Component == entities.QuantizedFloat:
		s += fmt.Sprintf(" bits=%d low=%g high=%g flags=%d", f.BitCount, f.LowValue, f.HighValue, f.EncodeFlags)
	case d.Kind == entities.QAngleBits || d.Kind == entities.QAnglePitchYaw:
		s += fmt.Sprintf(" bits=%d", f.BitCount)
	case d.Kind == entities.Pointer || d.Kind == entities.Table:
		s += fmt.Sprintf(" %s/%d", f.Serializer.Name, f.Serializer.Version)
		for _, p := range f.Polymorphic {
			s += fmt.Sprintf(" |%s/%d", p.Name, p.Version)
		}
	case d.Kind == entities.FixedArray || d.Kind == entities.DynamicArray:
		return s + " of " + describe(f.Elem)
	}
	if f.VarSerializer != "" {
		s += " [" + f.VarSerializer + "]"
	}
	return s
}

func TestKindString(t *testing.T) {
	for k, want := range map[entities.Kind]string{
		entities.Unknown:        "Unknown",
		entities.QuantizedFloat: "QuantizedFloat",
		entities.Table:          "Table",
		entities.Table + 1:      fmt.Sprintf("Kind(%d)", entities.Table+1),
	} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
