package bitread

// bitWriter builds test streams one bit at a time. It is deliberately
// naive so that it can serve as the reference for Reader.
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

// ubvForms are the extra-bit counts of the four UBitVar encodings.
var ubvForms = [4]int{0, 4, 8, 28}

// ubvMinForm returns the shortest UBitVar form that can hold v.
func ubvMinForm(v uint32) int {
	switch {
	case v < 1<<4:
		return 0
	case v < 1<<8:
		return 1
	case v < 1<<12:
		return 2
	}
	return 3
}

// ubitvar writes v in the given form, which must be able to hold it.
func (w *bitWriter) ubitvar(v uint32, form int) {
	w.bits(uint64(v&0xF|uint32(form)<<4), 6)
	w.bits(uint64(v>>4), ubvForms[form])
}

// fpForms are the payload widths of the five UBitVarFP encodings. They are
// spelled out again here, rather than shared with the reader, so a mistake
// in either shows up as a mismatch.
var fpForms = [5]int{2, 4, 10, 17, 31}

// fpMinForm returns the shortest UBitVarFP form that can hold v.
func fpMinForm(v uint32) int {
	for i, width := range fpForms {
		if v < 1<<width {
			return i
		}
	}
	panic("UBitVarFP value too large")
}

// ubitvarFP writes v in the given form (0..4), which must be able to hold it.
func (w *bitWriter) ubitvarFP(v uint32, form int) {
	for range form {
		w.bit(false)
	}
	if form < 4 {
		w.bit(true)
	}
	w.bits(uint64(v), fpForms[form])
}

func (w *bitWriter) varuint(v uint64) {
	for v >= 0x80 {
		w.bits(v&0x7F|0x80, 8)
		v >>= 7
	}
	w.bits(v, 8)
}

func (w *bitWriter) varint32(v int32) {
	w.varuint(uint64(uint32(v<<1) ^ uint32(v>>31)))
}

func (w *bitWriter) varint64(v int64) {
	w.varuint(uint64(v<<1) ^ uint64(v>>63))
}
