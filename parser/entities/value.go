package entities

import (
	"math"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
)

// SimTimeTick is the length of a tick in seconds, by which a SimTime
// field's tick count is scaled.
const SimTimeTick = 1.0 / 64

// Value is one field's decoded value. Which accessor applies follows from
// the field's Decoder.Kind:
//
//   - Int: VarInt32, VarInt64, MinusOne
//   - Uint: VarUint32, VarUint64, Fixed64, Handle, and SimTime in ticks;
//     also a DynamicArray's length, and a Pointer's: 0 if null, otherwise
//     1 plus its polymorphic type
//   - Bool: Bool
//   - Str: String, and Bytes as raw bytes
//   - Float: Float32, QuantizedFloat, Coord, Normal, and SimTime in seconds
//   - Vector: Vector and VectorNormal (x, y, z, w), and the QAngle kinds
//     (pitch, yaw, roll), in degrees
type Value struct {
	u uint64
	f [4]float32
	s string
}

func (v Value) Int() int64         { return int64(v.u) }
func (v Value) Uint() uint64       { return v.u }
func (v Value) Bool() bool         { return v.u != 0 }
func (v Value) Str() string        { return v.s }
func (v Value) Float() float32     { return v.f[0] }
func (v Value) Vector() [4]float32 { return v.f }

// readValue reads a value of field f, which must not be a FixedArray or
// Table: those hold no value of their own.
func readValue(r *bitread.Reader, f *Field) Value {
	var v Value
	switch d := f.Decoder; d.Kind {
	case Bool:
		if r.ReadBit() {
			v.u = 1
		}
	case Pointer:
		if r.ReadBit() {
			v.u = 1
			if len(f.Polymorphic) > 0 {
				v.u += uint64(r.ReadUBitVar())
			}
		}
	case VarInt32:
		v.u = uint64(int64(r.ReadVarInt32()))
	case VarInt64:
		v.u = uint64(r.ReadVarInt64())
	case VarUint32, Handle, DynamicArray:
		v.u = uint64(r.ReadVarUint32())
	case VarUint64:
		v.u = r.ReadVarUint64()
	case Fixed64:
		lo := r.ReadBits(32)
		v.u = uint64(r.ReadBits(32))<<32 | uint64(lo)
	case MinusOne:
		v.u = uint64(int64(r.ReadVarUint32()) - 1)
	case String:
		v.s = r.ReadString()
	case Bytes:
		v.s = string(r.ReadBytes(int(r.ReadVarUint32())))
	case SimTime:
		v.u = uint64(r.ReadVarUint32())
		v.f[0] = float32(v.u) * SimTimeTick
	case Float32, QuantizedFloat, Coord, Normal:
		v.f[0] = readFloat(r, f, d.Kind)
	case Vector:
		for i := range min(d.Count, len(v.f)) {
			v.f[i] = readFloat(r, f, d.Component)
		}
	case VectorNormal:
		v.f = readVectorNormal(r)
	case QAngleCoord:
		var has [3]bool
		for i := range has {
			has[i] = r.ReadBit()
		}
		for i, ok := range has {
			if ok {
				v.f[i] = readCoord(r)
			}
		}
	case QAngleBits:
		for i := range 3 {
			v.f[i] = readAngle(r, uint(f.BitCount))
		}
	case QAngleFloat:
		for i := range 3 {
			v.f[i] = r.ReadFloat32()
		}
	case QAnglePrecise:
		var has [3]bool
		for i := range has {
			has[i] = r.ReadBit()
		}
		for i, ok := range has {
			if ok {
				v.f[i] = readAngle(r, 20) - 180
			}
		}
	case QAnglePitchYaw:
		v.f[0] = readAngle(r, uint(f.BitCount))
		v.f[1] = readAngle(r, uint(f.BitCount))
	}
	return v
}

func readFloat(r *bitread.Reader, f *Field, k Kind) float32 {
	switch k {
	case QuantizedFloat:
		return f.quant.read(r)
	case Coord:
		return readCoord(r)
	case Normal:
		return readNormal(r)
	}
	return r.ReadFloat32()
}

// readCoord reads a bit coord: integer and fraction presence bits, then a
// sign bit if either is set, a 14-bit integer part less one and a 5-bit
// fraction in 32nds.
func readCoord(r *bitread.Reader) float32 {
	hasInt, hasFrac := r.ReadBit(), r.ReadBit()
	if !hasInt && !hasFrac {
		return 0
	}
	neg := r.ReadBit()
	var v float32
	if hasInt {
		v = float32(r.ReadBits(14) + 1)
	}
	if hasFrac {
		v += float32(r.ReadBits(5)) / 32
	}
	if neg {
		v = -v
	}
	return v
}

// readNormal reads a sign bit and an 11-bit magnitude in [0, 1].
func readNormal(r *bitread.Reader) float32 {
	neg := r.ReadBit()
	v := float32(r.ReadBits(11)) / 2047
	if neg {
		v = -v
	}
	return v
}

// readVectorNormal reads a unit vector: x and y presence bits, x and y as
// normals, and the sign of z, whose size follows from x and y.
func readVectorNormal(r *bitread.Reader) [4]float32 {
	var v [4]float32
	hasX, hasY := r.ReadBit(), r.ReadBit()
	if hasX {
		v[0] = readNormal(r)
	}
	if hasY {
		v[1] = readNormal(r)
	}
	negZ := r.ReadBit()
	if sq := v[0]*v[0] + v[1]*v[1]; sq < 1 {
		v[2] = float32(math.Sqrt(float64(1 - sq)))
	}
	if negZ {
		v[2] = -v[2]
	}
	return v
}

// readAngle reads an angle in degrees of n bits spanning [0, 360).
func readAngle(r *bitread.Reader, n uint) float32 {
	return float32(r.ReadBits(n)) * 360 / float32(uint64(1)<<n)
}

// Entity origins are sent as a cell number and an offset in the cell, per
// axis. Cells are cellWidth units wide, and cell 0 starts at -worldHalf.
const (
	cellWidth = 512
	worldHalf = 16384
)

// WorldCoord returns a world coordinate from an origin's cell and offset
// values, such as m_cellX and m_vecX.
func WorldCoord(cell, offset Value) float64 {
	return float64(cell.u)*cellWidth - worldHalf + float64(offset.f[0])
}
