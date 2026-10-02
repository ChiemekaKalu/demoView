package stringtables

// bitWriter builds test streams one bit at a time, in the same naive way as
// the one in parser/bitread, so that it can serve as a reference.
type bitWriter struct {
	buf []byte
	n   int // bits written
}

func (w *bitWriter) bit(b bool) {
	if w.n%8 == 0 {
		w.buf = append(w.buf, 0)
	}
	if b {
		w.buf[w.n/8] |= 1 << (w.n % 8)
	}
	w.n++
}

// bits writes the low n bits of v, least significant first.
func (w *bitWriter) bits(v uint64, n int) {
	for i := range n {
		w.bit(v>>i&1 != 0)
	}
}

func (w *bitWriter) bytes(p []byte) {
	for _, c := range p {
		w.bits(uint64(c), 8)
	}
}

func (w *bitWriter) str(s string) {
	w.bytes([]byte(s))
	w.bits(0, 8)
}

func (w *bitWriter) varuint(v uint64) {
	for v >= 0x80 {
		w.bits(v&0x7F|0x80, 8)
		v >>= 7
	}
	w.bits(v, 8)
}

// ubitvar writes v in the shortest UBitVar form: a 6-bit prefix whose top
// two bits pick 0, 4, 8 or 28 more bits above its low four.
func (w *bitWriter) ubitvar(v uint32) {
	form, extra := 0, 0
	switch {
	case v >= 1<<12:
		form, extra = 3, 28
	case v >= 1<<8:
		form, extra = 2, 8
	case v >= 1<<4:
		form, extra = 1, 4
	}
	w.bits(uint64(v&0xF)|uint64(form)<<4, 6)
	w.bits(uint64(v>>4), extra)
}

// index writes the step from index prev to index i.
func (w *bitWriter) index(prev, i int) {
	if i == prev+1 {
		w.bit(true)
		return
	}
	w.bit(false)
	w.varuint(uint64(i - prev - 2))
}

// key writes a key that reuses nothing from the history.
func (w *bitWriter) key(s string) {
	w.bit(true)
	w.bit(false)
	w.str(s)
}

// keyRef writes a key made of the first n bytes of history key pos, then
// rest.
func (w *bitWriter) keyRef(pos, n int, rest string) {
	w.bit(true)
	w.bit(true)
	w.bits(uint64(pos), 5)
	w.bits(uint64(n), 5)
	w.str(rest)
}

// value writes a variable-size value with a UBitVar length, for a table
// without flagCompressedValues.
func (w *bitWriter) value(v []byte) {
	w.bit(true)
	w.ubitvar(uint32(len(v)))
	w.bytes(v)
}
