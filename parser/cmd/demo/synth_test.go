package main

import (
	"encoding/binary"

	"github.com/klauspost/compress/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// testFrame is a frame for buildDemo.
type testFrame struct {
	cmd      demo.Command
	tick     int32
	compress bool   // snappy-compress the payload
	payload  []byte // decompressed
}

// buildDemo returns a demo file holding frames.
func buildDemo(frames ...testFrame) []byte {
	b := append([]byte(nil), demo.Magic[:]...)
	b = binary.LittleEndian.AppendUint32(b, 0) // fileinfo offset
	b = binary.LittleEndian.AppendUint32(b, 0) // spawngroups offset
	for _, f := range frames {
		cmd, p := uint64(f.cmd), f.payload
		if f.compress {
			cmd |= 0x40
			p = snappy.Encode(nil, p)
		}
		b = binary.AppendUvarint(b, cmd)
		b = binary.AppendUvarint(b, uint64(uint32(f.tick)))
		b = binary.AppendUvarint(b, uint64(len(p)))
		b = append(b, p...)
	}
	return b
}

// pbBytes marshals m, panicking on error.
func pbBytes(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

// testMsg is one packet message for packetBits.
type testMsg struct {
	typ  uint32
	body []byte
}

// packetBits returns the bit stream of msgs and the number of padding bits
// after the last one.
func packetBits(msgs ...testMsg) ([]byte, int) {
	var w bitWriter
	for _, m := range msgs {
		w.ubitvar(m.typ)
		w.varuint(uint64(len(m.body)))
		w.bytes(m.body)
	}
	return w.buf, len(w.buf)*8 - w.n
}

// packet returns a marshaled CDemoPacket holding msgs.
func packet(msgs ...testMsg) []byte {
	data, _ := packetBits(msgs...)
	return pbBytes(&pb.CDemoPacket{Data: data})
}

// bitWriter builds bit streams least significant bit first. It mirrors the
// one in package bitread's tests.
type bitWriter struct {
	buf []byte
	n   int // bits written
}

func (w *bitWriter) bits(v uint64, n int) {
	for i := range n {
		if w.n%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		w.buf[w.n/8] |= byte(v>>i&1) << (w.n % 8)
		w.n++
	}
}

func (w *bitWriter) bytes(p []byte) {
	for _, c := range p {
		w.bits(uint64(c), 8)
	}
}

// ubitvar writes v in the shortest UBitVar form.
func (w *bitWriter) ubitvar(v uint32) {
	form := 3
	switch {
	case v < 1<<4:
		form = 0
	case v < 1<<8:
		form = 1
	case v < 1<<12:
		form = 2
	}
	w.bits(uint64(v&0xF|uint32(form)<<4), 6)
	w.bits(uint64(v>>4), [4]int{0, 4, 8, 28}[form])
}

func (w *bitWriter) varuint(v uint64) {
	for v >= 0x80 {
		w.bits(v&0x7F|0x80, 8)
		v >>= 7
	}
	w.bits(v, 8)
}

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
