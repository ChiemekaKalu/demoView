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
	"slices"
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
	// The fake reads the demo as the parser does, so "agree" really agrees.
	file, err := parse.Run(context.Background(), bytes.NewReader(stdin))
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"error":%q}`+"\n", err.Error())
		os.Exit(1)
	}
	file.ParserVersion = "9.9.9-oracle"
	if mode == "disagree" {
		file.Players[0].SteamID = "76561198000000000"
	}
	os.MkdirAll(filepath.Join(out, "rounds"), 0o755)
	var match bytes.Buffer
	json.NewEncoder(&match).Encode(file)
	os.WriteFile(filepath.Join(out, "match.json"), match.Bytes(), 0o644)
	// Round files record how much of the demo arrived on stdin, and the
	// environment the oracle got.
	for _, name := range []string{"01.json", "02.json"} {
		rf, _ := json.Marshal(helperRound{Round: name, StdinBytes: len(stdin), Env: os.Environ()})
		os.WriteFile(filepath.Join(out, "rounds", name), rf, 0o644)
	}
	os.Exit(0)
}

// helperRound is a round file from the fake oracle.
type helperRound struct {
	Round      string   `json:"round"`
	StdinBytes int      `json:"stdin_bytes"`
	Env        []string `json:"env"`
}

// fakeOracle runs TestHelperOracle in the given mode.
func fakeOracle(mode string) *oracle {
	return &oracle{
		argv: []string{os.Args[0], "-test.run=^TestHelperOracle$", "--"},
		env:  []string{"ORACLE_HELPER=" + mode},
	}
}

func newOracleHarness(t *testing.T, mode string) *harness {
	h := newHarness(t, parse.RunStats, "")
	h.srv.oracle = fakeOracle(mode)
	return h
}

func TestOracleAgreeShipsOurMatchJSON(t *testing.T) {
	h := newOracleHarness(t, "agree")
	h.putDemo(testDemo)

	status, body := h.parse()
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if v := factsOf(body).ParserVersion; v != parse.Version {
		t.Errorf("shipped parser_version %q, want ours (%s)", v, parse.Version)
	}
	if stored := h.stored("parsed/" + matchID + "/match.json"); !bytes.Equal(stored, body) {
		t.Error("stored match.json differs from the response")
	}
	// The demo was streamed to the oracle in full, and its round files shipped.
	for _, name := range []string{"01.json", "02.json"} {
		var rf helperRound
		json.Unmarshal(h.stored("parsed/"+matchID+"/rounds/"+name), &rf)
		if rf.StdinBytes != len(testDemo) {
			t.Errorf("%s: oracle read %d bytes of stdin, want %d", name, rf.StdinBytes, len(testDemo))
		}
	}
	entry := h.lastLog()
	checkLog(t, entry, map[string]any{
		"match_source":         "parser",
		"parser_version":       parse.Version,
		"match_parser_version": nil,
		"put_objects":          float64(3),
		"oracle_stderr":        nil,
	})
	if _, ok := entry["oracle_ms"].(float64); !ok {
		t.Errorf("log oracle_ms = %v, want a number", entry["oracle_ms"])
	}
	if slices.ContainsFunc(h.messages(), func(m string) bool { return strings.Contains(m, "disagrees") }) {
		t.Errorf("logged a disagreement:\n%s", h.logs)
	}
}

// The oracle sees none of the server's secrets.
func TestOracleEnvironment(t *testing.T) {
	t.Setenv("PARSE_SECRET", "sentinel-parse-secret")
	t.Setenv("R2_SECRET_ACCESS_KEY", "sentinel-r2-secret")
	h := newOracleHarness(t, "agree")
	h.putDemo(testDemo)
	if status, body := h.parse(); status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	var rf helperRound
	json.Unmarshal(h.stored("parsed/"+matchID+"/rounds/01.json"), &rf)
	if !slices.Contains(rf.Env, "ORACLE_HELPER=agree") || !slices.ContainsFunc(rf.Env, func(v string) bool { return strings.HasPrefix(v, "PATH=") }) {
		t.Errorf("oracle environment %q lacks PATH or its own variable", rf.Env)
	}
	for _, v := range rf.Env {
		if strings.Contains(v, "sentinel") {
			t.Errorf("oracle environment has %s", v)
		}
	}
}

func TestOracleDisagreeShipsTheOracles(t *testing.T) {
	h := newOracleHarness(t, "disagree")
	h.putDemo(testDemo)

	status, body := h.parse()
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if v := factsOf(body).ParserVersion; v != "9.9.9-oracle" {
		t.Errorf("shipped parser_version %q, want the oracle's", v)
	}
	if stored := h.stored("parsed/" + matchID + "/match.json"); !bytes.Equal(stored, body) {
		t.Error("stored match.json differs from the response")
	}
	h.stored("parsed/" + matchID + "/rounds/01.json")

	lines := h.logLines()
	i := slices.IndexFunc(lines, func(l map[string]any) bool { return l["mismatch"] != nil })
	if i < 0 {
		t.Fatalf("no mismatch warning in:\n%s", h.logs)
	}
	if w := lines[i]; w["severity"] != "WARNING" || !strings.HasPrefix(fmt.Sprint(w["mismatch"]), "players[0]: ") || w["match_id"] != matchID {
		t.Errorf("warning = %v", w)
	}
	checkLog(t, h.lastLog(), map[string]any{
		"result":               "ok",
		"match_source":         "oracle",
		"parser_version":       parse.Version,
		"match_parser_version": "9.9.9-oracle",
	})
}

func TestOracleCrashIs500(t *testing.T) {
	h := newOracleHarness(t, "crash")
	h.putDemo(testDemo)

	status, body := h.parse()
	if status != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", status)
	}
	if msg := errorBody(t, body); strings.Contains(msg, "boom") {
		t.Errorf("body error = %q, want the oracle's message in the log only", msg)
	}
	for _, key := range []string{"match.json", "rounds/01.json"} {
		if _, _, err := h.store.Open(context.Background(), "parsed/"+matchID+"/"+key); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s was written after the oracle failed", key)
		}
	}
	entry := h.lastLog()
	checkLog(t, entry, map[string]any{"result": "error", "match_source": nil, "oracle_stderr": `{"error":"demoinfocs: boom"}`})
	if msg := fmt.Sprint(entry["error"]); !strings.Contains(msg, "demoinfocs: boom") {
		t.Errorf("log error = %q, want the oracle's message", msg)
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
			h := newHarness(t, parse.RunStats, "")
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
			t.Logf("source=%v duration_ms=%v walk_ms=%v oracle_ms=%v rounds=%v", entry["match_source"], entry["duration_ms"], entry["walk_ms"], entry["oracle_ms"], entry["rounds"])
		})
	}
}
