package stringtables

import (
	"fmt"

	"github.com/klauspost/compress/snappy"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
	"github.com/ChiemekaKalu/demoview/parser/demo"
)

const (
	historySize    = 32 // keys remembered for prefix reuse
	historyBits    = 5  // bits in a history position
	prefixLenBits  = 5  // bits in a reused prefix length
	legacySizeBits = 17 // bits in a value length without VarintBitCounts
	maxPadBits     = 7  // bits that may follow the last entry

	// flagCompressedValues is the table flag that lets a variable-size value
	// be snappy-compressed. Each such value then carries a bit saying if it is.
	flagCompressedValues = 1
)

// decode applies n entries from a create or update message's string_data.
// Each entry is:
//
//	bit       1: the index is one more than the last; 0: a varint V follows
//	          and the index grows by V+2 (the index starts at -1)
//	bit       a key follows; without one, the entry keeps its key
//	  bit     the key starts with a prefix of a recent key:
//	    5     position in the key history, oldest first
//	    5     prefix length in bytes
//	  string  the rest of the key, zero-terminated
//	bit       a value follows; without one, the value is now empty
//	  fixed size:    UserDataSizeBits bits
//	  variable size: a compressed bit, only with flagCompressedValues
//	                 length in bytes: a UBitVar, or 17 bits without VarintBitCounts
//	                 the bytes, snappy-compressed if the bit was set
//
// The history holds the last 32 keys decoded in this message. An entry
// without a key adds nothing to it, which is unconfirmed: the fixture demos
// never send one after a keyed entry. Once the entries are read, at most 7
// bits of padding may remain.
func (t *Table) decode(data []byte, n int32) error {
	r := bitread.New(data)
	var h keyHistory
	idx := -1
	for e := range int(n) {
		var err error
		if idx, err = t.readEntry(r, &h, idx); err != nil {
			return t.errorf("entry %d of %d, index %d: %v", e, n, idx, err)
		}
	}
	if left := r.BitsLeft(); left > maxPadBits {
		return t.errorf("%d bits left over after %d entries", left, n)
	}
	return nil
}

// readEntry reads the entry that follows index prev, stores it, and returns
// its index.
func (t *Table) readEntry(r *bitread.Reader, h *keyHistory, prev int) (int, error) {
	idx := prev + 1
	if !r.ReadBit() {
		idx += int(r.ReadVarUint32()) + 1
	}
	var key *string
	if r.ReadBit() {
		k, err := h.read(r)
		if err != nil {
			return idx, err
		}
		key = &k
	}
	var value []byte
	if r.ReadBit() {
		var err error
		if value, err = t.readValue(r); err != nil {
			return idx, err
		}
	}
	if err := r.Err(); err != nil {
		return idx, err
	}
	if idx < 0 || idx > len(t.entries) {
		return idx, fmt.Errorf("index is past the end (%d entries)", len(t.entries))
	}
	t.set(idx, key, value)
	return idx, nil
}

// keyHistory holds the most recent keys, oldest first.
type keyHistory struct {
	keys [historySize]string
	n    int
}

// read reads a key and pushes it onto the history.
func (h *keyHistory) read(r *bitread.Reader) (string, error) {
	key := ""
	if r.ReadBit() {
		pos := int(r.ReadBits(historyBits))
		size := int(r.ReadBits(prefixLenBits))
		var prev string
		if pos < h.n {
			prev = h.keys[pos]
		}
		if size > len(prev) {
			return "", fmt.Errorf("key reuses %d bytes of history key %d %q", size, pos, prev)
		}
		key = prev[:size]
	}
	key += r.ReadString()
	if h.n == historySize {
		copy(h.keys[:], h.keys[1:])
		h.n--
	}
	h.keys[h.n] = key
	h.n++
	return key, nil
}

// readValue reads one value in t's encoding.
func (t *Table) readValue(r *bitread.Reader) ([]byte, error) {
	if t.UserDataFixedSize {
		return readFixed(r, int(t.UserDataSizeBits))
	}
	compressed := t.Flags&flagCompressedValues != 0 && r.ReadBit()
	var size uint32
	if t.VarintBitCounts {
		size = r.ReadUBitVar()
	} else {
		size = r.ReadBits(legacySizeBits)
	}
	b := r.ReadBytes(int(size))
	if !compressed || b == nil {
		return b, nil
	}
	return decompress(b)
}

// readFixed reads n bits into ceil(n/8) bytes, least significant bit first.
func readFixed(r *bitread.Reader, n int) ([]byte, error) {
	if n < 0 || n > r.BitsLeft() {
		return nil, fmt.Errorf("fixed-size value of %d bits with %d left", n, r.BitsLeft())
	}
	b := make([]byte, (n+7)/8)
	for i := range b {
		b[i] = byte(r.ReadBits(uint(min(8, n-8*i))))
	}
	return b, nil
}

func decompress(b []byte) ([]byte, error) {
	n, err := snappy.DecodedLen(b)
	if err != nil {
		return nil, fmt.Errorf("snappy: %w", err)
	}
	if n > demo.MaxPayloadSize {
		return nil, fmt.Errorf("snappy data decompresses to %d bytes, over limit %d", n, demo.MaxPayloadSize)
	}
	out, err := snappy.Decode(nil, b)
	if err != nil {
		return nil, fmt.Errorf("snappy: %w", err)
	}
	return out, nil
}
