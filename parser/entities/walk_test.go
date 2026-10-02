package entities_test

import (
	"io"
	"os"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/stringtables"
)

// walker decodes a demo's entities frame by frame.
type walker struct {
	classes *entities.Classes
	tables  stringtables.Tables
	state   *entities.State
	packets int // svc_PacketEntities applied

	// before and after, if set, run around each frame, with the state as
	// of the end of the last frame and of this one.
	before, after func(f demo.Frame)
	// snapshot, if set, gets each DEM_FullPacket's svc_PacketEntities.
	snapshot func(f demo.Frame, m *pb.CSVCMsg_PacketEntities)
	// collect, if set, gets each svc_PacketEntities that is applied.
	collect func(m *pb.CSVCMsg_PacketEntities)
}

// run walks the demo at path to its end. Any error fails the test.
func (w *walker) run(t testing.TB, path string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	r, err := demo.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	d := demo.NewDecoder(r)
	var mr demo.MessageReader
	var sers *entities.Serializers
	for {
		f, err := d.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if w.before != nil {
			w.before(f)
		}
		switch f.Cmd {
		case demo.CmdSendTables:
			var st pb.CDemoSendTables
			if err := f.Unmarshal(&st); err != nil {
				t.Fatal(err)
			}
			if sers, err = entities.DecodeSendTables(&st); err != nil {
				t.Fatal(err)
			}
		case demo.CmdClassInfo:
			var ci pb.CDemoClassInfo
			if err := f.Unmarshal(&ci); err != nil {
				t.Fatal(err)
			}
			if w.classes, err = entities.NewClasses(&ci, sers); err != nil {
				t.Fatal(err)
			}
			w.state = entities.NewState(w.classes)
		case demo.CmdPacket, demo.CmdSignonPacket, demo.CmdFullPacket:
			w.packet(t, &mr, f)
		}
		if w.after != nil {
			w.after(f)
		}
	}
}

// packet applies the string-table and entity messages in f. A
// DEM_FullPacket's entities are a snapshot of the state the packet before it
// left, which leaves some fields out, so they apply only to an empty state.
func (w *walker) packet(t testing.TB, mr *demo.MessageReader, f demo.Frame) {
	t.Helper()
	if err := mr.Reset(f); err != nil {
		t.Fatal(err)
	}
	for {
		m, err := mr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.tables.Apply(m); err != nil {
			t.Fatal(err)
		}
		if m.Type != entities.MsgPacketEntities {
			continue
		}
		pe := new(pb.CSVCMsg_PacketEntities)
		if err := m.Unmarshal(pe); err != nil {
			t.Fatal(err)
		}
		if f.Cmd == demo.CmdFullPacket && w.packets > 0 {
			if w.snapshot != nil {
				w.snapshot(f, pe)
			}
			continue
		}
		if err := w.state.Apply(pe, w.tables.Get(stringtables.InstanceBaseline)); err != nil {
			t.Fatalf("%s frame at offset %d, tick %d: %v", f.Cmd, f.Offset, f.Tick, err)
		}
		w.packets++
		if w.collect != nil {
			w.collect(pe)
		}
	}
}

// ref resolves a field of the named class, failing the test if it can't.
func (w *walker) ref(t testing.TB, class, field string) entities.Ref {
	t.Helper()
	c := w.classes.ByName(class)
	if c == nil {
		t.Fatalf("no class %s", class)
	}
	r, err := c.Ref(field)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
