package bitread

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// stream returns the bytes written by f.
func stream(f func(w *bitWriter)) []byte {
	var w bitWriter
	f(&w)
	return w.buf
}

// eachOffset writes filler bits, then whatever write writes, then optional
// trailing bits, and runs check on a reader positioned after the filler.
// Filler lengths 0..71 put the value at every bit alignment and across word
// boundaries. With no trailing bits the reads near the end of the buffer
// take the slow path; with 64 trailing bits they take the buffered one.
//
// If check passes and the reader has no error, the reader must have consumed
// exactly what write wrote.
func eachOffset(t *testing.T, write func(w *bitWriter), check func(r *Reader) error) {
	t.Helper()
	eachOffsetTails(t, []int{0, 64}, write, check)
}

// eachOffsetTails is eachOffset with a choice of trailing bit counts. Cases
// that expect ErrOverflow use only 0, since trailing data would satisfy them.
func eachOffsetTails(t *testing.T, tails []int, write func(w *bitWriter), check func(r *Reader) error) {
	t.Helper()
	for lead := range 72 {
		for _, tail := range tails {
			var w bitWriter
			for i := range lead {
				w.bit(i%3 == 0)
			}
			write(&w)
			end := w.n
			w.bits(math.MaxUint64, tail)

			r := New(w.buf)
			for i := range lead {
				if r.ReadBit() != (i%3 == 0) {
					t.Fatalf("lead %d tail %d: filler bit %d wrong", lead, tail, i)
				}
			}
			if err := check(r); err != nil {
				t.Fatalf("lead %d tail %d: %v", lead, tail, err)
			}
			if got := r.Position() + r.BitsLeft(); got != len(w.buf)*8 {
				t.Fatalf("lead %d tail %d: Position+BitsLeft = %d, want %d", lead, tail, got, len(w.buf)*8)
			}
			if r.Err() == nil && r.Position() != end {
				t.Fatalf("lead %d tail %d: Position = %d, want %d", lead, tail, r.Position(), end)
			}
		}
	}
}

// expect returns an error describing a mismatch between got and want, and
// between r's error and wantErr (nil meaning no error).
func expect(r *Reader, got, want any, wantErr error) error {
	switch err := r.Err(); {
	case wantErr == nil && err != nil:
		return fmt.Errorf("unexpected error: %v", err)
	case wantErr != nil && !errors.Is(err, wantErr):
		return fmt.Errorf("error = %v, want %v", err, wantErr)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		return fmt.Errorf("got %v, want %v", got, want)
	}
	return nil
}

// TestBitOrder pins the bit order to hand-computed bytes, so the reader and
// the test writer cannot share a mistake.
func TestBitOrder(t *testing.T) {
	in := []byte{0xB4, 0x5A} // 1011_0100 0101_1010
	reads := []struct {
		n    uint
		want uint32
	}{
		{3, 0b100},     // low three bits of 0xB4
		{7, 0b1010110}, // top five of 0xB4, then low two of 0x5A
		{6, 0b010110},  // top six of 0x5A
	}
	r := New(in)
	var w bitWriter
	for _, c := range reads {
		if got := r.ReadBits(c.n); got != c.want {
			t.Errorf("ReadBits(%d) = %#b, want %#b", c.n, got, c.want)
		}
		w.bits(uint64(c.want), int(c.n))
	}
	if !bytes.Equal(w.buf, in) {
		t.Errorf("writer produced % x, want % x", w.buf, in)
	}

	r = New([]byte{0b0000_0101})
	for i, want := range []bool{true, false, true, false} {
		if got := r.ReadBit(); got != want {
			t.Errorf("bit %d = %v, want %v", i, got, want)
		}
	}
}

func TestReadBitsAcrossBoundaries(t *testing.T) {
	const pattern uint64 = 0xDEADBEEFCAFEF00D
	for _, n := range []uint{0, 1, 2, 7, 8, 9, 15, 16, 17, 24, 31, 32} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			// Two values back to back, so the second starts wherever the first ended.
			a := uint32(pattern & (1<<n - 1))
			b := uint32(pattern >> 32 & (1<<n - 1))
			eachOffset(t, func(w *bitWriter) {
				w.bits(uint64(a), int(n))
				w.bits(uint64(b), int(n))
			}, func(r *Reader) error {
				if err := expect(r, r.ReadBits(n), a, nil); err != nil {
					return err
				}
				return expect(r, r.ReadBits(n), b, nil)
			})
		})
	}
}

func TestReadBitsEdges(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		skip int
		n    uint
		want uint32
		err  error
	}{
		{"zero bits of empty", nil, 0, 0, 0, nil},
		{"zero bits at end", []byte{0xFF}, 8, 0, 0, nil},
		{"zero bits unaligned", []byte{0xFF}, 3, 0, 0, nil},
		{"32 aligned", []byte{0x78, 0x56, 0x34, 0x12}, 0, 32, 0x12345678, nil},
		{"32 all ones", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 5, 32, math.MaxUint32, nil},
		{"32 unaligned", stream(func(w *bitWriter) { w.bits(0b101, 3); w.bits(0x80000001, 32) }), 3, 32, 0x80000001, nil},
		{"32 with 31 left", []byte{0xFF, 0xFF, 0xFF, 0xFF}, 1, 32, 0, ErrOverflow},
		{"1 of empty", nil, 0, 1, 0, ErrOverflow},
		{"33 is malformed", make([]byte, 16), 0, 33, 0, ErrMalformed},
		{"64 is malformed", make([]byte, 16), 0, 64, 0, ErrMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := New(c.in)
			r.SkipBits(c.skip)
			pos := r.Position()
			if err := expect(r, r.ReadBits(c.n), c.want, c.err); err != nil {
				t.Fatal(err)
			}
			if c.err == nil && r.Position() != pos+int(c.n) {
				t.Fatalf("Position = %d, want %d", r.Position(), pos+int(c.n))
			}
		})
	}
}

func TestReadUBitVar(t *testing.T) {
	// 0xBA in the 4-extra-bit form: prefix 0x1A (low nibble 0xA, form 0x10),
	// then 0xB.
	if r := New([]byte{0xDA, 0x02}); r.ReadUBitVar() != 0xBA || r.Position() != 10 {
		t.Fatalf("hand-built 0xBA: wrong value or position %d", r.Position())
	}

	cases := []struct {
		name string
		v    uint32
		form int // 0: 6 bits, 1: 0x10 + 4, 2: 0x20 + 8, 3: 0x30 + 28
	}{
		{"6-bit zero", 0, 0},
		{"6-bit max", 15, 0},
		{"0x10 small", 0x1A, 1},
		{"0x10 max", 0xFF, 1},
		{"0x20 small", 0x100, 2},
		{"0x20 max", 0xFFF, 2},
		{"0x30 small", 0x1000, 3},
		{"0x30 mid", 0x12345678, 3},
		{"0x30 max", math.MaxUint32, 3},
		{"0x10 non-minimal", 5, 1},
		{"0x30 non-minimal", 5, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eachOffset(t, func(w *bitWriter) {
				w.ubitvar(c.v, c.form)
			}, func(r *Reader) error {
				return expect(r, r.ReadUBitVar(), c.v, nil)
			})
		})
	}
}

func TestReadUBitVarFP(t *testing.T) {
	// 9 in the 4-bit form: flag bits 0, 1, then 1001.
	if r := New([]byte{0x26}); r.ReadUBitVarFP() != 9 || r.Position() != 6 {
		t.Fatalf("hand-built 9: wrong value or position %d", r.Position())
	}

	cases := []struct {
		name string
		v    uint32
		form int // payload width 2, 4, 10, 17 or 31
	}{
		{"2-bit zero", 0, 0},
		{"2-bit max", 3, 0},
		{"4-bit", 9, 1},
		{"4-bit max", 15, 1},
		{"10-bit max", 1<<10 - 1, 2},
		{"17-bit", 1234, 3},
		{"17-bit max", 1<<17 - 1, 3},
		{"31-bit", 1 << 30, 4},
		{"31-bit max", 1<<31 - 1, 4},
		{"31-bit non-minimal", 7, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eachOffset(t, func(w *bitWriter) {
				w.ubitvarFP(c.v, c.form)
			}, func(r *Reader) error {
				return expect(r, r.ReadUBitVarFP(), c.v, nil)
			})
		})
	}
}

func TestReadVarUint(t *testing.T) {
	if r := New([]byte{0xAC, 0x02}); r.ReadVarUint32() != 300 {
		t.Fatal("hand-built 300 misread")
	}
	values := []uint64{
		0, 1, 127, 128, 300, 1<<14 - 1, 1 << 14, 1<<21 - 1, 1 << 21, 1<<28 - 1, 1 << 28,
		math.MaxUint32, 1 << 35, 1 << 56, 1<<63 - 1, math.MaxUint64,
	}
	for _, v := range values {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			if v <= math.MaxUint32 {
				eachOffset(t, func(w *bitWriter) { w.varuint(v) }, func(r *Reader) error {
					return expect(r, r.ReadVarUint32(), v, nil)
				})
			}
			eachOffset(t, func(w *bitWriter) { w.varuint(v) }, func(r *Reader) error {
				return expect(r, r.ReadVarUint64(), v, nil)
			})
		})
	}
}

func TestReadVarUintLimits(t *testing.T) {
	ff := func(n int, last ...byte) []byte {
		return append(bytes.Repeat([]byte{0xFF}, n), last...)
	}
	cases := []struct {
		name string
		in   []byte
		is64 bool
		want uint64
		err  error
	}{
		{"32: 5 bytes", ff(4, 0x0F), false, math.MaxUint32, nil},
		{"32: 5th byte high bits dropped", ff(4, 0x7F), false, math.MaxUint32, nil},
		{"32: non-minimal zero", []byte{0x80, 0x80, 0x00}, false, 0, nil},
		{"32: 6 bytes", ff(5, 0x01), false, 0, ErrMalformed},
		{"32: 5 continuation bytes, then end", ff(5), false, 0, ErrMalformed},
		{"32: cut short", ff(3), false, 0, ErrOverflow},
		{"32: empty", nil, false, 0, ErrOverflow},
		{"64: 10 bytes", ff(9, 0x01), true, math.MaxUint64, nil},
		{"64: 11 bytes", ff(10, 0x01), true, 0, ErrMalformed},
		{"64: cut short", ff(9), true, 0, ErrOverflow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eachOffsetTails(t, tailsFor(c.err), func(w *bitWriter) { w.bytes(c.in) }, func(r *Reader) error {
				if c.is64 {
					return expect(r, r.ReadVarUint64(), c.want, c.err)
				}
				return expect(r, r.ReadVarUint32(), c.want, c.err)
			})
		})
	}
}

// tailsFor returns the trailing bit counts to test a case expecting err with.
func tailsFor(err error) []int {
	if err == ErrOverflow {
		return []int{0}
	}
	return []int{0, 64}
}

func TestReadVarIntZigzag(t *testing.T) {
	cases32 := []struct {
		v   int32
		enc uint32
	}{
		{0, 0}, {-1, 1}, {1, 2}, {-2, 3}, {2, 4},
		{math.MaxInt32, math.MaxUint32 - 1}, {math.MinInt32, math.MaxUint32},
	}
	for _, c := range cases32 {
		t.Run(fmt.Sprint("int32 ", c.v), func(t *testing.T) {
			eachOffset(t, func(w *bitWriter) { w.varuint(uint64(c.enc)) }, func(r *Reader) error {
				return expect(r, r.ReadVarInt32(), c.v, nil)
			})
		})
	}
	cases64 := []struct {
		v   int64
		enc uint64
	}{
		{0, 0}, {-1, 1}, {1, 2}, {-2, 3},
		{math.MaxInt32, math.MaxUint32 - 1}, {math.MinInt32, math.MaxUint32},
		{math.MaxInt64, math.MaxUint64 - 1}, {math.MinInt64, math.MaxUint64},
	}
	for _, c := range cases64 {
		t.Run(fmt.Sprint("int64 ", c.v), func(t *testing.T) {
			eachOffset(t, func(w *bitWriter) { w.varuint(c.enc) }, func(r *Reader) error {
				return expect(r, r.ReadVarInt64(), c.v, nil)
			})
		})
	}
}

func TestReadFloat32(t *testing.T) {
	for _, bits := range []uint32{
		0, 0x80000000, math.Float32bits(1.5), math.Float32bits(-273.15),
		0x7F800000, 0xFF800000, 0x7FC00001, 1, // ±Inf, a NaN, smallest denormal
	} {
		eachOffset(t, func(w *bitWriter) { w.bits(uint64(bits), 32) }, func(r *Reader) error {
			return expect(r, math.Float32bits(r.ReadFloat32()), bits, nil)
		})
	}
}

func TestReadBytes(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, n := range []int{0, 1, 7, 8, 9, 16, 17, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			want := make([]byte, n)
			for i := range want {
				want[i] = byte(rng.Uint32())
			}
			write := func(w *bitWriter) {
				w.bytes(want)
				w.bits(0x2D, 6) // marker: the read must stop exactly at the end
			}
			eachOffset(t, write, func(r *Reader) error {
				if err := expect(r, r.ReadBytes(n), want, nil); err != nil {
					return err
				}
				return expect(r, r.ReadBits(6), 0x2D, nil)
			})
			eachOffset(t, write, func(r *Reader) error {
				got := make([]byte, n)
				r.ReadBytesInto(got)
				if err := expect(r, got, want, nil); err != nil {
					return err
				}
				return expect(r, r.ReadBits(6), 0x2D, nil)
			})
		})
	}
}

func TestReadBytesCopies(t *testing.T) {
	in := []byte{1, 2, 3, 4}
	got := New(in).ReadBytes(4)
	got[0] = 99
	if in[0] != 1 {
		t.Fatal("ReadBytes aliased the input buffer")
	}
}

func TestReadBytesErrors(t *testing.T) {
	cases := []struct {
		name string
		skip int
		n    int
		err  error
	}{
		{"one too many", 0, 5, ErrOverflow},
		{"unaligned, one bit short", 1, 4, ErrOverflow},
		{"huge", 0, 1 << 40, ErrOverflow},
		{"negative", 0, -1, ErrMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := New([]byte{1, 2, 3, 4})
			r.SkipBits(c.skip)
			if err := expect(r, r.ReadBytes(c.n) == nil, true, c.err); err != nil {
				t.Fatal(err)
			}
		})
	}

	p := []byte{9, 9, 9, 9, 9}
	r := New([]byte{1, 2, 3, 4})
	r.ReadBytesInto(p)
	if err := expect(r, p, make([]byte, 5), ErrOverflow); err != nil {
		t.Fatalf("ReadBytesInto: %v", err)
	}
}

func TestReadString(t *testing.T) {
	cases := []struct {
		name string
		s    string
	}{
		{"empty", ""},
		{"short", "de_dust2"},
		{"utf-8", "s1mple™"},
		{"longer than the stack buffer", strings.Repeat("ab", 150)},
		{"max length", strings.Repeat("x", MaxStringLen)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eachOffset(t, func(w *bitWriter) {
				w.str(c.s)
				w.bits(0x2D, 6)
			}, func(r *Reader) error {
				if err := expect(r, r.ReadString(), c.s, nil); err != nil {
					return err
				}
				return expect(r, r.ReadBits(6), 0x2D, nil)
			})
		})
	}
}

func TestReadStringErrors(t *testing.T) {
	long := []byte(strings.Repeat("x", MaxStringLen+1))
	cases := []struct {
		name string
		in   []byte
		err  error
	}{
		{"unterminated", []byte("abc"), ErrOverflow},
		{"max length, unterminated", long[:MaxStringLen], ErrOverflow},
		{"too long", append(long, 0), ErrMalformed},
		{"too long, unterminated", long, ErrMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eachOffsetTails(t, tailsFor(c.err), func(w *bitWriter) { w.bytes(c.in) }, func(r *Reader) error {
				return expect(r, r.ReadString(), "", c.err)
			})
		})
	}
}

func TestSkipBits(t *testing.T) {
	for _, pre := range []uint{0, 5, 61} {
		for skip := range 200 {
			var w bitWriter
			w.bits(0x15, int(pre))
			w.bits(math.MaxUint64, skip%64)
			for range skip / 64 {
				w.bits(math.MaxUint64, 64)
			}
			w.bits(0x2D, 6)
			r := New(w.buf)
			for range pre {
				r.ReadBit()
			}
			r.SkipBits(skip)
			if err := expect(r, r.ReadBits(6), 0x2D, nil); err != nil {
				t.Fatalf("pre %d skip %d: %v", pre, skip, err)
			}
		}
	}

	cases := []struct {
		name string
		skip int
		err  error
	}{
		{"to the end", 32, nil},
		{"past the end", 33, ErrOverflow},
		{"far past the end", math.MaxInt, ErrOverflow},
		{"negative", -1, ErrMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := New([]byte{1, 2, 3, 4})
			r.SkipBits(c.skip)
			if err := expect(r, r.BitsLeft(), 0, c.err); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// allReads calls every read method once, for checking zero results after an
// error.
var allReads = []struct {
	name string
	read func(r *Reader) any
	zero any
}{
	{"ReadBit", func(r *Reader) any { return r.ReadBit() }, false},
	{"ReadBits", func(r *Reader) any { return r.ReadBits(7) }, uint32(0)},
	{"ReadUint8", func(r *Reader) any { return r.ReadUint8() }, uint8(0)},
	{"ReadFloat32", func(r *Reader) any { return r.ReadFloat32() }, float32(0)},
	{"ReadUBitVar", func(r *Reader) any { return r.ReadUBitVar() }, uint32(0)},
	{"ReadUBitVarFP", func(r *Reader) any { return r.ReadUBitVarFP() }, uint32(0)},
	{"ReadVarUint32", func(r *Reader) any { return r.ReadVarUint32() }, uint32(0)},
	{"ReadVarUint64", func(r *Reader) any { return r.ReadVarUint64() }, uint64(0)},
	{"ReadVarInt32", func(r *Reader) any { return r.ReadVarInt32() }, int32(0)},
	{"ReadVarInt64", func(r *Reader) any { return r.ReadVarInt64() }, int64(0)},
	{"ReadBytes", func(r *Reader) any { return r.ReadBytes(2) == nil }, true},
	{"ReadBytesInto", func(r *Reader) any { p := []byte{1, 2}; r.ReadBytesInto(p); return p[0] | p[1] }, byte(0)},
	{"ReadString", func(r *Reader) any { return r.ReadString() }, ""},
}

func TestStickyError(t *testing.T) {
	// 0x55 everywhere: every read would succeed with a non-zero value.
	data := bytes.Repeat([]byte{0x55}, 64)
	for _, c := range allReads {
		t.Run(c.name, func(t *testing.T) {
			r := New(data)
			r.ReadBits(33)
			first := r.Err()
			if !errors.Is(first, ErrMalformed) {
				t.Fatalf("Err = %v, want ErrMalformed", first)
			}
			if got := c.read(r); got != c.zero {
				t.Fatalf("got %v after error, want %v", got, c.zero)
			}
			if r.Err() != first {
				t.Fatalf("Err changed to %v; the first error must stick", r.Err())
			}
			if r.BitsLeft() != 0 {
				t.Fatalf("BitsLeft = %d after error, want 0", r.BitsLeft())
			}
		})
	}

	r := New([]byte{0xFF})
	if got := r.ReadBits(4); got != 0xF {
		t.Fatalf("ReadBits(4) = %#x", got)
	}
	if got := r.ReadBits(8); got != 0 || !errors.Is(r.Err(), ErrOverflow) {
		t.Fatalf("overrunning ReadBits = %#x, err %v; want 0, ErrOverflow", got, r.Err())
	}
	r.Reset([]byte{0xFF})
	if r.Err() != nil || r.ReadBits(8) != 0xFF {
		t.Fatal("Reset did not clear the error")
	}
}

// TestFailingReadReturnsZero checks that a read which runs out partway
// returns zero rather than the bits it managed to read.
func TestFailingReadReturnsZero(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		read func(r *Reader) any
	}{
		{"ReadBits", []byte{0xFF}, func(r *Reader) any { return r.ReadBits(9) }},
		{"ReadUBitVar", stream(func(w *bitWriter) { w.bits(0x3F, 6); w.bits(0xFFF, 12) }), func(r *Reader) any { return r.ReadUBitVar() }},
		{"ReadUBitVarFP", stream(func(w *bitWriter) { w.bits(0, 4); w.bits(0xFFFF, 16) }), func(r *Reader) any { return r.ReadUBitVarFP() }},
		{"ReadVarUint32", []byte{0xFF, 0xFF}, func(r *Reader) any { return r.ReadVarUint32() }},
		{"ReadVarUint64", []byte{0xFF, 0xFF}, func(r *Reader) any { return r.ReadVarUint64() }},
		{"ReadString aligned", []byte("abc"), func(r *Reader) any { return r.ReadString() }},
		{"ReadString unaligned", []byte{0xFF, 'a', 'b'}, func(r *Reader) any { r.ReadBit(); return r.ReadString() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := New(c.in)
			if got := c.read(r); !isZero(got) || !errors.Is(r.Err(), ErrOverflow) {
				t.Fatalf("got %v, err %v; want zero, ErrOverflow", got, r.Err())
			}
		})
	}
}

func TestFirstErrorWins(t *testing.T) {
	r := New([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	r.ReadVarUint32()
	r.ReadBits(1)
	if !errors.Is(r.Err(), ErrMalformed) || errors.Is(r.Err(), ErrOverflow) {
		t.Fatalf("Err = %v, want the first error (ErrMalformed)", r.Err())
	}
}

// FuzzRoundTrip decodes the input into a list of typed values, writes them
// with the reference writer, and checks the Reader reads them all back. It
// then replays the same reads over the raw input to check that arbitrary
// bytes never cause a panic and that errors stick.
func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 5, 0xFF, 2, 3, 0xAA, 0xBB, 0xCC, 0xDD, 9, 12, 'h', 'i'})
	f.Add([]byte("\x04\x40\xff\xff\xff\xff\x0a\x05hello\x0b\x30\x03\x02"))
	f.Add(bytes.Repeat([]byte{0x80}, 64))
	f.Fuzz(func(t *testing.T, data []byte) {
		ops := decodeOps(data)

		var w bitWriter
		for _, o := range ops {
			o.write(&w)
		}
		r := New(w.buf)
		for i, o := range ops {
			if err := o.check(r); err != nil {
				t.Fatalf("op %d (%s): %v", i, o.name(), err)
			}
		}
		if r.Err() != nil || r.Position() != w.n || r.BitsLeft() != len(w.buf)*8-w.n {
			t.Fatalf("end: err %v, Position %d, BitsLeft %d; wrote %d bits", r.Err(), r.Position(), r.BitsLeft(), w.n)
		}

		raw := New(data)
		for i, o := range ops {
			pos, failed := raw.Position(), raw.Err() != nil
			got := o.read(raw)
			if raw.Position()+raw.BitsLeft() != len(data)*8 {
				t.Fatalf("raw op %d: Position %d + BitsLeft %d != %d", i, raw.Position(), raw.BitsLeft(), len(data)*8)
			}
			if raw.Err() == nil && raw.Position() < pos {
				t.Fatalf("raw op %d: Position went back from %d to %d", i, pos, raw.Position())
			}
			if failed && !isZero(got) {
				t.Fatalf("raw op %d (%s): got %v after an error", i, o.name(), got)
			}
		}
	})
}

// fuzzOp is one typed value in a fuzzed stream.
type fuzzOp struct {
	kind byte
	n    uint   // bit width, byte count, skip length or encoding form
	v    uint64 // value
	b    []byte // bytes or string contents
}

const numKinds = 14

var kindNames = [numKinds]string{
	"ReadBit", "ReadBits", "ReadUBitVar", "ReadUBitVarFP", "ReadVarUint32", "ReadVarUint64",
	"ReadVarInt32", "ReadVarInt64", "ReadFloat32", "ReadBytes", "ReadString", "SkipBits",
	"ReadUint8", "ReadBytesInto",
}

func (o fuzzOp) name() string { return kindNames[o.kind] }

// decodeOps turns fuzz input into ops, reading parameters from the bytes
// that follow each kind byte.
func decodeOps(data []byte) []fuzzOp {
	next := func() byte {
		if len(data) == 0 {
			return 0
		}
		c := data[0]
		data = data[1:]
		return c
	}
	u64 := func() uint64 {
		var v uint64
		for i := range 8 {
			v |= uint64(next()) << (8 * i)
		}
		return v >> (next() % 64) // spread values across all sizes
	}
	var ops []fuzzOp
	for len(data) > 0 && len(ops) < 512 {
		o := fuzzOp{kind: next() % numKinds}
		switch o.kind {
		case 0:
			o.v = uint64(next() & 1)
		case 1:
			o.n = uint(next() % 33)
			o.v = u64() & (1<<o.n - 1)
		case 2:
			o.n = uint(next() % 4)
			o.v = uint64(uint32(u64()))
			o.n = max(o.n, uint(ubvMinForm(uint32(o.v))))
		case 3:
			o.n = uint(next() % 5)
			o.v = u64() & (1<<31 - 1)
			o.n = max(o.n, uint(fpMinForm(uint32(o.v))))
		case 4, 6, 8, 12:
			o.v = uint64(uint32(u64()))
			if o.kind == 12 {
				o.v &= 0xFF
			}
		case 5, 7:
			o.v = u64()
		case 9, 10, 13:
			o.b = make([]byte, next()%40)
			for i := range o.b {
				o.b[i] = next()
				if o.kind == 10 && o.b[i] == 0 {
					o.b[i] = 1
				}
			}
		case 11:
			o.n = uint(next() % 150)
		}
		ops = append(ops, o)
	}
	return ops
}

func (o fuzzOp) write(w *bitWriter) {
	switch o.kind {
	case 0:
		w.bit(o.v != 0)
	case 1:
		w.bits(o.v, int(o.n))
	case 2:
		w.ubitvar(uint32(o.v), int(o.n))
	case 3:
		w.ubitvarFP(uint32(o.v), int(o.n))
	case 4, 5:
		w.varuint(o.v)
	case 6:
		w.varint32(int32(o.v))
	case 7:
		w.varint64(int64(o.v))
	case 8:
		w.bits(o.v, 32)
	case 9, 13:
		w.bytes(o.b)
	case 10:
		w.str(string(o.b))
	case 11:
		for i := range int(o.n) {
			w.bit(i%5 == 1)
		}
	case 12:
		w.bits(o.v, 8)
	}
}

// read performs the op's read and returns the result.
func (o fuzzOp) read(r *Reader) any {
	switch o.kind {
	case 0:
		return r.ReadBit()
	case 1:
		return r.ReadBits(o.n)
	case 2:
		return r.ReadUBitVar()
	case 3:
		return r.ReadUBitVarFP()
	case 4:
		return r.ReadVarUint32()
	case 5:
		return r.ReadVarUint64()
	case 6:
		return r.ReadVarInt32()
	case 7:
		return r.ReadVarInt64()
	case 8:
		return math.Float32bits(r.ReadFloat32())
	case 9:
		return r.ReadBytes(len(o.b))
	case 10:
		return r.ReadString()
	case 11:
		r.SkipBits(int(o.n))
		return nil
	case 12:
		return r.ReadUint8()
	case 13:
		p := make([]byte, len(o.b))
		r.ReadBytesInto(p)
		return p
	}
	panic("unknown op")
}

// check reads the op back and compares it with what was written.
func (o fuzzOp) check(r *Reader) error {
	got := o.read(r)
	var want any
	switch o.kind {
	case 0:
		want = o.v != 0
	case 1, 2, 3, 4, 8:
		want = uint32(o.v)
	case 5:
		want = o.v
	case 6:
		want = int32(o.v)
	case 7:
		want = int64(o.v)
	case 9, 13:
		want = o.b
	case 10:
		want = string(o.b)
	case 11:
		want = nil
	case 12:
		want = uint8(o.v)
	}
	if b, ok := want.([]byte); ok {
		if !bytes.Equal(got.([]byte), b) {
			return fmt.Errorf("got % x, want % x", got, b)
		}
		return r.Err()
	}
	if got != want {
		return fmt.Errorf("got %v, want %v", got, want)
	}
	return r.Err()
}

// isZero reports whether v is a zero result: zero, false, "", nil, or a
// slice of zero bytes.
func isZero(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case []byte:
		return !slices.ContainsFunc(v, func(c byte) bool { return c != 0 })
	case bool:
		return !v
	case string:
		return v == ""
	}
	return fmt.Sprint(v) == "0"
}
