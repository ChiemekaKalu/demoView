package stringtables

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/klauspost/compress/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// tableSeed is one FuzzApply input: a table's settings, and the entry data
// of a create message and of an update message for it.
type tableSeed struct {
	flags         int32
	fixed, varint bool
	sizeBits      int32
	compress      bool // snappy-compress the create message's string data
	n             int32
	data          []byte
	changed       int32
	update        []byte
}

// FuzzApply creates a table from fuzzed settings and entry data, then
// updates it, both through Apply. Each message must apply or fail with
// ErrBadDemo, without a panic, and the table must stay consistent: no more
// entries than the messages carried, and fixed-size values of the declared
// size. The seeds come from the test bit writer. The spec's exit check runs
// it for 10 minutes:
//
//	go test ./stringtables -run '^$' -fuzz '^FuzzApply$' -fuzztime 10m
func FuzzApply(f *testing.F) {
	for _, s := range applySeeds() {
		f.Add(s.flags, s.fixed, s.varint, s.sizeBits, s.compress, s.n, s.data, s.changed, s.update)
	}
	f.Fuzz(func(t *testing.T, flags int32, fixed, varint bool, sizeBits int32, compress bool, n int32, data []byte, changed int32, upd []byte) {
		create := &pb.CSVCMsg_CreateStringTable{
			Name:                 proto.String("t"),
			NumEntries:           proto.Int32(n),
			Flags:                proto.Int32(flags),
			UserDataFixedSize:    proto.Bool(fixed),
			UserDataSizeBits:     proto.Int32(sizeBits),
			UsingVarintBitcounts: proto.Bool(varint),
			StringData:           data,
		}
		if compress {
			create.StringData = snappy.Encode(nil, data)
			create.DataCompressed = proto.Bool(true)
			create.UncompressedSize = proto.Int32(int32(len(data)))
		}
		var ts Tables
		tbl := apply(t, &ts, MsgCreate, create)
		if tbl == nil {
			return
		}
		created := tbl.Len()
		if created > max(0, int(n)) {
			t.Fatalf("%d entries from a create message of %d", created, n)
		}
		apply(t, &ts, MsgUpdate, &pb.CSVCMsg_UpdateStringTable{
			TableId:           proto.Int32(0),
			NumChangedEntries: proto.Int32(changed),
			StringData:        upd,
		})
		if tbl.Len() > created+max(0, int(changed)) {
			t.Fatalf("%d entries after %d and an update of %d", tbl.Len(), created, changed)
		}
		for i, e := range tbl.Entries() {
			if fixed && len(e.Value) != 0 && len(e.Value) != (int(sizeBits)+7)/8 {
				t.Fatalf("entry %d: %d-byte value in a table of %d-bit values", i, len(e.Value), sizeBits)
			}
		}
	})
}

// apply applies m as a packet message of type typ, failing the test on an
// error that isn't ErrBadDemo.
func apply(t *testing.T, ts *Tables, typ demo.MsgType, m proto.Message) *Table {
	t.Helper()
	body, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := ts.Apply(demo.Message{Type: typ, Body: body})
	if err != nil && !errors.Is(err, demo.ErrBadDemo) {
		t.Fatalf("Apply %s: %v, want ErrBadDemo", typ, err)
	}
	return tbl
}

// applySeeds are valid inputs for FuzzApply, one for each value encoding.
func applySeeds() []tableSeed {
	bits := func(write func(w *bitWriter)) []byte {
		var w bitWriter
		write(&w)
		return w.buf
	}
	packed := snappy.Encode(nil, bytes.Repeat([]byte("baseline "), 8))
	return []tableSeed{
		{},
		{ // UBitVar lengths and a key that reuses history
			varint: true, n: 3, changed: 2,
			data: bits(func(w *bitWriter) {
				w.index(-1, 0)
				w.key("alpha")
				w.value([]byte("A"))
				w.index(0, 1)
				w.keyRef(0, 3, "ine")
				w.bit(false)
				w.index(1, 2)
				w.key("big")
				w.value(bytes.Repeat([]byte{0xA5}, 300))
			}),
			update: bits(func(w *bitWriter) {
				w.index(-1, 0)
				w.bit(false)
				w.value([]byte("new"))
				w.index(0, 3)
				w.key("d")
				w.value([]byte("D"))
			}),
		},
		{ // fixed-size values
			fixed: true, sizeBits: 12, n: 2, changed: 1,
			data: bits(func(w *bitWriter) {
				for i, v := range []uint64{0b10, 0xABC} {
					w.index(i-1, i)
					w.key(fmt.Sprint(i))
					w.bit(true)
					w.bits(v, 12)
				}
			}),
			update: bits(func(w *bitWriter) {
				w.index(-1, 1)
				w.bit(false)
				w.bit(true)
				w.bits(0x123, 12)
			}),
		},
		{ // 17-bit lengths and snappy values, in compressed string data
			flags: flagCompressedValues, compress: true, n: 2,
			data: bits(func(w *bitWriter) {
				w.index(-1, 0)
				w.key("a")
				w.bit(true)
				w.bit(false)
				w.bits(3, 17)
				w.bytes([]byte("abc"))
				w.index(0, 1)
				w.key("b")
				w.bit(true)
				w.bit(true)
				w.bits(uint64(len(packed)), 17)
				w.bytes(packed)
			}),
		},
	}
}
