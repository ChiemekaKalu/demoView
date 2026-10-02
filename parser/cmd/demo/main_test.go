package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/parse/parsetest"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// writeTemp writes data to a file in a fresh temp dir and returns its path.
func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runCLI runs the CLI and returns its exit code, stdout and stderr.
func runCLI(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// checkErrLine fails unless stderr is exactly one JSON error line with code.
func checkErrLine(t *testing.T, stderr, code string) {
	t.Helper()
	if strings.Count(stderr, "\n") != 1 || !strings.HasSuffix(stderr, "\n") {
		t.Fatalf("stderr is not one line: %q", stderr)
	}
	dec := json.NewDecoder(strings.NewReader(stderr))
	dec.DisallowUnknownFields()
	var line errorLine
	if err := dec.Decode(&line); err != nil {
		t.Fatalf("stderr %q: %v", stderr, err)
	}
	if line.Code != code || line.Error == "" {
		t.Fatalf("got %+v, want code %q and a message", line, code)
	}
}

// matchDemo is a valid one-round demo that parse can turn into a match.
var matchDemo = parsetest.Match()

func TestParseFlagOrder(t *testing.T) {
	file := writeTemp(t, "x.dem", matchDemo)
	orders := [][]string{
		{"parse", file, "--out", "DIR"},
		{"parse", "--out", "DIR", file},
		{"parse", file, "--out", "DIR", "--events-only"},
		{"parse", "--events-only", file, "-out=DIR"},
		{"parse", "--out=DIR", "--", file},
	}
	for _, args := range orders {
		dir := filepath.Join(t.TempDir(), "out")
		args = slices.Clone(args)
		for i, a := range args {
			args[i] = strings.ReplaceAll(a, "DIR", dir)
		}
		code, _, stderr := runCLI(args...)
		if code != 0 {
			t.Fatalf("%q: exit %d, stderr %s", args, code, stderr)
		}
		if _, err := os.Stat(filepath.Join(dir, "match.json")); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
	}
}

func TestParseWritesMatchJSON(t *testing.T) {
	file := writeTemp(t, "x.dem", matchDemo)
	dir := filepath.Join(t.TempDir(), "nested", "out")
	code, stdout, stderr := runCLI("parse", file, "--out", dir)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, "match.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got match.File
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(matchDemo)
	want := match.Info{Map: "de_mirage", TickRate: 64, DemoSHA256: hex.EncodeToString(sum[:])}
	if got.SchemaVersion != 1 || got.ParserVersion != parse.Version || got.Match != want ||
		len(got.Players) != 2 || len(got.Rounds) != 1 || len(got.Kills) != 1 || len(got.Scoreboard) != 2 {
		t.Fatalf("match.json:\n%s", b)
	}
}

func TestDiff(t *testing.T) {
	file := writeTemp(t, "x.dem", matchDemo)
	dir := filepath.Join(t.TempDir(), "out")
	if code, _, stderr := runCLI("parse", file, "--out", dir); code != 0 {
		t.Fatalf("parse: exit %d, stderr %s", code, stderr)
	}
	ref := filepath.Join(dir, "match.json")

	code, stdout, stderr := runCLI("diff", file, "--against", ref)
	if code != 0 || strings.TrimSpace(stdout) != "OK" || stderr != "" {
		t.Errorf("same output: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	b, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	other := writeTemp(t, "ref.json", bytes.Replace(b, []byte(`"elimination"`), []byte(`"bomb_defused"`), 1))
	code, stdout, stderr = runCLI("diff", file, "--against", other)
	if code != 1 || !strings.Contains(stdout, "bomb_defused") {
		t.Errorf("changed reason: exit %d, stdout %q", code, stdout)
	}
	checkErrLine(t, stderr, "mismatch")

	for _, args := range [][]string{{"diff", file}, {"diff", "--against", ref}} {
		code, _, stderr := runCLI(args...)
		if code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		checkErrLine(t, stderr, "usage")
	}
}

func TestRunErrors(t *testing.T) {
	good := writeTemp(t, "good.dem", testDemo)
	badMagic := writeTemp(t, "magic.dem", []byte("HL2DEMO\x00 not a cs2 demo"))
	truncated := writeTemp(t, "trunc.dem", testDemo[:len(testDemo)-5])
	frames := parsetest.MatchFrames()
	noStop := writeTemp(t, "nostop.dem", parsetest.Build(frames[:len(frames)-1]...))
	missing := filepath.Join(t.TempDir(), "missing.dem")
	out := filepath.Join(t.TempDir(), "out")

	tests := []struct {
		args []string
		exit int
		code string
	}{
		{nil, 2, "usage"},
		{[]string{"bogus"}, 2, "usage"},
		{[]string{"parse", good}, 2, "usage"},
		{[]string{"parse", "--out", out}, 2, "usage"},
		{[]string{"parse", good, good, "--out", out}, 2, "usage"},
		{[]string{"parse", good, "--bogus", "--out", out}, 2, "usage"},
		{[]string{"parse", good, "--out"}, 2, "usage"},
		{[]string{"inspect"}, 2, "usage"},
		{[]string{"parse", badMagic, "--out", out}, 1, "bad_demo"},
		{[]string{"parse", truncated, "--out", out}, 1, "bad_demo"},
		{[]string{"parse", noStop, "--out", out}, 1, "bad_demo"},
		{[]string{"inspect", truncated}, 1, "bad_demo"},
		{[]string{"parse", missing, "--out", out}, 3, "internal"},
	}
	for _, tc := range tests {
		code, stdout, stderr := runCLI(tc.args...)
		if code != tc.exit {
			t.Errorf("%q: exit %d, want %d (stderr %s)", tc.args, code, tc.exit, stderr)
			continue
		}
		if stdout != "" {
			t.Errorf("%q: unexpected stdout %q", tc.args, stdout)
		}
		checkErrLine(t, stderr, tc.code)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("failed parses created %s", out)
	}
}

// TestRunUndecodable covers demos whose frames are intact but whose payloads
// or packet messages don't decode.
func TestRunUndecodable(t *testing.T) {
	overrun, _ := packetBits(testMsg{55, make([]byte, 10)})
	excess, _ := packetBits(testMsg{4, nil})
	tests := []struct {
		name string
		demo []byte
		want string
	}{
		{"snappy", append(buildDemo(), 0x47, 0x01, 0x01, 'z'), "Packet payload: snappy"}, // compressed Packet holding "z"
		{"header proto", buildDemo(testFrame{demo.CmdFileHeader, 0, true, []byte{0x2A, 0x09, 'd', 'e'}}), "FileHeader payload"},
		{"packet proto", buildDemo(testFrame{demo.CmdPacket, 1, false, []byte{0x1A, 0x05, 'a'}}), "Packet payload"},
		{"message size", buildDemo(testFrame{demo.CmdSignonPacket, -1, true, pbBytes(&pb.CDemoPacket{Data: overrun[:5]})}), "body overruns the packet"},
		{"trailing bits", buildDemo(testFrame{demo.CmdPacket, 1, false, pbBytes(&pb.CDemoPacket{Data: append(excess, 0)})}), "bad header with 10 bits left"},
	}
	for _, tc := range tests {
		file := writeTemp(t, "bad.dem", tc.demo)
		out := filepath.Join(t.TempDir(), "out")
		for _, args := range [][]string{{"parse", file, "--out", out}, {"inspect", file}} {
			code, stdout, stderr := runCLI(args...)
			if code != 1 || stdout != "" {
				t.Errorf("%s: %s: exit %d, stdout %q, stderr %s", tc.name, args[0], code, stdout, stderr)
				continue
			}
			checkErrLine(t, stderr, "bad_demo")
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("%s: %s: stderr %s lacks %q", tc.name, args[0], stderr, tc.want)
			}
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("%s: failed parse created %s", tc.name, out)
		}
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"parse", "-h"}, {"inspect", "--help"}} {
		code, stdout, stderr := runCLI(args...)
		if code != 0 || stderr != "" || !strings.Contains(stdout, "usage:") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestParseArgs(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	out := fs.String("out", "", "")
	v := fs.Bool("v", false, "")
	pos, err := parseArgs(fs, "t", []string{"a", "--out", "d", "b", "-v", "--", "-x", "--out"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b", "-x", "--out"}; !slices.Equal(pos, want) {
		t.Errorf("positionals %q, want %q", pos, want)
	}
	if *out != "d" || !*v {
		t.Errorf("out=%q v=%v", *out, *v)
	}
}
