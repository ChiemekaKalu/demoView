package parsetest

import (
	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// eventNames are the game events EventList describes, with ids from 1 in
// this order. Every key is a player.
var eventNames = []struct {
	name string
	keys []string
}{
	{"begin_new_match", nil},
	{"round_prestart", nil},
	{"player_spawn", []string{"userid"}},
	{"round_freeze_end", nil},
	{"player_death", []string{"userid", "attacker"}},
	{"cs_win_panel_match", nil},
}

// EventList returns the game event descriptor list for GameEvent's events.
func EventList() Msg {
	var list pb.CMsgSource1LegacyGameEventList
	for i, e := range eventNames {
		d := &pb.CMsgSource1LegacyGameEventListDescriptorT{Eventid: proto.Int32(int32(i + 1)), Name: proto.String(e.name)}
		for _, k := range e.keys {
			d.Keys = append(d.Keys, &pb.CMsgSource1LegacyGameEventListKeyT{Type: proto.Int32(int32(events.TypePlayer)), Name: proto.String(k)})
		}
		list.Descriptors = append(list.Descriptors, d)
	}
	return Msg{Type: uint32(pb.EBaseGameEvents_GE_Source1LegacyGameEventList), Body: Marshal(&list)}
}

// GameEvent returns the named event from EventList, with userids as its
// player keys in order.
func GameEvent(name string, userids ...int32) Msg {
	id := 0
	for i, e := range eventNames {
		if e.name == name {
			id = i + 1
		}
	}
	if id == 0 {
		panic("parsetest: no event " + name)
	}
	m := &pb.CMsgSource1LegacyGameEvent{Eventid: proto.Int32(int32(id))}
	for _, u := range userids {
		m.Keys = append(m.Keys, &pb.CMsgSource1LegacyGameEventKeyT{Type: proto.Int32(int32(events.TypePlayer)), ValShort: proto.Int32(u)})
	}
	return Msg{Type: uint32(pb.EBaseGameEvents_GE_Source1LegacyGameEvent), Body: Marshal(m)}
}

// The players of Match: slot 0 plays CT and slot 1 plays T.
const (
	CTSteamID = 1000
	TSteamID  = 1001
)

// MatchFrames returns the frames of a one-round 1v1 with no FileHeader or
// ServerInfo: the CT player kills the T player at tick 300. The last frame
// is Stop, at tick 400.
func MatchFrames() []Frame { return matchFrames(1) }

// matchFrames returns the frames of an n-round 1v1 with no FileHeader or
// ServerInfo. Every round takes 300 ticks, from 100: freeze time ends 100
// ticks in, and the CT player kills the T player 100 ticks later. Stop
// comes 100 ticks after the last kill.
func matchFrames(n int) []Frame {
	cvars := Msg{Type: uint32(pb.NET_Messages_net_SetConVar), Body: Marshal(&pb.CNETMsg_SetConVar{
		Convars: &pb.CMsg_CVars{Cvars: []*pb.CMsg_CVars_CVar{{Name: proto.String("mp_roundtime_defuse"), Value: proto.String("1.92")}}},
	})}
	userinfo := &pb.CDemoStringTablesTableT{TableName: proto.String("userinfo")}
	for slot, name := range []string{"ct", "t"} {
		info := &pb.CMsgPlayerInfo{Name: proto.String(name), Xuid: proto.Uint64(uint64(CTSteamID + slot)), Userid: proto.Int32(int32(slot))}
		userinfo.Items = append(userinfo.Items, &pb.CDemoStringTablesItemsT{Str: proto.String(string(rune('0' + slot))), Data: Marshal(info)})
	}
	endOfMatch := Msg{Type: uint32(pb.ECstrike15UserMessages_CS_UM_EndOfMatchAllPlayersData), Body: Marshal(&pb.CCSUsrMsg_EndOfMatchAllPlayersData{
		Allplayerdata: []*pb.CCSUsrMsg_EndOfMatchAllPlayersData_PlayerData{
			{Slot: proto.Int32(0), Xuid: proto.Uint64(CTSteamID), Name: proto.String("CT player"), Teamnumber: proto.Int32(3)},
			{Slot: proto.Int32(1), Xuid: proto.Uint64(TSteamID), Name: proto.String("T player"), Teamnumber: proto.Int32(2)},
		},
	})}
	frames := []Frame{
		{Cmd: demo.CmdSignonPacket, Tick: -1, Payload: Packet(EventList(), cvars)},
		{Cmd: demo.CmdStringTables, Tick: -1, Payload: Marshal(&pb.CDemoStringTables{Tables: []*pb.CDemoStringTablesTableT{userinfo}})},
	}
	for i := range int32(n) {
		start := []Msg{GameEvent("round_prestart"), GameEvent("player_spawn", 0x100), GameEvent("player_spawn", 0x101)}
		if i == 0 {
			start = append(start, GameEvent("begin_new_match"))
		}
		kill := []Msg{GameEvent("player_death", 0x101, 0x100)}
		if i == int32(n)-1 {
			kill = append(kill, endOfMatch)
		}
		frames = append(frames,
			Frame{Cmd: demo.CmdPacket, Tick: 100 + 300*i, Payload: Packet(start...)},
			Frame{Cmd: demo.CmdPacket, Tick: 200 + 300*i, Payload: Packet(GameEvent("round_freeze_end"))},
			Frame{Cmd: demo.CmdPacket, Tick: 300 + 300*i, Payload: Packet(kill...)},
		)
	}
	end := 100 + 300*int32(n)
	return append(frames,
		Frame{Cmd: demo.CmdPacket, Tick: end, Payload: Packet(GameEvent("cs_win_panel_match"))},
		Frame{Cmd: demo.CmdStop, Tick: end},
	)
}

// Match returns a complete demo of a one-round 1v1 on de_mirage at 64
// ticks: a FileHeader and ServerInfo, then MatchFrames.
func Match() []byte { return MatchRounds(1) }

// MatchRounds returns a complete demo like Match's, of an n-round 1v1 with
// a kill in every round, as matchFrames describes.
func MatchRounds(n int) []byte {
	header := Marshal(&pb.CDemoFileHeader{DemoFileStamp: proto.String("PBDEMS2\x00"), MapName: proto.String("de_mirage")})
	serverInfo := Msg{Type: uint32(pb.SVC_Messages_svc_ServerInfo), Body: Marshal(&pb.CSVCMsg_ServerInfo{TickInterval: proto.Float32(1.0 / 64)})}
	frames := []Frame{
		{Cmd: demo.CmdFileHeader, Payload: header},
		{Cmd: demo.CmdSignonPacket, Tick: -1, Payload: Packet(serverInfo)},
	}
	return Build(append(frames, matchFrames(n)...)...)
}
