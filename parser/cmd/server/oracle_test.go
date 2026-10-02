package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/store"
)

// TestHelperOracle is not a test. It is the fake oracle: the test binary
// re-runs itself with ORACLE_HELPER set and the oracle's own arguments after
// "--". The mode in ORACLE_HELPER picks what it writes.
func TestHelperOracle(t *testing.T) {
	mode := os.Getenv("ORACLE_HELPER")
	if mode == "" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	// Expect: export - --out <dir>
	if len(args) != 4 || args[0] != "export" || args[1] != "-" || args[2] != "--out" {
		fmt.Fprintf(os.Stderr, `{"error":"bad args %q"}`+"\n", args)
		os.Exit(2)
	}
	out := args[3]
	stdin, _ := io.ReadAll(os.Stdin)
	if mode == "crash" {
		fmt.Fprintln(os.Stderr, `{"error":"demoinfocs: boom"}`)
		os.Exit(1)
	}
	players := `[]`
	if mode == "disagree" {
		players = `[{"idx":0,"steam_id":"76561198000000000","name":"x","team":"A"}]`
	}
	match := fmt.Sprintf(`{"schema_version":1,"parser_version":"9.9.9-oracle","match":{"map":"de_mirage","tick_rate":64,"demo_sha256":""},"players":%s,"rounds":[],"kills":[],"scoreboard":[]}`, players)
	os.MkdirAll(filepath.Join(out, "rounds"), 0o755)
	os.WriteFile(filepath.Join(out, "match.json"), []byte(match), 0o644)
	// Round files record how much of the demo arrived on stdin.
	for _, name := range []string{"01.json", "02.json"} {
		os.WriteFile(filepath.Join(out, "rounds", name), []byte(fmt.Sprintf(`{"round":%q,"stdin_bytes":%d}`, name, len(stdin))), 0o644)
	}
	os.Exit(0)
}

// fakeOracle runs TestHelperOracle in the given mode.
func fakeOracle(mode string) *oracle {
	return &oracle{
		argv: []string{os.Args[0], "-test.run=^TestHelperOracle$", "--"},
		env:  []string{"ORACLE_HELPER=" + mode},
	}
}

func newOracleHarness(t *testing.T, mode string) *harness {
	h := newHarness(t, parse.Run, "")
	h.srv.oracle = fakeOracle(mode)
	return h
}

// logs returns every log line decoded.
func (h *harness) logLines() []map[string]any {
	h.t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			h.t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestOracleAgreeShipsOurMatchJSON(t *testing.T) {
	h := newOracleHarness(t, "agree")
	h.putDemo(emptyDemo)

	status, body := h.parse()
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if v := parserVersionOf(body); v != parse.Version {
		t.Errorf("shipped parser_version %q, want ours (%s)", v, parse.Version)
	}
	if stored := h.stored("parsed/" + matchID + "/match.json"); !bytes.Equal(stored, body) {
		t.Error("stored match.json differs from the response")
	}
	// The demo was streamed to the oracle in full, and its round files shipped.
	for _, name := range []string{"01.json", "02.json"} {
		var rf struct {
			StdinBytes int `json:"stdin_bytes"`
		}
		json.Unmarshal(h.stored("parsed/"+matchID+"/rounds/"+name), &rf)
		if rf.StdinBytes != len(emptyDemo) {
			t.Errorf("%s: oracle read %d bytes of stdin, want %d", name, rf.StdinBytes, len(emptyDemo))
		}
	}
	entry := h.lastLog()
	if entry["match_source"] != "parser" || entry["parser_version"] != parse.Version {
		t.Errorf("log = %v", entry)
	}
	if len(h.logLines()) != 1 {
		t.Errorf("want only the parse line, got:\n%s", h.logs)
	}
}

func TestOracleDisagreeShipsTheOracles(t *testing.T) {
	h := newOracleHarness(t, "disagree")
	h.putDemo(emptyDemo)

	status, body := h.parse()
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if v := parserVersionOf(body); v != "9.9.9-oracle" {
		t.Errorf("shipped parser_version %q, want the oracle's", v)
	}
	if stored := h.stored("parsed/" + matchID + "/match.json"); !bytes.Equal(stored, body) {
		t.Error("stored match.json differs from the response")
	}
	h.stored("parsed/" + matchID + "/rounds/01.json")

	lines := h.logLines()
	if len(lines) != 2 {
		t.Fatalf("want a warning and the parse line, got:\n%s", h.logs)
	}
	if w := lines[0]; w["severity"] != "WARNING" || w["mismatch"] != "players: 0 vs 1" || w["match_id"] != matchID {
		t.Errorf("warning = %v", w)
	}
	if p := lines[1]; p["match_source"] != "oracle" || p["parser_version"] != "9.9.9-oracle" || p["result"] != "ok" {
		t.Errorf("parse log = %v", p)
	}
}

func TestOracleCrashIs500(t *testing.T) {
	h := newOracleHarness(t, "crash")
	h.putDemo(emptyDemo)

	status, body := h.parse()
	if status != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", status)
	}
	if msg := errorBody(t, body); !strings.Contains(msg, "demoinfocs: boom") {
		t.Errorf("error = %q, want the oracle's message", msg)
	}
	for _, key := range []string{"match.json", "rounds/01.json"} {
		if _, _, err := h.store.Open(context.Background(), "parsed/"+matchID+"/"+key); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s was written after the oracle failed", key)
		}
	}
	if entry := h.lastLog(); entry["result"] != "error" || entry["match_source"] != nil {
		t.Errorf("log = %v", entry)
	}
}

func TestOracleNotRunForABadDemo(t *testing.T) {
	h := newOracleHarness(t, "crash") // would turn the 422 into a 500 if it ran
	h.putDemo([]byte("not a demo"))
	if status, _ := h.parse(); status != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422 from our parser", status)
	}
}

func TestFirstMismatch(t *testing.T) {
	base := func(mod func(m map[string]any)) []byte {
		m := map[string]any{
			"parser_version": "x",
			"players": []any{
				map[string]any{"idx": 0, "steam_id": "1", "team": "A"},
				map[string]any{"idx": 1, "steam_id": "2", "team": "B"},
			},
			"rounds": []any{map[string]any{"n": 1, "start_tick": 10, "freeze_end_tick": 20, "end_tick": 30}},
			"kills":  []any{map[string]any{"round": 1, "tick": 25, "attacker": 0, "victim": 1, "weapon": "ak47"}},
		}
		if mod != nil {
			mod(m)
		}
		b, _ := json.Marshal(m)
		return b
	}
	ours := base(nil)
	tests := []struct {
		name string
		mod  func(map[string]any)
		want string
	}{
		{"identical", nil, ""},
		{"only versions differ", func(m map[string]any) { m["parser_version"] = "y" }, ""},
		{"team", func(m map[string]any) {
			m["players"].([]any)[1].(map[string]any)["team"] = "A"
		}, "players[1]"},
		{"freeze end", func(m map[string]any) {
			m["rounds"].([]any)[0].(map[string]any)["freeze_end_tick"] = 21
		}, "rounds[0]"},
		{"kill count", func(m map[string]any) { m["kills"] = []any{} }, "kills: 1 vs 0"},
		{"weapon", func(m map[string]any) {
			m["kills"].([]any)[0].(map[string]any)["weapon"] = "m4a1"
		}, "kills[0]"},
		{"null attacker", func(m map[string]any) {
			m["kills"].([]any)[0].(map[string]any)["attacker"] = nil
		}, "kills[0]"},
	}
	for _, tc := range tests {
		got := firstMismatch(ours, base(tc.mod))
		if (tc.want == "") != (got == "") || !strings.HasPrefix(got, tc.want) {
			t.Errorf("%s: firstMismatch = %q, want prefix %q", tc.name, got, tc.want)
		}
	}
	if got := firstMismatch(ours, []byte("{")); !strings.HasPrefix(got, "oracle's is not valid") {
		t.Errorf("invalid JSON: %q", got)
	}
}

func TestOracleFromEnv(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if o, err := oracleFromEnv(getenv); o != nil || err != nil {
		t.Errorf("unset: %v, %v", o, err)
	}
	env["PARSER_ROUNDS"] = "parser"
	if _, err := oracleFromEnv(getenv); err == nil {
		t.Error("unknown mode accepted")
	}
	env["PARSER_ROUNDS"] = "oracle"
	env["PARSER_ORACLE_BIN"] = filepath.Join(t.TempDir(), "missing-oracle")
	if _, err := oracleFromEnv(getenv); err == nil {
		t.Error("missing binary accepted")
	}
	env["PARSER_ORACLE_BIN"] = os.Args[0]
	if o, err := oracleFromEnv(getenv); err != nil || o == nil {
		t.Errorf("existing binary: %v, %v", o, err)
	}
}

// demoStore serves one demo file from disk and keeps everything else in a
// temporary Dir, so the real fixtures need no copying.
type demoStore struct {
	store.Store
	key, path string
}

func (d demoStore) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if key != d.key {
		return d.Store.Open(ctx, key)
	}
	f, err := os.Open(d.path)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// TestRealDemosWithOracle runs the whole path on testdata/demos with a real
// oracle build. It skips without demos or under -short.
func TestRealDemosWithOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	demos, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "demos", "*.dem"))
	if len(demos) == 0 {
		t.Skip("no demos in testdata/demos")
	}
	bin := filepath.Join(t.TempDir(), "oracle")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", bin, ".")
	build.Dir = filepath.Join("..", "..", "..", "tools", "oracle")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the oracle: %v\n%s", err, out)
	}

	for _, p := range demos {
		t.Run(filepath.Base(p), func(t *testing.T) {
			h := newHarness(t, parse.Run, "")
			h.store = demoStore{Store: h.store, key: demoKey, path: p}
			h.srv.store = h.store
			h.srv.oracle = &oracle{argv: []string{bin}}

			status, body := h.parse()
			if status != http.StatusOK {
				t.Fatalf("status %d: %s", status, body)
			}
			var shipped struct {
				Rounds []struct{ N int } `json:"rounds"`
			}
			if err := json.Unmarshal(body, &shipped); err != nil || len(shipped.Rounds) == 0 {
				t.Fatalf("shipped match.json has no rounds: %v", err)
			}
			for _, r := range shipped.Rounds {
				h.stored(fmt.Sprintf("parsed/%s/rounds/%02d.json", matchID, r.N))
			}
			entry := h.lastLog()
			t.Logf("source=%v duration_ms=%v oracle_ms=%v rounds=%d", entry["match_source"], entry["duration_ms"], entry["oracle_ms"], len(shipped.Rounds))
		})
	}
}
