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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/parse/parsetest"
	"github.com/ChiemekaKalu/demoview/parser/store"
)

const (
	secret  = "test-secret"
	matchID = "0123456789abc"
	demoKey = "demos/0123456789abc.dem"
)

// testDemo is a valid one-round demo.
var testDemo = parsetest.Match()

type harness struct {
	t     *testing.T
	store store.Store
	srv   *server
	logs  *bytes.Buffer
}

func newHarness(t *testing.T, parse ParseFunc, stubDir string) *harness {
	t.Helper()
	h := &harness{t: t, store: store.NewDir(t.TempDir()), logs: &bytes.Buffer{}}
	h.srv = newServer(h.store, secret, parse, stubDir, newLogger(h.logs))
	h.srv.resetPeak = func() bool { return true }
	h.srv.peakMem = func() int64 { return 123 << 20 }
	return h
}

func (h *harness) putDemo(data []byte) {
	h.t.Helper()
	if err := h.store.Put(context.Background(), demoKey, data); err != nil {
		h.t.Fatal(err)
	}
}

// post sends POST /parse and returns the status and body.
func (h *harness) post(secretHeader, body string) (int, []byte) {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/parse", strings.NewReader(body))
	if secretHeader != "" {
		req.Header.Set("X-Parse-Secret", secretHeader)
	}
	return h.do(req)
}

func (h *harness) do(req *http.Request) (int, []byte) {
	rec := httptest.NewRecorder()
	h.srv.routes().ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// newParseRequest returns an authorized POST /parse for the test demo.
func newParseRequest(ctx context.Context) *http.Request {
	body := fmt.Sprintf(`{"match_id":%q,"demo_key":%q}`, matchID, demoKey)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/parse", strings.NewReader(body))
	req.Header.Set("X-Parse-Secret", secret)
	return req
}

func (h *harness) parse() (int, []byte) {
	return h.do(newParseRequest(context.Background()))
}

func (h *harness) stored(key string) []byte {
	h.t.Helper()
	rc, _, err := h.store.Open(context.Background(), key)
	if err != nil {
		h.t.Fatalf("Open(%s): %v", key, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		h.t.Fatal(err)
	}
	return data
}

// logLines returns every log line decoded.
func (h *harness) logLines() []map[string]any {
	h.t.Helper()
	return decodeLines(h.t, h.logs.String())
}

func decodeLines(t *testing.T, logs string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// lastLog decodes the last log line.
func (h *harness) lastLog() map[string]any {
	h.t.Helper()
	lines := h.logLines()
	return lines[len(lines)-1]
}

// messages lists the message of every log line.
func (h *harness) messages() []string {
	h.t.Helper()
	var out []string
	for _, l := range h.logLines() {
		out = append(out, fmt.Sprint(l["message"]))
	}
	return out
}

// checkLog reports each field of entry that differs from want. A nil in
// want means the field must be absent.
func checkLog(t *testing.T, entry, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if got, ok := entry[k]; got != v || (v == nil && ok) {
			t.Errorf("log %s = %v, want %v", k, got, v)
		}
	}
}

func errorBody(t *testing.T, body []byte) string {
	t.Helper()
	var e struct{ Error string }
	if err := json.Unmarshal(body, &e); err != nil || e.Error == "" {
		t.Fatalf("body %s is not {\"error\": ...}", body)
	}
	return e.Error
}

func neverParse(t *testing.T) ParseFunc {
	return func(context.Context, io.Reader) (match.File, parse.Stats, error) {
		t.Error("parse called")
		return match.File{}, parse.Stats{}, nil
	}
}

func failParse(err error) ParseFunc {
	return func(context.Context, io.Reader) (match.File, parse.Stats, error) {
		return match.File{}, parse.Stats{}, err
	}
}

func TestAuth(t *testing.T) {
	for _, header := range []string{"", "wrong", secret + "x"} {
		h := newHarness(t, neverParse(t), "")
		h.putDemo(testDemo)
		status, body := h.post(header, fmt.Sprintf(`{"match_id":%q,"demo_key":%q}`, matchID, demoKey))
		if status != http.StatusUnauthorized {
			t.Errorf("secret %q: status %d, want 401", header, status)
		}
		errorBody(t, body)
		if got := h.lastLog()["message"]; got != "unauthorized parse request" {
			t.Errorf("secret %q: log message %v", header, got)
		}
	}
}

func TestBadRequest(t *testing.T) {
	for _, body := range []string{
		`not json`,
		`{"demo_key":"demos/x.dem"}`,
		`{"match_id":"../etc","demo_key":"demos/x.dem"}`,
		`{"match_id":"abc"}`,
	} {
		h := newHarness(t, neverParse(t), "")
		if status, _ := h.post(secret, body); status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, status)
		}
		if msgs := h.messages(); len(msgs) != 1 || msgs[0] != "parse" {
			t.Errorf("%s: logged %q, want only the summary line", body, msgs)
		}
		checkLog(t, h.lastLog(), map[string]any{"result": "bad_request", "status": float64(400), "severity": "WARNING"})
	}
}

func TestParseOK(t *testing.T) {
	h := newHarness(t, parse.RunStats, "")
	h.putDemo(testDemo)

	status, body := h.parse()
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	var file match.File
	if err := json.Unmarshal(body, &file); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(testDemo)
	if file.Match.DemoSHA256 != hex.EncodeToString(sum[:]) || file.SchemaVersion != 1 || file.ParserVersion != parse.Version {
		t.Errorf("match.json = %+v", file)
	}
	if stored := h.stored("parsed/" + matchID + "/match.json"); !bytes.Equal(stored, body) {
		t.Errorf("stored match.json differs from the response:\n%s\n%s", stored, body)
	}

	// A retry overwrites the same key.
	if status, _ := h.parse(); status != http.StatusOK {
		t.Errorf("retry status %d", status)
	}
}

func TestParseLogLines(t *testing.T) {
	h := newHarness(t, parse.RunStats, "")
	h.putDemo(testDemo)
	req := newParseRequest(context.Background())
	req.Header.Set("X-Attempt", "2")
	req.Header.Set("X-Request-Id", "msg-1")
	_, body := h.do(req)

	lines := h.logLines()
	if first, last := lines[0]["message"], lines[len(lines)-1]["message"]; first != "parse start" || last != "parse" {
		t.Fatalf("log runs from %v to %v, want parse start to parse", first, last)
	}
	ids := map[string]any{"match_id": matchID, "demo_key": demoKey, "attempt": float64(2), "request_id": "msg-1"}
	checkLog(t, lines[0], ids)
	checkLog(t, lines[0], map[string]any{"severity": "INFO", "parser_version": parse.Version, "in_flight": float64(1)})

	entry := h.lastLog()
	checkLog(t, entry, ids)
	sum := sha256.Sum256(testDemo)
	checkLog(t, entry, map[string]any{
		"severity":             "INFO",
		"event":                "parse",
		"result":               "ok",
		"status":               float64(200),
		"parser_version":       parse.Version,
		"in_flight":            float64(1),
		"peak_memory_bytes":    float64(123 << 20),
		"peak_scope":           "parse",
		"demo_size":            float64(len(testDemo)),
		"bytes_read":           float64(len(testDemo)),
		"frames":               float64(8),
		"last_tick":            float64(400),
		"put_objects":          float64(1),
		"put_bytes":            float64(len(body)),
		"match_source":         "parser",
		"map":                  "de_mirage",
		"demo_sha256":          hex.EncodeToString(sum[:]),
		"players":              float64(2),
		"rounds":               float64(1),
		"kills":                float64(1),
		"error":                nil,
		"stack":                nil,
		"stub":                 nil,
		"oracle_ms":            nil,
		"match_parser_version": nil,
	})
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Errorf("log duration_ms = %v, want a number", entry["duration_ms"])
	}
	if n := strings.Count(h.logs.String(), `"message":"parse",`); n != 1 {
		t.Errorf("%d summary lines, want 1:\n%s", n, h.logs)
	}
}

func TestPeakScope(t *testing.T) {
	h := newHarness(t, parse.RunStats, "")
	h.putDemo(testDemo)
	h.srv.resetPeak = func() bool { return false }
	h.parse()
	if got := h.lastLog()["peak_scope"]; got != "process" {
		t.Errorf("peak_scope = %v, want process when the peak can't be reset", got)
	}
}

func TestNotesAreCapped(t *testing.T) {
	notes := make([]string, 25)
	for i := range notes {
		notes[i] = fmt.Sprintf("note %d", i)
	}
	h := newHarness(t, func(context.Context, io.Reader) (match.File, parse.Stats, error) {
		return match.File{}, parse.Stats{Notes: notes}, fmt.Errorf("%w: no live rounds", demo.ErrBadDemo)
	}, "")
	h.putDemo(testDemo)
	h.parse()

	lines := h.logLines()
	n := lines[len(lines)-2]
	logged, _ := n["notes"].([]any)
	if n["message"] != "match logic notes" || n["count"] != float64(25) || len(logged) != 20 || n["match_id"] != matchID {
		t.Errorf("notes line = %v, want 20 of 25 notes", n)
	}
}

func TestParseBadDemo(t *testing.T) {
	h := newHarness(t, parse.RunStats, "")
	h.putDemo([]byte("this is not a demo at all"))

	status, body := h.parse()
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body %s", status, body)
	}
	if msg := errorBody(t, body); !strings.Contains(msg, "bad magic") {
		t.Errorf("error = %q, want the demo error", msg)
	}
	if entry := h.lastLog(); entry["result"] != "bad_demo" || entry["severity"] != "WARNING" {
		t.Errorf("log = %v", entry)
	}
	if _, _, err := h.store.Open(context.Background(), "parsed/"+matchID+"/match.json"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("match.json was written for a bad demo: %v", err)
	}
}

func TestParseTruncatedDemo(t *testing.T) {
	h := newHarness(t, parse.RunStats, "")
	// A preamble, then a frame header that promises more bytes than follow.
	h.putDemo(append(append([]byte{}, testDemo...), 0x01, 0x00, 0x7f, 'x'))
	if status, body := h.parse(); status != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422; body %s", status, body)
	}
}

func TestParseMissingDemo(t *testing.T) {
	h := newHarness(t, neverParse(t), "")
	status, body := h.parse()
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", status)
	}
	if msg := errorBody(t, body); !strings.Contains(msg, demoKey) {
		t.Errorf("error = %q, want it to name the key", msg)
	}
	if entry := h.lastLog(); entry["result"] != "demo_not_found" {
		t.Errorf("log result = %v", entry["result"])
	}
}

// failingStore wraps a Store and fails the chosen operation.
type failingStore struct {
	store.Store
	failOpen, failPut bool
}

var errStoreDown = errors.New("store unavailable")

func (f *failingStore) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if f.failOpen {
		return nil, 0, errStoreDown
	}
	return f.Store.Open(ctx, key)
}

func (f *failingStore) Put(ctx context.Context, key string, data []byte) error {
	if f.failPut {
		return errStoreDown
	}
	return f.Store.Put(ctx, key, data)
}

func TestStoreFailureIs500(t *testing.T) {
	for _, fs := range []*failingStore{{failOpen: true}, {failPut: true}} {
		h := newHarness(t, parse.RunStats, "")
		h.putDemo(testDemo)
		fs.Store = h.store
		h.srv.store = fs

		status, body := h.parse()
		if status != http.StatusInternalServerError {
			t.Errorf("%+v: status %d, want 500", fs, status)
		}
		// The store's error is for the log only: the Worker shows the body
		// on a public page.
		if msg := errorBody(t, body); msg != "parser error (see parser logs for match "+matchID+")" {
			t.Errorf("%+v: body error %q, want the generic one", fs, msg)
		}
		checkLog(t, h.lastLog(), map[string]any{"result": "error", "severity": "ERROR", "error": errStoreDown.Error()})
	}
}

func TestPanicIs500AndServingGoesOn(t *testing.T) {
	h := newHarness(t, func(context.Context, io.Reader) (match.File, parse.Stats, error) {
		panic("decoder bug")
	}, "")
	h.putDemo(testDemo)

	status, body := h.parse()
	if status != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", status)
	}
	if msg := errorBody(t, body); strings.Contains(msg, "decoder bug") {
		t.Errorf("body error %q shows the panic", msg)
	}
	entry := h.lastLog()
	checkLog(t, entry, map[string]any{"message": "parse", "result": "panic", "status": float64(500), "severity": "ERROR", "error": "panic: decoder bug"})
	if stack, _ := entry["stack"].(string); !strings.Contains(stack, "TestPanicIs500AndServingGoesOn") {
		t.Errorf("stack = %q, want the panicking function", stack)
	}

	h.srv.parse = parse.RunStats
	if status, body := h.parse(); status != http.StatusOK {
		t.Errorf("next parse: status %d, body %s", status, body)
	}
	if n := h.srv.inFlight.Load(); n != 0 {
		t.Errorf("%d in flight after both parses ended", n)
	}
}

func TestCanceledIsAWarning(t *testing.T) {
	h := newHarness(t, parse.RunStats, "")
	h.putDemo(testDemo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.do(newParseRequest(ctx))
	checkLog(t, h.lastLog(), map[string]any{"result": "canceled", "status": float64(503), "severity": "WARNING"})
}

// brokenWriter is a ResponseWriter whose connection has gone.
type brokenWriter struct{ *httptest.ResponseRecorder }

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestResponseWriteFailureIsLogged(t *testing.T) {
	h := newHarness(t, parse.RunStats, "")
	h.putDemo(testDemo)
	h.srv.routes().ServeHTTP(brokenWriter{httptest.NewRecorder()}, newParseRequest(context.Background()))
	checkLog(t, h.lastLog(), map[string]any{
		"message":  "response write failed",
		"severity": "WARNING",
		"match_id": matchID,
		"status":   float64(200),
		"error":    "broken pipe",
	})
}

func TestParserErrorIs500(t *testing.T) {
	h := newHarness(t, failParse(errors.New("decoder bug")), "")
	h.putDemo(testDemo)
	if status, _ := h.parse(); status != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", status)
	}
}

func TestParserBadDemoErrorIs422(t *testing.T) {
	h := newHarness(t, failParse(fmt.Errorf("frame 9: %w", demo.ErrBadDemo)), "")
	h.putDemo(testDemo)
	if status, _ := h.parse(); status != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422", status)
	}
}

func TestStubMode(t *testing.T) {
	dir := t.TempDir()
	fixture := map[string]string{
		"match.json":     `{"schema_version":1,"fixture":true}`,
		"rounds/01.json": `{"round":1}`,
		"rounds/02.json": `{"round":2}`,
	}
	for name, data := range fixture {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	h := newHarness(t, neverParse(t), dir)
	if status, _ := h.parse(); status != http.StatusUnprocessableEntity {
		t.Errorf("stub with no demo: status %d, want 422", status)
	}

	h.putDemo(testDemo)
	status, body := h.parse()
	if status != http.StatusOK || string(body) != fixture["match.json"] {
		t.Fatalf("status %d, body %s", status, body)
	}
	for name, data := range fixture {
		if got := string(h.stored("parsed/" + matchID + "/" + name)); got != data {
			t.Errorf("%s = %s, want %s", name, got, data)
		}
	}
	checkLog(t, h.lastLog(), map[string]any{"stub": true, "result": "ok", "put_objects": float64(3), "match_parser_version": nil})
}

func TestStubModeMissingFixtureIs500(t *testing.T) {
	h := newHarness(t, neverParse(t), t.TempDir())
	h.putDemo(testDemo)
	if status, _ := h.parse(); status != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", status)
	}
}

func TestHealthz(t *testing.T) {
	h := newHarness(t, neverParse(t), "")
	rec := httptest.NewRecorder()
	h.srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status %d", rec.Code)
	}
}

func TestParseRejectsGet(t *testing.T) {
	h := newHarness(t, neverParse(t), "")
	rec := httptest.NewRecorder()
	h.srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/parse", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", rec.Code)
	}
}

func TestRunNeedsSecretAndStore(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	discard := slog.New(slog.DiscardHandler)
	if err := run(context.Background(), getenv, discard); err == nil || !strings.Contains(err.Error(), "PARSE_SECRET") {
		t.Errorf("no secret: %v", err)
	}
	env["PARSE_SECRET"] = "s"
	if err := run(context.Background(), getenv, discard); err == nil || !strings.Contains(err.Error(), "R2_BUCKET") {
		t.Errorf("no store config: %v", err)
	}
}

func TestRunLogsStartAndShutdown(t *testing.T) {
	env := map[string]string{
		"PORT":                 "0",
		"PARSE_SECRET":         "sentinel-parse-secret",
		"R2_ACCOUNT_ID":        "sentinel-account",
		"R2_ACCESS_KEY_ID":     "sentinel-key-id",
		"R2_SECRET_ACCESS_KEY": "sentinel-secret-key",
		"R2_BUCKET":            "sentinel-bucket",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shut down as soon as it is listening
	var logs bytes.Buffer
	if err := run(ctx, func(k string) string { return env[k] }, newLogger(&logs)); err != nil {
		t.Fatal(err)
	}

	lines := decodeLines(t, logs.String())
	if len(lines) != 3 {
		t.Fatalf("want listening, shutdown and stopped, got:\n%s", &logs)
	}
	listening := lines[0]
	checkLog(t, listening, map[string]any{"message": "listening", "parser_version": parse.Version, "store": "r2", "rounds": "none", "stub": false})
	for _, k := range []string{"port", "gomaxprocs"} {
		if n, _ := listening[k].(float64); n <= 0 {
			t.Errorf("listening %s = %v, want a positive number", k, listening[k])
		}
	}
	if _, ok := listening["peak_reset"].(bool); !ok {
		t.Errorf("listening peak_reset = %v, want a bool", listening["peak_reset"])
	}
	checkLog(t, lines[1], map[string]any{"message": "shutdown", "in_flight": float64(0)})
	checkLog(t, lines[2], map[string]any{"message": "stopped", "severity": "INFO"})
	if strings.Contains(logs.String(), "sentinel") {
		t.Errorf("logs show the store config or a secret:\n%s", &logs)
	}
}

func TestParseVmHWM(t *testing.T) {
	status := []byte("Name:\tserver\nVmPeak:\t  900000 kB\nVmHWM:\t   51234 kB\nVmRSS:\t   40000 kB\n")
	if n, ok := parseVmHWM(status); !ok || n != 51234*1024 {
		t.Errorf("parseVmHWM = %d, %v", n, ok)
	}
	if _, ok := parseVmHWM([]byte("Name:\tserver\n")); ok {
		t.Error("parseVmHWM found VmHWM in a status without it")
	}
	if peakMemory() <= 0 {
		t.Error("peakMemory() <= 0")
	}
}
