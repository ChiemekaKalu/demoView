package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ChiemekaKalu/demoview/parser/store"
)

// newShadowHarness is a parser-mode server with PARSER_SHADOW=oracle and
// the fake oracle in oracleMode.
func newShadowHarness(t *testing.T, oracleMode string) *harness {
	h := newParserHarness(t, parseWithRounds, oracleMode)
	h.srv.shadow = true
	return h
}

// After a parse ships our round files, the shadow compare runs the oracle,
// logs that its output matches, and writes that verdict after the manifest.
func TestShadowMatch(t *testing.T) {
	h := newShadowHarness(t, "rounds")
	h.putDemo(twoRounds)
	puts := h.recordPuts()

	if status, body := h.parse(); status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if keys := puts.keys(); len(keys) != 5 || !slices.Equal(keys[3:], []string{"manifest.json", "shadow.json"}) {
		t.Errorf("wrote %q, want the parse's 4 files, then shadow.json", keys)
	}
	if _, names := h.readManifest(); slices.Contains(names, "shadow.json") {
		t.Errorf("manifest lists shadow.json: %q", names)
	}
	checkVerdict(t, h, shadowVerdict{Result: "match", RoundSource: "parser", RoundsCompared: 2, ByCategory: map[string]int{}, First: []string{}})
	msgs := h.messages()
	if got := msgs[len(msgs)-2:]; !slices.Equal(got, []string{"parse", "shadow"}) {
		t.Fatalf("log ends %q, want the summary then the shadow line", got)
	}
	entry := h.lastLog()
	checkLog(t, entry, map[string]any{
		"severity":        "INFO",
		"event":           "shadow",
		"result":          "match",
		"match_id":        matchID,
		"parse_id":        parseID,
		"rounds_compared": float64(2),
		"mismatches":      float64(0),
		"error":           nil,
	})
	if c, ok := entry["by_category"].(map[string]any); !ok || len(c) != 0 {
		t.Errorf("by_category = %v, want {}", entry["by_category"])
	}
	if f, ok := entry["first"].([]any); !ok || len(f) != 0 {
		t.Errorf("first = %v, want []", entry["first"])
	}
	if _, ok := entry["oracle_ms"].(float64); !ok {
		t.Errorf("oracle_ms = %v, want a number", entry["oracle_ms"])
	}
}

// A shadow mismatch is counted by category, with examples, and changes
// nothing that shipped.
func TestShadowMismatch(t *testing.T) {
	h := newShadowHarness(t, "rounds-differ")
	h.putDemo(twoRounds)

	status, body := h.parse()
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if m, names := h.readManifest(); m.RoundSource != "parser" || len(names) != 3 {
		t.Errorf("manifest round_source %q, files %q; want ours", m.RoundSource, names)
	}
	entry := h.lastLog()
	checkLog(t, entry, map[string]any{"severity": "WARNING", "result": "mismatch", "rounds_compared": float64(1), "mismatches": float64(2)})
	if want := map[string]any{"round.players.xy": float64(1), "round_files": float64(1)}; !reflect.DeepEqual(entry["by_category"], want) {
		t.Errorf("by_category = %v, want %v", entry["by_category"], want)
	}
	first, _ := entry["first"].([]any)
	if len(first) != 2 || !strings.HasPrefix(fmt.Sprint(first[0]), `round[1].players[1000].xy[0]: ours "(0, 0), 1 of 2 off`) ||
		first[1] != "round_files[02.json]: ours present, oracle absent" {
		t.Errorf("first = %q", first)
	}
	checkVerdict(t, h, shadowVerdict{
		Result: "mismatch", RoundSource: "parser", RoundsCompared: 1, Mismatches: 2,
		ByCategory: map[string]int{"round.players.xy": 1, "round_files": 1},
		First:      []string{fmt.Sprint(first[0]), fmt.Sprint(first[1])},
	})
}

// checkVerdict checks the parse's shadow.json against want, whatever its
// oracle time.
func checkVerdict(t *testing.T, h *harness, want shadowVerdict) {
	t.Helper()
	var got shadowVerdict
	if err := json.Unmarshal(h.stored(outPrefix+"shadow.json"), &got); err != nil {
		t.Fatalf("shadow.json: %v", err)
	}
	got.OracleMS = 0
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shadow.json\n got %+v\nwant %+v", got, want)
	}
}

// An oracle failure is a shadow error, not a parse error.
func TestShadowError(t *testing.T) {
	h := newShadowHarness(t, "crash")
	h.putDemo(twoRounds)

	if status, body := h.parse(); status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	entry := h.lastLog()
	checkLog(t, entry, map[string]any{"message": "shadow", "severity": "WARNING", "result": "error", "rounds_compared": float64(0)})
	e := fmt.Sprint(entry["error"])
	if !strings.Contains(e, "demoinfocs: boom") {
		t.Errorf("error = %q, want the oracle's", e)
	}
	checkVerdict(t, h, shadowVerdict{Result: "error", RoundSource: "parser", ByCategory: map[string]int{}, First: []string{}, Error: e})
}

// Only our round files are shadowed: after a fallback the oracle's shipped.
func TestShadowSkipsFallback(t *testing.T) {
	h := newShadowHarness(t, "agree")
	h.putDemo(testDemo) // one round, so implausible

	if status, body := h.parse(); status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	if slices.Contains(h.messages(), "shadow") {
		t.Errorf("shadowed a fallback:\n%s", h.logs)
	}
}

// gatedStore holds every Open after the first, which is the shadow
// compare's, until release is closed. It closes opened when one is held.
type gatedStore struct {
	store.Store
	opens           atomic.Int32
	opened, release chan struct{}
}

func (g *gatedStore) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if g.opens.Add(1) > 1 {
		close(g.opened)
		<-g.release
	}
	return g.Store.Open(ctx, key)
}

// The response reaches the client whole while the shadow compare is held,
// and a server shutdown waits for the compare.
func TestShadowRunsAfterTheResponse(t *testing.T) {
	h := newShadowHarness(t, "rounds")
	h.putDemo(twoRounds)
	gate := &gatedStore{Store: h.srv.store, opened: make(chan struct{}), release: make(chan struct{})}
	h.srv.store = gate
	ts := httptest.NewServer(h.srv.routes())
	defer ts.Close()
	var once sync.Once
	release := func() { once.Do(func() { close(gate.release) }) }
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	body := fmt.Sprintf(`{"match_id":%q,"demo_key":%q,"parse_id":%q}`, matchID, demoKey, parseID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/parse", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Parse-Secret", secret)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("no response while the shadow compare is held: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("response body unfinished while the shadow compare is held: %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.ContentLength != int64(len(got)) || resp.Header.Get("X-Parse-Id") != parseID {
		t.Fatalf("status %d, content length %d of %d bytes, X-Parse-Id %q", resp.StatusCode, resp.ContentLength, len(got), resp.Header.Get("X-Parse-Id"))
	}

	select {
	case <-gate.opened:
	case <-ctx.Done():
		t.Fatal("the shadow compare never started")
	}
	if n := h.srv.inFlight.Load(); n != 1 {
		t.Errorf("%d in flight during the shadow compare, want 1", n)
	}
	if slices.Contains(h.messages(), "shadow") {
		t.Error("shadow line logged before the compare ran")
	}
	release()
	ts.Close() // waits for the handler, and so the compare
	checkLog(t, h.lastLog(), map[string]any{"message": "shadow", "result": "match"})
	if n := h.srv.inFlight.Load(); n != 0 {
		t.Errorf("%d in flight after the compare, want 0", n)
	}
}
