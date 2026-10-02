package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/parse/parsetest"
	"github.com/ChiemekaKalu/demoview/parser/roundfile"
	"github.com/ChiemekaKalu/demoview/parser/store"
)

// twoRounds is the smallest plausible match: two rounds, a kill in each.
var twoRounds = parsetest.MatchRounds(2)

// unknownEntityDemo is a one-round match whose entity classes lack the
// fields round files need, as a game update could bring.
var unknownEntityDemo = parsetest.Build(append(parsetest.ClassFrames("CCSPlayerController", "CCSPlayerPawn",
	"CSmokeGrenadeProjectile", "CFlashbangProjectile", "CHEGrenadeProjectile", "CMolotovProjectile", "CDecoyProjectile"),
	parsetest.MatchFrames()...)...)

// testRounds stands in for the entity stage on synthetic demos, which have
// none: a two-frame round file per round of m, with every player standing
// still and the round's kills as events.
func testRounds(m match.File) []roundfile.File {
	files := make([]roundfile.File, len(m.Rounds))
	for i, r := range m.Rounds {
		f := roundfile.File{SchemaVersion: m.SchemaVersion, Round: r.N, Hz: 16, StartTick: r.FreezeEndTick, Frames: 2}
		for _, p := range m.Players {
			x := int16(100 * p.Idx)
			f.Players = append(f.Players, roundfile.Player{Idx: p.Idx, X: []int16{x, x}, Y: []int16{0, 0}, Yaw: []int16{90, 90}, HP: []int16{100, 100}})
		}
		for k, kill := range m.Kills {
			if kill.Round == r.N {
				f.Events = append(f.Events, roundfile.Event{Frame: 1, Type: roundfile.EventKill, Kill: k})
			}
		}
		files[i] = f
	}
	return files
}

// parseWithRounds is parse.Parse with testRounds for round files.
func parseWithRounds(ctx context.Context, r io.Reader, opt parse.Options) (parse.Output, parse.Stats, error) {
	out, st, err := parse.Parse(ctx, r, parse.Options{})
	if err == nil && opt.Rounds {
		out.Rounds = testRounds(out.Match)
	}
	return out, st, err
}

// newParserHarness is a server with PARSER_ROUNDS=parser and, unless
// oracleMode is "", the fake oracle in that mode.
func newParserHarness(t *testing.T, parseFn ParseFunc, oracleMode string) *harness {
	h := newHarness(t, parseFn, "")
	h.srv.parserRounds = true
	if oracleMode != "" {
		h.srv.oracle = fakeOracle(oracleMode)
	}
	return h
}

// readManifest reads the parse's manifest, checks every file it lists is
// stored as listed, and returns it with the files' names under the prefix.
func (h *harness) readManifest() (manifest, []string) {
	h.t.Helper()
	var m manifest
	if err := json.Unmarshal(h.stored(outPrefix+"manifest.json"), &m); err != nil {
		h.t.Fatal(err)
	}
	var names []string
	for _, f := range m.Files {
		data := h.stored(f.Key)
		sum := sha256.Sum256(data)
		if f.Bytes != len(data) || f.SHA256 != hex.EncodeToString(sum[:]) {
			h.t.Errorf("%s: manifest says %d bytes %s, stored is %d bytes", f.Key, f.Bytes, f.SHA256, len(data))
		}
		names = append(names, strings.TrimPrefix(f.Key, outPrefix))
	}
	return m, names
}

// logLine returns the first log line with message msg, or nil.
func (h *harness) logLine(msg string) map[string]any {
	h.t.Helper()
	for _, l := range h.logLines() {
		if l["message"] == msg {
			return l
		}
	}
	return nil
}

// recordingStore records the names of the files put under the parse's
// prefix, in order.
type recordingStore struct {
	store.Store
	mu   sync.Mutex
	puts []string
}

func (r *recordingStore) Put(ctx context.Context, key string, data []byte) error {
	r.mu.Lock()
	r.puts = append(r.puts, strings.TrimPrefix(key, outPrefix))
	r.mu.Unlock()
	return r.Store.Put(ctx, key, data)
}

func (r *recordingStore) keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.puts)
}

// recordPuts makes the server's store a recordingStore, from now on.
func (h *harness) recordPuts() *recordingStore {
	r := &recordingStore{Store: h.srv.store}
	h.srv.store = r
	return r
}

// A plausible match ships our round files, then match.json, then the
// manifest listing them, and the oracle never runs.
func TestParserRoundsShip(t *testing.T) {
	h := newParserHarness(t, parseWithRounds, "crash") // a 500 if it ran
	h.putDemo(twoRounds)
	puts := h.recordPuts()

	status, body := h.parse()
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if want := []string{"rounds/01.json", "rounds/02.json", "match.json", "manifest.json"}; !slices.Equal(puts.keys(), want) {
		t.Errorf("wrote %q, want %q", puts.keys(), want)
	}
	m, names := h.readManifest()
	if want := []string{"rounds/01.json", "rounds/02.json", "match.json"}; !slices.Equal(names, want) {
		t.Errorf("manifest lists %q, want %q", names, want)
	}
	if m.MatchSource != "parser" || m.RoundSource != "parser" {
		t.Errorf("manifest sources %q and %q, want parser", m.MatchSource, m.RoundSource)
	}
	if stored := h.stored(outPrefix + match.FileName); !bytes.Equal(stored, body) {
		t.Error("stored match.json differs from the response")
	}
	var file match.File
	if err := json.Unmarshal(body, &file); err != nil {
		t.Fatal(err)
	}
	want, err := encodeRounds(testRounds(file))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range want {
		if got := h.stored(outPrefix + "rounds/" + f.name); !bytes.Equal(got, f.data) {
			t.Errorf("%s = %s, want %s", f.name, got, f.data)
		}
	}
	checkLog(t, h.lastLog(), map[string]any{
		"result":       "ok",
		"match_source": "parser",
		"round_source": "parser",
		"rounds":       float64(2),
		"put_objects":  float64(4),
		"oracle_ms":    nil,
	})
	if h.logLine("round file fallback") != nil {
		t.Errorf("logged a fallback:\n%s", h.logs)
	}
}

// The M6 6.5 exit check: an unknown entity field is an entity-stage error.
// Our match.json still ships, the oracle's round files stand in for ours,
// and a warning says why.
func TestEntityErrorFallsBackToOracle(t *testing.T) {
	h := newParserHarness(t, parse.Parse, "agree")
	h.putDemo(unknownEntityDemo)

	status, body := h.parse()
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if v := factsOf(body).ParserVersion; v != parse.Version {
		t.Errorf("shipped parser_version %q, want ours (%s)", v, parse.Version)
	}
	var rf helperRound
	if err := json.Unmarshal(h.stored(outPrefix+"rounds/01.json"), &rf); err != nil || rf.StdinBytes != len(unknownEntityDemo) {
		t.Errorf("rounds/01.json is not the oracle's for this demo: %+v, %v", rf, err)
	}
	if m, _ := h.readManifest(); m.MatchSource != "parser" || m.RoundSource != "oracle" {
		t.Errorf("manifest sources %q and %q, want parser and oracle", m.MatchSource, m.RoundSource)
	}
	w := h.logLine("round file fallback")
	checkLog(t, w, map[string]any{
		"severity":        "WARNING",
		"match_id":        matchID,
		"parse_id":        parseID,
		"fallback_reason": "entity_error",
		"round_source":    "oracle",
		"live_rounds":     float64(1),
		"kills":           float64(1),
	})
	if e := fmt.Sprint(w["entity_error"]); !strings.Contains(e, "m_steamID") {
		t.Errorf("entity_error = %q, want the unknown field", e)
	}
	checkLog(t, h.lastLog(), map[string]any{"result": "ok", "match_source": "parser", "round_source": "oracle"})
}

// With an oracle to stand in, a match of one round is implausible, so the
// oracle's round files ship instead of ours.
func TestImplausibleMatchFallsBackToOracle(t *testing.T) {
	h := newParserHarness(t, parseWithRounds, "agree")
	h.putDemo(testDemo)

	if status, body := h.parse(); status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	checkLog(t, h.logLine("round file fallback"), map[string]any{
		"fallback_reason": "implausible_match",
		"round_source":    "oracle",
		"live_rounds":     float64(1),
		"kills":           float64(1),
		"entity_error":    nil,
	})
	checkLog(t, h.lastLog(), map[string]any{"result": "ok", "match_source": "parser", "round_source": "oracle", "put_objects": float64(4)})
}

// Without an oracle, a fallback ships match.json alone, and the parse
// still succeeds.
func TestFallbackWithoutOracle(t *testing.T) {
	h := newParserHarness(t, parse.Parse, "")
	h.putDemo(unknownEntityDemo)

	if status, body := h.parse(); status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if m, names := h.readManifest(); m.RoundSource != "none" || !slices.Equal(names, []string{match.FileName}) {
		t.Errorf("manifest round_source %q, files %q; want none and match.json alone", m.RoundSource, names)
	}
	checkLog(t, h.logLine("round file fallback"), map[string]any{
		"severity":        "WARNING",
		"fallback_reason": "entity_error",
		"round_source":    "none",
	})
	checkLog(t, h.lastLog(), map[string]any{"result": "ok", "status": float64(200), "round_source": "none", "oracle_ms": nil})
}

func TestFallbackReason(t *testing.T) {
	output := func(rounds, kills int, roundsErr error) parse.Output {
		return parse.Output{Match: match.File{Rounds: make([]match.Round, rounds), Kills: make([]match.Kill, kills)}, RoundsErr: roundsErr}
	}
	entityErr := errors.New("entities: unknown field")
	for _, tc := range []struct {
		name   string
		out    parse.Output
		oracle bool
		want   string
	}{
		{"plausible", output(2, 1, nil), true, ""},
		{"entity error", output(24, 150, entityErr), true, "entity_error"},
		{"entity error, no oracle", output(24, 150, entityErr), false, "entity_error"},
		{"one round", output(1, 5, nil), true, "implausible_match"},
		{"no kills", output(24, 0, nil), true, "implausible_match"},
		{"one round, no oracle", output(1, 5, nil), false, ""},
	} {
		if got := fallbackReason(tc.out, tc.oracle); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// With PARSER_ROUNDS=parser the oracle is only a fallback, so the default
// binary may be missing, but a PARSER_ORACLE_BIN that is set may not.
func TestOracleFromEnvParserMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	env := map[string]string{"PARSER_ROUNDS": "parser"}
	getenv := func(k string) string { return env[k] }
	if o, err := oracleFromEnv(getenv); o != nil || err != nil {
		t.Errorf("no oracle on PATH: %v, %v; want no fallback", o, err)
	}
	name := "oracle"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if o, err := oracleFromEnv(getenv); o == nil || err != nil {
		t.Errorf("oracle on PATH: %v, %v", o, err)
	}
	env["PARSER_ORACLE_BIN"] = filepath.Join(t.TempDir(), "missing-oracle")
	if _, err := oracleFromEnv(getenv); err == nil {
		t.Error("missing PARSER_ORACLE_BIN accepted")
	}
}

func TestShadowFromEnv(t *testing.T) {
	for _, tc := range []struct {
		shadow, rounds string
		oracle         bool
		want           bool
		err            string
	}{
		{"", "", false, false, ""},
		{"", "parser", true, false, ""},
		{"oracle", "parser", true, true, ""},
		{"oracle", "parser", false, false, "needs an oracle"},
		{"oracle", "oracle", true, false, "needs PARSER_ROUNDS=parser"},
		{"oracle", "", false, false, "needs PARSER_ROUNDS=parser"},
		{"parser", "parser", true, false, `want "oracle"`},
	} {
		env := map[string]string{"PARSER_SHADOW": tc.shadow, "PARSER_ROUNDS": tc.rounds}
		got, err := shadowFromEnv(func(k string) string { return env[k] }, tc.oracle)
		if got != tc.want || (err == nil) != (tc.err == "") || (err != nil && !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("PARSER_SHADOW=%q PARSER_ROUNDS=%q oracle=%v: %v, %v; want %v, %q", tc.shadow, tc.rounds, tc.oracle, got, err, tc.want, tc.err)
		}
	}
}

// The listening line says what the server will ship and compare, and a
// shadow it can't run stops it at startup.
func TestRunParserMode(t *testing.T) {
	env := map[string]string{
		"PORT":              "0",
		"PARSE_SECRET":      "s",
		"PARSER_STORE_DIR":  t.TempDir(),
		"PARSER_ROUNDS":     "parser",
		"PARSER_ORACLE_BIN": os.Args[0],
		"PARSER_SHADOW":     "oracle",
	}
	getenv := func(k string) string { return env[k] }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var logs bytes.Buffer
	if err := run(ctx, getenv, newLogger(&logs)); err != nil {
		t.Fatal(err)
	}
	checkLog(t, decodeLines(t, logs.String())[0], map[string]any{"message": "listening", "store": "dir", "rounds": "parser", "oracle": true, "shadow": true})

	env["PARSER_ROUNDS"] = "oracle"
	if err := run(ctx, getenv, newLogger(io.Discard)); err == nil || !strings.Contains(err.Error(), "PARSER_SHADOW") {
		t.Errorf("shadow with PARSER_ROUNDS=oracle: %v, want refused", err)
	}
}

// TestRealDemosParserRounds runs the public fixtures through the server
// with PARSER_ROUNDS=parser and a local store. It skips without them or
// under -short.
func TestRealDemosParserRounds(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	for _, name := range []string{"faceit_nuke_2026.dem", "valve_mm_mirage_2024.dem"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join("..", "..", "testdata", "demos", name)
			if _, err := os.Stat(p); err != nil {
				t.Skip("no demo")
			}
			h := newParserHarness(t, parse.Parse, "")
			h.store = demoStore{Store: h.store, key: demoKey, path: p}
			h.srv.store = h.store

			status, body := h.parse()
			if status != http.StatusOK {
				t.Fatalf("status %d: %s", status, body)
			}
			var file match.File
			if err := json.Unmarshal(body, &file); err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, r := range file.Rounds {
				want = append(want, "rounds/"+roundfile.Name(r.N))
			}
			m, names := h.readManifest()
			if want = append(want, match.FileName); !slices.Equal(names, want) {
				t.Errorf("manifest lists %q, want %q", names, want)
			}
			if m.MatchSource != "parser" || m.RoundSource != "parser" {
				t.Errorf("manifest sources %q and %q, want parser", m.MatchSource, m.RoundSource)
			}
			entry := h.lastLog()
			checkLog(t, entry, map[string]any{"result": "ok", "match_source": "parser", "round_source": "parser"})
			if h.logLine("round file fallback") != nil {
				t.Errorf("logged a fallback:\n%s", h.logs)
			}
			t.Logf("rounds=%v put_bytes=%v duration_ms=%v", entry["rounds"], entry["put_bytes"], entry["duration_ms"])
		})
	}
}
