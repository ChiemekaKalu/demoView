package entities

import (
	"math"
	"slices"
)

// BitWriter writes bit streams in the order bitread reads them: each byte
// from its least significant bit, and each value from its least
// significant bit.
type BitWriter struct {
	buf []byte
	n   int // bits written
}

// Data returns the bytes written so far, the last one zero-padded.
func (w *BitWriter) Data() []byte { return w.buf }

func (w *BitWriter) Bit(b bool) {
	if w.n%8 == 0 {
		w.buf = append(w.buf, 0)
	}
	if b {
		w.buf[w.n/8] |= 1 << (w.n % 8)
	}
	w.n++
}

func (w *BitWriter) Bits(v uint64, n int) {
	for i := range n {
		w.Bit(v>>i&1 != 0)
	}
}

func (w *BitWriter) VarUint(v uint64) {
	for v >= 0x80 {
		w.Bits(v&0x7F|0x80, 8)
		v >>= 7
	}
	w.Bits(v, 8)
}

func (w *BitWriter) VarInt(v int64) { w.VarUint(uint64(v<<1 ^ v>>63)) }

func (w *BitWriter) Float32(f float32) { w.Bits(uint64(math.Float32bits(f)), 32) }

func (w *BitWriter) Str(s string) {
	for i := range len(s) {
		w.Bits(uint64(s[i]), 8)
	}
	w.Bits(0, 8)
}

// UBitVar writes v as bitread.ReadUBitVar reads it.
func (w *BitWriter) UBitVar(v uint32) {
	switch {
	case v < 1<<4:
		w.Bits(uint64(v), 6)
	case v < 1<<8:
		w.Bits(uint64(v&15|16), 6)
		w.Bits(uint64(v>>4), 4)
	case v < 1<<12:
		w.Bits(uint64(v&15|32), 6)
		w.Bits(uint64(v>>4), 8)
	default:
		w.Bits(uint64(v&15|48), 6)
		w.Bits(uint64(v>>4), 28)
	}
}

// UBitVarFP writes v as bitread.ReadUBitVarFP reads it.
func (w *BitWriter) UBitVarFP(v uint32) {
	for i, width := range []int{2, 4, 10, 17} {
		if v < 1<<width {
			w.Bits(1<<i, i+1)
			w.Bits(uint64(v), width)
			return
		}
	}
	w.Bits(0, 4)
	w.Bits(uint64(v), 31)
}

// op writes the Huffman code of a field path op.
func (w *BitWriter) op(op fieldPathOp) {
	for _, b := range fieldPathCodes()[op] {
		w.Bit(b)
	}
}

// fieldPathCodes returns each op's code, bit by bit, from fieldPathTree.
func fieldPathCodes() [numFieldPathOps][]bool {
	var codes [numFieldPathOps][]bool
	var walk func(n int16, code []bool)
	walk = func(n int16, code []bool) {
		for b, c := range fieldPathTree[n] {
			next := append(slices.Clone(code), b == 1)
			if c < 0 {
				codes[^c] = next
			} else {
				walk(c, next)
			}
		}
	}
	walk(0, nil)
	return codes
}

// FieldPaths writes a field path list that yields paths, then opFinish.
// Each path takes one op: PushNAndNonTopological to keep or deepen the
// path, or PopNAndNonTopological to make it shallower. Paths start from
// [-1].
func (w *BitWriter) FieldPaths(paths ...[]int) {
	cur := []int{-1}
	for _, p := range paths {
		if len(p) >= len(cur) {
			w.op(opPushNAndNonTopological)
			w.nonTopo(cur, p, 1)
			w.UBitVar(uint32(len(p) - len(cur)))
			for _, v := range p[len(cur):] {
				w.UBitVarFP(uint32(v))
			}
		} else {
			w.op(opPopNAndNonTopological)
			w.UBitVarFP(uint32(len(cur) - len(p)))
			w.nonTopo(cur[:len(p)], p, 0)
		}
		cur = slices.Clone(p)
	}
	w.op(opFinish)
}

// nonTopo writes the flag bits and deltas, less bias, from cur to p for
// the indices they share.
func (w *BitWriter) nonTopo(cur, p []int, bias int) {
	for i := range cur {
		d := p[i] - cur[i]
		w.Bit(d != 0)
		if d != 0 {
			w.VarInt(int64(d - bias))
		}
	}
}
