// Package parsetest builds synthetic demo files for tests: frames, packets
// and the bit-packed messages inside them.
package parsetest

import (
	"encoding/binary"

	"github.com/klauspost/compress/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// Frame is one frame for Build.
type Frame struct {
	Cmd      demo.Command
	Tick     int32
	Compress bool   // snappy-compress the payload
	Payload  []byte // decompressed
}

// Build returns a demo file holding frames, after a preamble with zero
// offsets.
func Build(frames ...Frame) []byte {
	b := append([]byte(nil), demo.Magic[:]...)
	b = binary.LittleEndian.AppendUint32(b, 0) // fileinfo offset
	b = binary.LittleEndian.AppendUint32(b, 0) // spawngroups offset
	for _, f := range frames {
		cmd, p := uint64(f.Cmd), f.Payload
		if f.Compress {
			cmd |= 0x40
			p = snappy.Encode(nil, p)
		}
		b = binary.AppendUvarint(b, cmd)
		b = binary.AppendUvarint(b, uint64(uint32(f.Tick)))
		b = binary.AppendUvarint(b, uint64(len(p)))
		b = append(b, p...)
	}
	return b
}

// Marshal marshals m, panicking on error.
func Marshal(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

// Msg is one packet message.
type Msg struct {
	Type uint32
	Body []byte
}

// PacketBits returns the bit stream of msgs and the number of padding bits
// after the last one.
func PacketBits(msgs ...Msg) ([]byte, int) {
	var w bitWriter
	for _, m := range msgs {
		w.ubitvar(m.Type)
		w.varuint(uint64(len(m.Body)))
		w.bytes(m.Body)
	}
	return w.buf, len(w.buf)*8 - w.n
}

// Packet returns a marshaled CDemoPacket holding msgs, the payload of a
// Packet, SignonPacket or FullPacket frame.
func Packet(msgs ...Msg) []byte {
	data, _ := PacketBits(msgs...)
	return Marshal(&pb.CDemoPacket{Data: data})
}

// bitWriter builds bit streams least significant bit first. It mirrors the
// one in package bitread's tests.
type bitWriter struct {
	buf []byte
	n   int // bits written
}

func (w *bitWriter) bits(v uint64, n int) {
	for i := range n {
		if w.n%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		w.buf[w.n/8] |= byte(v>>i&1) << (w.n % 8)
		w.n++
	}
}

func (w *bitWriter) bytes(p []byte) {
	for _, c := range p {
		w.bits(uint64(c), 8)
	}
}

// ubitvar writes v in the shortest UBitVar form.
func (w *bitWriter) ubitvar(v uint32) {
	form := 3
	switch {
	case v < 1<<4:
		form = 0
	case v < 1<<8:
		form = 1
	case v < 1<<12:
		form = 2
	}
	w.bits(uint64(v&0xF|uint32(form)<<4), 6)
	w.bits(uint64(v>>4), [4]int{0, 4, 8, 28}[form])
}

func (w *bitWriter) varuint(v uint64) {
	for v >= 0x80 {
		w.bits(v&0x7F|0x80, 8)
		v >>= 7
	}
	w.bits(v, 8)
}
