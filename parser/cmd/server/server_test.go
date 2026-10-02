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
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/store"
)

const (
	secret  = "test-secret"
	matchID = "0123456789abc"
	demoKey = "demos/0123456789abc.dem"
)

// emptyDemo is a valid demo with no frames: the preamble alone.
var emptyDemo = append([]byte("PBDEMS2\x00"), make([]byte, 8)...)

type harness struct {
	t     *testing.T
	store store.Store
	srv   *server
	logs  *bytes.Buffer
}

func newHarness(t *testing.T, parse ParseFunc, stubDir string) *harness {
	t.Helper()
	h := &harness{t: t, store: store.NewDir(t.TempDir()), logs: &bytes.Buffer{}}
	h.srv = newServer(h.store, secret, parse, stubDir, h.logs)
	h.srv.resetPeak = func() {}
	h.srv.peakMem = func() int64 { return 123 << 20 }
	return h
}

func (h *harness) putDemo(data []byte) {
	h.t.Helper()
	if err := h.store.Put(context.Background(), demoKey, data); err != nil {
		h.t.Fatal(err)
	}
}

// post sends POST /parse and returns the status and decoded JSON body.
func (h *harness) post(secretHeader, body string) (int, []byte) {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/parse", strings.NewReader(body))
	if secretHeader != "" {
		req.Header.Set("X-Parse-Secret", secretHeader)
	}
	rec := httptest.NewRecorder()
	h.srv.routes().ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (h *harness) parse() (int, []byte) {
	return h.post(secret, fmt.Sprintf(`{"match_id":%q,"demo_key":%q}`, matchID, demoKey))
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

// lastLog decodes the last log line.
func (h *harness) lastLog() map[string]any {
	h.t.Helper()
	lines := strings.Split(strings.TrimSpace(h.logs.String()), "\n")
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		h.t.Fatalf("log line %q is not JSON: %v", lines[len(lines)-1], err)
	}
	return entry
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
	return func(context.Context, io.Reader) (match.File, error) {
		t.Error("parse called")
		return match.File{}, nil
	}
}

func TestAuth(t *testing.T) {
	for _, header := range []string{"", "wrong", secret + "x"} {
		h := newHarness(t, neverParse(t), "")
		h.putDemo(emptyDemo)
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
	}
}

func TestParseOK(t *testing.T) {
	h := newHarness(t, parse.Run, "")
	h.putDemo(emptyDemo)

	status, body := h.parse()
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}
	var file match.File
	if err := json.Unmarshal(body, &file); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(emptyDemo)
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

func TestParseLogLine(t *testing.T) {
	h := newHarness(t, parse.Run, "")
	h.putDemo(emptyDemo)
	h.parse()

	entry := h.lastLog()
	want := map[string]any{
		"severity":          "INFO",
		"message":           "parse",
		"match_id":          matchID,
		"demo_key":          demoKey,
		"demo_size":         float64(len(emptyDemo)),
		"peak_memory_bytes": float64(123 << 20),
		"parser_version":    parse.Version,
		"result":            "ok",
		"status":            float64(200),
	}
	for k, v := range want {
		if entry[k] != v {
			t.Errorf("log %s = %v, want %v", k, entry[k], v)
		}
	}
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Errorf("log duration_ms = %v, want a number", entry["duration_ms"])
	}
	for _, k := range []string{"error", "stub"} {
		if _, ok := entry[k]; ok {
			t.Errorf("log has %s on success", k)
		}
	}
	if strings.Count(h.logs.String(), "\n") != 1 {
		t.Errorf("want exactly one log line per parse, got:\n%s", h.logs)
	}
}

func TestParseBadDemo(t *testing.T) {
	h := newHarness(t, parse.Run, "")
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
	h := newHarness(t, parse.Run, "")
	// A preamble, then a frame header that promises more bytes than follow.
	h.putDemo(append(append([]byte{}, emptyDemo...), 0x01, 0x00, 0x7f, 'x'))
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
		h := newHarness(t, parse.Run, "")
		h.putDemo(emptyDemo)
		fs.Store = h.store
		h.srv.store = fs

		status, body := h.parse()
		if status != http.StatusInternalServerError {
			t.Errorf("%+v: status %d, want 500", fs, status)
		}
		errorBody(t, body)
		if entry := h.lastLog(); entry["result"] != "error" || entry["severity"] != "ERROR" {
			t.Errorf("log = %v", entry)
		}
	}
}

func TestParserErrorIs500(t *testing.T) {
	h := newHarness(t, func(context.Context, io.Reader) (match.File, error) {
		return match.File{}, errors.New("decoder bug")
	}, "")
	h.putDemo(emptyDemo)
	if status, _ := h.parse(); status != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", status)
	}
}

func TestParserBadDemoErrorIs422(t *testing.T) {
	h := newHarness(t, func(context.Context, io.Reader) (match.File, error) {
		return match.File{}, fmt.Errorf("frame 9: %w", demo.ErrBadDemo)
	}, "")
	h.putDemo(emptyDemo)
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

	h.putDemo(emptyDemo)
	status, body := h.parse()
	if status != http.StatusOK || string(body) != fixture["match.json"] {
		t.Fatalf("status %d, body %s", status, body)
	}
	for name, data := range fixture {
		if got := string(h.stored("parsed/" + matchID + "/" + name)); got != data {
			t.Errorf("%s = %s, want %s", name, got, data)
		}
	}
	if entry := h.lastLog(); entry["stub"] != true || entry["result"] != "ok" {
		t.Errorf("log = %v", entry)
	}
}

func TestStubModeMissingFixtureIs500(t *testing.T) {
	h := newHarness(t, neverParse(t), t.TempDir())
	h.putDemo(emptyDemo)
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
	if err := run(context.Background(), getenv, io.Discard); err == nil || !strings.Contains(err.Error(), "PARSE_SECRET") {
		t.Errorf("no secret: %v", err)
	}
	env["PARSE_SECRET"] = "s"
	if err := run(context.Background(), getenv, io.Discard); err == nil || !strings.Contains(err.Error(), "R2_BUCKET") {
		t.Errorf("no store config: %v", err)
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
