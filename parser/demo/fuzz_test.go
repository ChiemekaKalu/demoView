package demo_test

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/parse/parsetest"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

var errIO = errors.New("injected I/O error")

// FuzzFrames runs the frame loop over arbitrary input: Reader, Decoder with
// snappy, and MessageReader on every packet frame. If ioErrAt is in range,
// the input fails with errIO after that many bytes. The loop must end in
// io.EOF or in an error that wraps exactly one of ErrBadDemo and errIO, and
// must never panic. The seeds are parsetest's synthetic demos, plain and
// compressed. The spec's exit check runs it for 10 minutes:
//
//	go test ./demo -run '^$' -fuzz '^FuzzFrames$' -fuzztime 10m
func FuzzFrames(f *testing.F) {
	frames := parsetest.MatchFrames()
	var compressed []parsetest.Frame
	for _, fr := range frames {
		fr.Compress = true
		compressed = append(compressed, fr)
	}
	bits, _ := parsetest.PacketBits(parsetest.EventList())
	full := parsetest.Frame{Cmd: demo.CmdFullPacket, Tick: 1, Compress: true, Payload: parsetest.Marshal(&pb.CDemoFullPacket{
		StringTable: &pb.CDemoStringTables{},
		Packet:      &pb.CDemoPacket{Data: bits},
	})}
	match := parsetest.Match()
	f.Add([]byte{}, -1)
	f.Add(parsetest.Build(), -1)
	f.Add(match, -1)
	f.Add(match, len(match)/2)
	f.Add(parsetest.Build(compressed...), -1)
	f.Add(parsetest.Build(full, parsetest.Frame{Cmd: demo.CmdStop}), -1)
	f.Fuzz(func(t *testing.T, data []byte, ioErrAt int) {
		var in io.Reader = bytes.NewReader(data)
		if ioErrAt >= 0 && ioErrAt < len(data) {
			in = io.MultiReader(bytes.NewReader(data[:ioErrAt]), iotest.ErrReader(errIO))
		}
		r, err := demo.NewReader(in)
		if err != nil {
			checkErr(t, "NewReader", err)
			return
		}
		d := demo.NewDecoder(r)
		var mr demo.MessageReader
		last := int64(-1)
		for {
			fr, err := d.Next()
			if err == io.EOF {
				return
			}
			if err != nil {
				checkErr(t, "Next", err)
				if fr, again := d.Next(); again != err || fr.Payload != nil {
					t.Fatalf("Next after %v: %v, want the same error and no frame", err, again)
				}
				return
			}
			if fr.Offset <= last || fr.Offset >= int64(len(data)) || len(fr.Payload) > demo.MaxPayloadSize {
				t.Fatalf("frame at offset %d after one at %d, with a %d-byte payload, from %d bytes", fr.Offset, last, len(fr.Payload), len(data))
			}
			last = fr.Offset
			switch fr.Cmd {
			case demo.CmdPacket, demo.CmdSignonPacket, demo.CmdFullPacket:
				readMessages(t, &mr, fr)
			}
		}
	})
}

// readMessages reads every message in the packet frame f.
func readMessages(t *testing.T, mr *demo.MessageReader, f demo.Frame) {
	t.Helper()
	if err := mr.Reset(f); err != nil {
		checkErr(t, "Reset", err)
		return
	}
	for {
		_, err := mr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			checkErr(t, "MessageReader.Next", err)
			return
		}
	}
}

// checkErr fails unless err is a bad demo or an I/O error, and not both.
func checkErr(t *testing.T, op string, err error) {
	t.Helper()
	if errors.Is(err, demo.ErrBadDemo) == errors.Is(err, errIO) {
		t.Fatalf("%s: %v, want an error wrapping either ErrBadDemo or the reader's error", op, err)
	}
}
