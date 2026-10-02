package entities

import (
	"fmt"
	"math"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/stringtables"
)

// CachedBaselines returns how many decoded baselines s holds.
func (s *State) CachedBaselines() int { return len(s.baselines) }

// Mismatch is a value that a snapshot and the state disagree on.
type Mismatch struct {
	Entity          int
	Class, Field    string // the field by full name, elements by number
	State, Snapshot Value
}

// CheckSnapshot decodes m, a non-delta svc_PacketEntities, on its own and
// compares it with s. It returns an error if the entities differ, and
// otherwise every value m carries that differs from s's, and how many
// values it compared.
func CheckSnapshot(s *State, m *pb.CSVCMsg_PacketEntities, baselines *stringtables.Table) ([]Mismatch, int, error) {
	if m.GetLegacyIsDelta() {
		return nil, 0, fmt.Errorf("not a snapshot")
	}
	fresh := NewState(s.classes)
	r := bitread.New(m.GetEntityData())
	var out []Mismatch
	compared, entities := 0, 0
	idx := -1
	for range int(m.GetUpdatedEntries()) {
		idx += int(r.ReadUBitVar()) + 1
		if cmd := r.ReadBits(2); cmd != cmdCreate {
			return nil, 0, fmt.Errorf("entity %d: command %d in a snapshot", idx, cmd)
		}
		c := s.classes.ByID(int(r.ReadBits(uint(s.classes.IDBits()))))
		serial := r.ReadBits(serialBits)
		r.ReadVarUint32()
		e := s.Entity(idx)
		if c == nil || e == nil || e.Class != c || e.Serial != serial {
			return nil, 0, fmt.Errorf("entity %d: snapshot has %v serial %d, state has %v", idx, c, serial, e)
		}
		entities++
		base, err := fresh.baseline(c, altBaseline(m, idx), baselines)
		if err != nil {
			return nil, 0, err
		}
		state := base.clone()
		paths, err := readFieldPaths(r, nil)
		if err != nil {
			return nil, 0, err
		}
		for i := range paths {
			p := &paths[i]
			if err := set(r, c.Serializer, state, p); err != nil {
				return nil, 0, err
			}
			want, _ := valueAt(state, p)
			got, _ := valueAt(e.root, p)
			compared++
			if !sameValue(got, want) {
				out = append(out, Mismatch{idx, c.Name, pathName(c.Serializer, p), got, want})
			}
		}
	}
	for range s.All() {
		entities--
	}
	if entities != 0 {
		return nil, 0, fmt.Errorf("the state has %d entities the snapshot doesn't", -entities)
	}
	return out, compared, r.Err()
}

// valueAt returns the value at path p in the state n.
func valueAt(n *node, p *fieldPath) (Value, bool) {
	for d := 0; d < p.last; d++ {
		i := int(p.idx[d])
		if i >= len(n.vals) || n.kids == nil || n.kids[i] == nil {
			return Value{}, false
		}
		n = n.kids[i]
	}
	i := int(p.idx[p.last])
	if i >= len(n.vals) {
		return Value{}, false
	}
	return n.vals[i], true
}

// pathName names the fields along p, with array elements by number.
func pathName(s *Serializer, p *fieldPath) string {
	f := s.Fields[p.idx[0]]
	name := f.FullName()
	for d := 1; d <= p.last; d++ {
		switch f.Decoder.Kind {
		case Pointer, Table:
			f = f.Serializer.Fields[p.idx[d]]
			name += "." + f.FullName()
		default:
			f = f.Elem
			name += fmt.Sprintf(".%d", p.idx[d])
		}
	}
	return name
}

func sameValue(a, b Value) bool {
	if a.u != b.u || a.s != b.s {
		return false
	}
	for i := range a.f {
		if math.Float32bits(a.f[i]) != math.Float32bits(b.f[i]) {
			return false
		}
	}
	return true
}
