package parse

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse/parsetest"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// eventDescs are the game events the wiring test uses, indexed by id (from
// 1), with their keys, all players.
var eventDescs = []struct {
	name string
	keys []string // all TypePlayer
}{
	1: {"begin_new_match", nil},
	2: {"round_prestart", nil},
	3: {"player_spawn", []string{"userid"}},
	4: {"round_freeze_end", nil},
	5: {"player_death", []string{"userid", "attacker"}},
	6: {"cs_win_panel_match", nil},
}

func eventListMsg() parsetest.Msg {
	var list pb.CMsgSource1LegacyGameEventList
	for id, d := range eventDescs[1:] {
		desc := &pb.CMsgSource1LegacyGameEventListDescriptorT{Eventid: proto.Int32(int32(id + 1)), Name: proto.String(d.name)}
		for _, k := range d.keys {
			desc.Keys = append(desc.Keys, &pb.CMsgSource1LegacyGameEventListKeyT{Type: proto.Int32(int32(events.TypePlayer)), Name: proto.String(k)})
		}
		list.Descriptors = append(list.Descriptors, desc)
	}
	return parsetest.Msg{Type: uint32(pb.EBaseGameEvents_GE_Source1LegacyGameEventList), Body: parsetest.Marshal(&list)}
}

// gameEvent returns the message for event id with the given userids.
func gameEvent(id int32, userids ...int32) parsetest.Msg {
	m := &pb.CMsgSource1LegacyGameEvent{Eventid: proto.Int32(id)}
	for _, u := range userids {
		m.Keys = append(m.Keys, &pb.CMsgSource1LegacyGameEventKeyT{Type: proto.Int32(int32(events.TypePlayer)), ValShort: proto.Int32(u)})
	}
	return parsetest.Msg{Type: uint32(pb.EBaseGameEvents_GE_Source1LegacyGameEvent), Body: parsetest.Marshal(m)}
}

func userinfoTables() []byte {
	tbl := &pb.CDemoStringTablesTableT{TableName: proto.String("userinfo")}
	for slot, name := range []string{"ct", "t"} {
		info := &pb.CMsgPlayerInfo{Name: proto.String(name), Xuid: proto.Uint64(1000 + uint64(slot)), Userid: proto.Int32(int32(slot))}
		tbl.Items = append(tbl.Items, &pb.CDemoStringTablesItemsT{Str: proto.String(string(rune('0' + slot))), Data: parsetest.Marshal(info)})
	}
	return parsetest.Marshal(&pb.CDemoStringTables{Tables: []*pb.CDemoStringTablesTableT{tbl}})
}

// TestRunMatch checks that Run feeds events, string tables, cvars and the
// end-of-match message to match logic: a one-round 1v1.
func TestRunMatch(t *testing.T) {
	cvars := parsetest.Msg{Type: uint32(pb.NET_Messages_net_SetConVar), Body: parsetest.Marshal(&pb.CNETMsg_SetConVar{
		Convars: &pb.CMsg_CVars{Cvars: []*pb.CMsg_CVars_CVar{{Name: proto.String("mp_roundtime_defuse"), Value: proto.String("1.92")}}},
	})}
	eom := parsetest.Msg{Type: uint32(pb.ECstrike15UserMessages_CS_UM_EndOfMatchAllPlayersData), Body: parsetest.Marshal(&pb.CCSUsrMsg_EndOfMatchAllPlayersData{
		Allplayerdata: []*pb.CCSUsrMsg_EndOfMatchAllPlayersData_PlayerData{
			{Slot: proto.Int32(0), Xuid: proto.Uint64(1000), Name: proto.String("CT player"), Teamnumber: proto.Int32(3)},
			{Slot: proto.Int32(1), Xuid: proto.Uint64(1001), Name: proto.String("T player"), Teamnumber: proto.Int32(2)},
		},
	})}
	data := parsetest.Build(
		parsetest.Frame{Cmd: demo.CmdFileHeader, Payload: header},
		parsetest.Frame{Cmd: demo.CmdSignonPacket, Tick: -1, Payload: parsetest.Packet(serverInfo, eventListMsg(), cvars)},
		parsetest.Frame{Cmd: demo.CmdStringTables, Tick: -1, Payload: userinfoTables()},
		parsetest.Frame{Cmd: demo.CmdPacket, Tick: 100, Payload: parsetest.Packet(gameEvent(2), gameEvent(3, 0x100), gameEvent(3, 0x101), gameEvent(1))},
		parsetest.Frame{Cmd: demo.CmdPacket, Tick: 200, Payload: parsetest.Packet(gameEvent(4))},
		parsetest.Frame{Cmd: demo.CmdPacket, Tick: 300, Payload: parsetest.Packet(gameEvent(5, 0x101, 0x100), eom)},
		parsetest.Frame{Cmd: demo.CmdPacket, Tick: 400, Payload: parsetest.Packet(gameEvent(6))},
	)
	got, notes, err := run(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if !strings.HasPrefix(n, "the match ended ") { // a one-round match ends early
			t.Errorf("note: %s", n)
		}
	}
	if len(got.Players) != 2 || got.Players[0].Name != "CT player" || got.Players[0].Team != "A" || got.Players[1].SteamID != "1001" {
		t.Errorf("players %+v", got.Players)
	}
	want := match.Round{N: 1, StartTick: 100, FreezeEndTick: 200, EndTick: 300, Winner: "A", Reason: "elimination", SideA: "CT"}
	if len(got.Rounds) != 1 || got.Rounds[0] != want {
		t.Errorf("rounds %+v", got.Rounds)
	}
	if len(got.Kills) != 1 || *got.Kills[0].Attacker != 0 || got.Kills[0].Victim != 1 || got.Scoreboard[0].Kills != 1 {
		t.Errorf("kills %+v, scoreboard %+v", got.Kills, got.Scoreboard)
	}
}

// An event that doesn't decode is a bad demo, and the error says which
// frame holds it.
func TestRunBadEventNamesFrame(t *testing.T) {
	data := parsetest.Build(
		parsetest.Frame{Cmd: demo.CmdSignonPacket, Tick: -1, Payload: parsetest.Packet(eventListMsg())},
		parsetest.Frame{Cmd: demo.CmdPacket, Tick: 7, Payload: parsetest.Packet(gameEvent(99))},
	)
	_, err := Run(context.Background(), bytes.NewReader(data))
	if !errors.Is(err, events.ErrUnknownEvent) || !strings.Contains(err.Error(), "Packet frame at offset ") || !strings.Contains(err.Error(), "tick 7") {
		t.Errorf("error = %v, want ErrUnknownEvent naming the frame", err)
	}
}
