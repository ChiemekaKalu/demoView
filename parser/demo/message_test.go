package demo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// bitWriter builds test bit streams one bit at a time, least significant bit
// first. It mirrors the one in package bitread's tests.
type bitWriter struct {
	buf []byte
	n   int // bits written
}

func (w *bitWriter) bit(b bool) {
	if w.n%8 == 0 {
		w.buf = append(w.buf, 0)
	}
	if b {
		w.buf[w.n/8] |= 1 << (w.n % 8)
	}
	w.n++
}

// bits writes the low n bits of v, least significant first.
func (w *bitWriter) bits(v uint64, n int) {
	for i := range n {
		w.bit(v>>i&1 != 0)
	}
}

func (w *bitWriter) bytes(p []byte) {
	for _, c := range p {
		w.bits(uint64(c), 8)
	}
}

// ubitvar writes v in the shortest of Valve's UBitVar forms: a 6-bit prefix
// whose top two bits select 0, 4, 8 or 28 more bits.
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

// message writes one packet message: type, size and body.
func (w *bitWriter) message(typ uint32, body []byte) {
	w.ubitvar(typ)
	w.varuint(uint64(len(body)))
	w.bytes(body)
}

// packetFrame wraps a message bit stream in a frame of the given command.
func packetFrame(t testing.TB, cmd Command, data []byte) Frame {
	t.Helper()
	var m proto.Message = &pb.CDemoPacket{Data: data}
	if cmd == CmdFullPacket {
		m = &pb.CDemoFullPacket{
			StringTable: &pb.CDemoStringTables{},
			Packet:      &pb.CDemoPacket{Data: data},
		}
	}
	return Frame{Cmd: cmd, Offset: 1234, Payload: marshal(t, m)}
}

// readAll reads every message in f and returns copies of them.
func readAll(t testing.TB, mr *MessageReader, f Frame) ([]Message, error) {
	t.Helper()
	if err := mr.Reset(f); err != nil {
		return nil, err
	}
	var msgs []Message
	for {
		m, err := mr.Next()
		if err == io.EOF {
			return msgs, nil
		}
		if err != nil {
			return msgs, err
		}
		msgs = append(msgs, Message{m.Type, bytes.Clone(m.Body)})
	}
}

func TestMessageReader(t *testing.T) {
	// Types cover all four UBitVar forms, and sizes cover one- and two-byte
	// varints, so most bodies start off a byte boundary.
	want := []Message{
		{4, []byte{0x08, 0x01}},
		{55, bytes.Repeat([]byte{0xA5, 0x5A, 0xFF}, 100)},
		{207, nil},
		{40, []byte("server info")},
		{3000, []byte{0}},
		{1 << 20, []byte("form 3")},
		{0, nil},
	}
	var w bitWriter
	for _, m := range want {
		w.message(uint32(m.Type), m.Body)
	}
	pad := len(w.buf)*8 - w.n

	var mr MessageReader
	for _, cmd := range []Command{CmdPacket, CmdSignonPacket, CmdFullPacket} {
		t.Run(cmd.String(), func(t *testing.T) {
			got, err := readAll(t, &mr, packetFrame(t, cmd, w.buf))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Fatalf("got %d messages, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i].Type != want[i].Type || !bytes.Equal(got[i].Body, want[i].Body) {
					t.Errorf("message %d: got %v %x, want %v %x", i, got[i].Type, got[i].Body, want[i].Type, want[i].Body)
				}
			}
			if mr.TrailingBits() != pad {
				t.Errorf("TrailingBits = %d, want %d", mr.TrailingBits(), pad)
			}
			if _, err := mr.Next(); err != io.EOF {
				t.Errorf("want sticky io.EOF, got %v", err)
			}
		})
	}
}

func TestMessageReaderEmpty(t *testing.T) {
	var mr MessageReader
	msgs, err := readAll(t, &mr, packetFrame(t, CmdPacket, nil))
	if err != nil || len(msgs) != 0 || mr.TrailingBits() != 0 {
		t.Errorf("got %v, %v, %d trailing bits; want nothing", msgs, err, mr.TrailingBits())
	}
}

// A message header is 14, 18, 22 or 42 bits and its body whole bytes, so a
// packet ends with 0, 2, 4 or 6 bits of padding.
func TestMessageReaderPadding(t *testing.T) {
	for _, tc := range []struct {
		types []uint32
		pad   int
	}{
		{[]uint32{4}, 2},             // 14 bits
		{[]uint32{40}, 6},            // 18 bits
		{[]uint32{4, 4}, 4},          // 28 bits
		{[]uint32{40, 40}, 4},        // 36 bits
		{[]uint32{4, 4, 4, 4}, 0},    // 56 bits
		{[]uint32{1 << 20, 4}, 0},    // 42+14 bits
		{[]uint32{40, 40, 4, 4}, 0},  // 36+28 bits
		{[]uint32{4, 40, 40, 40}, 4}, // 14+54 bits
		{[]uint32{3000, 3000}, 4},    // 44 bits
	} {
		var w bitWriter
		for _, typ := range tc.types {
			w.message(typ, []byte{1, 2, 3})
		}
		var mr MessageReader
		msgs, err := readAll(t, &mr, packetFrame(t, CmdPacket, w.buf))
		if err != nil || len(msgs) != len(tc.types) {
			t.Errorf("types %v: got %d messages, %v", tc.types, len(msgs), err)
			continue
		}
		if mr.TrailingBits() != tc.pad {
			t.Errorf("types %v: TrailingBits = %d, want %d", tc.types, mr.TrailingBits(), tc.pad)
		}
	}
}

// Two zero bytes after the padding form a 14-bit net_NOP message with an
// empty body, so they read as one rather than as excess padding.
func TestMessageReaderZeroTail(t *testing.T) {
	var w bitWriter
	w.message(4, nil)
	w.bytes([]byte{0, 0})
	var mr MessageReader
	msgs, err := readAll(t, &mr, packetFrame(t, CmdPacket, w.buf))
	if err != nil || len(msgs) != 2 || msgs[0].Type != 4 || msgs[1].Type != 0 || len(msgs[1].Body) != 0 {
		t.Fatalf("got %v, %v; want net_Tick then an empty net_NOP", msgs, err)
	}
	if mr.TrailingBits() != 4 {
		t.Errorf("TrailingBits = %d, want 4", mr.TrailingBits())
	}
}

func TestMessageReaderBad(t *testing.T) {
	// one is a valid 14-bit message (type 4, empty body) plus 2 bits of
	// padding.
	var one bitWriter
	one.message(4, nil)

	var truncSize bitWriter
	truncSize.ubitvar(4)
	truncSize.bits(0x80, 8) // a varint byte that promises another

	var truncBody bitWriter
	truncBody.message(4, nil)
	truncBody.ubitvar(55)
	truncBody.varuint(10)
	truncBody.bytes([]byte("abc"))

	var longVarint bitWriter
	longVarint.ubitvar(4)
	longVarint.bytes([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01})

	var twelve bitWriter // two 14-bit messages, then 12 bits of zeros
	twelve.message(4, nil)
	twelve.message(4, nil)
	twelve.bits(0, 12)

	var hugeSize bitWriter
	hugeSize.ubitvar(55)
	hugeSize.varuint(1<<32 - 1)
	hugeSize.bytes(make([]byte, 16))

	tests := []struct {
		name, data, want string
	}{
		{"truncated body", string(truncBody.buf), "message 1 at bit 14: type 55: 10-byte body overruns"},
		{"huge size", string(hugeSize.buf), "message 0 at bit 0: type 55: 4294967295-byte body overruns"},
		{"truncated type", "\xff", "message 0 at bit 0: bad header with 8 bits left"},
		{"truncated size", string(truncSize.buf), "message 0 at bit 0: bad header with 16 bits left"},
		{"overlong size varint", string(longVarint.buf), "message 0 at bit 0: bad header"},
		{"8 bits and no message", "\x00", "message 0 at bit 0: bad header with 8 bits left"},
		{"10 bits after the last message", string(one.buf) + "\x00", "message 1 at bit 14: bad header with 10 bits left"},
		{"12 bits after the last message", string(twelve.buf), "message 2 at bit 28: bad header with 12 bits left"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mr MessageReader
			_, err := readAll(t, &mr, packetFrame(t, CmdSignonPacket, []byte(tc.data)))
			if !errors.Is(err, ErrBadDemo) {
				t.Fatalf("want ErrBadDemo, got %v", err)
			}
			want := "offset 1234: SignonPacket " + tc.want
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q lacks %q", err, want)
			}
			if _, again := mr.Next(); again != err {
				t.Errorf("error not sticky: got %v then %v", err, again)
			}
			// Reset clears the error.
			if msgs, err := readAll(t, &mr, packetFrame(t, CmdPacket, one.buf)); err != nil || len(msgs) != 1 {
				t.Errorf("after Reset: %v, %v", msgs, err)
			}
		})
	}
}

func TestMessageReaderBadPayload(t *testing.T) {
	var mr MessageReader
	for _, cmd := range []Command{CmdPacket, CmdFullPacket} {
		err := mr.Reset(Frame{Cmd: cmd, Offset: 7, Payload: []byte{0x1A, 0x05, 'a'}})
		if !errors.Is(err, ErrBadDemo) || !strings.Contains(err.Error(), fmt.Sprintf("offset 7: %s payload", cmd)) {
			t.Errorf("%s: want ErrBadDemo, got %v", cmd, err)
		}
		if _, again := mr.Next(); again != err {
			t.Errorf("%s: Next after failed Reset: got %v, want %v", cmd, again, err)
		}
	}
}

func TestMessageReaderNotPacket(t *testing.T) {
	var mr MessageReader
	err := mr.Reset(Frame{Cmd: CmdFileHeader})
	if err == nil || errors.Is(err, ErrBadDemo) {
		t.Fatalf("want a non-ErrBadDemo error, got %v", err)
	}
}

func TestMessageReaderAllocs(t *testing.T) {
	var w bitWriter
	for i := range 300 {
		w.message(uint32(i%60), bytes.Repeat([]byte{byte(i)}, i%50))
	}
	var mr MessageReader
	if err := mr.Reset(packetFrame(t, CmdPacket, w.buf)); err != nil {
		t.Fatal(err)
	}
	next := func() {
		if _, err := mr.Next(); err != nil {
			t.Fatal(err)
		}
	}
	for range 60 { // grow the body buffer to the largest size
		next()
	}
	if n := testing.AllocsPerRun(100, next); n != 0 {
		t.Errorf("Next allocates %v times per message, want 0", n)
	}
}

// FuzzMessageReader checks that any packet data either reads to io.EOF with
// at most 7 bits left or fails with ErrBadDemo, without panicking.
func FuzzMessageReader(f *testing.F) {
	var w bitWriter
	w.message(4, []byte{8, 1})
	w.message(55, bytes.Repeat([]byte{0xAB}, 40))
	w.message(1<<20, nil)
	f.Add(w.buf)
	f.Add([]byte{})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	f.Fuzz(func(t *testing.T, data []byte) {
		var mr MessageReader
		if err := mr.Reset(packetFrame(t, CmdPacket, data)); err != nil {
			t.Fatal(err)
		}
		for n := 0; ; n++ {
			m, err := mr.Next()
			if err == io.EOF {
				if mr.TrailingBits() > maxPadBits {
					t.Fatalf("io.EOF with %d bits left", mr.TrailingBits())
				}
				return
			}
			if err != nil {
				if !errors.Is(err, ErrBadDemo) {
					t.Fatalf("error %v is not ErrBadDemo", err)
				}
				return
			}
			if n > len(data) || len(m.Body) > len(data) {
				t.Fatalf("message %d with %d-byte body from %d bytes of data", n, len(m.Body), len(data))
			}
		}
	})
}

func TestMsgTypeString(t *testing.T) {
	tests := map[MsgType]string{
		4:         "net_Tick",
		16:        "bi_RebroadcastGameEvent",
		40:        "svc_ServerInfo",
		55:        "svc_PacketEntities",
		205:       "GE_Source1LegacyGameEventList",
		207:       "GE_Source1LegacyGameEvent",
		2:         "MsgType(2)",
		123:       "MsgType(123)",
		1<<32 - 1: "MsgType(4294967295)",
	}
	for typ, want := range tests {
		if got := typ.String(); got != want {
			t.Errorf("MsgType(%d).String() = %q, want %q", uint32(typ), got, want)
		}
	}
}

func TestMsgTypeEnumsDisjoint(t *testing.T) {
	seen := map[int32]string{}
	for _, names := range msgTypeNames {
		for id, name := range names {
			if prev, ok := seen[id]; ok {
				t.Errorf("id %d is both %s and %s", id, prev, name)
			}
			seen[id] = name
		}
	}
}
