package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
)

// testDemo is a small valid demo: the preamble and four frames.
var testDemo = slices.Concat(demo.Magic[:], []byte{
	0, 0, 0, 0, 0, 0, 0, 0, // offsets
	0x01, 0x00, 0x03, 'a', 'b', 'c', // FileHeader, tick 0
	0x48, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x01, 'z', // compressed SignonPacket, tick -1
	0x07, 0x05, 0x00, // Packet, tick 5, empty
	0x00, 0x06, 0x00, // Stop, tick 6
})

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

func TestParseFlagOrder(t *testing.T) {
	file := writeTemp(t, "x.dem", testDemo)
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
	file := writeTemp(t, "x.dem", testDemo)
	dir := filepath.Join(t.TempDir(), "nested", "out")
	code, stdout, stderr := runCLI("parse", file, "--out", dir)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, "match.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(testDemo)
	want := map[string]any{
		"schema_version": 1.0,
		"parser_version": "0.1.0",
		"match": map[string]any{
			"map":         "",
			"tick_rate":   0.0,
			"demo_sha256": hex.EncodeToString(sum[:]),
		},
		"players":    []any{},
		"rounds":     []any{},
		"kills":      []any{},
		"scoreboard": []any{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("match.json:\n%s", b)
	}
}

func TestRunErrors(t *testing.T) {
	good := writeTemp(t, "good.dem", testDemo)
	badMagic := writeTemp(t, "magic.dem", []byte("HL2DEMO\x00 not a cs2 demo"))
	truncated := writeTemp(t, "trunc.dem", testDemo[:len(testDemo)-5])
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

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"parse", "-h"}, {"inspect", "--help"}} {
		code, stdout, stderr := runCLI(args...)
		if code != 0 || stderr != "" || !strings.Contains(stdout, "usage:") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestInspect(t *testing.T) {
	file := writeTemp(t, "x.dem", testDemo)
	code, stdout, stderr := runCLI("inspect", file)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}
	for _, want := range []string{
		"fileinfo_offset=0",
		"ticks:    first=0 last=6",
		"0 frames after Stop",
		"FileHeader",
		"SignonPacket",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	var total []string
	for line := range strings.Lines(stdout) {
		if f := strings.Fields(line); len(f) > 0 && f[0] == "total" {
			total = f
		}
	}
	if want := []string{"total", "4", "1", "4"}; !slices.Equal(total, want) {
		t.Errorf("total row %q, want %q", total, want)
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
