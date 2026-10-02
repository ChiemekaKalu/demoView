package players

import (
	"errors"
	"slices"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"google.golang.org/protobuf/proto"
)

func TestSlotOf(t *testing.T) {
	tests := []struct {
		userid int32
		want   Slot
	}{
		{0, 0},
		{7, 7},
		{256, 0},      // FACEIT: slot 0 before a reconnect
		{512, 0},      // ... and after it
		{514, 2},      // FACEIT
		{65280, 0},    // Valve MM sets the high byte on every player
		{65289, 9},    // Valve MM
		{0xFFFF, 255}, // "no player"
		{-1, 255},
	}
	for _, tt := range tests {
		if got := SlotOf(tt.userid); got != tt.want {
			t.Errorf("SlotOf(%d) = %d, want %d", tt.userid, got, tt.want)
		}
	}
}

func playerInfo(t *testing.T, m *pb.CMsgPlayerInfo) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func item(key string, data []byte) *pb.CDemoStringTablesItemsT {
	return &pb.CDemoStringTablesItemsT{Str: proto.String(key), Data: data}
}

func snapshot(items ...*pb.CDemoStringTablesItemsT) *pb.CDemoStringTables {
	return &pb.CDemoStringTables{Tables: []*pb.CDemoStringTablesTableT{
		// Another table with a slot-like key and data that isn't a
		// CMsgPlayerInfo, which must be ignored.
		{TableName: proto.String("instancebaseline"), Items: []*pb.CDemoStringTablesItemsT{item("0", []byte{0xff, 0xff})}},
		{TableName: proto.String("userinfo"), Items: items},
	}}
}

var (
	hammy = Info{Name: "HammyS1337X-", SteamID64: 76561198404529647, UserID: 256}
	furyy = Info{Name: "FuRyy-", SteamID64: 76561198994771421, UserID: 257}
	gotv  = Info{Name: "DemoRecorder", UserID: 10, Fake: true, HLTV: true}
	bot   = Info{Name: "Niko", UserID: 3, Fake: true}
)

func encode(t *testing.T, i Info) []byte {
	return playerInfo(t, &pb.CMsgPlayerInfo{
		Name:       proto.String(i.Name),
		Xuid:       proto.Uint64(i.SteamID64),
		Userid:     proto.Int32(i.UserID),
		Fakeplayer: proto.Bool(i.Fake),
		Ishltv:     proto.Bool(i.HLTV),
	})
}

func TestApplyStringTables(t *testing.T) {
	var tbl Table
	gotvData := playerInfo(t, &pb.CMsgPlayerInfo{
		Name:       proto.String(gotv.Name),
		Xuid:       proto.Uint64(0),
		Userid:     proto.Int32(gotv.UserID),
		Steamid:    proto.Uint64(90071996842377227), // GOTV's server id; not a SteamID64
		Fakeplayer: proto.Bool(true),
		Ishltv:     proto.Bool(true),
	})
	err := tbl.ApplyStringTables(snapshot(
		item("0", encode(t, hammy)),
		item("1", nil),
		item("2", []byte{}),
		item("10", gotvData),
	))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := tbl.Get(0); !ok || got != hammy {
		t.Errorf("Get(0) = %+v, %v; want %+v, true", got, ok, hammy)
	}
	for _, s := range []Slot{1, 2, 3, 255} {
		if got, ok := tbl.Get(s); ok {
			t.Errorf("Get(%d) = %+v, true; want empty", s, got)
		}
	}
	if got, ok := tbl.Get(10); !ok || got != gotv {
		t.Errorf("Get(10) = %+v, %v; want %+v, true", got, ok, gotv)
	}
	if got, _ := tbl.Get(10); got.Bot() {
		t.Error("GOTV reports Bot() = true")
	}
	// Lookup matches on the slot alone.
	if got, ok := tbl.Lookup(512); !ok || got != hammy {
		t.Errorf("Lookup(512) = %+v, %v; want %+v, true", got, ok, hammy)
	}
	if _, ok := tbl.Lookup(0xFFFF); ok {
		t.Error("Lookup(0xFFFF) found a player")
	}

	// A later snapshot: slot 0 is now empty (the player left), slot 1 has
	// joined. Slot 0 keeps its last known player.
	if err := tbl.ApplyStringTables(snapshot(item("0", nil), item("1", encode(t, furyy)))); err != nil {
		t.Fatal(err)
	}
	if got, ok := tbl.Get(0); !ok || got != hammy {
		t.Errorf("after empty entry, Get(0) = %+v, %v; want %+v, true", got, ok, hammy)
	}
	if got, ok := tbl.Get(1); !ok || got != furyy {
		t.Errorf("Get(1) = %+v, %v; want %+v, true", got, ok, furyy)
	}

	var slots []Slot
	for s := range tbl.All() {
		slots = append(slots, s)
	}
	if want := []Slot{0, 1, 10}; !slices.Equal(slots, want) {
		t.Errorf("All() slots = %v, want %v", slots, want)
	}
}

func TestApplyStringTablesIgnoresMissingTable(t *testing.T) {
	var tbl Table
	if err := tbl.ApplyStringTables(nil); err != nil {
		t.Errorf("nil snapshot: %v", err)
	}
	other := &pb.CDemoStringTables{Tables: []*pb.CDemoStringTablesTableT{{TableName: proto.String("lightstyles")}}}
	if err := tbl.ApplyStringTables(other); err != nil {
		t.Errorf("no userinfo table: %v", err)
	}
	for s, i := range tbl.All() {
		t.Errorf("table not empty: slot %d = %+v", s, i)
	}
}

func TestApplyStringTablesBad(t *testing.T) {
	tests := []struct {
		name string
		bad  *pb.CDemoStringTablesItemsT
	}{
		{"key not a number", item("x", []byte{0x0a, 0x00})},
		{"key out of range", item("256", []byte{0x0a, 0x00})},
		{"negative key", item("-1", []byte{0x0a, 0x00})},
		{"bad protobuf", item("3", []byte{0x0a, 0x05, 'a'})}, // string runs past the end
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tbl Table
			tbl.Set(0, hammy)
			// The good entry comes first, and must not be applied.
			err := tbl.ApplyStringTables(snapshot(item("1", encode(t, furyy)), tt.bad))
			if !errors.Is(err, demo.ErrBadDemo) {
				t.Fatalf("err = %v, want ErrBadDemo", err)
			}
			if _, ok := tbl.Get(1); ok {
				t.Error("table changed by a failed snapshot")
			}
			if got, _ := tbl.Get(0); got != hammy {
				t.Errorf("Get(0) = %+v, want %+v", got, hammy)
			}
		})
	}
}

func TestSet(t *testing.T) {
	var tbl Table
	tbl.Set(3, bot)
	if got, ok := tbl.Lookup(3); !ok || got != bot || !got.Bot() {
		t.Errorf("Lookup(3) = %+v, %v; want bot %+v", got, ok, bot)
	}
	// A new player in the slot replaces the old one.
	tbl.Set(3, furyy)
	if got, _ := tbl.Get(3); got != furyy {
		t.Errorf("Get(3) = %+v, want %+v", got, furyy)
	}
}

func TestDecodeEndOfMatch(t *testing.T) {
	type pd = pb.CCSUsrMsg_EndOfMatchAllPlayersData_PlayerData
	msg := &pb.CCSUsrMsg_EndOfMatchAllPlayersData{
		Allplayerdata: []*pd{
			{Slot: proto.Int32(0), Xuid: proto.Uint64(hammy.SteamID64), Name: proto.String(hammy.Name), Teamnumber: proto.Int32(3)},
			{Slot: proto.Int32(4), Xuid: proto.Uint64(0), Name: proto.String("Niko"), Teamnumber: proto.Int32(2), Isbot: proto.Bool(true),
				Items: []*pb.CEconItemPreviewDataBlock{{Defindex: proto.Uint32(7)}}},
		},
		Scene: proto.Int32(1),
	}
	body, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeEndOfMatch(body)
	if err != nil {
		t.Fatal(err)
	}
	want := []EndOfMatchPlayer{
		{Slot: 0, SteamID64: hammy.SteamID64, Name: hammy.Name, Team: 3},
		{Slot: 4, Name: "Niko", Team: 2, Bot: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("DecodeEndOfMatch = %+v\nwant %+v", got, want)
	}

	if got, err := DecodeEndOfMatch(nil); err != nil || len(got) != 0 {
		t.Errorf("empty body: %v, %v; want no players", got, err)
	}

	noSlot, _ := proto.Marshal(&pb.CCSUsrMsg_EndOfMatchAllPlayersData{Allplayerdata: []*pd{{Name: proto.String("x")}}})
	for name, b := range map[string][]byte{
		"bad protobuf": {0x0a, 0x05, 0x08},
		"no slot":      noSlot,
	} {
		if _, err := DecodeEndOfMatch(b); !errors.Is(err, demo.ErrBadDemo) {
			t.Errorf("%s: err = %v, want ErrBadDemo", name, err)
		}
	}
}

func TestMsgEndOfMatch(t *testing.T) {
	if MsgEndOfMatch != 375 {
		t.Errorf("MsgEndOfMatch = %d, want 375", MsgEndOfMatch)
	}
}
