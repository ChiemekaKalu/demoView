package stringtables

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/klauspost/compress/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// varTable returns a create message for a table of variable-size values
// with UBitVar lengths, holding n entries encoded in w.
func varTable(name string, n int, w *bitWriter) *pb.CSVCMsg_CreateStringTable {
	return &pb.CSVCMsg_CreateStringTable{
		Name:                 proto.String(name),
		NumEntries:           proto.Int32(int32(n)),
		StringData:           w.buf,
		UsingVarintBitcounts: proto.Bool(true),
	}
}

func update(id, n int, w *bitWriter) *pb.CSVCMsg_UpdateStringTable {
	return &pb.CSVCMsg_UpdateStringTable{
		TableId:           proto.Int32(int32(id)),
		NumChangedEntries: proto.Int32(int32(n)),
		StringData:        w.buf,
	}
}

func mustCreate(t *testing.T, ts *Tables, m *pb.CSVCMsg_CreateStringTable) *Table {
	t.Helper()
	tbl, err := ts.Create(m)
	if err != nil {
		t.Fatal(err)
	}
	return tbl
}

// checkEntries fails unless tbl holds exactly want, in index order.
func checkEntries(t *testing.T, tbl *Table, want ...Entry) {
	t.Helper()
	if tbl.Len() != len(want) {
		t.Errorf("%s: %d entries, want %d", tbl.Name, tbl.Len(), len(want))
	}
	for i, w := range want {
		got, ok := tbl.Entry(i)
		if !ok || got.Key != w.Key || !bytes.Equal(got.Value, w.Value) {
			t.Errorf("%s[%d] = %q %q, want %q %q", tbl.Name, i, got.Key, got.Value, w.Key, w.Value)
		}
	}
}

func TestCreate(t *testing.T) {
	big := bytes.Repeat([]byte{0xA5}, 300) // a 2-byte UBitVar length
	var w bitWriter
	w.index(-1, 0)
	w.key("alpha")
	w.value([]byte("A"))
	w.index(0, 1)
	w.keyRef(0, 3, "ine")
	w.bit(false)
	w.index(1, 2)
	w.key("big")
	w.value(big)

	var ts Tables
	tbl := mustCreate(t, &ts, varTable("t", 3, &w))
	checkEntries(t, tbl, Entry{"alpha", []byte("A")}, Entry{"alpine", nil}, Entry{"big", big})
	if tbl.ID != 0 || tbl.Name != "t" || !tbl.VarintBitCounts || tbl.UserDataFixedSize {
		t.Errorf("table settings %+v", tbl)
	}
	if v, ok := tbl.Value("big"); !ok || !bytes.Equal(v, big) {
		t.Errorf(`Value("big") = %d bytes, %v`, len(v), ok)
	}
	if v, ok := tbl.Value("alpine"); !ok || v != nil {
		t.Errorf(`Value("alpine") = %q, %v; want nil, true`, v, ok)
	}
	if _, ok := tbl.Value("beta"); ok {
		t.Error(`Value("beta") found`)
	}
	if got := ts.Get("t"); got != tbl {
		t.Errorf(`Get("t") = %p, want %p`, got, tbl)
	}
	if got := ts.ByID(0); got != tbl {
		t.Errorf("ByID(0) = %p, want %p", got, tbl)
	}
	if ts.Get("u") != nil || ts.ByID(1) != nil || ts.ByID(-1) != nil {
		t.Error("lookup of a missing table found one")
	}
}

// TestKeyHistory checks that the history is a window of the last 32 keys,
// oldest first: after 34 keys, position 0 is the third, and after 35, the
// 34th key has moved to position 30.
func TestKeyHistory(t *testing.T) {
	var w bitWriter
	var want []Entry
	for i := range 34 {
		k := fmt.Sprintf("%02d-key", i)
		w.index(i-1, i)
		w.key(k)
		w.bit(false)
		want = append(want, Entry{Key: k})
	}
	w.index(33, 34)
	w.keyRef(0, 3, "first")
	w.bit(false)
	w.index(34, 35)
	w.keyRef(30, 6, "-last")
	w.bit(false)
	want = append(want, Entry{Key: "02-first"}, Entry{Key: "33-key-last"})

	var ts Tables
	checkEntries(t, mustCreate(t, &ts, varTable("t", 36, &w)), want...)
}

func TestFixedSize(t *testing.T) {
	var w bitWriter
	for i, v := range []uint64{0b10, 0xABC} {
		w.index(i-1, i)
		w.key(fmt.Sprint(i))
		w.bit(true)
		w.bits(v, 12)
	}
	var ts Tables
	tbl := mustCreate(t, &ts, &pb.CSVCMsg_CreateStringTable{
		Name:              proto.String("fixed"),
		NumEntries:        proto.Int32(2),
		UserDataFixedSize: proto.Bool(true),
		UserDataSize:      proto.Int32(2),
		UserDataSizeBits:  proto.Int32(12),
		StringData:        w.buf,
	})
	checkEntries(t, tbl, Entry{"0", []byte{0x02, 0x00}}, Entry{"1", []byte{0xBC, 0x0A}})
}

func TestLegacySizeBits(t *testing.T) {
	var w bitWriter
	w.index(-1, 0)
	w.key("k")
	w.bit(true)
	w.bits(3, 17)
	w.bytes([]byte("abc"))
	var ts Tables
	tbl := mustCreate(t, &ts, &pb.CSVCMsg_CreateStringTable{
		Name:       proto.String("legacy"),
		NumEntries: proto.Int32(1),
		StringData: w.buf,
	})
	checkEntries(t, tbl, Entry{"k", []byte("abc")})
}

func TestCompressed(t *testing.T) {
	plain := []byte("plain value")
	packed := bytes.Repeat([]byte("baseline "), 20)
	enc := snappy.Encode(nil, packed)

	var w bitWriter
	w.index(-1, 0)
	w.key("a")
	w.bit(true)
	w.bit(false) // not compressed
	w.ubitvar(uint32(len(plain)))
	w.bytes(plain)
	w.index(0, 1)
	w.key("b")
	w.bit(true)
	w.bit(true) // compressed
	w.ubitvar(uint32(len(enc)))
	w.bytes(enc)

	var ts Tables
	m := varTable("values", 2, &w)
	m.Flags = proto.Int32(flagCompressedValues)
	m.StringData = snappy.Encode(nil, w.buf)
	m.DataCompressed = proto.Bool(true)
	m.UncompressedSize = proto.Int32(int32(len(w.buf)))
	checkEntries(t, mustCreate(t, &ts, m), Entry{"a", plain}, Entry{"b", packed})
}

func TestUpdate(t *testing.T) {
	var w bitWriter
	for i, k := range []string{"a", "b", "c"} {
		w.index(i-1, i)
		w.key(k)
		w.value([]byte(strings.ToUpper(k)))
	}
	var ts Tables
	mustCreate(t, &ts, varTable("other", 0, &bitWriter{}))
	tbl := mustCreate(t, &ts, varTable("t", 3, &w))

	// Entry 0 gets a new value and keeps its key, entry 2 loses its value,
	// and entry 3 is new.
	w = bitWriter{}
	w.index(-1, 0)
	w.bit(false)
	w.value([]byte("new"))
	w.index(0, 2)
	w.bit(false)
	w.bit(false)
	w.index(2, 3)
	w.key("d")
	w.value([]byte("D"))
	got, err := ts.Update(update(1, 3, &w))
	if err != nil {
		t.Fatal(err)
	}
	if got != tbl {
		t.Errorf("Update returned table %d, want %d", got.ID, tbl.ID)
	}
	checkEntries(t, tbl, Entry{"a", []byte("new")}, Entry{"b", []byte("B")}, Entry{"c", nil}, Entry{"d", []byte("D")})

	// A new key replaces the old one in lookups.
	w = bitWriter{}
	w.index(-1, 1)
	w.key("bb")
	w.value([]byte("B2"))
	if _, err := ts.Update(update(1, 1, &w)); err != nil {
		t.Fatal(err)
	}
	if _, ok := tbl.Value("b"); ok {
		t.Error(`Value("b") still found after the key changed`)
	}
	if v, ok := tbl.Value("bb"); !ok || string(v) != "B2" {
		t.Errorf(`Value("bb") = %q, %v`, v, ok)
	}
}

func TestErrors(t *testing.T) {
	entries := func(f func(w *bitWriter)) []byte {
		var w bitWriter
		f(&w)
		return w.buf
	}
	oneEntry := entries(func(w *bitWriter) {
		w.index(-1, 0)
		w.key("k")
		w.value([]byte("v"))
	})
	tests := []struct {
		name   string
		create *pb.CSVCMsg_CreateStringTable
		update *pb.CSVCMsg_UpdateStringTable
		want   string
	}{
		{name: "unknown table", update: &pb.CSVCMsg_UpdateStringTable{TableId: proto.Int32(1)}, want: "update for table 1"},
		{name: "duplicate name", create: &pb.CSVCMsg_CreateStringTable{Name: proto.String("base")}, want: "created twice"},
		{name: "truncated", create: &pb.CSVCMsg_CreateStringTable{
			Name: proto.String("t"), NumEntries: proto.Int32(2), StringData: oneEntry, UsingVarintBitcounts: proto.Bool(true),
		}, want: "entry 1 of 2"},
		{name: "left over", create: &pb.CSVCMsg_CreateStringTable{
			Name: proto.String("t"), NumEntries: proto.Int32(1), StringData: append(oneEntry, 0), UsingVarintBitcounts: proto.Bool(true),
		}, want: "bits left over"},
		{name: "index past end", update: &pb.CSVCMsg_UpdateStringTable{
			TableId: proto.Int32(0), NumChangedEntries: proto.Int32(1), StringData: entries(func(w *bitWriter) {
				w.index(-1, 2)
				w.key("k")
				w.bit(false)
			}),
		}, want: "past the end"},
		{name: "prefix too long", create: &pb.CSVCMsg_CreateStringTable{
			Name: proto.String("t"), NumEntries: proto.Int32(2), UsingVarintBitcounts: proto.Bool(true), StringData: entries(func(w *bitWriter) {
				w.index(-1, 0)
				w.key("ab")
				w.bit(false)
				w.index(0, 1)
				w.keyRef(0, 3, "c")
				w.bit(false)
			}),
		}, want: "reuses 3 bytes"},
		{name: "fixed size overruns", create: &pb.CSVCMsg_CreateStringTable{
			Name: proto.String("t"), NumEntries: proto.Int32(1), UserDataFixedSize: proto.Bool(true), UserDataSizeBits: proto.Int32(64),
			StringData: entries(func(w *bitWriter) {
				w.index(-1, 0)
				w.key("k")
				w.bit(true)
				w.bits(0, 8)
			}),
		}, want: "fixed-size value of 64 bits"},
		{name: "bad snappy data", create: &pb.CSVCMsg_CreateStringTable{
			Name: proto.String("t"), DataCompressed: proto.Bool(true), StringData: []byte{0xFF},
		}, want: "snappy"},
		{name: "wrong uncompressed size", create: &pb.CSVCMsg_CreateStringTable{
			Name: proto.String("t"), DataCompressed: proto.Bool(true), StringData: snappy.Encode(nil, oneEntry), UncompressedSize: proto.Int32(9),
		}, want: "want 9"},
		{name: "bad snappy value", create: &pb.CSVCMsg_CreateStringTable{
			Name: proto.String("t"), NumEntries: proto.Int32(1), Flags: proto.Int32(flagCompressedValues), UsingVarintBitcounts: proto.Bool(true),
			StringData: entries(func(w *bitWriter) {
				w.index(-1, 0)
				w.key("k")
				w.bit(true)
				w.bit(true)
				w.ubitvar(1)
				w.bytes([]byte{0xFF})
			}),
		}, want: "snappy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ts Tables
			mustCreate(t, &ts, &pb.CSVCMsg_CreateStringTable{Name: proto.String("base")})
			var err error
			if tt.create != nil {
				_, err = ts.Create(tt.create)
			} else {
				_, err = ts.Update(tt.update)
			}
			if !errors.Is(err, demo.ErrBadDemo) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want ErrBadDemo containing %q", err, tt.want)
			}
			if ts.ByID(1) != nil {
				t.Error("a failed create added a table")
			}
		})
	}
}

func TestApply(t *testing.T) {
	msg := func(typ demo.MsgType, m proto.Message) demo.Message {
		body, err := proto.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return demo.Message{Type: typ, Body: body}
	}
	var w bitWriter
	w.index(-1, 0)
	w.key("k")
	w.value([]byte("v"))
	create := msg(MsgCreate, varTable("t", 1, &w))

	var ts Tables
	tbl, err := ts.Apply(create)
	if err != nil || tbl == nil || tbl.Name != "t" {
		t.Fatalf("Apply(create) = %v, %v", tbl, err)
	}
	w = bitWriter{}
	w.index(-1, 0)
	w.bit(false)
	w.value([]byte("v2"))
	if got, err := ts.Apply(msg(MsgUpdate, update(0, 1, &w))); err != nil || got != tbl {
		t.Fatalf("Apply(update) = %v, %v", got, err)
	}
	checkEntries(t, tbl, Entry{"k", []byte("v2")})

	if got, err := ts.Apply(msg(demo.MsgType(pb.SVC_Messages_svc_ServerInfo), &pb.CSVCMsg_ServerInfo{})); got != nil || err != nil {
		t.Errorf("Apply(svc_ServerInfo) = %v, %v; want nil, nil", got, err)
	}
	if _, err := ts.Apply(demo.Message{Type: MsgUpdate, Body: []byte{0xFF}}); !errors.Is(err, demo.ErrBadDemo) {
		t.Errorf("Apply(malformed body) err = %v, want ErrBadDemo", err)
	}

	if got, err := ts.Apply(msg(MsgClearAll, &pb.CSVCMsg_ClearAllStringTables{})); got != nil || err != nil {
		t.Errorf("Apply(clear) = %v, %v; want nil, nil", got, err)
	}
	if ts.Get("t") != nil || ts.ByID(0) != nil {
		t.Error("table still there after svc_ClearAllStringTables")
	}
	if tbl, err := ts.Apply(create); err != nil || tbl.ID != 0 {
		t.Errorf("create after clear = %v, %v; want table 0", tbl, err)
	}
}

func TestApplySnapshot(t *testing.T) {
	var w bitWriter
	for i, k := range []string{"a", "b", "c"} {
		w.index(i-1, i)
		w.key(k)
		w.value([]byte(k))
	}
	var ts Tables
	tbl := mustCreate(t, &ts, varTable("t", 3, &w))
	keep := mustCreate(t, &ts, varTable("keep", 0, &bitWriter{}))

	item := func(k, v string) *pb.CDemoStringTablesItemsT {
		it := &pb.CDemoStringTablesItemsT{Str: proto.String(k)}
		if v != "" {
			it.Data = []byte(v)
		}
		return it
	}
	ts.ApplySnapshot(&pb.CDemoStringTables{Tables: []*pb.CDemoStringTablesTableT{
		{TableName: proto.String("t"), Items: []*pb.CDemoStringTablesItemsT{item("x", "X"), item("b", "")}},
		{TableName: proto.String("new"), TableFlags: proto.Int32(2), Items: []*pb.CDemoStringTablesItemsT{item("n", "N")}},
	}})

	checkEntries(t, tbl, Entry{"x", []byte("X")}, Entry{"b", nil})
	if _, ok := tbl.Value("a"); ok {
		t.Error(`Value("a") found after the snapshot replaced it`)
	}
	if ts.Get("keep") != keep || keep.Len() != 0 {
		t.Error("a table the snapshot doesn't list changed")
	}
	nt := ts.Get("new")
	if nt == nil || nt.ID != 2 || nt.Flags != 2 {
		t.Fatalf(`Get("new") = %+v, want table 2 with flags 2`, nt)
	}
	checkEntries(t, nt, Entry{"n", []byte("N")})
}
