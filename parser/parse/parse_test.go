package parse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse/parsetest"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

var (
	header = parsetest.Marshal(&pb.CDemoFileHeader{
		DemoFileStamp: proto.String("PBDEMS2\x00"),
		MapName:       proto.String("de_mirage"),
	})
	serverInfo = parsetest.Msg{
		Type: uint32(pb.SVC_Messages_svc_ServerInfo),
		Body: parsetest.Marshal(&pb.CSVCMsg_ServerInfo{TickInterval: proto.Float32(1.0 / 64)}),
	}
	tick = parsetest.Msg{
		Type: uint32(pb.NET_Messages_net_Tick),
		Body: parsetest.Marshal(&pb.CNETMsg_Tick{Tick: proto.Uint32(5)}),
	}
)

// goodDemo has a FileHeader, a compressed SignonPacket with ServerInfo, a
// Packet, Stop and a FileInfo.
var goodDemo = parsetest.Build(
	parsetest.Frame{Cmd: demo.CmdFileHeader, Payload: header},
	parsetest.Frame{Cmd: demo.CmdSignonPacket, Tick: -1, Compress: true, Payload: parsetest.Packet(serverInfo, tick)},
	parsetest.Frame{Cmd: demo.CmdPacket, Tick: 5, Payload: parsetest.Packet(tick)},
	parsetest.Frame{Cmd: demo.CmdStop, Tick: 6},
	parsetest.Frame{Cmd: demo.CmdFileInfo, Tick: 6, Payload: parsetest.Marshal(&pb.CDemoFileInfo{PlaybackTicks: proto.Int32(6)})},
)

func TestRun(t *testing.T) {
	got, err := Run(context.Background(), bytes.NewReader(goodDemo))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(goodDemo)
	want := match.File{
		SchemaVersion: match.SchemaVersion,
		ParserVersion: Version,
		Match:         match.Info{Map: "de_mirage", TickRate: 64, DemoSHA256: hex.EncodeToString(sum[:])},
	}
	if got.SchemaVersion != want.SchemaVersion || got.ParserVersion != want.ParserVersion || got.Match != want.Match {
		t.Errorf("Run = %+v, want %+v", got, want)
	}
}

func TestRunWithoutHeaderOrServerInfo(t *testing.T) {
	got, err := Run(context.Background(), bytes.NewReader(parsetest.Build(
		parsetest.Frame{Cmd: demo.CmdPacket, Tick: 1, Payload: parsetest.Packet(tick)},
	)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Match.Map != "" || got.Match.TickRate != 0 {
		t.Errorf("Match = %+v, want no map or tick rate", got.Match)
	}
}

func TestRunBadDemo(t *testing.T) {
	badInfo := parsetest.Msg{Type: serverInfo.Type, Body: []byte{0x6A, 0x09, 'x'}} // field 13 length-delimited, truncated
	tests := map[string][]byte{
		"bad magic":        []byte("HL2DEMO\x00 not a cs2 demo"),
		"short preamble":   []byte("PBDEMS2"),
		"truncated frame":  goodDemo[:len(goodDemo)-3],
		"bad header proto": parsetest.Build(parsetest.Frame{Cmd: demo.CmdFileHeader, Payload: []byte{0x2A, 0x09, 'd'}}),
		"bad server info":  parsetest.Build(parsetest.Frame{Cmd: demo.CmdSignonPacket, Payload: parsetest.Packet(badInfo)}),
	}
	for name, data := range tests {
		_, err := Run(context.Background(), bytes.NewReader(data))
		if !errors.Is(err, demo.ErrBadDemo) {
			t.Errorf("%s: error = %v, want ErrBadDemo", name, err)
		}
	}
}

// failingReader returns its data, then err instead of io.EOF.
type failingReader struct {
	r   io.Reader
	err error
}

func (f *failingReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF {
		return n, f.err
	}
	return n, err
}

func TestRunReaderErrorIsNotBadDemo(t *testing.T) {
	errNet := errors.New("connection reset")
	_, err := Run(context.Background(), &failingReader{bytes.NewReader(goodDemo[:40]), errNet})
	if !errors.Is(err, errNet) || errors.Is(err, demo.ErrBadDemo) {
		t.Errorf("error = %v, want the reader's error and not ErrBadDemo", err)
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, bytes.NewReader(goodDemo)); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// TestRealDemos parses whatever demos are in testdata/demos. They are not in
// git, so it skips when there are none.
func TestRealDemos(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	paths, _ := filepath.Glob(filepath.Join("..", "testdata", "demos", "*.dem"))
	if len(paths) == 0 {
		t.Skip("no demos in testdata/demos")
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			f, err := os.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			start := time.Now()
			got, err := Run(context.Background(), f)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("map=%s tick_rate=%d sha256=%s in %v", got.Match.Map, got.Match.TickRate, got.Match.DemoSHA256, time.Since(start))
			if !strings.HasPrefix(got.Match.Map, "de_") || got.Match.TickRate != 64 {
				t.Errorf("Match = %+v, want a de_ map at 64 ticks", got.Match)
			}
		})
	}
}
