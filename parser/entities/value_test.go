package entities

import (
	"math"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
)

func TestReadValue(t *testing.T) {
	quantized := &Field{BitCount: 10, LowValue: -64, HighValue: 64, EncodeFlags: qEncodeZero}
	quantized.quant = newQuantizer(quantized)
	poly := &Field{Polymorphic: []*Serializer{{}, {}}}

	tests := []struct {
		name  string
		f     *Field
		kind  Kind
		write func(w *BitWriter)
		want  Value
	}{
		{"bool", nil, Bool, func(w *BitWriter) { w.Bit(true) }, Value{u: 1}},
		{"zigzag", nil, VarInt32, func(w *BitWriter) { w.VarInt(-100) }, Value{u: uint64(1<<64 - 100)}},
		{"zigzag 64", nil, VarInt64, func(w *BitWriter) { w.VarInt(-1 << 40) }, Value{u: uint64(1<<64 - 1<<40)}},
		{"varint", nil, VarUint32, func(w *BitWriter) { w.VarUint(300) }, Value{u: 300}},
		{"varint 64", nil, VarUint64, func(w *BitWriter) { w.VarUint(1 << 40) }, Value{u: 1 << 40}},
		{"handle", nil, Handle, func(w *BitWriter) { w.VarUint(0xFFFFFF) }, Value{u: 0xFFFFFF}},
		{"fixed64", nil, Fixed64, func(w *BitWriter) { w.Bits(76561198000000000, 64) }, Value{u: 76561198000000000}},
		{"minus one", nil, MinusOne, func(w *BitWriter) { w.VarUint(0) }, Value{u: math.MaxUint64}},
		{"string", nil, String, func(w *BitWriter) { w.Str("de_nuke") }, Value{s: "de_nuke"}},
		{"bytes", nil, Bytes, func(w *BitWriter) { w.VarUint(2); w.Bits(0xBEEF, 16) }, Value{s: "\xef\xbe"}},
		{"float", nil, Float32, func(w *BitWriter) { w.Float32(-2.5) }, Value{f: [4]float32{-2.5}}},
		{"simtime", nil, SimTime, func(w *BitWriter) { w.VarUint(128) }, Value{u: 128, f: [4]float32{2}}},
		{"coord", nil, Coord, func(w *BitWriter) {
			w.Bit(true)    // integer part
			w.Bit(true)    // fraction
			w.Bit(true)    // negative
			w.Bits(99, 14) // 100
			w.Bits(8, 5)   // 8/32
		}, Value{f: [4]float32{-100.25}}},
		{"coord zero", nil, Coord, func(w *BitWriter) { w.Bits(0, 2) }, Value{}},
		{"normal", nil, Normal, func(w *BitWriter) { w.Bit(true); w.Bits(2047, 11) }, Value{f: [4]float32{-1}}},
		{"quantized", quantized, QuantizedFloat, func(w *BitWriter) { w.Bit(false); w.Bits(0, 10) }, Value{f: [4]float32{-64}}},
		{"quantized zero", quantized, QuantizedFloat, func(w *BitWriter) { w.Bit(true) }, Value{}},
		{"vector", &Field{Decoder: Decoder{Count: 3, Component: Float32}}, Vector, func(w *BitWriter) {
			w.Float32(1)
			w.Float32(2)
			w.Float32(3)
		}, Value{f: [4]float32{1, 2, 3}}},
		{"vector normal", nil, VectorNormal, func(w *BitWriter) {
			w.Bit(true) // x
			w.Bit(false)
			w.Bit(false)
			w.Bits(0, 11) // x = 0
			w.Bit(true)   // z is negative
		}, Value{f: [4]float32{0, 0, -1}}},
		{"qangle coord", nil, QAngleCoord, func(w *BitWriter) {
			w.Bit(false)
			w.Bit(true) // yaw only
			w.Bit(false)
			w.Bits(0b01, 2) // integer part only
			w.Bit(false)
			w.Bits(89, 14)
		}, Value{f: [4]float32{0, 90, 0}}},
		{"qangle bits", &Field{BitCount: 8}, QAngleBits, func(w *BitWriter) { w.Bits(64, 8); w.Bits(128, 8); w.Bits(0, 8) }, Value{f: [4]float32{90, 180, 0}}},
		{"qangle float", nil, QAngleFloat, func(w *BitWriter) { w.Float32(1); w.Float32(2); w.Float32(3) }, Value{f: [4]float32{1, 2, 3}}},
		{"qangle precise", nil, QAnglePrecise, func(w *BitWriter) {
			w.Bit(true)
			w.Bit(true)
			w.Bit(false)
			w.Bits(1<<19, 20) // 180 - 180
			w.Bits(3<<18, 20) // 270 - 180
		}, Value{f: [4]float32{0, 90, 0}}},
		{"qangle pitch yaw", &Field{BitCount: 4}, QAnglePitchYaw, func(w *BitWriter) { w.Bits(4, 4); w.Bits(8, 4) }, Value{f: [4]float32{90, 180, 0}}},
		{"array length", nil, DynamicArray, func(w *BitWriter) { w.VarUint(5) }, Value{u: 5}},
		{"null pointer", nil, Pointer, func(w *BitWriter) { w.Bit(false) }, Value{}},
		{"pointer", nil, Pointer, func(w *BitWriter) { w.Bit(true) }, Value{u: 1}},
		{"polymorphic pointer", poly, Pointer, func(w *BitWriter) { w.Bit(true); w.UBitVar(2) }, Value{u: 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.f
			if f == nil {
				f = &Field{}
			}
			f.Decoder.Kind = tc.kind
			var w BitWriter
			tc.write(&w)
			w.Bits(0b11, 2) // a marker after the value
			r := bitread.New(w.Data())
			got := readValue(r, f)
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
			if m := r.ReadBits(2); m != 0b11 || r.Err() != nil {
				t.Errorf("value didn't end where it should: then %b, %v", m, r.Err())
			}
		})
	}
}

func TestQuantizer(t *testing.T) {
	const flagBits = qRoundDown | qRoundUp | qEncodeZero
	tests := []struct {
		name      string
		bits      int
		low, high float32
		flags     int32
		want      quantizer // bits, low, high, and the flags that cost a bit
	}{
		// Rounding down with low 0 shrinks high a step, and the steps then
		// make low exact, so it needs no flag bit.
		{"origin offset", 15, 0, 1024, qRoundDown, quantizer{bits: 15, low: 0, high: 1024 - 1024.0/(1<<15)}},
		// Encode-zero with zero at an end of the range becomes rounding.
		{"zero is low", 12, 0, 64, qEncodeZero, quantizer{bits: 12, low: 0, high: 64 - 64.0/(1<<12)}},
		// Zero inside the range, off the steps, keeps its flag bit.
		{"zero inside", 20, -16384, 16384, qEncodeZero, quantizer{bits: 20, low: -16384, high: 16384, flags: qEncodeZero}},
		{"zero outside", 10, 0.1, 10, qEncodeZero, quantizer{bits: 10, low: 0.1, high: 10}},
		// Encode-integers drops the other flags and widens the range to a
		// power of two; bits above the known flags are ignored.
		{"integers", 8, 0, 1, -8, quantizer{bits: 8, low: 0, high: 1 - 1.0/256}},
		{"integers widened", 4, 0, 100, qEncodeIntegers | qRoundUp, quantizer{bits: 7, low: 0, high: 128 - 1}},
		{"no flags", 8, 0, 100, 0, quantizer{bits: 8, low: 0, high: 100}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := newQuantizer(&Field{BitCount: tc.bits, LowValue: tc.low, HighValue: tc.high, EncodeFlags: tc.flags})
			got := quantizer{bits: q.bits, low: q.low, high: q.high, flags: q.flags & flagBits}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestQuantizerRead(t *testing.T) {
	f := &Field{BitCount: 20, LowValue: -16384, HighValue: 16384, EncodeFlags: qEncodeZero}
	q := newQuantizer(f)
	var w BitWriter
	w.Bit(true) // zero
	w.Bit(false)
	w.Bits(0, 20) // low
	r := bitread.New(w.Data())
	if v := q.read(r); v != 0 {
		t.Errorf("flagged zero read as %g", v)
	}
	if v := q.read(r); v != -16384 {
		t.Errorf("step 0 read as %g, want -16384", v)
	}
}

func TestWorldCoord(t *testing.T) {
	cell, offset := Value{u: 34}, Value{f: [4]float32{352}}
	if got := WorldCoord(cell, offset); got != 1376 {
		t.Errorf("got %g, want 1376", got)
	}
}
