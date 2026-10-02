// Package players tracks who is in each player slot of a CS2 demo.
//
// Game events name players by userid, whose low byte is the player slot. The
// higher bits vary: they change when a player reconnects, and Valve MM demos
// set them on every player. The "userinfo" string table, keyed by slot,
// says who is in each slot. Each value is a CMsgPlayerInfo.
//
// The table arrives as plain protobuf in two places. DEM_StringTables at
// signon carries every table. A DEM_FullPacket's string_table carries only
// the tables that changed since the previous full packet, so most full
// packets have no userinfo. When userinfo is there, it lists every slot, with
// empty values for empty slots. On the FACEIT fixture, 8 of 38 snapshots
// have it, the last at tick 38401 of about 140,000.
//
// Changes between snapshots arrive as svc_UpdateStringTable messages, which
// this package doesn't decode. Apply player_connect events with Table.Set to
// cover them: a bot that fills in for a disconnected player, for example, may
// get kills before any snapshot lists it.
package players

import (
	"fmt"
	"iter"
	"strconv"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"google.golang.org/protobuf/proto"
)

// Slot is a player slot: the low byte of a userid, and the key of the
// userinfo string table.
type Slot uint8

// SlotOf returns the slot that a game event's userid refers to: its low byte.
// The game's "no player" userid, 0xFFFF or -1, gives slot 255, which no
// string table fills.
func SlotOf(userid int32) Slot { return Slot(userid & 0xFF) }

// Info is what the demo says about the player in one slot.
//
// SteamID64 comes from CMsgPlayerInfo's xuid, not its steamid field: GOTV has
// xuid 0 but a game-server id in steamid.
type Info struct {
	Name      string
	SteamID64 uint64 // CMsgPlayerInfo.xuid: 0 for bots and GOTV
	UserID    int32  // the full userid, high bits included; SlotOf(UserID) is the slot
	Fake      bool   // a bot or GOTV
	HLTV      bool   // GOTV (SourceTV)
}

// Bot reports whether i is a bot: a fake player that isn't GOTV.
func (i Info) Bot() bool { return i.Fake && !i.HLTV }

// userInfoTable is the string table that holds CMsgPlayerInfo by slot.
const userInfoTable = "userinfo"

// Table maps slots to players. The zero value is an empty table, ready to
// use.
//
// A slot keeps its last known player when the player leaves: a snapshot that
// shows the slot empty doesn't erase it. That way, events that trail a
// disconnect still resolve, and so do players who left before the last
// snapshot (the Valve MM fixture's final snapshot is missing two of its ten
// players). A new player in the slot replaces the old one.
type Table struct {
	info [256]Info
	set  [256]bool
}

// Set records info as the player in slot. Use it to apply player_connect
// events, with slot = SlotOf(userid).
func (t *Table) Set(slot Slot, info Info) {
	t.info[slot], t.set[slot] = info, true
}

// Get returns the player in slot, and whether the slot has ever held one.
func (t *Table) Get(slot Slot) (Info, bool) {
	return t.info[slot], t.set[slot]
}

// Lookup returns the player that a game event's userid refers to. It matches
// on the slot alone, so a userid whose high bits differ from Info.UserID
// still finds the player.
func (t *Table) Lookup(userid int32) (Info, bool) {
	return t.Get(SlotOf(userid))
}

// All yields every filled slot and its player, in slot order.
func (t *Table) All() iter.Seq2[Slot, Info] {
	return func(yield func(Slot, Info) bool) {
		for s := range t.info {
			if t.set[s] && !yield(Slot(s), t.info[s]) {
				return
			}
		}
	}
}

// ApplyStringTables applies the userinfo table from a string-table snapshot:
// a DEM_StringTables payload, or a DEM_FullPacket's string_table. Other
// tables are ignored, and so is a nil snapshot or one without userinfo,
// which means userinfo hasn't changed. Empty entries leave their slot as it
// was (see Table).
//
// A key that isn't a slot number, or a value that isn't a CMsgPlayerInfo,
// gives an error wrapping demo.ErrBadDemo, and the table is left unchanged.
func (t *Table) ApplyStringTables(st *pb.CDemoStringTables) error {
	type entry struct {
		slot Slot
		info Info
	}
	var entries []entry
	for _, tbl := range st.GetTables() {
		if tbl.GetTableName() != userInfoTable {
			continue
		}
		for _, it := range tbl.GetItems() {
			if len(it.GetData()) == 0 {
				continue
			}
			n, err := strconv.ParseUint(it.GetStr(), 10, 8)
			if err != nil {
				return badDemof("userinfo key %q is not a slot", it.GetStr())
			}
			info, err := ParsePlayerInfo(it.GetData())
			if err != nil {
				return fmt.Errorf("%w (slot %d)", err, n)
			}
			entries = append(entries, entry{Slot(n), info})
		}
	}
	for _, e := range entries {
		t.Set(e.slot, e.info)
	}
	return nil
}

// ParsePlayerInfo decodes a userinfo string-table value, a CMsgPlayerInfo.
// Malformed data gives an error wrapping demo.ErrBadDemo.
func ParsePlayerInfo(data []byte) (Info, error) {
	var m pb.CMsgPlayerInfo
	if err := proto.Unmarshal(data, &m); err != nil {
		return Info{}, badDemof("userinfo value: %v", err)
	}
	return Info{
		Name:      m.GetName(),
		SteamID64: m.GetXuid(),
		UserID:    m.GetUserid(),
		Fake:      m.GetFakeplayer(),
		HLTV:      m.GetIshltv(),
	}, nil
}

// badDemof returns an error wrapping demo.ErrBadDemo.
func badDemof(format string, args ...any) error {
	return fmt.Errorf("%w: players: %s", demo.ErrBadDemo, fmt.Sprintf(format, args...))
}
