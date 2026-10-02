package entities

import (
	"fmt"
	"iter"
	"strconv"
	"strings"

	"github.com/ChiemekaKalu/demoview/parser/bitread"
	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/stringtables"
)

// MsgPacketEntities is the packet message that State.Apply reads.
const MsgPacketEntities = demo.MsgType(pb.SVC_Messages_svc_PacketEntities)

const (
	// maxEntities is one more than the highest entity index.
	maxEntities = 1 << handleIndexBits
	// maxPadBits is the most bits entity data may have left at its end.
	maxPadBits = 7
)

// A handle is 24 bits: an entity's serial number above its index. Creates
// send 17-bit serials, but they stay below 1<<handleSerialBits.
const (
	serialBits       = 17
	handleIndexBits  = 14
	handleSerialBits = 10
)

// Entity is one entity and its current state.
type Entity struct {
	Index  int
	Serial uint32
	Class  *Class
	root   *node
}

// Handle returns the handle that refers to e.
func (e *Entity) Handle() uint32 {
	return (e.Serial&(1<<handleSerialBits-1))<<handleIndexBits | uint32(e.Index)
}

// State is the set of entities that svc_PacketEntities messages build.
type State struct {
	classes   *Classes
	list      []*Entity // by index
	baselines map[baselineKey]baseline
	r         bitread.Reader
	paths     []fieldPath // scratch
}

// NewState returns a State with no entities, of the given classes.
func NewState(classes *Classes) *State {
	return &State{classes: classes, baselines: map[baselineKey]baseline{}}
}

// Entity returns the entity at index i, or nil if there is none.
func (s *State) Entity(i int) *Entity {
	if i < 0 || i >= len(s.list) {
		return nil
	}
	return s.list[i]
}

// ByHandle returns the entity that handle h refers to, or nil if there is
// none or its serial number has moved on.
func (s *State) ByHandle(h uint32) *Entity {
	e := s.Entity(int(h & (1<<handleIndexBits - 1)))
	if e == nil || e.Handle() != h {
		return nil
	}
	return e
}

// All yields every entity in index order.
func (s *State) All() iter.Seq[*Entity] {
	return func(yield func(*Entity) bool) {
		for _, e := range s.list {
			if e != nil && !yield(e) {
				return
			}
		}
	}
}

// Apply applies an svc_PacketEntities message. Entities start from their
// class's baseline in baselines, the instancebaseline string table, and a
// message that isn't a delta replaces every entity.
//
// A DEM_FullPacket repeats the state as of the packet before it, so a
// reader going through a demo in order should apply only the first. The
// later ones also leave some fields out, such as simulation times and
// players' match stats, which applying them would reset.
//
// Entity data that doesn't decode, or that leaves a byte or more unread,
// gives an error wrapping demo.ErrBadDemo, and one about a class or decoder
// this package doesn't know wraps ErrUnknown. The entries before the bad
// one stay applied.
func (s *State) Apply(m *pb.CSVCMsg_PacketEntities, baselines *stringtables.Table) error {
	if !m.GetLegacyIsDelta() {
		clear(s.list)
	}
	s.r.Reset(m.GetEntityData())
	idx := -1
	for n := range int(m.GetUpdatedEntries()) {
		idx += int(s.r.ReadUBitVar()) + 1
		if err := s.entry(idx, m, baselines); err != nil {
			return fmt.Errorf("%w: packet entities: entry %d of %d, entity %d: %v", cause(err), n, m.GetUpdatedEntries(), idx, err)
		}
	}
	if err := s.skipIndexList(m); err != nil {
		return fmt.Errorf("%w: packet entities: %v", demo.ErrBadDemo, err)
	}
	if err := s.r.Err(); err != nil {
		return fmt.Errorf("%w: packet entities: %v", demo.ErrBadDemo, err)
	}
	if left := s.r.BitsLeft(); left > maxPadBits {
		return fmt.Errorf("%w: packet entities: %d bits left over after %d entries", demo.ErrBadDemo, left, m.GetUpdatedEntries())
	}
	return nil
}

// Entry commands. The low bit leaves the PVS, and the high bit makes a
// leave a delete, or an update a create.
const (
	cmdUpdate = 0
	cmdLeave  = 1
	cmdCreate = 2
	cmdDelete = 3
)

// entry applies one entry of m, for the entity at index idx.
func (s *State) entry(idx int, m *pb.CSVCMsg_PacketEntities, baselines *stringtables.Table) error {
	if idx >= maxEntities {
		return fmt.Errorf("index over %d", maxEntities-1)
	}
	switch s.r.ReadBits(2) {
	case cmdUpdate:
		// Older demos give each update two more bits. With the low one
		// set, the entity hasn't changed.
		if m.GetHasPvsVisBitsDeprecated() != 0 && s.r.ReadBits(2)&1 != 0 {
			return nil
		}
		e := s.Entity(idx)
		if e == nil {
			return fmt.Errorf("update of an entity that doesn't exist")
		}
		// An alternate baseline replaces the state the update applies to.
		if alt := altBaseline(m, idx); alt >= 0 {
			base, err := s.baseline(e.Class, alt, baselines)
			if err != nil {
				return err
			}
			e.root.assign(base)
		}
		return s.read(e)
	case cmdCreate:
		id := int(s.r.ReadBits(uint(s.classes.IDBits())))
		serial := s.r.ReadBits(serialBits)
		s.r.ReadVarUint32() // small, and not used here
		c := s.classes.ByID(id)
		if c == nil {
			return unknownf("create with class id %d of %d", id, s.classes.Len())
		}
		base, err := s.baseline(c, altBaseline(m, idx), baselines)
		if err != nil {
			return err
		}
		if idx >= len(s.list) {
			s.list = append(s.list, make([]*Entity, idx+1-len(s.list))...)
		}
		e := &Entity{Index: idx, Serial: serial, Class: c, root: base.clone()}
		s.list[idx] = e
		return s.read(e)
	default:
		// An entity that leaves the PVS comes back with a create.
		if idx < len(s.list) {
			s.list[idx] = nil
		}
		return nil
	}
}

// read applies a field path list and its values to e.
func (s *State) read(e *Entity) error {
	var err error
	if s.paths, err = readFields(&s.r, e.Class.Serializer, e.root, s.paths); err != nil {
		return fmt.Errorf("%s: %w", e.Class.Name, err)
	}
	return nil
}

// skipIndexList skips the list of entity indices that older delta messages
// append to their entries: a UBitVar count, then a UBitVar for each. Newer
// ones send it in the non_transmitted_entities field instead. It changes no
// entity's state.
func (s *State) skipIndexList(m *pb.CSVCMsg_PacketEntities) error {
	if !m.GetLegacyIsDelta() || m.NonTransmittedEntities != nil {
		return nil
	}
	n := s.r.ReadUBitVar()
	if n > maxEntities {
		return fmt.Errorf("index list of %d entries", n)
	}
	for range n {
		s.r.ReadUBitVar()
	}
	return nil
}

// altBaseline returns the alternate baseline m gives the entity at index
// idx, as an instancebaseline entry index, or -1 for none.
func altBaseline(m *pb.CSVCMsg_PacketEntities, idx int) int {
	for _, a := range m.GetAlternateBaselines() {
		if int(a.GetEntityIndex()) == idx {
			return int(a.GetBaselineIndex())
		}
	}
	return -1
}

// baselineKey identifies a decoded baseline: a class's own, with entry -1,
// or an alternate one, by its instancebaseline entry index.
type baselineKey struct{ class, entry int }

// baseline is a decoded baseline and the string-table value it came from.
type baseline struct {
	data  []byte
	state *node
}

// baseline returns class c's baseline state, or the alternate baseline in
// entry if it is 0 or more, which must be one of c's. It decodes a
// baseline again only when its string-table value has changed, and caches
// at most one more than the table has entries per class. A class with no
// baseline, or no table, starts with every field zero.
func (s *State) baseline(c *Class, entry int, baselines *stringtables.Table) (*node, error) {
	if entry < 0 || baselines == nil {
		entry = -1
	}
	id := strconv.Itoa(c.ID)
	var data []byte
	switch {
	case baselines == nil:
	case entry < 0:
		data, _ = baselines.Value(id)
	default:
		e, ok := baselines.Entry(entry)
		if !ok || e.Key != id && !strings.HasPrefix(e.Key, id+":") {
			return nil, fmt.Errorf("alternate baseline %d isn't one of %s's", entry, c.Name)
		}
		data = e.Value
	}
	key := baselineKey{c.ID, entry}
	if b, ok := s.baselines[key]; ok && sameBytes(b.data, data) {
		return b.state, nil
	}
	state := newNode(len(c.Serializer.Fields))
	if len(data) > 0 {
		r := bitread.New(data)
		var err error
		if s.paths, err = readFields(r, c.Serializer, state, s.paths); err != nil {
			return nil, fmt.Errorf("%s baseline: %w", c.Name, err)
		}
		if left := r.BitsLeft(); left > maxPadBits {
			return nil, fmt.Errorf("%s baseline: %d bits left over", c.Name, left)
		}
	}
	s.baselines[key] = baseline{data, state}
	return state, nil
}

// sameBytes reports whether a and b are the same slice. String-table
// values are replaced, never changed in place, so this spots a new value
// without comparing bytes.
func sameBytes(a, b []byte) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}
