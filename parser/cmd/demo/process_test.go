package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The spec's budget for parsing the largest fixture on one CPU.
const (
	budgetTime   = 60 * time.Second
	budgetMemory = 512 << 20
)

// TestRunTwiceIdentical parses every local demo twice, each time in a new
// process, and checks that the two match.json files are byte-identical.
func TestRunTwiceIdentical(t *testing.T) {
	demos := localDemos(t)
	exe := buildCLI(t)
	for _, path := range demos {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".dem"), func(t *testing.T) {
			t.Parallel()
			first, second := parseInChild(t, exe, path), parseInChild(t, exe, path)
			if i := firstDiff(first, second); i >= 0 {
				t.Errorf("match.json differs between runs at byte %d of %d and %d:\n%q\n%q",
					i, len(first), len(second), around(first, i), around(second, i))
			}
		})
	}
}

// TestBudget parses the largest local demo in a child process with
// GOMAXPROCS=1, and checks that it takes under 60 s and under 512 MB of
// peak resident memory. Where runPeak can't measure the peak, it checks the
// time only.
func TestBudget(t *testing.T) {
	demos := localDemos(t)
	var largest string
	var size int64
	for _, p := range demos {
		if fi, err := os.Stat(p); err == nil && fi.Size() > size {
			largest, size = p, fi.Size()
		}
	}
	exe := buildCLI(t)

	cmd := exec.Command(exe, "parse", largest, "--out", t.TempDir())
	cmd.Env = append(os.Environ(), "GOMAXPROCS=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	peak, err := runPeak(cmd)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, stderr.Bytes())
	}

	t.Logf("%.1f MiB demo at GOMAXPROCS=1: %v wall, peak RSS %.1f MiB", float64(size)/(1<<20), elapsed.Round(time.Millisecond), float64(peak)/(1<<20))
	if elapsed >= budgetTime {
		t.Errorf("parse took %v, want under %v", elapsed, budgetTime)
	}
	switch {
	case peak < 0:
		t.Logf("peak RSS is not measurable on %s", runtime.GOOS)
	case peak >= budgetMemory:
		t.Errorf("peak RSS %d bytes, want under %d", peak, budgetMemory)
	}
}

// localDemos returns the demos in testdata/demos. They are not in git, so
// it skips the test when there are none, and under -short.
func localDemos(t *testing.T) []string {
	t.Helper()
	if testing.Short() {
		t.Skip("slow")
	}
	paths, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "demos", "*.dem"))
	if len(paths) == 0 {
		t.Skip("no demos in testdata/demos")
	}
	return paths
}

// buildCLI builds this command and returns the path of the binary.
func buildCLI(t *testing.T) string {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	exe := filepath.Join(t.TempDir(), "demo")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command(gobin, "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return exe
}

// parseInChild runs "demo parse" on path in a new process and returns the
// match.json it writes.
func parseInChild(t *testing.T, exe, path string) []byte {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command(exe, "parse", path, "--out", dir).CombinedOutput(); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "match.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// firstDiff returns the index of the first byte where a and b differ, or -1
// if they are equal.
func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return min(len(a), len(b))
	}
	return -1
}

// around returns up to 40 bytes of b on each side of i.
func around(b []byte, i int) []byte {
	return b[max(0, i-40):min(len(b), i+40)]
}
