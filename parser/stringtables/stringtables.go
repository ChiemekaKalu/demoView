// Package stringtables decodes a demo's string tables from the
// svc_CreateStringTable and svc_UpdateStringTable messages in its packets.
//
// A string table is a list of entries numbered from 0, each a key and a
// value of bytes. Tables are numbered in creation order, which is how
// updates name them.
//
// The DEM_StringTables and DEM_FullPacket snapshots repeat the tables as
// plain protobuf. On the fixture demos, the tables built from packets match
// every snapshot byte for byte, so applying snapshots is optional.
package stringtables

import (
	"fmt"
	"iter"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// Names of the tables that later stages read.
const (
	// InstanceBaseline holds entity baselines. A key is a class id in
	// decimal, or "<class id>:<n>" for extra baselines, which the 2024 MM
	// fixture has for CCSGOViewModel.
	InstanceBaseline = "instancebaseline"
	// UserInfo holds players. A key is a player slot in decimal, and a
	// value is a CMsgPlayerInfo, or empty for an empty slot.
	UserInfo = "userinfo"
)

// Packet message types that Tables.Apply handles.
const (
	MsgCreate   = demo.MsgType(pb.SVC_Messages_svc_CreateStringTable)
	MsgUpdate   = demo.MsgType(pb.SVC_Messages_svc_UpdateStringTable)
	MsgClearAll = demo.MsgType(pb.SVC_Messages_svc_ClearAllStringTables)
)

// Entry is one string-table entry.
type Entry struct {
	Key   string
	Value []byte
}

// Table is one string table. Its settings come from svc_CreateStringTable
// and say how values are encoded; don't change them.
type Table struct {
	ID    int // position in creation order: svc_UpdateStringTable's table_id
	Name  string
	Flags int32 // bit 0 lets variable-size values be snappy-compressed

	// UserDataFixedSize says every value is UserDataSizeBits bits long,
	// UserDataSize bytes rounded up.
	UserDataFixedSize bool
	UserDataSize      int32
	UserDataSizeBits  int32
	// VarintBitCounts says variable-size value lengths are UBitVars rather
	// than 17-bit counts.
	VarintBitCounts bool

	entries []Entry
	keys    map[string]int // entry index by key
}

// Len returns the number of entries.
func (t *Table) Len() int { return len(t.entries) }

// Entry returns the entry at index i, if there is one.
func (t *Table) Entry(i int) (Entry, bool) {
	if i < 0 || i >= len(t.entries) {
		return Entry{}, false
	}
	return t.entries[i], true
}

// Value returns the value of the entry with the given key, if there is one.
func (t *Table) Value(key string) ([]byte, bool) {
	i, ok := t.keys[key]
	if !ok {
		return nil, false
	}
	return t.entries[i].Value, true
}

// Entries yields every entry and its index, in index order.
//
// Values are never modified in place, since an update replaces the slice, so
// a caller may keep one.
func (t *Table) Entries() iter.Seq2[int, Entry] {
	return func(yield func(int, Entry) bool) {
		for i, e := range t.entries {
			if !yield(i, e) {
				return
			}
		}
	}
}

// set stores entry i, which may be at most Len. A nil key keeps the entry's
// key, but the value always replaces the old one.
func (t *Table) set(i int, key *string, value []byte) {
	if i == len(t.entries) {
		t.entries = append(t.entries, Entry{})
	}
	e := &t.entries[i]
	if key != nil {
		if j, ok := t.keys[e.Key]; ok && j == i {
			delete(t.keys, e.Key)
		}
		if t.keys == nil {
			t.keys = map[string]int{}
		}
		e.Key = *key
		t.keys[e.Key] = i
	}
	if len(value) == 0 {
		value = nil
	}
	e.Value = value
}

// Tables is the set of string tables created so far. The zero value has no
// tables and is ready to use.
type Tables struct {
	list   []*Table
	byName map[string]*Table
}

// Get returns the table with the given name, or nil if there is none.
func (ts *Tables) Get(name string) *Table { return ts.byName[name] }

// ByID returns the table with the given id, or nil if there is none.
func (ts *Tables) ByID(id int) *Table {
	if id < 0 || id >= len(ts.list) {
		return nil
	}
	return ts.list[id]
}

// All yields every table in id order.
func (ts *Tables) All() iter.Seq[*Table] {
	return func(yield func(*Table) bool) {
		for _, t := range ts.list {
			if !yield(t) {
				return
			}
		}
	}
}

// Apply applies m if it is a string-table message, and returns the table it
// created or changed. Any other message is ignored and gives nil, and so does
// svc_ClearAllStringTables, which removes every table. A malformed message
// gives an error wrapping demo.ErrBadDemo (see Create and Update).
func (ts *Tables) Apply(m demo.Message) (*Table, error) {
	switch m.Type {
	case MsgCreate:
		var c pb.CSVCMsg_CreateStringTable
		if err := m.Unmarshal(&c); err != nil {
			return nil, err
		}
		return ts.Create(&c)
	case MsgUpdate:
		var u pb.CSVCMsg_UpdateStringTable
		if err := m.Unmarshal(&u); err != nil {
			return nil, err
		}
		return ts.Update(&u)
	case MsgClearAll:
		ts.Clear()
	}
	return nil, nil
}

// Clear removes every table, as svc_ClearAllStringTables does. The next
// table created gets id 0.
func (ts *Tables) Clear() {
	ts.list = nil
	clear(ts.byName)
}

// Create adds the table that m describes, with its initial entries, and
// returns it. A name already in use gives an error, and so does malformed
// entry data; both wrap demo.ErrBadDemo, and leave ts unchanged.
func (ts *Tables) Create(m *pb.CSVCMsg_CreateStringTable) (*Table, error) {
	if ts.byName[m.GetName()] != nil {
		return nil, badDemof("table %q created twice", m.GetName())
	}
	t := &Table{
		ID:                len(ts.list),
		Name:              m.GetName(),
		Flags:             m.GetFlags(),
		UserDataFixedSize: m.GetUserDataFixedSize(),
		UserDataSize:      m.GetUserDataSize(),
		UserDataSizeBits:  m.GetUserDataSizeBits(),
		VarintBitCounts:   m.GetUsingVarintBitcounts(),
	}
	data := m.GetStringData()
	if m.GetDataCompressed() {
		var err error
		if data, err = decompress(data); err != nil {
			return nil, t.errorf("string_data: %v", err)
		}
		if want := m.GetUncompressedSize(); len(data) != int(want) {
			return nil, t.errorf("string_data decompresses to %d bytes, want %d", len(data), want)
		}
	}
	if err := t.decode(data, m.GetNumEntries()); err != nil {
		return nil, err
	}
	ts.add(t)
	return t, nil
}

// Update applies the changed entries in m to the table it names, and returns
// that table. An unknown table or malformed entry data gives an error
// wrapping demo.ErrBadDemo, and the entries before the bad one are applied.
func (ts *Tables) Update(m *pb.CSVCMsg_UpdateStringTable) (*Table, error) {
	t := ts.ByID(int(m.GetTableId()))
	if t == nil {
		return nil, badDemof("update for table %d, but %d tables exist", m.GetTableId(), len(ts.list))
	}
	if err := t.decode(m.GetStringData(), m.GetNumChangedEntries()); err != nil {
		return nil, err
	}
	return t, nil
}

// ApplySnapshot replaces the entries of each table in st with the
// snapshot's: a DEM_StringTables payload, or a DEM_FullPacket's
// string_table, which lists only the tables that changed. Item i of a
// snapshot table is entry i, and client-side items are ignored. A table that
// doesn't exist yet is created from its name and flags, with its other
// settings zero, as if svc_CreateStringTable had left them out.
func (ts *Tables) ApplySnapshot(st *pb.CDemoStringTables) {
	for _, s := range st.GetTables() {
		t := ts.Get(s.GetTableName())
		if t == nil {
			t = &Table{ID: len(ts.list), Name: s.GetTableName(), Flags: s.GetTableFlags()}
			ts.add(t)
		}
		t.entries = make([]Entry, 0, len(s.GetItems()))
		clear(t.keys)
		for i, it := range s.GetItems() {
			key := it.GetStr()
			t.set(i, &key, it.GetData())
		}
	}
}

// add appends t, whose ID must be the next one.
func (ts *Tables) add(t *Table) {
	ts.list = append(ts.list, t)
	if ts.byName == nil {
		ts.byName = map[string]*Table{}
	}
	ts.byName[t.Name] = t
}

// errorf returns an error wrapping demo.ErrBadDemo about table t.
func (t *Table) errorf(format string, args ...any) error {
	return badDemof("table %d (%s): %s", t.ID, t.Name, fmt.Sprintf(format, args...))
}

func badDemof(format string, args ...any) error {
	return fmt.Errorf("%w: stringtables: %s", demo.ErrBadDemo, fmt.Sprintf(format, args...))
}
