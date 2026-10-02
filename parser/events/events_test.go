package events_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

type listKey = pb.CMsgSource1LegacyGameEventListKeyT
type eventKey = pb.CMsgSource1LegacyGameEventKeyT

// descriptor builds a list descriptor whose keys alternate name, type.
func descriptor(id int32, name string, keys ...any) *pb.CMsgSource1LegacyGameEventListDescriptorT {
	d := &pb.CMsgSource1LegacyGameEventListDescriptorT{Eventid: proto.Int32(id), Name: proto.String(name)}
	for i := 0; i < len(keys); i += 2 {
		d.Keys = append(d.Keys, &listKey{Name: proto.String(keys[i].(string)), Type: proto.Int32(int32(keys[i+1].(events.KeyType)))})
	}
	return d
}

func listBody(t testing.TB, descs ...*pb.CMsgSource1LegacyGameEventListDescriptorT) []byte {
	t.Helper()
	return marshal(t, &pb.CMsgSource1LegacyGameEventList{Descriptors: descs})
}

func eventBody(t testing.TB, id int32, keys ...*eventKey) []byte {
	t.Helper()
	return marshal(t, &pb.CMsgSource1LegacyGameEvent{Eventid: proto.Int32(id), Keys: keys})
}

func marshal(t testing.TB, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func typ(t events.KeyType) *int32 { return proto.Int32(int32(t)) }

// allTypes has one key of every known type, plus one of a type the package
// doesn't know.
var allTypes = descriptor(42, "everything",
	"s", events.TypeString,
	"f", events.TypeFloat,
	"l", events.TypeLong,
	"sh", events.TypeShort,
	"b", events.TypeByte,
	"t", events.TypeBool,
	"u", events.TypeUint64,
	"pawn", events.TypePawn,
	"player", events.TypePlayer,
	"x", events.KeyType(99),
)

func allTypesEvent(t testing.TB) []byte {
	return eventBody(t, 42,
		&eventKey{Type: typ(events.TypeString), ValString: proto.String("ak47")},
		&eventKey{Type: typ(events.TypeFloat), ValFloat: proto.Float32(-1.5)},
		&eventKey{Type: typ(events.TypeLong), ValLong: proto.Int32(math.MinInt32)},
		&eventKey{Type: typ(events.TypeShort), ValShort: proto.Int32(-7)},
		&eventKey{Type: typ(events.TypeByte), ValByte: proto.Int32(100)},
		&eventKey{Type: typ(events.TypeBool), ValBool: proto.Bool(true)},
		&eventKey{Type: typ(events.TypeUint64), ValUint64: proto.Uint64(76561198000000001)},
		&eventKey{Type: typ(events.TypePawn), ValLong: proto.Int32(-1)},
		&eventKey{Type: typ(events.TypePlayer), ValShort: proto.Int32(65535)},
		&eventKey{Type: typ(99), ValLong: proto.Int32(5)},
	)
}

func TestDecodeTypes(t *testing.T) {
	var d events.Decoder
	if err := d.HandleList(listBody(t, descriptor(1, "other"), allTypes)); err != nil {
		t.Fatal(err)
	}
	e, err := d.Decode(allTypesEvent(t), 1234)
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "everything" || e.Tick != 1234 {
		t.Errorf("got %s at %d, want everything at 1234", e.Name, e.Tick)
	}
	if v, ok := e.String("s"); v != "ak47" || !ok {
		t.Errorf("String(s) = %q, %v", v, ok)
	}
	if v, ok := e.Float("f"); v != -1.5 || !ok {
		t.Errorf("Float(f) = %v, %v", v, ok)
	}
	if v, ok := e.Bool("t"); !v || !ok {
		t.Errorf("Bool(t) = %v, %v", v, ok)
	}
	if v, ok := e.Uint64("u"); v != 76561198000000001 || !ok {
		t.Errorf("Uint64(u) = %v, %v", v, ok)
	}
	for key, want := range map[string]int{"l": math.MinInt32, "sh": -7, "b": 100, "pawn": -1, "player": 65535} {
		if v, ok := e.Int(key); v != want || !ok {
			t.Errorf("Int(%s) = %v, %v; want %v", key, v, ok, want)
		}
	}
	// An unknown type is present but has no accessor.
	if !e.Has("x") {
		t.Error("Has(x) = false")
	}
	if v, ok := e.Int("x"); ok {
		t.Errorf("Int(x) = %v, true; want not ok", v)
	}
}

// Each accessor rejects a missing key and every type but its own.
func TestAccessorsWrongType(t *testing.T) {
	var d events.Decoder
	if err := d.HandleList(listBody(t, allTypes)); err != nil {
		t.Fatal(err)
	}
	e, err := d.Decode(allTypesEvent(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	accessors := map[string]func(string) bool{
		"Int":    func(k string) bool { _, ok := e.Int(k); return ok },
		"Uint64": func(k string) bool { _, ok := e.Uint64(k); return ok },
		"Bool":   func(k string) bool { _, ok := e.Bool(k); return ok },
		"Float":  func(k string) bool { _, ok := e.Float(k); return ok },
		"String": func(k string) bool { _, ok := e.String(k); return ok },
	}
	owns := map[string][]string{
		"Int":    {"l", "sh", "b", "pawn", "player"},
		"Uint64": {"u"},
		"Bool":   {"t"},
		"Float":  {"f"},
		"String": {"s"},
	}
	keys := []string{"s", "f", "l", "sh", "b", "t", "u", "pawn", "player", "x", "missing", ""}
	for name, get := range accessors {
		for _, k := range keys {
			want := false
			for _, o := range owns[name] {
				want = want || o == k
			}
			if got := get(k); got != want {
				t.Errorf("%s(%q) ok = %v, want %v", name, k, got, want)
			}
		}
	}
	if e.Has("missing") {
		t.Error(`Has("missing") = true`)
	}
}

// A zero Event has no keys and doesn't panic.
func TestZeroEvent(t *testing.T) {
	var e events.Event
	if e.Has("userid") {
		t.Error("Has = true")
	}
	if _, ok := e.Int("userid"); ok {
		t.Error("Int ok = true")
	}
	if _, ok := e.String("weapon"); ok {
		t.Error("String ok = true")
	}
}

// A key with no type of its own takes its descriptor's, and a key with no
// value reads as zero.
func TestDecodeSparseKeys(t *testing.T) {
	var d events.Decoder
	if err := d.HandleList(listBody(t, descriptor(3, "e", "a", events.TypeShort, "b", events.TypeString, "c", events.TypeBool))); err != nil {
		t.Fatal(err)
	}
	e, err := d.Decode(eventBody(t, 3,
		&eventKey{ValShort: proto.Int32(9)},
		&eventKey{Type: typ(events.TypeString)},
		&eventKey{},
	), 5)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := e.Int("a"); v != 9 || !ok {
		t.Errorf("Int(a) = %v, %v; want 9, true", v, ok)
	}
	if v, ok := e.String("b"); v != "" || !ok {
		t.Errorf("String(b) = %q, %v; want empty, true", v, ok)
	}
	if v, ok := e.Bool("c"); v || !ok {
		t.Errorf("Bool(c) = %v, %v; want false, true", v, ok)
	}
}

// A later list replaces the table, so the same id can name another event.
func TestHandleListReplaces(t *testing.T) {
	var d events.Decoder
	if err := d.HandleList(listBody(t, descriptor(10, "player_death", "userid", events.TypePlayer), descriptor(11, "round_end"))); err != nil {
		t.Fatal(err)
	}
	userid := &eventKey{Type: typ(events.TypePlayer), ValShort: proto.Int32(3)}
	first, err := d.Decode(eventBody(t, 10, userid), 1)
	if err != nil || first.Name != "player_death" {
		t.Fatalf("first list: %v, %v", first.Name, err)
	}

	// The next patch shifts the ids and drops round_end.
	if err := d.HandleList(listBody(t, descriptor(10, "player_hurt", "userid", events.TypePlayer), descriptor(12, "player_death", "userid", events.TypePlayer))); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int32]string{10: "player_hurt", 12: "player_death"} {
		e, err := d.Decode(eventBody(t, id, userid), 2)
		if err != nil || e.Name != want {
			t.Errorf("id %d: got %q, %v; want %s", id, e.Name, err, want)
		}
		if v, ok := e.Int("userid"); v != 3 || !ok {
			t.Errorf("id %d: Int(userid) = %v, %v", id, v, ok)
		}
	}
	if _, err := d.Decode(eventBody(t, 11), 2); !errors.Is(err, events.ErrUnknownEvent) {
		t.Errorf("old id 11: want ErrUnknownEvent, got %v", err)
	}
	if _, ok := d.Descriptor("round_end"); ok {
		t.Error("round_end still described after the new list")
	}
	if desc, ok := d.Descriptor("player_death"); !ok || desc.ID != 12 || len(desc.Keys) != 1 || desc.Keys[0] != (events.Key{Name: "userid", Type: events.TypePlayer}) {
		t.Errorf("Descriptor(player_death) = %+v, %v", desc, ok)
	}
	// An event decoded under the old list keeps its name and values.
	if v, ok := first.Int("userid"); first.Name != "player_death" || v != 3 || !ok {
		t.Errorf("old event changed: %s, %v, %v", first.Name, v, ok)
	}
}

func TestDecodeBeforeList(t *testing.T) {
	var d events.Decoder
	if _, ok := d.Descriptor("player_death"); ok {
		t.Error("Descriptor found with no list")
	}
	_, err := d.Decode(eventBody(t, 7), 99)
	if !errors.Is(err, events.ErrNoDescriptors) || !errors.Is(err, demo.ErrBadDemo) {
		t.Fatalf("want ErrNoDescriptors and ErrBadDemo, got %v", err)
	}
	if !strings.Contains(err.Error(), "id 7 at tick 99") {
		t.Errorf("error %q lacks the id and tick", err)
	}
}

func TestDecodeErrors(t *testing.T) {
	list := listBody(t, descriptor(1, "player_hurt", "health", events.TypeByte, "weapon", events.TypeString))
	health := &eventKey{Type: typ(events.TypeByte), ValByte: proto.Int32(80)}
	weapon := &eventKey{Type: typ(events.TypeString), ValString: proto.String("glock")}
	tests := []struct {
		name string
		body []byte
		is   error
		want string
	}{
		{"unknown id", eventBody(t, 2, health, weapon), events.ErrUnknownEvent, "id 2 at tick 50"},
		{"too few keys", eventBody(t, 1, health), demo.ErrBadDemo, "player_hurt (id 1) at tick 50 has 1 keys, its descriptor 2"},
		{"too many keys", eventBody(t, 1, health, weapon, weapon), demo.ErrBadDemo, "has 3 keys, its descriptor 2"},
		{"wrong type", eventBody(t, 1, weapon, weapon), demo.ErrBadDemo, `key "health" is string, its descriptor says byte`},
		{"malformed", []byte{0x1A, 0x05, 'a'}, demo.ErrBadDemo, "game event: "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d events.Decoder
			if err := d.HandleList(list); err != nil {
				t.Fatal(err)
			}
			_, err := d.Decode(tc.body, 50)
			if !errors.Is(err, tc.is) || !errors.Is(err, demo.ErrBadDemo) {
				t.Fatalf("want %v, got %v", tc.is, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q lacks %q", err, tc.want)
			}
			// The decoder still works.
			if e, err := d.Decode(eventBody(t, 1, health, weapon), 51); err != nil || e.Name != "player_hurt" {
				t.Errorf("after the error: %v, %v", e.Name, err)
			}
		})
	}
}

// A bad list is an error and keeps the table it would have replaced.
func TestHandleListErrors(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{"malformed", []byte{0x0A, 0x05, 'a'}, "game event list: "},
		{"duplicate id", listBody(t, descriptor(4, "a"), descriptor(4, "b")), `id 4 is both "a" and "b"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d events.Decoder
			if err := d.HandleList(listBody(t, descriptor(1, "old"))); err != nil {
				t.Fatal(err)
			}
			err := d.HandleList(tc.body)
			if !errors.Is(err, demo.ErrBadDemo) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want ErrBadDemo with %q, got %v", tc.want, err)
			}
			if e, err := d.Decode(eventBody(t, 1), 0); err != nil || e.Name != "old" {
				t.Errorf("table changed: %v, %v", e.Name, err)
			}
		})
	}
}

// An empty list is still a list: events are then unknown, not early.
func TestHandleListEmpty(t *testing.T) {
	var d events.Decoder
	if err := d.HandleList(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Decode(eventBody(t, 1), 0); !errors.Is(err, events.ErrUnknownEvent) {
		t.Errorf("want ErrUnknownEvent, got %v", err)
	}
}

// Within a descriptor, the first key of a name wins.
func TestDuplicateKeyName(t *testing.T) {
	var d events.Decoder
	if err := d.HandleList(listBody(t, descriptor(1, "e", "k", events.TypeShort, "k", events.TypeString))); err != nil {
		t.Fatal(err)
	}
	e, err := d.Decode(eventBody(t, 1,
		&eventKey{Type: typ(events.TypeShort), ValShort: proto.Int32(1)},
		&eventKey{Type: typ(events.TypeString), ValString: proto.String("two")},
	), 0)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := e.Int("k"); v != 1 || !ok {
		t.Errorf("Int(k) = %v, %v; want 1, true", v, ok)
	}
}

func TestKeyTypeString(t *testing.T) {
	for typ, want := range map[events.KeyType]string{
		events.TypeString: "string",
		events.TypePawn:   "pawn",
		events.TypePlayer: "player",
		0:                 "KeyType(0)",
		10:                "KeyType(10)",
		-1:                "KeyType(-1)",
	} {
		if got := typ.String(); got != want {
			t.Errorf("KeyType(%d).String() = %q, want %q", int32(typ), got, want)
		}
	}
}

// FuzzDecode checks that any event body either decodes or fails with
// ErrBadDemo, without panicking.
func FuzzDecode(f *testing.F) {
	var d events.Decoder
	if err := d.HandleList(listBody(f, allTypes)); err != nil {
		f.Fatal(err)
	}
	f.Add(allTypesEvent(f))
	f.Add([]byte{})
	f.Add([]byte{0x10, 0x2A})
	f.Fuzz(func(t *testing.T, body []byte) {
		e, err := d.Decode(body, 0)
		if err != nil {
			if !errors.Is(err, demo.ErrBadDemo) {
				t.Fatalf("error %v is not ErrBadDemo", err)
			}
			return
		}
		e.Has("s")
		e.Int("l")
		e.String("s")
	})
}
