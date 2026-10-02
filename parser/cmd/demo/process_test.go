package main

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
// process, and checks that the two runs write the same files, match.json and
// every round file, byte for byte.
func TestRunTwiceIdentical(t *testing.T) {
	demos := localDemos(t)
	exe := buildCLI(t)
	for _, path := range demos {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".dem"), func(t *testing.T) {
			t.Parallel()
			first, second := parseInChild(t, exe, path), parseInChild(t, exe, path)
			names := slices.Sorted(maps.Keys(first))
			if other := slices.Sorted(maps.Keys(second)); !slices.Equal(names, other) {
				t.Fatalf("the runs wrote different files: %v and %v", names, other)
			}
			if !slices.Contains(names, filepath.Join("rounds", "01.json")) {
				t.Errorf("no round files among %v", names)
			}
			for _, name := range names {
				a, b := first[name], second[name]
				if i := firstDiff(a, b); i >= 0 {
					t.Errorf("%s differs between runs at byte %d of %d and %d:\n%q\n%q",
						name, i, len(a), len(b), around(a, i), around(b, i))
				}
			}
		})
	}
}

// TestBudget parses the largest local demo in a child process with
// GOMAXPROCS=1, in full and events-only, and checks that each takes under
// 60 s and under 512 MB of peak resident memory. Where runPeak can't
// measure the peak, it checks the time only.
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
	for _, mode := range []string{"full", "events-only"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"parse", largest, "--out", t.TempDir()}
			if mode == "events-only" {
				args = append(args, "--events-only")
			}
			cmd := exec.Command(exe, args...)
			cmd.Env = append(os.Environ(), "GOMAXPROCS=1")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			start := time.Now()
			peak, err := runPeak(cmd)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("parse: %v\n%s", err, stderr.Bytes())
			}

			t.Logf("%.1f MiB demo, %s, GOMAXPROCS=1: %v wall, peak RSS %.1f MiB", float64(size)/(1<<20), mode, elapsed.Round(time.Millisecond), float64(peak)/(1<<20))
			if elapsed >= budgetTime {
				t.Errorf("parse took %v, want under %v", elapsed, budgetTime)
			}
			switch {
			case peak < 0:
				t.Logf("peak RSS is not measurable on %s", runtime.GOOS)
			case peak >= budgetMemory:
				t.Errorf("peak RSS %d bytes, want under %d", peak, budgetMemory)
			}
		})
	}
}

// TestDiffRounds parses the smallest local demo, then diffs the demo
// against that output, round files included: first as written, then with a
// round file changed and one missing.
func TestDiffRounds(t *testing.T) {
	demos := localDemos(t)
	smallest, size := "", int64(-1)
	for _, p := range demos {
		if fi, err := os.Stat(p); err == nil && (size < 0 || fi.Size() < size) {
			smallest, size = p, fi.Size()
		}
	}
	dir := t.TempDir()
	if code, _, stderr := runCLI("parse", smallest, "--out", dir); code != 0 {
		t.Fatalf("parse: exit %d, stderr %s", code, stderr)
	}
	ref := filepath.Join(dir, "match.json")
	if code, stdout, stderr := runCLI("diff", smallest, "--against", ref); code != 0 || strings.TrimSpace(stdout) != "OK" {
		t.Fatalf("against itself: exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}

	first := filepath.Join(dir, "rounds", "01.json")
	b, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, bytes.Replace(b, []byte(`"hp":[`), []byte(`"hp":[1`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "rounds", "02.json")); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runCLI("diff", smallest, "--against", ref)
	if code != 1 || !strings.Contains(stdout, "round[1].players[") || !strings.Contains(stdout, "rounds/02.json: ours present, reference absent") {
		t.Errorf("changed rounds: exit %d, stdout %q", code, stdout)
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
// files it writes, by path under the output directory.
func parseInChild(t *testing.T, exe, path string) map[string][]byte {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command(exe, "parse", path, "--out", dir).CombinedOutput(); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err == nil {
			files[rel], err = os.ReadFile(p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
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
