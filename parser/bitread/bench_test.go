package bitread

import (
	"math/rand/v2"
	"testing"
)

const benchCount = 4096 // values per stream

var sink uint64

// benchStream writes benchCount values plus 64 trailing bits.
func benchStream(write func(w *bitWriter, i int)) []byte {
	var w bitWriter
	for i := range benchCount {
		write(&w, i)
	}
	w.bits(0, 64)
	return w.buf
}

// reportPerValue adds an ns/value metric, given the number of full passes
// over a stream, and records acc so the reads are not optimized away.
func reportPerValue(b *testing.B, passes int, acc uint64) {
	sink = acc
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(passes*benchCount), "ns/value")
}

func BenchmarkReadBits(b *testing.B) {
	cases := []struct {
		name   string
		widths []uint
	}{
		{"1", []uint{1}},
		{"8", []uint{8}},
		{"17", []uint{17}},
		{"32", []uint{32}},
		{"mixed", []uint{1, 3, 5, 7, 9, 12, 17, 20, 32}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			widths := make([]uint, benchCount)
			for i := range widths {
				widths[i] = c.widths[i%len(c.widths)]
			}
			buf := benchStream(func(w *bitWriter, i int) { w.bits(uint64(i)*0x9E3779B97F4A7C15, int(widths[i])) })
			b.SetBytes(int64(len(buf)))
			var r Reader
			var acc uint64
			passes := 0
			for b.Loop() {
				r.Reset(buf)
				for _, n := range widths {
					acc += uint64(r.ReadBits(n))
				}
				passes++
			}
			reportPerValue(b, passes, acc)
		})
	}
}

func BenchmarkReadBit(b *testing.B) {
	buf := benchStream(func(w *bitWriter, i int) { w.bit(i%3 == 0) })
	b.SetBytes(int64(len(buf)))
	var r Reader
	var acc uint64
	passes := 0
	for b.Loop() {
		r.Reset(buf)
		for range benchCount {
			if r.ReadBit() {
				acc++
			}
		}
		passes++
	}
	reportPerValue(b, passes, acc)
}

// ubvValues returns values spread over all four UBitVar forms, weighted
// towards the small ones.
func ubvValues() []uint32 {
	rng := rand.New(rand.NewPCG(1, 2))
	v := make([]uint32, benchCount)
	for i := range v {
		switch p := rng.IntN(100); {
		case p < 50:
			v[i] = rng.Uint32N(1 << 4)
		case p < 80:
			v[i] = rng.Uint32N(1 << 8)
		case p < 95:
			v[i] = rng.Uint32N(1 << 12)
		default:
			v[i] = rng.Uint32()
		}
	}
	return v
}

func BenchmarkReadUBitVar(b *testing.B) {
	vals := ubvValues()
	buf := benchStream(func(w *bitWriter, i int) { w.ubitvar(vals[i], ubvMinForm(vals[i])) })
	b.SetBytes(int64(len(buf)))
	var r Reader
	var acc uint64
	passes := 0
	for b.Loop() {
		r.Reset(buf)
		for range benchCount {
			acc += uint64(r.ReadUBitVar())
		}
		passes++
	}
	reportPerValue(b, passes, acc)
}

func BenchmarkReadUBitVarFP(b *testing.B) {
	vals := ubvValues()
	buf := benchStream(func(w *bitWriter, i int) {
		v := vals[i] & (1<<31 - 1)
		w.ubitvarFP(v, fpMinForm(v))
	})
	b.SetBytes(int64(len(buf)))
	var r Reader
	var acc uint64
	passes := 0
	for b.Loop() {
		r.Reset(buf)
		for range benchCount {
			acc += uint64(r.ReadUBitVarFP())
		}
		passes++
	}
	reportPerValue(b, passes, acc)
}

func BenchmarkReadVarUint32(b *testing.B) {
	rng := rand.New(rand.NewPCG(3, 4))
	cases := []struct {
		name string
		gen  func() uint32
	}{
		{"1byte", func() uint32 { return rng.Uint32N(128) }},
		{"mixed", func() uint32 { return rng.Uint32() >> rng.UintN(32) }}, // 1 to 5 bytes
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			buf := benchStream(func(w *bitWriter, i int) {
				if i == 0 {
					w.bit(true) // keep every varint unaligned
				}
				w.varuint(uint64(c.gen()))
			})
			b.SetBytes(int64(len(buf)))
			var r Reader
			var acc uint64
			passes := 0
			for b.Loop() {
				r.Reset(buf)
				r.ReadBit()
				for range benchCount {
					acc += uint64(r.ReadVarUint32())
				}
				passes++
			}
			reportPerValue(b, passes, acc)
		})
	}
}

func BenchmarkReadBytesInto(b *testing.B) {
	for _, c := range []struct {
		name string
		lead int
	}{{"aligned", 0}, {"unaligned", 3}} {
		b.Run(c.name, func(b *testing.B) {
			var w bitWriter
			w.bits(0, c.lead)
			w.bytes(make([]byte, 1024))
			p := make([]byte, 1024)
			b.SetBytes(int64(len(p)))
			var r Reader
			for b.Loop() {
				r.Reset(w.buf)
				r.SkipBits(c.lead)
				r.ReadBytesInto(p)
			}
		})
	}
}
