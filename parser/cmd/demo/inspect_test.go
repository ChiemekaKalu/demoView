package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
)

// tableRows returns the fields of each output line whose first field is
// accepted by first.
func tableRows(out string, first func(string) bool) [][]string {
	var rows [][]string
	for line := range strings.Lines(out) {
		if f := strings.Fields(line); len(f) > 0 && first(f[0]) {
			rows = append(rows, f)
		}
	}
	return rows
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func TestInspect(t *testing.T) {
	file := writeTemp(t, "x.dem", testDemo)
	code, stdout, stderr := runCLI("inspect", file)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}

	_, signonPad := packetBits(testSignon...)
	_, packetPad := packetBits(testPacket...)
	for _, want := range []string{
		"fileinfo_offset=0",
		"ticks:    first=0 last=6",
		"1 frames after Stop",
		// FileHeader
		"map:      de_mirage\n",
		"server:   Synthetic Test Server\n",
		"client:   SourceTV Demo\n",
		`demo:     stamp="PBDEMS2\x00" version="valve_demo_2"`,
		"network:  protocol=14091 build=10599",
		// FileInfo
		"playback: ticks=96 frames=48 time=1.500s\n",
		fmt.Sprintf("packets:  3 holding 7 messages, max trailing bits %d\n", max(signonPad, packetPad)),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}

	// Frame table: payload bytes are counted decompressed.
	payloads := [][]byte{pbBytes(testHeader), packet(testSignon...), packet(testPacket...), pbBytes(testInfo)}
	var size, largest int
	for _, p := range payloads {
		size += len(p)
		largest = max(largest, len(p))
	}
	total := tableRows(stdout, func(s string) bool { return s == "total" })
	want := []string{"total", "6", "2", strconv.Itoa(size), strconv.Itoa(largest)}
	if len(total) != 1 || !slices.Equal(total[0], want) {
		t.Errorf("total rows %q, want %q", total, want)
	}

	// Message table: id, name, count and body bytes, sorted by id.
	type row struct{ count, bytes int }
	wantRows := map[uint32]*row{}
	for _, m := range slices.Concat(testSignon, testPacket) {
		r := wantRows[m.typ]
		if r == nil {
			r = new(row)
			wantRows[m.typ] = r
		}
		r.count++
		r.bytes += len(m.body)
	}
	names := map[uint32]string{
		4:   "net_Tick",
		40:  "svc_ServerInfo",
		55:  "svc_PacketEntities",
		123: "MsgType(123)",
		205: "GE_Source1LegacyGameEventList",
		207: "GE_Source1LegacyGameEvent",
	}
	var wantMsgs [][]string
	for _, id := range slices.Sorted(maps.Keys(wantRows)) {
		r := wantRows[id]
		wantMsgs = append(wantMsgs, []string{strconv.Itoa(int(id)), names[id], strconv.Itoa(r.count), strconv.Itoa(r.bytes)})
	}
	if got := tableRows(stdout, isNumber); !slices.EqualFunc(got, wantMsgs, slices.Equal) {
		t.Errorf("message rows\n got %q\nwant %q\noutput:\n%s", got, wantMsgs, stdout)
	}
}

func TestInspectNoHeaderOrInfo(t *testing.T) {
	file := writeTemp(t, "x.dem", buildDemo(testFrame{demo.CmdStop, 0, false, nil}))
	code, stdout, stderr := runCLI("inspect", file)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}
	for _, want := range []string{
		"map:      unknown (no FileHeader)",
		"playback: unknown (no FileInfo)",
		"packets:  0 holding 0 messages, max trailing bits 0",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if rows := tableRows(stdout, isNumber); len(rows) != 0 {
		t.Errorf("unexpected message rows %q", rows)
	}
}
