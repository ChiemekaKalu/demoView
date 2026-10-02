package stringtables

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/players"
)

// demoWalk applies every string-table message in a demo and checks the
// result against each snapshot as it arrives.
type demoWalk struct {
	t       *testing.T
	tables  Tables           // from packets alone
	seeded  Tables           // from packets, with every snapshot applied too
	classes map[int32]string // network name by class id, from DEM_ClassInfo
	creates int              // svc_CreateStringTable messages
	updates int              // svc_UpdateStringTable messages
	checked map[string]int   // snapshot comparisons, by table
	lastAt  map[string]int32 // tick of the last snapshot listing each table
	slots   int              // userinfo slots cross-checked with parser/players
}

func walkDemo(t *testing.T, r io.Reader) *demoWalk {
	t.Helper()
	dr, err := demo.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	d := demo.NewDecoder(dr)
	var mr demo.MessageReader
	w := &demoWalk{t: t, classes: map[int32]string{}, checked: map[string]int{}, lastAt: map[string]int32{}}
	for {
		f, err := d.Next()
		if err == io.EOF {
			return w
		}
		if err != nil {
			t.Fatal(err)
		}
		switch f.Cmd {
		case demo.CmdClassInfo:
			var ci pb.CDemoClassInfo
			if err := f.Unmarshal(&ci); err != nil {
				t.Fatal(err)
			}
			for _, c := range ci.GetClasses() {
				w.classes[c.GetClassId()] = c.GetNetworkName()
			}
		case demo.CmdStringTables:
			var st pb.CDemoStringTables
			if err := f.Unmarshal(&st); err != nil {
				t.Fatal(err)
			}
			w.snapshot(f, &st)
		case demo.CmdFullPacket:
			var fp pb.CDemoFullPacket
			if err := f.Unmarshal(&fp); err != nil {
				t.Fatal(err)
			}
			w.snapshot(f, fp.GetStringTable())
			w.packet(&mr, f)
		case demo.CmdPacket, demo.CmdSignonPacket:
			w.packet(&mr, f)
		}
	}
}

// packet applies the string-table messages in f. Apply fails on any entry
// list that doesn't decode or leaves a byte or more unread.
func (w *demoWalk) packet(mr *demo.MessageReader, f demo.Frame) {
	if err := mr.Reset(f); err != nil {
		w.t.Fatal(err)
	}
	for {
		m, err := mr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			w.t.Fatal(err)
		}
		switch m.Type {
		case MsgCreate:
			w.creates++
		case MsgUpdate:
			w.updates++
		}
		for _, ts := range []*Tables{&w.tables, &w.seeded} {
			if _, err := ts.Apply(m); err != nil {
				w.t.Fatalf("%s frame at offset %d, tick %d: %v", f.Cmd, f.Offset, f.Tick, err)
			}
		}
	}
}

// snapshot checks every table that st lists against the tables built from
// packets so far, and cross-checks userinfo with parser/players.
func (w *demoWalk) snapshot(f demo.Frame, st *pb.CDemoStringTables) {
	for _, s := range st.GetTables() {
		name := s.GetTableName()
		tbl := w.tables.Get(name)
		if tbl == nil {
			w.t.Errorf("tick %d: snapshot lists table %q, which packets never created", f.Tick, name)
			continue
		}
		w.checked[name]++
		w.lastAt[name] = f.Tick
		if tbl.Len() != len(s.GetItems()) {
			w.t.Errorf("tick %d: %s has %d entries, snapshot has %d", f.Tick, name, tbl.Len(), len(s.GetItems()))
		}
		for i, it := range s.GetItems() {
			e, _ := tbl.Entry(i)
			if e.Key != it.GetStr() || !bytes.Equal(e.Value, it.GetData()) {
				w.t.Errorf("tick %d: %s[%d] = %q (%d bytes), snapshot has %q (%d bytes)",
					f.Tick, name, i, e.Key, len(e.Value), it.GetStr(), len(it.GetData()))
			}
		}
		if name == UserInfo {
			w.crossCheckPlayers(f, st)
		}
	}
	w.seeded.ApplySnapshot(st)
}

// crossCheckPlayers checks that the userinfo table built from packets names
// the same player in each slot as parser/players reads from st.
func (w *demoWalk) crossCheckPlayers(f demo.Frame, st *pb.CDemoStringTables) {
	got := map[players.Slot]players.Info{}
	for _, e := range w.tables.Get(UserInfo).Entries() {
		if e.Value == nil {
			continue
		}
		slot, err := strconv.ParseUint(e.Key, 10, 8)
		if err != nil {
			w.t.Fatalf("tick %d: userinfo key %q is not a slot", f.Tick, e.Key)
		}
		info, err := players.ParsePlayerInfo(e.Value)
		if err != nil {
			w.t.Fatalf("tick %d: %v", f.Tick, err)
		}
		got[players.Slot(slot)] = info
	}
	var want players.Table
	if err := want.ApplyStringTables(st); err != nil {
		w.t.Fatal(err)
	}
	for slot, p := range want.All() {
		g, ok := got[slot]
		if !ok || g.Name != p.Name || g.SteamID64 != p.SteamID64 {
			w.t.Errorf("tick %d, slot %d: packets give %q %d (present: %v), snapshot gives %q %d",
				f.Tick, slot, g.Name, g.SteamID64, ok, p.Name, p.SteamID64)
		}
		delete(got, slot)
		w.slots++
	}
	for slot, g := range got {
		w.t.Errorf("tick %d, slot %d: packets give %q, snapshot has no one", f.Tick, slot, g.Name)
	}
}

func TestDemos(t *testing.T) {
	if testing.Short() {
		t.Skip("reads whole demos")
	}
	tests := []struct {
		file             string
		creates, updates int
	}{
		{"faceit_nuke_2026.dem", 13, 50},
		{"valve_mm_mirage_2024.dem", 12, 25},
	}
	for _, tt := range tests {
		t.Run(strings.TrimSuffix(tt.file, ".dem"), func(t *testing.T) {
			f, err := os.Open(filepath.Join("..", "testdata", "demos", tt.file))
			if errors.Is(err, fs.ErrNotExist) {
				t.Skipf("fixture not present: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			w := walkDemo(t, f)

			t.Logf("%d creates, %d updates; %d userinfo slots cross-checked with parser/players", w.creates, w.updates, w.slots)
			t.Logf("%2s  %-22s %7s  %s", "id", "table", "entries", "snapshots matched (last tick)")
			for tbl := range w.tables.All() {
				t.Logf("%2d  %-22s %7d  %d (%d)", tbl.ID, tbl.Name, tbl.Len(), w.checked[tbl.Name], w.lastAt[tbl.Name])
			}
			if w.creates != tt.creates || w.updates != tt.updates {
				t.Errorf("%d creates and %d updates, want %d and %d", w.creates, w.updates, tt.creates, tt.updates)
			}
			for _, name := range []string{InstanceBaseline, UserInfo} {
				if w.checked[name] == 0 {
					t.Errorf("no snapshot listed %s", name)
				}
			}
			if w.slots == 0 {
				t.Error("no userinfo slots cross-checked")
			}
			checkBaselines(t, w.tables.Get(InstanceBaseline), w.classes)
			checkSame(t, &w.seeded, &w.tables)
		})
	}
}

// checkBaselines checks that every instancebaseline key names a class, and
// that player pawns have a baseline.
func checkBaselines(t *testing.T, tbl *Table, classes map[int32]string) {
	t.Helper()
	if tbl == nil || tbl.Len() == 0 {
		t.Fatal("no instancebaseline entries")
	}
	extra := 0
	for i, e := range tbl.Entries() {
		id, n, hasN := strings.Cut(e.Key, ":")
		class, err := strconv.ParseInt(id, 10, 32)
		if _, ok := classes[int32(class)]; err != nil || !ok {
			t.Errorf("instancebaseline[%d] key %q is not a class id", i, e.Key)
		}
		if hasN {
			extra++
			if _, err := strconv.Atoi(n); err != nil {
				t.Errorf("instancebaseline[%d] key %q: suffix is not a number", i, e.Key)
			}
		}
		if len(e.Value) == 0 {
			t.Errorf("instancebaseline[%d] (%s) is empty", i, e.Key)
		}
	}
	t.Logf("instancebaseline: %d entries for %d classes, %d of them <class>:<n>", tbl.Len(), len(classes), extra)
	for id, name := range classes {
		if name != "CCSPlayerPawn" {
			continue
		}
		if _, ok := tbl.Value(strconv.Itoa(int(id))); !ok {
			t.Errorf("no baseline for CCSPlayerPawn (class %d)", id)
		}
	}
}

// checkSame checks that two sets of tables hold the same entries.
func checkSame(t *testing.T, got, want *Tables) {
	t.Helper()
	for w := range want.All() {
		g := got.ByID(w.ID)
		if g == nil || g.Name != w.Name || g.Len() != w.Len() {
			t.Errorf("seeded table %d is %v, want %s with %d entries", w.ID, g, w.Name, w.Len())
			continue
		}
		for i, we := range w.Entries() {
			if ge, _ := g.Entry(i); ge.Key != we.Key || !bytes.Equal(ge.Value, we.Value) {
				t.Errorf("seeded %s[%d] = %q, want %q", w.Name, i, ge.Key, we.Key)
			}
		}
	}
}
