// Package events decodes CS2 game events: the descriptor list
// (CMsgSource1LegacyGameEventList, message 205) and the events themselves
// (CMsgSource1LegacyGameEvent, message 207).
//
// An event carries only its descriptor id and its values in key order. Ids
// shift between game patches, so the package never hardcodes them: a Decoder
// learns them from the demo's own descriptor list, and callers look events
// and keys up by name.
package events

import (
	"fmt"
	"math"
	"strconv"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// ErrNoDescriptors is wrapped by the error Decode returns for an event that
// arrives before any descriptor list. It wraps demo.ErrBadDemo.
var ErrNoDescriptors = fmt.Errorf("%w: game event before the descriptor list", demo.ErrBadDemo)

// ErrUnknownEvent is wrapped by the error Decode returns for an event whose
// id is not in the descriptor list. It wraps demo.ErrBadDemo.
var ErrUnknownEvent = fmt.Errorf("%w: game event id not in the descriptor list", demo.ErrBadDemo)

// KeyType is the type of an event key, as numbered in the descriptor list.
// Each type is carried in one val_* field of the event's key_t. Types 1 to 7
// use the field numbered type+1. Types 8 and 9 reuse val_long and val_short.
// On real demos every key holds exactly the field its type names.
type KeyType int32

// Key types.
const (
	TypeString KeyType = 1 // val_string
	TypeFloat  KeyType = 2 // val_float
	TypeLong   KeyType = 3 // val_long
	TypeShort  KeyType = 4 // val_short
	TypeByte   KeyType = 5 // val_byte
	TypeBool   KeyType = 6 // val_bool
	TypeUint64 KeyType = 7 // val_uint64

	// TypePawn is a player pawn's entity handle, in val_long. Keys named
	// *_pawn use it. The handle is a uint32 stored as an int32, so one with
	// its top bit set reads as negative, and -1 means no pawn.
	TypePawn KeyType = 8

	// TypePlayer is a player's userid, in val_short: userid, attacker and
	// assister, among others. userid & 0xFF is the player slot, and 65535
	// means no player. A few player keys, such as player_chat's userid, are
	// plain TypeShort instead.
	TypePlayer KeyType = 9
)

var keyTypeNames = [...]string{
	TypeString: "string",
	TypeFloat:  "float",
	TypeLong:   "long",
	TypeShort:  "short",
	TypeByte:   "byte",
	TypeBool:   "bool",
	TypeUint64: "uint64",
	TypePawn:   "pawn",
	TypePlayer: "player",
}

// String returns the type's name, such as "short", or "KeyType(N)" for a type
// the package doesn't know.
func (t KeyType) String() string {
	if t > 0 && int(t) < len(keyTypeNames) {
		return keyTypeNames[t]
	}
	return "KeyType(" + strconv.FormatInt(int64(t), 10) + ")"
}

// Key is one key of an event descriptor.
type Key struct {
	Name string
	Type KeyType
}

// Descriptor describes one kind of event: its id in this demo, its name, and
// its keys in the order an event carries their values.
type Descriptor struct {
	ID    int32
	Name  string
	Keys  []Key
	index map[string]int // key name to position in Keys; the first wins
}

// Decoder turns game event messages into Events, using the most recent
// descriptor list. The zero value is ready to use. A Decoder is not safe for
// concurrent use.
type Decoder struct {
	byID   map[int32]*Descriptor // nil until the first list
	byName map[string]*Descriptor
}

// HandleList decodes the body of a descriptor list message (205) and makes it
// the table that Decode uses, replacing any earlier list. A body that doesn't
// decode, or that gives two descriptors the same id, is an error wrapping
// demo.ErrBadDemo, and leaves the table as it was.
func (d *Decoder) HandleList(body []byte) error {
	var list pb.CMsgSource1LegacyGameEventList
	if err := proto.Unmarshal(body, &list); err != nil {
		return fmt.Errorf("%w: game event list: %v", demo.ErrBadDemo, err)
	}
	byID := make(map[int32]*Descriptor, len(list.Descriptors))
	byName := make(map[string]*Descriptor, len(list.Descriptors))
	for _, ld := range list.Descriptors {
		desc := &Descriptor{
			ID:    ld.GetEventid(),
			Name:  ld.GetName(),
			Keys:  make([]Key, len(ld.Keys)),
			index: make(map[string]int, len(ld.Keys)),
		}
		if prev, ok := byID[desc.ID]; ok {
			return fmt.Errorf("%w: game event list: id %d is both %q and %q", demo.ErrBadDemo, desc.ID, prev.Name, desc.Name)
		}
		for i, lk := range ld.Keys {
			desc.Keys[i] = Key{Name: lk.GetName(), Type: KeyType(lk.GetType())}
			if _, dup := desc.index[desc.Keys[i].Name]; !dup {
				desc.index[desc.Keys[i].Name] = i
			}
		}
		byID[desc.ID] = desc
		byName[desc.Name] = desc
	}
	d.byID, d.byName = byID, byName
	return nil
}

// Descriptor returns the descriptor with the given name from the current
// list. ok is false if there is no list yet or the list doesn't name it.
func (d *Decoder) Descriptor(name string) (desc Descriptor, ok bool) {
	if p := d.byName[name]; p != nil {
		return *p, true
	}
	return Descriptor{}, false
}

// Decode decodes the body of a game event message (207) against the current
// descriptor list. tick is the demo-frame tick of the packet that carried it,
// which the Event records as is.
//
// An event before any list gives an error wrapping ErrNoDescriptors, and an
// id the list lacks gives one wrapping ErrUnknownEvent. A body that doesn't
// decode, or whose keys don't line up with its descriptor's in number and
// type, is also an error. All of them wrap demo.ErrBadDemo.
func (d *Decoder) Decode(body []byte, tick int32) (Event, error) {
	var m pb.CMsgSource1LegacyGameEvent
	if err := proto.Unmarshal(body, &m); err != nil {
		return Event{}, fmt.Errorf("%w: game event: %v", demo.ErrBadDemo, err)
	}
	id := m.GetEventid()
	if d.byID == nil {
		return Event{}, fmt.Errorf("%w: id %d at tick %d", ErrNoDescriptors, id, tick)
	}
	desc := d.byID[id]
	if desc == nil {
		return Event{}, fmt.Errorf("%w: id %d at tick %d", ErrUnknownEvent, id, tick)
	}
	if len(m.Keys) != len(desc.Keys) {
		return Event{}, fmt.Errorf("%w: game event %s (id %d) at tick %d has %d keys, its descriptor %d",
			demo.ErrBadDemo, desc.Name, id, tick, len(m.Keys), len(desc.Keys))
	}
	vals := make([]value, len(m.Keys))
	for i, k := range m.Keys {
		want := desc.Keys[i]
		if k.Type != nil && KeyType(*k.Type) != want.Type {
			return Event{}, fmt.Errorf("%w: game event %s (id %d) at tick %d: key %q is %s, its descriptor says %s",
				demo.ErrBadDemo, desc.Name, id, tick, want.Name, KeyType(*k.Type), want.Type)
		}
		vals[i] = valueOf(k, want.Type)
	}
	return Event{Name: desc.Name, Tick: tick, desc: desc, vals: vals}, nil
}

// value holds one key's value. Its type is the descriptor key's.
type value struct {
	num uint64 // an integer sign-extended to 64 bits, a float32's bits, or 1 for true
	str string
}

// valueOf reads k's value from the field that type t uses. A missing field
// reads as its zero value. A type the package doesn't know gives the zero
// value, which no accessor returns.
func valueOf(k *pb.CMsgSource1LegacyGameEventKeyT, t KeyType) value {
	switch t {
	case TypeString:
		return value{str: k.GetValString()}
	case TypeFloat:
		return value{num: uint64(math.Float32bits(k.GetValFloat()))}
	case TypeLong, TypePawn:
		return value{num: uint64(int64(k.GetValLong()))}
	case TypeShort, TypePlayer:
		return value{num: uint64(int64(k.GetValShort()))}
	case TypeByte:
		return value{num: uint64(int64(k.GetValByte()))}
	case TypeBool:
		if k.GetValBool() {
			return value{num: 1}
		}
	case TypeUint64:
		return value{num: k.GetValUint64()}
	}
	return value{}
}

// Event is one decoded game event. It doesn't share memory with the message
// body, so it stays valid after later calls to Decode or HandleList.
//
// The accessors look a key up by name. Each reports ok = false if the event
// has no such key or the key holds a different type.
type Event struct {
	Name string
	Tick int32 // the demo-frame tick given to Decode

	desc *Descriptor
	vals []value // one per desc.Keys
}

// get returns the value and type of the key named key.
func (e Event) get(key string) (value, KeyType, bool) {
	if e.desc == nil {
		return value{}, 0, false
	}
	i, ok := e.desc.index[key]
	if !ok {
		return value{}, 0, false
	}
	return e.vals[i], e.desc.Keys[i].Type, true
}

// Has reports whether the event has a key named key, whatever its type.
func (e Event) Has(key string) bool {
	_, _, ok := e.get(key)
	return ok
}

// Int returns the value of an integer key: TypeLong, TypeShort, TypeByte,
// TypePawn or TypePlayer.
func (e Event) Int(key string) (int, bool) {
	v, t, ok := e.get(key)
	switch {
	case !ok:
		return 0, false
	case t == TypeLong, t == TypeShort, t == TypeByte, t == TypePawn, t == TypePlayer:
		return int(int64(v.num)), true
	}
	return 0, false
}

// Uint64 returns the value of a TypeUint64 key, such as a SteamID64.
func (e Event) Uint64(key string) (uint64, bool) {
	v, t, ok := e.get(key)
	if !ok || t != TypeUint64 {
		return 0, false
	}
	return v.num, true
}

// Bool returns the value of a TypeBool key.
func (e Event) Bool(key string) (val, ok bool) {
	v, t, ok := e.get(key)
	if !ok || t != TypeBool {
		return false, false
	}
	return v.num != 0, true
}

// Float returns the value of a TypeFloat key.
func (e Event) Float(key string) (float32, bool) {
	v, t, ok := e.get(key)
	if !ok || t != TypeFloat {
		return 0, false
	}
	return math.Float32frombits(uint32(v.num)), true
}

// String returns the value of a TypeString key.
func (e Event) String(key string) (string, bool) {
	v, t, ok := e.get(key)
	if !ok || t != TypeString {
		return "", false
	}
	return v.str, true
}
