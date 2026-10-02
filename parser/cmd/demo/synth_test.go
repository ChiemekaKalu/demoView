package main

import (
	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/parse/parsetest"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// testFrame is a frame for buildDemo. These short local forms of the
// parsetest builders keep the many fixtures below compact.
type testFrame struct {
	cmd      demo.Command
	tick     int32
	compress bool   // snappy-compress the payload
	payload  []byte // decompressed
}

// buildDemo returns a demo file holding frames.
func buildDemo(frames ...testFrame) []byte {
	fs := make([]parsetest.Frame, len(frames))
	for i, f := range frames {
		fs[i] = parsetest.Frame{Cmd: f.cmd, Tick: f.tick, Compress: f.compress, Payload: f.payload}
	}
	return parsetest.Build(fs...)
}

// testMsg is one packet message.
type testMsg struct {
	typ  uint32
	body []byte
}

func toMsgs(msgs []testMsg) []parsetest.Msg {
	out := make([]parsetest.Msg, len(msgs))
	for i, m := range msgs {
		out[i] = parsetest.Msg{Type: m.typ, Body: m.body}
	}
	return out
}

// packetBits returns the bit stream of msgs and the number of padding bits
// after the last one.
func packetBits(msgs ...testMsg) ([]byte, int) { return parsetest.PacketBits(toMsgs(msgs)...) }

// packet returns a marshaled CDemoPacket holding msgs.
func packet(msgs ...testMsg) []byte { return parsetest.Packet(toMsgs(msgs)...) }

var pbBytes = parsetest.Marshal

// Contents of testDemo.
var (
	testHeader = &pb.CDemoFileHeader{
		DemoFileStamp:   proto.String("PBDEMS2\x00"),
		PatchVersion:    proto.Int32(14091),
		ServerName:      proto.String("Synthetic Test Server"),
		ClientName:      proto.String("SourceTV Demo"),
		MapName:         proto.String("de_mirage"),
		GameDirectory:   proto.String("csgo"),
		DemoVersionName: proto.String("valve_demo_2"),
		BuildNum:        proto.Int32(10599),
	}
	testInfo = &pb.CDemoFileInfo{
		PlaybackTime:   proto.Float32(1.5),
		PlaybackTicks:  proto.Int32(96),
		PlaybackFrames: proto.Int32(48),
	}
	tickBody = pbBytes(&pb.CNETMsg_Tick{Tick: proto.Uint32(5)})

	// testSignon and testPacket are the messages of testDemo's two
	// non-empty packets.
	testSignon = []testMsg{
		{uint32(pb.SVC_Messages_svc_ServerInfo), []byte("server info")},
		{uint32(pb.NET_Messages_net_Tick), tickBody},
	}
	testPacket = []testMsg{
		{uint32(pb.NET_Messages_net_Tick), tickBody},
		{uint32(pb.SVC_Messages_svc_PacketEntities), make([]byte, 300)},
		{uint32(pb.EBaseGameEvents_GE_Source1LegacyGameEvent), []byte{0x08, 0x17}},
		{123, nil},
	}
)

// testDemo is a small valid demo: a FileHeader, a compressed SignonPacket,
// two Packets (one empty), Stop and then a compressed FileInfo.
var testDemo = buildDemo(
	testFrame{demo.CmdFileHeader, 0, false, pbBytes(testHeader)},
	testFrame{demo.CmdSignonPacket, -1, true, packet(testSignon...)},
	testFrame{demo.CmdPacket, 5, false, packet(testPacket...)},
	testFrame{demo.CmdPacket, 5, false, packet()},
	testFrame{demo.CmdStop, 6, false, nil},
	testFrame{demo.CmdFileInfo, 6, true, pbBytes(testInfo)},
)
