// Package bitread reads the little-endian bit streams used inside CS2 demo
// packets.
//
// Bits are consumed from each byte starting at the least significant bit,
// and the first bit of a multi-bit value is its least significant bit.
//
// Errors are sticky. The first failed read records an error, that read and
// every later read return zero values, and Err reports what went wrong.
// Reads never panic, however corrupt the input.
package bitread

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
)

// MaxStringLen is the longest string ReadString accepts, not counting the
// terminating zero byte.
const MaxStringLen = 4096

var (
	// ErrOverflow means a read ran past the end of the buffer.
	ErrOverflow = errors.New("bitread: read past end of buffer")
	// ErrMalformed means the stream held a value no valid stream contains,
	// such as an over-long varint, or a read was asked for a bad length.
	ErrMalformed = errors.New("bitread: malformed data")
)

// Reader reads bits from a byte slice. The zero value is an empty reader;
// use New or Reset to give it data.
type Reader struct {
	buf   []byte
	pos   int    // index of the next byte to load into cache
	cache uint64 // buffered bits; the next bit is bit 0
	n     uint   // valid bits in cache, at most 63
	err   error
}

// New returns a Reader over b. The Reader does not copy b, so b must not
// change while it is being read.
func New(b []byte) *Reader {
	return &Reader{buf: b}
}

// Reset makes r read from b, clearing any error.
func (r *Reader) Reset(b []byte) {
	*r = Reader{buf: b}
}

// Err returns the first error encountered, or nil.
func (r *Reader) Err() error { return r.err }

// Position returns the number of bits read so far.
func (r *Reader) Position() int { return r.pos*8 - int(r.n) }

// BitsLeft returns the number of unread bits. It is zero after an error.
func (r *Reader) BitsLeft() int { return (len(r.buf)-r.pos)*8 + int(r.n) }

// refill tops up the cache to at least 56 bits, or as many as remain.
//
// Bits above r.n in the cache are either zero or the true next bits of the
// stream, so OR-ing overlapping data back in is harmless.
func (r *Reader) refill() {
	if r.pos+8 <= len(r.buf) {
		r.cache |= binary.LittleEndian.Uint64(r.buf[r.pos:]) << (r.n & 63)
		r.pos += int(63-r.n) >> 3 // whole bytes that fit
		r.n |= 56                 // r.n + 8*bytes, always 56..63
		return
	}
	for r.n < 56 && r.pos < len(r.buf) {
		r.cache |= uint64(r.buf[r.pos]) << (r.n & 63)
		r.pos++
		r.n += 8
	}
}

// fail records the first error, wrapping kind, and empties the reader so
// that every later read fails too. Formatting is skipped once an error is
// already set.
func (r *Reader) fail(kind error, format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("%w: %s", kind, fmt.Sprintf(format, args...))
	}
	r.pos = len(r.buf)
	r.cache, r.n = 0, 0
}

// overflow fails a read of n bits that the buffer cannot satisfy.
func (r *Reader) overflow(n int) {
	r.fail(ErrOverflow, "need %d bits at bit %d of %d", n, r.Position(), len(r.buf)*8)
}

// seek moves to an absolute bit offset, which must be within the buffer.
func (r *Reader) seek(bit int) {
	r.pos = bit >> 3
	r.cache, r.n = 0, 0
	if s := uint(bit & 7); s != 0 {
		r.cache = uint64(r.buf[r.pos]) >> s
		r.n = 8 - s
		r.pos++
	}
}

// ReadBit reads one bit.
func (r *Reader) ReadBit() bool {
	if r.n == 0 {
		return r.readBitSlow()
	}
	b := r.cache&1 != 0
	r.cache >>= 1
	r.n--
	return b
}

func (r *Reader) readBitSlow() bool {
	r.refill()
	if r.n == 0 {
		r.overflow(1)
		return false
	}
	return r.ReadBit()
}

// ReadBits reads an n-bit unsigned value, where n is at most 32.
// A larger n fails with ErrMalformed.
func (r *Reader) ReadBits(n uint) uint32 {
	if n > r.n || n > 32 {
		return r.readBitsSlow(n)
	}
	v := uint32(r.cache & (1<<n - 1))
	r.cache >>= n
	r.n -= n
	return v
}

func (r *Reader) readBitsSlow(n uint) uint32 {
	if n > 32 {
		r.fail(ErrMalformed, "ReadBits(%d) exceeds 32 bits", n)
		return 0
	}
	r.refill()
	if n > r.n {
		r.overflow(int(n))
		return 0
	}
	v := uint32(r.cache & (1<<n - 1))
	r.cache >>= n
	r.n -= n
	return v
}

// ReadUint8 reads 8 bits.
func (r *Reader) ReadUint8() uint8 { return uint8(r.ReadBits(8)) }

// ReadFloat32 reads 32 bits as an IEEE 754 single.
func (r *Reader) ReadFloat32() float32 { return math.Float32frombits(r.ReadBits(32)) }

// ubvExtra packs the number of extra bits ReadUBitVar reads (0, 4, 8 or 28)
// into bytes, indexed by bits 4 and 5 of the 6-bit prefix. Shifting a
// constant keeps the lookup in registers.
const ubvExtra uint32 = 28<<24 | 8<<16 | 4<<8

// ReadUBitVar reads Valve's variable-length unsigned int: a 6-bit prefix
// whose top two bits say how many more bits (0, 4, 8 or 28) extend its low
// four.
func (r *Reader) ReadUBitVar() uint32 {
	if r.n < 34 {
		r.refill()
		if r.n < 34 {
			return r.readUBitVarSlow()
		}
	}
	c := r.cache
	extra := uint(ubvExtra>>(c>>1&24)) & 0xFF
	// With no extra bits, bits 4 and 5 are zero, so c&0xF is the whole value.
	// The &63s change nothing but spare the compiler's oversized-shift checks.
	v := uint32(c&0xF) | uint32(c>>6&(1<<(extra&63)-1))<<4
	used := (6 + extra) & 63
	r.cache >>= used
	r.n -= used
	return v
}

func (r *Reader) readUBitVarSlow() uint32 {
	v := r.ReadBits(6)
	if extra := uint(ubvExtra>>(v>>1&24)) & 0xFF; extra != 0 {
		v = v&0xF | r.ReadBits(extra)<<4
	}
	if r.err != nil {
		return 0
	}
	return v
}

// ReadUBitVarFP's flag-bit counts (1, 2, 3, 4, 4) and payload widths (2, 4,
// 10, 17, 31), packed into bytes and indexed by the number of zero bits
// before the first one bit, where 4 means four zeros and no one bit.
const (
	fpPrefixes uint64 = 4<<32 | 4<<24 | 3<<16 | 2<<8 | 1
	fpWidths   uint64 = 31<<32 | 17<<24 | 10<<16 | 4<<8 | 2
)

func fpPrefix(i int) uint { return uint(fpPrefixes>>(i*8&63)) & 0xFF }
func fpWidth(i int) uint  { return uint(fpWidths>>(i*8&63)) & 0xFF }

// ReadUBitVarFP reads the variable-length int used in entity field paths: up
// to four flag bits select a payload of 2, 4, 10, 17 or 31 bits.
func (r *Reader) ReadUBitVarFP() uint32 {
	if r.n < 35 {
		r.refill()
		if r.n < 35 {
			return r.readUBitVarFPSlow()
		}
	}
	c := r.cache
	i := bits.TrailingZeros64(c | 0x10)
	p, w := fpPrefix(i), fpWidth(i)
	v := uint32(c >> (p & 63) & (1<<(w&63) - 1))
	used := (p + w) & 63
	r.cache >>= used
	r.n -= used
	return v
}

func (r *Reader) readUBitVarFPSlow() uint32 {
	i := 0
	for i < 4 && !r.ReadBit() {
		i++
	}
	v := r.ReadBits(fpWidth(i))
	if r.err != nil {
		return 0
	}
	return v
}

// ReadVarUint32 reads a protobuf-style base-128 varint of at most 5 bytes.
// The bytes need not be byte-aligned.
func (r *Reader) ReadVarUint32() uint32 {
	if r.n < 40 {
		r.refill()
		if r.n < 40 {
			return uint32(r.readVarUintSlow(5))
		}
	}
	c := r.cache
	if c&0x80 == 0 { // one byte, the common case
		r.cache >>= 8
		r.n -= 8
		return uint32(c & 0x7F)
	}
	// The first byte with its high bit clear is the last one.
	stop := ^c & 0x80_8080_8080
	if stop == 0 {
		r.fail(ErrMalformed, "varint longer than 5 bytes at bit %d", r.Position())
		return 0
	}
	used := uint(bits.TrailingZeros64(stop)) + 1 // 8, 16, 24, 32 or 40
	// Pack the five 7-bit groups together, then keep the ones that belong.
	x := c&0x7F | c>>1&(0x7F<<7) | c>>2&(0x7F<<14) | c>>3&(0x7F<<21) | c>>4&(0x7F<<28)
	x &= 1<<(used/8*7) - 1
	r.cache >>= used
	r.n -= used
	return uint32(x)
}

// ReadVarUint64 reads a protobuf-style base-128 varint of at most 10 bytes.
// The bytes need not be byte-aligned.
func (r *Reader) ReadVarUint64() uint64 {
	return r.readVarUintSlow(10)
}

// readVarUintSlow reads a varint of at most maxBytes bytes, one byte at a
// time.
func (r *Reader) readVarUintSlow(maxBytes int) uint64 {
	var x uint64
	for i := range maxBytes {
		b := r.ReadBits(8)
		x |= uint64(b&0x7F) << (7 * i)
		if b < 0x80 {
			if r.err != nil {
				return 0
			}
			return x
		}
	}
	r.fail(ErrMalformed, "varint longer than %d bytes at bit %d", maxBytes, r.Position()-maxBytes*8)
	return 0
}

// ReadVarInt32 reads a zigzag-encoded signed varint of at most 5 bytes.
func (r *Reader) ReadVarInt32() int32 {
	u := r.ReadVarUint32()
	return int32(u>>1) ^ -int32(u&1)
}

// ReadVarInt64 reads a zigzag-encoded signed varint of at most 10 bytes.
func (r *Reader) ReadVarInt64() int64 {
	u := r.ReadVarUint64()
	return int64(u>>1) ^ -int64(u&1)
}

// ReadBytes reads n bytes into a new slice, which the caller may keep. It
// returns nil on error.
func (r *Reader) ReadBytes(n int) []byte {
	if !r.haveBytes(n) {
		return nil
	}
	p := make([]byte, n)
	r.read(p)
	return p
}

// ReadBytesInto fills p from the stream without allocating. On error p is
// zeroed.
func (r *Reader) ReadBytesInto(p []byte) {
	if !r.haveBytes(len(p)) {
		clear(p)
		return
	}
	r.read(p)
}

// haveBytes reports whether n bytes remain, failing the reader if not.
func (r *Reader) haveBytes(n int) bool {
	if n < 0 {
		r.fail(ErrMalformed, "negative byte count %d", n)
		return false
	}
	if n > r.BitsLeft()>>3 {
		r.fail(ErrOverflow, "need %d bytes at bit %d of %d", n, r.Position(), len(r.buf)*8)
		return false
	}
	return true
}

// read fills p, which must fit in the remaining bits.
func (r *Reader) read(p []byte) {
	start := r.Position()
	i, s := start>>3, uint(start&7)
	if s == 0 {
		copy(p, r.buf[i:])
	} else {
		// Each output byte straddles two input bytes, so the last one needs
		// one byte past the end of p. It exists because start isn't aligned.
		src := r.buf[i : i+len(p)+1]
		j := 0
		for ; j+8 <= len(p); j += 8 {
			w := binary.LittleEndian.Uint64(src[j:])>>s | uint64(src[j+8])<<(64-s)
			binary.LittleEndian.PutUint64(p[j:], w)
		}
		for ; j < len(p); j++ {
			p[j] = src[j]>>s | src[j+1]<<(8-s)
		}
	}
	r.seek(start + len(p)*8)
}

// ReadString reads a zero-terminated string of at most MaxStringLen bytes
// and consumes the terminator. A longer string fails with ErrMalformed.
func (r *Reader) ReadString() string {
	start := r.Position()
	if start&7 == 0 {
		i := start >> 3
		end := min(len(r.buf), i+MaxStringLen+1)
		k := bytes.IndexByte(r.buf[i:end], 0)
		if k < 0 {
			if end-i > MaxStringLen {
				r.failLongString(start)
			} else {
				r.overflow((end - i + 1) * 8) // the bytes left plus a terminator
			}
			return ""
		}
		s := string(r.buf[i : i+k])
		r.seek(start + (k+1)*8)
		return s
	}
	var tmp [128]byte
	b := tmp[:0]
	for {
		c := byte(r.ReadBits(8))
		if c == 0 {
			if r.err != nil {
				return ""
			}
			return string(b)
		}
		if len(b) == MaxStringLen {
			r.failLongString(start)
			return ""
		}
		b = append(b, c)
	}
}

func (r *Reader) failLongString(start int) {
	r.fail(ErrMalformed, "string at bit %d longer than %d bytes", start, MaxStringLen)
}

// SkipBits discards n bits.
func (r *Reader) SkipBits(n int) {
	if n >= 0 && uint(n) <= r.n {
		r.cache >>= uint(n)
		r.n -= uint(n)
		return
	}
	if n < 0 {
		r.fail(ErrMalformed, "negative skip of %d bits", n)
		return
	}
	if n > r.BitsLeft() {
		r.overflow(n)
		return
	}
	r.seek(r.Position() + n)
}
