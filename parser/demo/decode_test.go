package demo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// marshal encodes m, failing the test on error. Missing required fields are
// allowed, so tests can build partial messages.
func marshal(t testing.TB, m proto.Message) []byte {
	t.Helper()
	b, err := proto.MarshalOptions{AllowPartial: true}.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// compressed returns a frame carrying payload snappy-compressed.
func compressed(cmd Command, tick uint32, payload []byte) rawFrame {
	return rawFrame{uint32(cmd) | compressedFlag, tick, snappy.Encode(nil, payload)}
}

// newDecoder returns a Decoder over the in-memory demo b.
func newDecoder(t testing.TB, b []byte) *Decoder {
	t.Helper()
	r, err := NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return NewDecoder(r)
}

func TestDecoder(t *testing.T) {
	big := bytes.Repeat([]byte("abcdefgh"), 4096)
	small := []byte("short payload")
	raw := []rawFrame{
		{uint32(CmdFileHeader), 0, []byte("plain")},
		compressed(CmdPacket, 1, big),
		compressed(CmdPacket, 2, small), // reuses the buffer grown for big
		compressed(CmdSignonPacket, 0xFFFFFFFF, nil),
		{uint32(CmdStop), 3, nil},
	}
	want := []Frame{
		{Cmd: CmdFileHeader, Tick: 0, Payload: []byte("plain")},
		{Cmd: CmdPacket, Compressed: true, Tick: 1, Payload: big},
		{Cmd: CmdPacket, Compressed: true, Tick: 2, Payload: small},
		{Cmd: CmdSignonPacket, Compressed: true, Tick: -1, Payload: nil},
		{Cmd: CmdStop, Tick: 3},
	}
	off := int64(headerSize)
	for i, f := range raw {
		want[i].Offset = off
		off += int64(len(f.appendTo(nil)))
	}

	d := newDecoder(t, synth(raw...))
	for i, w := range want {
		got, err := d.Next()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if got.Cmd != w.Cmd || got.Compressed != w.Compressed || got.Tick != w.Tick ||
			got.Offset != w.Offset || !bytes.Equal(got.Payload, w.Payload) {
			t.Fatalf("frame %d:\n got %+v\nwant %+v", i, got, w)
		}
	}
	for range 2 {
		if _, err := d.Next(); err != io.EOF {
			t.Fatalf("want io.EOF after last frame, got %v", err)
		}
	}
}

func TestDecoderAllocs(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789"), 500)
	var raw []rawFrame
	for i := range 300 {
		raw = append(raw, compressed(CmdPacket, uint32(i), payload))
	}
	d := newDecoder(t, synth(raw...))
	next := func() {
		if _, err := d.Next(); err != nil {
			t.Fatal(err)
		}
	}
	next() // grows the buffers
	if n := testing.AllocsPerRun(100, next); n != 0 {
		t.Errorf("Next allocates %v times per frame, want 0", n)
	}
}

func TestDecoderBadSnappy(t *testing.T) {
	valid := snappy.Encode(nil, bytes.Repeat([]byte("xyz"), 100))
	tests := []struct {
		name    string
		payload []byte
		want    string
	}{
		{"corrupt", []byte("z"), "snappy"},
		{"truncated", valid[:len(valid)-1], "snappy"},
		{"empty", nil, "snappy"},
		{"bad length varint", bytes.Repeat([]byte{0xFF}, 6), "snappy"},
		{"absurd length", binary.AppendUvarint(nil, MaxPayloadSize+1), "over limit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			good := rawFrame{uint32(CmdFileHeader), 0, []byte("ok")}
			bad := rawFrame{uint32(CmdPacket) | compressedFlag, 1, tc.payload}
			d := newDecoder(t, synth(good, bad))
			if _, err := d.Next(); err != nil {
				t.Fatalf("good frame: %v", err)
			}
			_, err := d.Next()
			if !errors.Is(err, ErrBadDemo) {
				t.Fatalf("want ErrBadDemo, got %v", err)
			}
			at := fmt.Sprintf("offset %d: Packet payload", headerSize+len(good.appendTo(nil)))
			for _, s := range []string{at, tc.want} {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q lacks %q", err, s)
				}
			}
			if _, again := d.Next(); again != err {
				t.Errorf("error not sticky: got %v then %v", err, again)
			}
		})
	}
}

func TestDecoderPassesReaderErrors(t *testing.T) {
	in := synth(rawFrame{uint32(CmdPacket), 1, []byte("abc")})
	d := newDecoder(t, in[:len(in)-1])
	if _, err := d.Next(); !errors.Is(err, ErrBadDemo) || !strings.Contains(err.Error(), "truncated payload") {
		t.Fatalf("want truncated payload error, got %v", err)
	}
}

func TestFrameUnmarshal(t *testing.T) {
	hdr := &pb.CDemoFileHeader{
		DemoFileStamp:   proto.String("PBDEMS2\x00"),
		PatchVersion:    proto.Int32(14091),
		ServerName:      proto.String("Valve CS2 Test Server"),
		MapName:         proto.String("de_mirage"),
		BuildNum:        proto.Int32(10599),
		ServerStartTick: proto.Int32(-1),
	}
	info := &pb.CDemoFileInfo{
		PlaybackTime:   proto.Float32(2400.5),
		PlaybackTicks:  proto.Int32(153632),
		PlaybackFrames: proto.Int32(76816),
	}
	d := newDecoder(t, synth(
		rawFrame{uint32(CmdFileHeader), 0, marshal(t, hdr)},
		compressed(CmdFileInfo, 153632, marshal(t, info)),
	))
	for _, want := range []proto.Message{hdr, info} {
		f, err := d.Next()
		if err != nil {
			t.Fatal(err)
		}
		got := want.ProtoReflect().New().Interface()
		if err := f.Unmarshal(got); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(got, want) {
			t.Errorf("%s:\n got %v\nwant %v", f.Cmd, got, want)
		}
	}
}

// A FileHeader without its required demo_file_stamp still decodes, because
// the parser doesn't use that field.
func TestFrameUnmarshalMissingRequired(t *testing.T) {
	f := Frame{Cmd: CmdFileHeader, Payload: marshal(t, &pb.CDemoFileHeader{MapName: proto.String("de_nuke")})}
	var h pb.CDemoFileHeader
	if err := f.Unmarshal(&h); err != nil {
		t.Fatal(err)
	}
	if h.GetMapName() != "de_nuke" || h.DemoFileStamp != nil {
		t.Errorf("got %v", &h)
	}
}

func TestFrameUnmarshalBad(t *testing.T) {
	valid := marshal(t, &pb.CDemoFileHeader{MapName: proto.String("de_mirage")})
	tests := map[string][]byte{
		"truncated string":  valid[:len(valid)-1],
		"truncated fixed64": {0x61, 1, 2},
		"bad wire type":     {0x0F},
		"overlong length":   {0x2A, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F},
		"truncated tag":     {0x80},
		"zero field number": {0x00, 0x00},
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			f := Frame{Cmd: CmdFileHeader, Offset: 99, Payload: payload}
			err := f.Unmarshal(new(pb.CDemoFileHeader))
			if !errors.Is(err, ErrBadDemo) {
				t.Fatalf("want ErrBadDemo, got %v", err)
			}
			if want := "offset 99: FileHeader payload"; !strings.Contains(err.Error(), want) {
				t.Errorf("error %q lacks %q", err, want)
			}
		})
	}
}
