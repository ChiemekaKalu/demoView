package pb

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestDemoFileHeaderRoundTrip(t *testing.T) {
	in := &CDemoFileHeader{
		DemoFileStamp:           proto.String("PBDEMS2\x00"),
		PatchVersion:            proto.Int32(14091),
		ServerName:              proto.String("Valve Counter-Strike 2 Server"),
		ClientName:              proto.String("SourceTV Demo"),
		MapName:                 proto.String("de_mirage"),
		GameDirectory:           proto.String("csgo"),
		FullpacketsVersion:      proto.Int32(2),
		AllowClientsideEntities: proto.Bool(true),
		BuildNum:                proto.Int32(10599),
		ServerStartTick:         proto.Int32(-1),
	}

	b, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	out := &CDemoFileHeader{}
	if err := proto.Unmarshal(b, out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !proto.Equal(in, out) {
		t.Fatalf("round trip mismatch:\n in: %v\nout: %v", in, out)
	}
	if got := out.GetMapName(); got != "de_mirage" {
		t.Errorf("MapName = %q, want de_mirage", got)
	}
	if got := out.GetServerStartTick(); got != -1 {
		t.Errorf("ServerStartTick = %d, want -1", got)
	}
}

// TestMessageTypes names every message the parser depends on, so dropping one
// from the vendored protos fails to compile. It also checks that each is
// registered under its bare (package-less) proto name.
func TestMessageTypes(t *testing.T) {
	msgs := map[string]proto.Message{
		// demo.proto
		"CDemoFileHeader":   &CDemoFileHeader{},
		"CDemoFileInfo":     &CDemoFileInfo{},
		"CDemoPacket":       &CDemoPacket{},
		"CDemoFullPacket":   &CDemoFullPacket{},
		"CDemoSendTables":   &CDemoSendTables{},
		"CDemoClassInfo":    &CDemoClassInfo{},
		"CDemoStringTables": &CDemoStringTables{},
		// netmessages.proto
		"CSVCMsg_ServerInfo":          &CSVCMsg_ServerInfo{},
		"CSVCMsg_ClassInfo":           &CSVCMsg_ClassInfo{},
		"CSVCMsg_CreateStringTable":   &CSVCMsg_CreateStringTable{},
		"CSVCMsg_UpdateStringTable":   &CSVCMsg_UpdateStringTable{},
		"CSVCMsg_PacketEntities":      &CSVCMsg_PacketEntities{},
		"CSVCMsg_FlattenedSerializer": &CSVCMsg_FlattenedSerializer{},
		// networkbasetypes.proto
		"CNETMsg_Tick":   &CNETMsg_Tick{},
		"CMsgPlayerInfo": &CMsgPlayerInfo{},
		// gameevents.proto
		"CMsgSource1LegacyGameEventList": &CMsgSource1LegacyGameEventList{},
		"CMsgSource1LegacyGameEvent":     &CMsgSource1LegacyGameEvent{},
	}
	for want, m := range msgs {
		if got := string(m.ProtoReflect().Descriptor().FullName()); got != want {
			t.Errorf("full name = %q, want %q", got, want)
		}
	}
}

// TestEnumValues pins the wire ids the parser switches on. If a regenerate
// against a newer upstream commit renumbers any of them, this fails.
func TestEnumValues(t *testing.T) {
	tests := []struct {
		name      string
		got, want int32
	}{
		// demo.proto: EDemoCommands (frame command types)
		{"DEM_Error", int32(EDemoCommands_DEM_Error), -1},
		{"DEM_Stop", int32(EDemoCommands_DEM_Stop), 0},
		{"DEM_FileHeader", int32(EDemoCommands_DEM_FileHeader), 1},
		{"DEM_FileInfo", int32(EDemoCommands_DEM_FileInfo), 2},
		{"DEM_SyncTick", int32(EDemoCommands_DEM_SyncTick), 3},
		{"DEM_SendTables", int32(EDemoCommands_DEM_SendTables), 4},
		{"DEM_ClassInfo", int32(EDemoCommands_DEM_ClassInfo), 5},
		{"DEM_StringTables", int32(EDemoCommands_DEM_StringTables), 6},
		{"DEM_Packet", int32(EDemoCommands_DEM_Packet), 7},
		{"DEM_SignonPacket", int32(EDemoCommands_DEM_SignonPacket), 8},
		{"DEM_ConsoleCmd", int32(EDemoCommands_DEM_ConsoleCmd), 9},
		{"DEM_CustomData", int32(EDemoCommands_DEM_CustomData), 10},
		{"DEM_CustomDataCallbacks", int32(EDemoCommands_DEM_CustomDataCallbacks), 11},
		{"DEM_UserCmd", int32(EDemoCommands_DEM_UserCmd), 12},
		{"DEM_FullPacket", int32(EDemoCommands_DEM_FullPacket), 13},
		{"DEM_SaveGame", int32(EDemoCommands_DEM_SaveGame), 14},
		{"DEM_SpawnGroups", int32(EDemoCommands_DEM_SpawnGroups), 15},
		{"DEM_AnimationData", int32(EDemoCommands_DEM_AnimationData), 16},
		{"DEM_AnimationHeader", int32(EDemoCommands_DEM_AnimationHeader), 17},
		{"DEM_Recovery", int32(EDemoCommands_DEM_Recovery), 18},
		{"DEM_Max", int32(EDemoCommands_DEM_Max), 19},
		{"DEM_IsCompressed", int32(EDemoCommands_DEM_IsCompressed), 64},

		// netmessages.proto: SVC_Messages (packet message types)
		{"svc_ServerInfo", int32(SVC_Messages_svc_ServerInfo), 40},
		{"svc_FlattenedSerializer", int32(SVC_Messages_svc_FlattenedSerializer), 41},
		{"svc_ClassInfo", int32(SVC_Messages_svc_ClassInfo), 42},
		{"svc_CreateStringTable", int32(SVC_Messages_svc_CreateStringTable), 44},
		{"svc_UpdateStringTable", int32(SVC_Messages_svc_UpdateStringTable), 45},
		{"svc_PacketEntities", int32(SVC_Messages_svc_PacketEntities), 55},

		// networkbasetypes.proto: NET_Messages
		{"net_Tick", int32(NET_Messages_net_Tick), 4},

		// gameevents.proto: EBaseGameEvents
		{"GE_Source1LegacyGameEventList", int32(EBaseGameEvents_GE_Source1LegacyGameEventList), 205},
		{"GE_Source1LegacyGameEvent", int32(EBaseGameEvents_GE_Source1LegacyGameEvent), 207},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}
