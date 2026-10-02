package entities

import (
	"math"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
)

// Quantized float encode flags.
const (
	qRoundDown      = 1 << 0 // a flag bit can say the value is exactly low
	qRoundUp        = 1 << 1 // a flag bit can say the value is exactly high
	qEncodeZero     = 1 << 2 // a flag bit can say the value is exactly 0
	qEncodeIntegers = 1 << 3 // the range is widened so integers land on steps
)

// quantizer reads a quantized float: up to three flag bits, as its flags
// say, then bits bits spanning low to high. The flags and range are those
// left after the field's own are normalized, as the encoder normalizes
// them; a flag the steps already make exact needs no bit.
type quantizer struct {
	bits      uint
	low, high float32
	flags     int32
	mul       float32 // encoding: steps per unit
	decMul    float32 // decoding: 1 / (steps - 1)
}

// newQuantizer prepares f's quantizer. Arithmetic is float32 throughout,
// as on the encoding side, because whether a flag bit is sent depends on
// exact rounding.
func newQuantizer(f *Field) quantizer {
	q := quantizer{bits: uint(f.BitCount), low: f.LowValue, high: f.HighValue, flags: f.EncodeFlags}
	q.normalizeFlags()

	steps := uint64(1) << q.bits
	switch {
	case q.flags&qRoundDown != 0:
		q.high -= (q.high - q.low) / float32(steps)
	case q.flags&qRoundUp != 0:
		q.low += (q.high - q.low) / float32(steps)
	}
	if q.flags&qEncodeIntegers != 0 {
		delta := max(q.high-q.low, 1)
		span := uint64(1) << uint(math.Ceil(math.Log2(float64(delta))))
		for uint64(1)<<q.bits < span {
			q.bits++
		}
		steps = uint64(1) << q.bits
		q.high = q.low + float32(span) - float32(span)/float32(steps)
	}
	q.assignMultipliers(steps)

	if q.flags&qRoundDown != 0 && q.quantize(q.low) == q.low {
		q.flags &^= qRoundDown
	}
	if q.flags&qRoundUp != 0 && q.quantize(q.high) == q.high {
		q.flags &^= qRoundUp
	}
	if q.flags&qEncodeZero != 0 && q.quantize(0) == 0 {
		q.flags &^= qEncodeZero
	}
	return q
}

// normalizeFlags drops flags that can't apply to the range and turns
// encode-zero into rounding when zero is an end of it.
func (q *quantizer) normalizeFlags() {
	if q.low == 0 && q.flags&qRoundDown != 0 || q.high == 0 && q.flags&qRoundUp != 0 {
		q.flags &^= qEncodeZero
	}
	if q.low == 0 && q.flags&qEncodeZero != 0 {
		q.flags |= qRoundDown
		q.flags &^= qEncodeZero
	}
	if q.high == 0 && q.flags&qEncodeZero != 0 {
		q.flags |= qRoundUp
		q.flags &^= qEncodeZero
	}
	if !(q.low < 0 && q.high > 0) {
		q.flags &^= qEncodeZero
	}
	if q.flags&qEncodeIntegers != 0 {
		q.flags &^= qRoundUp | qRoundDown | qEncodeZero
	}
}

// assignMultipliers sets the encoding multiplier as large as fits the
// steps, backing off when float32 rounding would overshoot.
func (q *quantizer) assignMultipliers(steps uint64) {
	top := float32(steps - 1)
	span := q.high - q.low
	mul := top
	if span > 0 {
		mul = top / span
	}
	for _, back := range []float32{0.9999, 0.99, 0.9, 0.8, 0.7} {
		if float32(mul*span) <= top {
			break
		}
		mul = float32(top/span) * back
	}
	q.mul = mul
	q.decMul = 1 / top
}

// quantize returns the value v decodes as.
func (q *quantizer) quantize(v float32) float32 {
	switch {
	case v < q.low:
		return q.low
	case v > q.high:
		return q.high
	}
	i := uint32(float32((v - q.low) * q.mul))
	return q.low + float32((q.high-q.low)*float32(float32(i)*q.decMul))
}

func (q *quantizer) read(r *bitread.Reader) float32 {
	if q.flags&qRoundDown != 0 && r.ReadBit() {
		return q.low
	}
	if q.flags&qRoundUp != 0 && r.ReadBit() {
		return q.high
	}
	if q.flags&qEncodeZero != 0 && r.ReadBit() {
		return 0
	}
	u := r.ReadBits(q.bits)
	return q.low + float32((q.high-q.low)*float32(float32(u)*q.decMul))
}
