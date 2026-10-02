package demo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
)

const (
	testFileInfoOffset    = 0x1234
	testSpawnGroupsOffset = 0x5678
)

// rawFrame is a frame as stored on disk. cmd includes the compression flag.
type rawFrame struct {
	cmd     uint32
	tick    uint32
	payload []byte
}

func (f rawFrame) appendTo(b []byte) []byte {
	b = binary.AppendUvarint(b, uint64(f.cmd))
	b = binary.AppendUvarint(b, uint64(f.tick))
	b = binary.AppendUvarint(b, uint64(len(f.payload)))
	return append(b, f.payload...)
}

// synth builds an in-memory demo: the preamble followed by frames.
func synth(frames ...rawFrame) []byte {
	b := append([]byte(nil), Magic[:]...)
	b = binary.LittleEndian.AppendUint32(b, testFileInfoOffset)
	b = binary.LittleEndian.AppendUint32(b, testSpawnGroupsOffset)
	for _, f := range frames {
		b = f.appendTo(b)
	}
	return b
}

func TestReaderFrames(t *testing.T) {
	raw := []rawFrame{
		{uint32(CmdFileHeader), 0, []byte("header")},
		{uint32(CmdSignonPacket) | compressedFlag, 0xFFFFFFFF, []byte("signon")},
		{uint32(CmdPacket) | compressedFlag, 300, bytes.Repeat([]byte{0xAB}, 200)},
		{uint32(CmdPacket), 1<<31 + 5, nil},
		{uint32(CmdStop), 301, nil},
		{uint32(CmdFileInfo), 301, []byte("after stop")},
		{30, 302, []byte{1}},
	}
	want := []Frame{
		{Cmd: CmdFileHeader, Tick: 0, Payload: []byte("header")},
		{Cmd: CmdSignonPacket, Compressed: true, Tick: -1, Payload: []byte("signon")},
		{Cmd: CmdPacket, Compressed: true, Tick: 300, Payload: bytes.Repeat([]byte{0xAB}, 200)},
		{Cmd: CmdPacket, Tick: -1<<31 + 5},
		{Cmd: CmdStop, Tick: 301},
		{Cmd: CmdFileInfo, Tick: 301, Payload: []byte("after stop")},
		{Cmd: 30, Tick: 302, Payload: []byte{1}},
	}
	off := int64(headerSize)
	for i, f := range raw {
		want[i].Offset = off
		off += int64(len(f.appendTo(nil)))
	}
	in := synth(raw...)

	readers := map[string]func(io.Reader) io.Reader{
		"plain":   func(r io.Reader) io.Reader { return r },
		"onebyte": iotest.OneByteReader,
		"half":    iotest.HalfReader,
		"dataerr": iotest.DataErrReader,
	}
	for name, wrap := range readers {
		t.Run(name, func(t *testing.T) {
			r, err := NewReader(wrap(bytes.NewReader(in)))
			if err != nil {
				t.Fatal(err)
			}
			if h := r.Header(); h.FileInfoOffset != testFileInfoOffset || h.SpawnGroupsOffset != testSpawnGroupsOffset {
				t.Fatalf("header %+v", h)
			}
			for i, w := range want {
				got, err := r.Next()
				if err != nil {
					t.Fatalf("frame %d: %v", i, err)
				}
				if got.Cmd != w.Cmd || got.Compressed != w.Compressed || got.Tick != w.Tick ||
					got.Offset != w.Offset || !bytes.Equal(got.Payload, w.Payload) {
					t.Fatalf("frame %d:\n got %+v\nwant %+v", i, got, w)
				}
			}
			for range 2 {
				if _, err := r.Next(); err != io.EOF {
					t.Fatalf("want io.EOF after last frame, got %v", err)
				}
			}
		})
	}
}

func TestReaderNoFrames(t *testing.T) {
	r, err := NewReader(bytes.NewReader(synth()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("want io.EOF, got %v", err)
	}
}

func TestReaderBadFrames(t *testing.T) {
	good := synth(rawFrame{uint32(CmdFileHeader), 0, []byte("abc")})
	badOffset := len(good)
	tests := []struct {
		name string
		tail []byte
		want string
	}{
		{"truncated payload", []byte{7, 1, 10, 'x', 'y'}, "truncated payload (2 of 10 bytes)"},
		{"payload missing", []byte{7, 1, 1}, "truncated payload (0 of 1 bytes)"},
		{"truncated command varint", []byte{0x87}, "truncated command"},
		{"missing tick", []byte{7}, "truncated tick"},
		{"truncated tick varint", []byte{7, 0xff, 0xff}, "truncated tick"},
		{"missing size", []byte{7, 1}, "truncated size"},
		{"truncated size varint", []byte{7, 1, 0x80, 0x80}, "truncated size"},
		{"long command varint", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x00, 1, 0}, "command varint longer than 5 bytes"},
		{"long tick varint", []byte{7, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01, 0}, "tick varint longer than 5 bytes"},
		{"oversized payload", binary.AppendUvarint([]byte{7, 1}, MaxPayloadSize+1), "exceeds limit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewReader(bytes.NewReader(slices.Concat(good, tc.tail)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.Next(); err != nil {
				t.Fatalf("good frame: %v", err)
			}
			_, err = r.Next()
			if !errors.Is(err, ErrBadDemo) {
				t.Fatalf("want ErrBadDemo, got %v", err)
			}
			if at := fmt.Sprintf("offset %d:", badOffset); !strings.Contains(err.Error(), at) {
				t.Errorf("error %q lacks %q", err, at)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q lacks %q", err, tc.want)
			}
			if _, again := r.Next(); again != err {
				t.Errorf("error not sticky: got %v then %v", err, again)
			}
		})
	}
}

func TestNewReaderBadInput(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"short", Magic[:5]},
		{"bad magic", []byte("HL2DEMO\x00\x00\x00\x00\x00\x00\x00\x00\x00")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewReader(bytes.NewReader(tc.in)); !errors.Is(err, ErrBadDemo) {
				t.Fatalf("want ErrBadDemo, got %v", err)
			}
		})
	}
}

func TestReaderIOError(t *testing.T) {
	errDisk := errors.New("disk on fire")
	in := synth(rawFrame{uint32(CmdPacket), 5, []byte("payload")})
	payloadAt := len(in) - len("payload")
	tests := []struct {
		name   string
		prefix []byte // bytes served before the error
		err    error
	}{
		{"in header", Magic[:4], errDisk},
		{"between frames", in, errDisk},
		{"in varint", in[:headerSize+1], errDisk},
		{"in payload", in[:payloadAt+3], errDisk},
		// io.ErrUnexpectedEOF from the source (a dropped download, say) is an
		// I/O failure, not a truncated demo.
		{"source unexpected EOF", in[:payloadAt+3], io.ErrUnexpectedEOF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := io.MultiReader(bytes.NewReader(tc.prefix), iotest.ErrReader(tc.err))
			r, err := NewReader(src)
			for err == nil {
				_, err = r.Next()
			}
			if !errors.Is(err, tc.err) {
				t.Fatalf("want %v, got %v", tc.err, err)
			}
			if errors.Is(err, ErrBadDemo) || err == io.EOF {
				t.Fatalf("I/O error misclassified: %v", err)
			}
		})
	}
}

func TestCommandString(t *testing.T) {
	tests := map[Command]string{
		CmdStop:            "Stop",
		CmdPacket:          "Packet",
		CmdSignonPacket:    "SignonPacket",
		CmdAnimationHeader: "AnimationHeader",
		CmdRecovery:        "Recovery",
		19:                 "Command(19)",
		1 << 31:            "Command(2147483648)",
	}
	for c, want := range tests {
		if got := c.String(); got != want {
			t.Errorf("Command(%d).String() = %q, want %q", uint32(c), got, want)
		}
	}
}
