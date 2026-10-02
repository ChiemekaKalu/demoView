package matchlogic

import (
	"slices"
	"strconv"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/players"
)

// testEvent is a descriptor the Recorder tests use, with the key types the
// game gives it.
type testEvent struct {
	name string
	keys []testKey
}

type testKey struct {
	name string
	typ  events.KeyType
}

var testEvents = []testEvent{
	{"player_connect", []testKey{{"name", events.TypeString}, {"userid", events.TypePlayer}, {"xuid", events.TypeUint64}, {"bot", events.TypeBool}}},
	{"player_death", []testKey{{"userid", events.TypePlayer}, {"attacker", events.TypePlayer}, {"assister", events.TypePlayer}, {"assistedflash", events.TypeBool}, {"weapon", events.TypeString}, {"headshot", events.TypeBool}}},
	{"player_hurt", []testKey{{"userid", events.TypePlayer}, {"attacker", events.TypePlayer}, {"health", events.TypeByte}, {"dmg_health", events.TypeShort}}},
	{"player_team", []testKey{{"userid", events.TypePlayer}, {"team", events.TypeByte}, {"oldteam", events.TypeByte}}},
	{"player_spawn", []testKey{{"userid", events.TypePlayer}}},
	{"round_end", []testKey{{"winner", events.TypeByte}, {"reason", events.TypeByte}, {"message", events.TypeString}}},
	{"server_cvar", []testKey{{"cvarname", events.TypeString}, {"cvarvalue", events.TypeString}}},
	{"weapon_fire", []testKey{{"userid", events.TypePlayer}}},
}

// newTestDecoder returns a Decoder that knows testEvents, with ids from 1.
func newTestDecoder(t *testing.T) *events.Decoder {
	t.Helper()
	var list pb.CMsgSource1LegacyGameEventList
	for i, te := range testEvents {
		d := &pb.CMsgSource1LegacyGameEventListDescriptorT{Eventid: proto.Int32(int32(i + 1)), Name: proto.String(te.name)}
		for _, k := range te.keys {
			d.Keys = append(d.Keys, &pb.CMsgSource1LegacyGameEventListKeyT{Type: proto.Int32(int32(k.typ)), Name: proto.String(k.name)})
		}
		list.Descriptors = append(list.Descriptors, d)
	}
	var dec events.Decoder
	if err := dec.HandleList(mustMarshal(t, &list)); err != nil {
		t.Fatal(err)
	}
	return &dec
}

// event decodes a game event with the given values, in key order: string,
// bool, uint64 or int.
func event(t *testing.T, dec *events.Decoder, tick int32, name string, vals ...any) events.Event {
	t.Helper()
	i := slices.IndexFunc(testEvents, func(te testEvent) bool { return te.name == name })
	m := &pb.CMsgSource1LegacyGameEvent{Eventid: proto.Int32(int32(i + 1))}
	for j, k := range testEvents[i].keys {
		key := &pb.CMsgSource1LegacyGameEventKeyT{Type: proto.Int32(int32(k.typ))}
		switch v := vals[j].(type) {
		case string:
			key.ValString = proto.String(v)
		case bool:
			key.ValBool = proto.Bool(v)
		case uint64:
			key.ValUint64 = proto.Uint64(v)
		case int:
			if k.typ == events.TypeByte {
				key.ValByte = proto.Int32(int32(v))
			} else {
				key.ValShort = proto.Int32(int32(v))
			}
		}
		m.Keys = append(m.Keys, key)
	}
	e, err := dec.Decode(mustMarshal(t, m), tick)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// userinfo returns a string-table snapshot with one userinfo entry per
// slot.
func userinfo(t *testing.T, slots map[int]*pb.CMsgPlayerInfo) *pb.CDemoStringTables {
	tbl := &pb.CDemoStringTablesTableT{TableName: proto.String("userinfo")}
	for slot := range 64 {
		item := &pb.CDemoStringTablesItemsT{Str: proto.String(strconv.Itoa(slot))}
		if info, ok := slots[slot]; ok {
			item.Data = mustMarshal(t, info)
		}
		tbl.Items = append(tbl.Items, item)
	}
	return &pb.CDemoStringTables{Tables: []*pb.CDemoStringTablesTableT{tbl}}
}

func TestRecorderIdentity(t *testing.T) {
	dec := newTestDecoder(t)
	rec := NewRecorder()
	const (
		x1 = uint64(76500000000000001) // fake SteamID64s
		x2 = uint64(76500000000000002)
		x3 = uint64(76500000000000003)
	)
	// A slot named before any userinfo: a placeholder, filled in later.
	rec.Event(event(t, dec, 1, "player_spawn", 9))
	if err := rec.StringTables(userinfo(t, map[int]*pb.CMsgPlayerInfo{
		3:  {Name: proto.String("h1"), Xuid: proto.Uint64(x1), Userid: proto.Int32(0x103)},
		7:  {Name: proto.String("h2"), Xuid: proto.Uint64(x2), Userid: proto.Int32(7)},
		10: {Name: proto.String("GOTV"), Userid: proto.Int32(10), Fakeplayer: proto.Bool(true), Ishltv: proto.Bool(true)},
	})); err != nil {
		t.Fatal(err)
	}
	rec.Event(event(t, dec, 2, "player_connect", "h3", 9, x3, false))
	// userid high bits vary; the slot is the low byte. 65535 is nobody.
	rec.Event(event(t, dec, 3, "player_death", 0x203, 7, 0xFFFF, false, "ak47", true))
	// h2 leaves; someone else takes slot 7, then h2 comes back in slot 5.
	rec.Event(event(t, dec, 4, "player_connect", "Ava", 7, uint64(0), true))
	rec.Event(event(t, dec, 5, "player_spawn", 7))
	rec.Event(event(t, dec, 6, "player_connect", "h2 renamed", 5, x2, false))
	rec.Event(event(t, dec, 7, "player_spawn", 5))
	// Another bot named Ava, in another slot, is the same bot.
	rec.Event(event(t, dec, 8, "player_connect", "Ava", 12, uint64(0), true))
	rec.Event(event(t, dec, 9, "player_spawn", 12))
	rec.Event(event(t, dec, 10, "weapon_fire", 3)) // not recorded
	rec.EndOfMatch([]players.EndOfMatchPlayer{
		{Slot: 3, SteamID64: x1, Name: "H1", Team: 3},
		{Slot: 5, SteamID64: x2, Name: "H2", Team: 2},
		{Slot: 20, SteamID64: 76500000000000099, Name: "late", Team: 2},
	})

	log := rec.Log()
	name := func(p int) string {
		if p == NoPlayer {
			return "-"
		}
		return log.Players[p].Name()
	}
	var got []string
	for _, e := range log.Entries {
		got = append(got, e.Kind.String()+" "+name(e.Player)+" "+name(e.Attacker)+" "+name(e.Assister))
	}
	want := []string{
		"player_spawn h3 - -",
		"player_death H1 h2 renamed -",
		"player_spawn Ava - -",
		"player_spawn h2 renamed - -",
		"player_spawn Ava - -",
	}
	// Names are the end-of-match names where there are any: h2 is "H2".
	want[1], want[3] = "player_death H1 H2 -", "player_spawn H2 - -"
	if !slices.Equal(got, want) {
		t.Errorf("entries\n got %q\nwant %q", got, want)
	}
	if e := log.Entries[1]; !e.Headshot || e.Weapon != "ak47" {
		t.Errorf("death = %+v, want an ak47 headshot", e)
	}

	var people []string
	for _, p := range log.Players {
		people = append(people, p.Name()+"/"+p.FinalSide.String())
	}
	wantPeople := []string{"h3/Side(0)", "H1/CT", "H2/T", "GOTV/Side(0)", "Ava/Side(0)", "late/T"}
	if !slices.Equal(people, wantPeople) {
		t.Errorf("participants\n got %q\nwant %q", people, wantPeople)
	}
}

func TestRecorderRecords(t *testing.T) {
	dec := newTestDecoder(t)
	rec := NewRecorder()
	rec.Event(event(t, dec, 1, "player_team", 4, 3, 2))
	rec.Event(event(t, dec, 2, "player_hurt", 4, 5, 0, 140))
	rec.Event(event(t, dec, 3, "round_end", 2, 9, "#SFUI_Notice_Terrorists_Win"))
	rec.Event(event(t, dec, 4, "server_cvar", "mp_roundtime_defuse", "1.92"))
	rec.Event(event(t, dec, 4, "server_cvar", "sv_cheats", "0")) // not used
	rec.ConVars(5, &pb.CMsg_CVars{Cvars: []*pb.CMsg_CVars_CVar{
		{Name: proto.String("mp_maxrounds"), Value: proto.String("24")},
		{Name: proto.String("mp_friendlyfire"), Value: proto.String("1")},
	}})
	rec.VotePass(6, &pb.CCSUsrMsg_VotePass{Team: proto.Int32(2), DispStr: proto.String("#SFUI_vote_passed_timeout")})
	rec.VotePass(7, &pb.CCSUsrMsg_VotePass{Team: proto.Int32(3), DispStr: proto.String("#SFUI_vote_passed_surrender_q")})

	es := rec.Log().Entries
	if len(es) != 6 {
		t.Fatalf("%d entries, want 6: %+v", len(es), es)
	}
	if e := es[0]; e.Kind != PlayerTeam || e.Side != CT || e.OldSide != T {
		t.Errorf("player_team = %+v", e)
	}
	if e := es[1]; e.Kind != PlayerHurt || e.Health != 0 || e.Damage != 140 || e.Player == e.Attacker {
		t.Errorf("player_hurt = %+v", e)
	}
	if e := es[2]; e.Kind != RoundEnd || e.Side != T || e.Reason != 9 {
		t.Errorf("round_end = %+v", e)
	}
	if e := es[3]; e.Kind != ConVar || e.Name != "mp_roundtime_defuse" || e.Value != "1.92" {
		t.Errorf("server_cvar = %+v", e)
	}
	if e := es[4]; e.Kind != ConVar || e.Name != "mp_maxrounds" || e.Value != "24" {
		t.Errorf("net_SetConVar = %+v", e)
	}
	if e := es[5]; e.Kind != Surrender || e.Side != CT || e.Tick != 7 {
		t.Errorf("surrender = %+v", e)
	}
}
