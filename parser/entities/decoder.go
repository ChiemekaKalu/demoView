package entities

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Kind says how a field is encoded in entity data. Field.BitCount,
// LowValue, HighValue and EncodeFlags are the parameters of the kinds that
// mention them.
type Kind uint8

const (
	Unknown Kind = iota // no decoder chosen; see Serializers.Unclassified

	Bool      // one bit
	VarInt32  // zigzag varint
	VarInt64  // zigzag varint
	VarUint32 // varint
	VarUint64 // varint; also enums and resource handles
	Fixed64   // 64 bits (encoder fixed64)
	MinusOne  // varint of the value plus one, so -1 is 0
	Handle    // entity handle, a varint
	String    // zero-terminated
	Bytes     // a varint length, then that many bytes

	Float32        // 32-bit IEEE float (bit count 0 or 32)
	QuantizedFloat // BitCount bits spanning LowValue to HighValue, per EncodeFlags
	Coord          // bit coord (encoder coord)
	Normal         // bit normal (encoder normal)
	SimTime        // simulation time in ticks

	Vector       // Decoder.Count components, each a Decoder.Component
	VectorNormal // unit 3-vector (encoder normal)

	QAngleCoord    // encoder qangle, bit count 0
	QAngleBits     // encoder qangle: three angles of BitCount bits
	QAngleFloat    // encoder qangle, bit count 32
	QAnglePrecise  // encoder qangle_precise
	QAnglePitchYaw // encoder qangle_pitch_yaw: pitch and yaw of BitCount bits

	FixedArray   // Decoder.Count elements, each a Field.Elem one path level down
	DynamicArray // a varint length; elements are Field.Elem one path level down
	Pointer      // a presence bit; Field.Serializer's fields are one path level down
	Table        // an inline struct: Field.Serializer's fields are one path level down
)

var kindNames = [...]string{
	Unknown:        "Unknown",
	Bool:           "Bool",
	VarInt32:       "VarInt32",
	VarInt64:       "VarInt64",
	VarUint32:      "VarUint32",
	VarUint64:      "VarUint64",
	Fixed64:        "Fixed64",
	MinusOne:       "MinusOne",
	Handle:         "Handle",
	String:         "String",
	Bytes:          "Bytes",
	Float32:        "Float32",
	QuantizedFloat: "QuantizedFloat",
	Coord:          "Coord",
	Normal:         "Normal",
	SimTime:        "SimTime",
	Vector:         "Vector",
	VectorNormal:   "VectorNormal",
	QAngleCoord:    "QAngleCoord",
	QAngleBits:     "QAngleBits",
	QAngleFloat:    "QAngleFloat",
	QAnglePrecise:  "QAnglePrecise",
	QAnglePitchYaw: "QAnglePitchYaw",
	FixedArray:     "FixedArray",
	DynamicArray:   "DynamicArray",
	Pointer:        "Pointer",
	Table:          "Table",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// Decoder is the decoder chosen for a field.
type Decoder struct {
	Kind      Kind
	Count     int  // FixedArray: elements; Vector: components
	Component Kind // Vector: Float32, QuantizedFloat or Coord
}

func (d Decoder) String() string {
	switch d.Kind {
	case Vector:
		return fmt.Sprintf("Vector(%d %s)", d.Count, d.Component)
	case FixedArray:
		return fmt.Sprintf("FixedArray(%d)", d.Count)
	}
	return d.Kind.String()
}

// scalars are the types read as one plain value. Named integer types take
// the kind of the integer they wrap.
var scalars = map[string]Kind{
	"bool":   Bool,
	"int8":   VarInt32,
	"int16":  VarInt32,
	"int32":  VarInt32,
	"int64":  VarInt64,
	"uint8":  VarUint32,
	"uint16": VarUint32,
	"uint32": VarUint32,
	"uint64": VarUint64,

	"CUtlString":      String,
	"CUtlSymbolLarge": String,
	"CUtlBinaryBlock": Bytes,
	"CGlobalSymbol":   String,

	"CEntityHandle":        Handle,
	"CGameSceneNodeHandle": Handle,
	"HSequence":            MinusOne,

	"CEntityIndex":       VarInt32,
	"CPlayerSlot":        VarInt32,
	"GameTick_t":         VarInt32,
	"AttachmentHandle_t": VarUint32,
	"CUtlStringToken":    VarUint32,
	"Color":              VarUint32,
	"WorldGroupId_t":     VarUint32,
	"ResourceId_t":       VarUint64,

	// Enums that enumName doesn't recognize.
	"BloodType":               VarUint64,
	"CSPlayerState":           VarUint64,
	"CSWeaponMode":            VarUint64,
	"PlayerConnectedState":    VarUint64,
	"QuestProgress::Reason":   VarUint64,
	"ShatterPanelMode":        VarUint64,
	"WeaponGameplayAnimState": VarUint64,
}

// templates are the template types read as one plain value.
var templates = map[string]Kind{
	"CHandle":       Handle,
	"CStrongHandle": VarUint64,
}

// dynamicArrays are the template types that hold a variable-length array.
var dynamicArrays = map[string]bool{
	"CNetworkUtlVectorBase":        true,
	"CUtlVector":                   true,
	"CUtlVectorEmbeddedNetworkVar": true,
}

var floats = map[string]bool{
	"float32":                  true,
	"CNetworkedQuantizedFloat": true,
	"GameTime_t":               true,
}

// vectors maps vector types to their number of float components.
var vectors = map[string]int{
	"Vector":     3,
	"VectorWS":   3,
	"Vector2D":   2,
	"Vector4D":   4,
	"Quaternion": 4,
}

// simTimeNames are fields that hold a simulation time. Older demos give
// them no encoder or var serializer to say so.
var simTimeNames = map[string]bool{
	"m_flSimulationTime": true,
	"m_flAnimTime":       true,
}

// simTimeSerializers are the var serializers that newer demos give
// simulation times.
var simTimeSerializers = map[string]bool{
	"simulationTimeSerializer":             true,
	"animTimeSerializer":                   true,
	"funcRotatingSimulationTimeSerializer": true,
}

// varSerializerHints are var serializers that leave the wire format to the
// field's type and parameters: their names say what the value means or how
// the server prepares it, not how it is encoded. The 2024 fixture sends the
// same fields, typed and parameterized the same, without them, and entity
// data decodes the same either way.
var varSerializerHints = map[string]bool{
	"ClampHealth":              true,
	"angle_normalize_positive": true,
	"cellx":                    true,
	"celly":                    true,
	"cellz":                    true,
	"color":                    true,
	"ehandle":                  true,
	"fish_pos_x":               true,
	"fish_pos_y":               true,
	"fish_pos_z":               true,
	"gameSceneNode":            true,
	"gameSceneNodeStepSimulationAnglesSerializer": true,
	"posx": true,
	"posy": true,
	"posz": true,
}

// classify chooses f's decoder from its type, encoder and var serializer,
// and describes its elements if it is an array. A struct that is an array
// element is inline (Table); anywhere else it is a separate object
// (Pointer), since structs embedded in a class are flattened into its
// fields.
func classify(f *Field, elem bool) {
	t, ok := parseType(f.Type)
	switch {
	case !ok:
	case t.count > 0 && t.base != "char":
		f.Decoder = Decoder{Kind: FixedArray, Count: t.count}
		f.Elem = element(f, t.elem)
	case dynamicArrays[t.base]:
		f.Decoder = Decoder{Kind: DynamicArray}
		f.Elem = element(f, t.arg)
	case f.Serializer != nil:
		f.Decoder.Kind = Pointer
		if elem && !t.pointer {
			f.Decoder.Kind = Table
		}
	default:
		f.Decoder = valueDecoder(f, t)
		if d := f.Decoder; d.Kind == QuantizedFloat || d.Kind == Vector && d.Component == QuantizedFloat {
			f.quant = newQuantizer(f)
		}
	}
}

// element returns the element of array field f, whose type is typ.
func element(f *Field, typ string) *Field {
	e := *f
	e.Type, e.Decoder, e.Elem = typ, Decoder{}, nil
	classify(&e, true)
	return &e
}

// valueDecoder chooses the decoder of a field that holds one value.
func valueDecoder(f *Field, t typeSpec) Decoder {
	d := typeDecoder(f, t)
	switch v := f.VarSerializer; {
	case v == "" || varSerializerHints[v]:
		return d
	case v == "minusone" && isInteger(d.Kind):
		return Decoder{Kind: MinusOne}
	case simTimeSerializers[v] && (d.Kind == Float32 || d.Kind == SimTime):
		return Decoder{Kind: SimTime}
	}
	return Decoder{}
}

// typeDecoder chooses a decoder from f's type and encoder.
func typeDecoder(f *Field, t typeSpec) Decoder {
	switch {
	case t.pointer:
		return Decoder{}
	case t.arg != "":
		return plain(f, templates[t.base])
	case t.base == "char" && t.count > 0:
		return plain(f, String)
	case floats[t.base]:
		return Decoder{Kind: floatKind(f)}
	case vectors[t.base] > 0:
		return vectorDecoder(f, vectors[t.base])
	case t.base == "QAngle":
		return Decoder{Kind: qangleKind(f)}
	case t.base == "uint64" && f.Encoder == "fixed64":
		return Decoder{Kind: Fixed64}
	}
	k, ok := scalars[t.base]
	if !ok && enumName.MatchString(t.base) {
		k = VarUint64
	}
	return plain(f, k)
}

// enumName matches the naming of Valve's enum types, such as
// EGrenadeThrowState or RenderMode_t. Networked enums are varints, and game
// updates add new ones. Valve's other named types ending in _t are integers,
// which are varints too, so even a wrong guess keeps the stream in step; the
// one float, GameTime_t, is matched before this.
var enumName = regexp.MustCompile(`^(E[A-Z][A-Za-z0-9]*|[A-Za-z][A-Za-z0-9_]*_t)$`)

// plain returns k for a field with no encoder.
func plain(f *Field, k Kind) Decoder {
	if f.Encoder != "" {
		return Decoder{}
	}
	return Decoder{Kind: k}
}

func floatKind(f *Field) Kind {
	switch f.Encoder {
	case "":
	case "coord":
		return Coord
	case "normal":
		return Normal
	case "simtime":
		return SimTime
	default:
		return Unknown
	}
	switch {
	case simTimeNames[f.Name]:
		return SimTime
	case f.BitCount <= 0 || f.BitCount >= 32:
		return Float32
	}
	return QuantizedFloat
}

func vectorDecoder(f *Field, n int) Decoder {
	if f.Encoder == "normal" {
		if n == 3 {
			return Decoder{Kind: VectorNormal}
		}
		return Decoder{}
	}
	switch c := floatKind(f); c {
	case Float32, QuantizedFloat, Coord:
		return Decoder{Kind: Vector, Count: n, Component: c}
	}
	return Decoder{}
}

func qangleKind(f *Field) Kind {
	bits := f.BitCount
	switch f.Encoder {
	case "qangle_precise":
		return QAnglePrecise
	case "qangle_pitch_yaw":
		if bits > 0 && bits < 32 {
			return QAnglePitchYaw
		}
	case "qangle":
		switch {
		case bits == 0:
			return QAngleCoord
		case bits == 32:
			return QAngleFloat
		case bits > 0 && bits < 32:
			return QAngleBits
		}
	}
	return Unknown
}

func isInteger(k Kind) bool {
	switch k {
	case VarInt32, VarInt64, VarUint32, VarUint64, MinusOne:
		return true
	}
	return false
}

// typeSpec is a parsed var type, such as "CHandle< CBaseEntity >[64]".
type typeSpec struct {
	base    string // "CHandle"
	arg     string // template argument: "CBaseEntity"
	elem    string // fixed array element type: "CHandle< CBaseEntity >"
	count   int    // fixed array length, or 0
	pointer bool   // the type ends in '*'
}

func parseType(s string) (typeSpec, bool) {
	var t typeSpec
	if i := strings.LastIndexByte(s, '['); i >= 0 {
		n, err := strconv.Atoi(strings.TrimSuffix(s[i+1:], "]"))
		if !strings.HasSuffix(s, "]") || err != nil || n <= 0 {
			return t, false
		}
		s = strings.TrimSpace(s[:i])
		t.elem, t.count = s, n
	}
	s, t.pointer = strings.CutSuffix(s, "*")
	if i := strings.IndexByte(s, '<'); i >= 0 {
		if !strings.HasSuffix(s, ">") {
			return t, false
		}
		t.arg = strings.TrimSpace(s[i+1 : len(s)-1])
		s = s[:i]
	}
	t.base = strings.TrimSpace(s)
	return t, t.base != ""
}
