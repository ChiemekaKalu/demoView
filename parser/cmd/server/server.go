package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/store"
)

// ParseFunc parses a streamed demo into match.json, with stats on how far
// it got even when it fails. Errors caused by a bad demo wrap
// demo.ErrBadDemo.
type ParseFunc func(ctx context.Context, r io.Reader) (match.File, parse.Stats, error)

// server is the parse service. See the spec's "Cloud Run route", which
// holds wherever the container runs.
type server struct {
	store  store.Store
	secret [sha256.Size]byte // hash of PARSE_SECRET
	parse  ParseFunc
	// stubDir, when set, holds match.json and rounds/*.json to copy instead
	// of parsing. M2 deploys the pipeline this way before the parser is done.
	stubDir string
	// oracle, when set, produces the round files (PARSER_ROUNDS=oracle).
	oracle *oracle

	log       *slog.Logger
	inFlight  atomic.Int64 // parses running now
	resetPeak func() bool
	peakMem   func() int64
}

func newServer(st store.Store, secret string, parseFn ParseFunc, stubDir string, log *slog.Logger) *server {
	return &server{
		store:     st,
		secret:    sha256.Sum256([]byte(secret)),
		parse:     parseFn,
		stubDir:   stubDir,
		log:       log,
		resetPeak: resetPeakMemory,
		peakMem:   peakMemory,
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /parse", s.handleParse)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeBody(w, http.StatusOK, []byte(`{"status":"ok"}`))
	})
	return mux
}

// parseRequest is the body of POST /parse.
type parseRequest struct {
	MatchID string `json:"match_id"`
	DemoKey string `json:"demo_key"`
}

// matchIDPattern matches the Worker's ids. The id becomes part of output
// keys, so nothing else is accepted.
var matchIDPattern = regexp.MustCompile(`^[0-9a-z]{1,64}$`)

var (
	errBadRequest = errors.New("bad request")
	errPanic      = errors.New("panic")
)

// maxNotes caps the match logic notes logged for one parse.
const maxNotes = 20

func (s *server) handleParse(w http.ResponseWriter, r *http.Request) {
	given := sha256.Sum256([]byte(r.Header.Get("X-Parse-Secret")))
	if subtle.ConstantTimeCompare(given[:], s.secret[:]) != 1 {
		s.log.Warn("unauthorized parse request")
		writeBody(w, http.StatusUnauthorized, errorJSON("missing or wrong X-Parse-Secret"))
		return
	}

	start := time.Now()
	req, err := decodeRequest(w, r)
	attempt, _ := strconv.Atoi(r.Header.Get("X-Attempt"))
	log := slog.New(s.log.Handler().WithAttrs(nonzero(
		slog.String("match_id", req.MatchID),
		slog.String("demo_key", req.DemoKey),
		slog.Int("attempt", attempt),
		slog.String("request_id", r.Header.Get("X-Request-Id")),
	)))
	var res runResult
	if err == nil {
		res, err = s.track(r.Context(), log, req)
	}
	status, result, level := classify(err)
	log.LogAttrs(r.Context(), level, "parse", s.summary(start, status, result, err, res)...)

	body := res.matchJSON
	switch {
	case status >= 500:
		// The details stay in the log line: the Worker shows this text on
		// the public match page, and store errors name the account.
		body = errorJSON(fmt.Sprintf("parser %s (see parser logs for match %s)", result, req.MatchID))
	case err != nil:
		body = errorJSON(err.Error())
	}
	if err := writeBody(w, status, body); err != nil {
		log.Warn("response write failed", "status", status, "bytes", len(body), "error", err)
	}
}

// decodeRequest reads and checks the body of POST /parse. Its errors wrap
// errBadRequest.
func decodeRequest(w http.ResponseWriter, r *http.Request) (parseRequest, error) {
	var req parseRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		return req, fmt.Errorf("%w: body must be JSON: %v", errBadRequest, err)
	}
	if !matchIDPattern.MatchString(req.MatchID) || req.DemoKey == "" {
		return req, fmt.Errorf("%w: body needs a match_id of [0-9a-z] and a demo_key", errBadRequest)
	}
	return req, nil
}

// track runs one parse after its start line, counting it in flight and
// measuring its peak memory.
func (s *server) track(ctx context.Context, log *slog.Logger, req parseRequest) (runResult, error) {
	n := s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	log.Info("parse start", "parser_version", parse.Version, "stub", s.stubDir != "", "in_flight", n)

	res := runResult{peakScope: "process"}
	if s.resetPeak() {
		res.peakScope = "parse"
	}
	err := s.runRecovered(ctx, req, &res)
	res.peakMem, res.inFlight = s.peakMem(), s.inFlight.Load()
	if notes := res.parse.Notes; len(notes) > 0 {
		log.Warn("match logic notes", "count", len(notes), "notes", notes[:min(len(notes), maxNotes)])
	}
	return res, err
}

// runResult is what one parse shipped and measured, for the response and
// the summary line.
type runResult struct {
	matchJSON []byte
	source    string // whose match.json shipped: "parser" or "oracle"
	demoSize  int64
	parse     parse.Stats
	store     storeStats
	stack     string // where the parse panicked

	inFlight  int64  // parses running as this one ended, itself included
	peakScope string // "parse", or "process" where the peak couldn't be reset
	peakMem   int64

	oracleMS     int64
	oraclePeak   int64
	oracleStderr string
}

// runRecovered is run with a panic turned into an error wrapping errPanic,
// so the parse still gets its summary line, with the stack, and a 500.
func (s *server) runRecovered(ctx context.Context, req parseRequest, res *runResult) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err, res.stack = fmt.Errorf("%w: %v", errPanic, p), string(debug.Stack())
		}
	}()
	return s.run(ctx, req, res)
}

// run produces the parse output for req, writes it to the store and fills
// in res. Every output key is overwritten, so a retry is safe. Round files
// are written first and match.json last, so its presence means the round
// files are there too.
func (s *server) run(ctx context.Context, req parseRequest, res *runResult) error {
	st := timedStore{s.store, &res.store}
	if s.stubDir != "" {
		return s.runStub(ctx, st, req, res)
	}
	rc, size, err := st.Open(ctx, req.DemoKey)
	if err != nil {
		return err
	}
	defer rc.Close() // even if the parse panics
	res.demoSize, res.source = size, "parser"
	file, stats, err := s.parse(ctx, rc)
	res.parse = stats
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := match.Write(&buf, file); err != nil {
		return err
	}
	res.matchJSON = buf.Bytes()

	var rounds []outFile
	if s.oracle != nil {
		if rounds, err = s.oracleRounds(ctx, st, req, res); err != nil {
			return err
		}
	}
	for _, f := range rounds {
		if err := st.Put(ctx, outputKey(req.MatchID, "rounds/"+f.name), f.data); err != nil {
			return err
		}
	}
	return st.Put(ctx, outputKey(req.MatchID, match.FileName), res.matchJSON)
}

// oracleRounds streams the demo a second time into the oracle and returns
// its round files. Round files index match.json's players and kills, so
// ours ships only when the consistency guard finds it agrees with the
// oracle's; otherwise the oracle's match.json ships with them, and a
// warning names the first difference.
func (s *server) oracleRounds(ctx context.Context, st store.Store, req parseRequest, res *runResult) ([]outFile, error) {
	start := time.Now()
	rc, _, err := st.Open(ctx, req.DemoKey)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	out, err := s.oracle.export(ctx, rc)
	res.oracleMS = time.Since(start).Milliseconds()
	res.oraclePeak, res.oracleStderr = out.peakMem, out.stderr
	if err != nil {
		// Our parser accepted this demo, so the oracle failing is not the
		// demo's fault: a 500, which the queue retries.
		return nil, err
	}
	if why := firstMismatch(res.matchJSON, out.matchJSON); why != "" {
		res.matchJSON, res.source = out.matchJSON, "oracle"
		s.log.Warn("match.json disagrees with the oracle's; shipping the oracle's", "match_id", req.MatchID, "mismatch", why)
	}
	return out.rounds, nil
}

// runStub checks the demo exists, then copies the fixture files to the
// match's output keys as they are.
func (s *server) runStub(ctx context.Context, st store.Store, req parseRequest, res *runResult) error {
	size, err := st.Stat(ctx, req.DemoKey)
	if err != nil {
		return err
	}
	res.demoSize = size
	matchJSON, err := os.ReadFile(filepath.Join(s.stubDir, match.FileName))
	if err != nil {
		return fmt.Errorf("stub: %w", err)
	}
	rounds, err := filepath.Glob(filepath.Join(s.stubDir, "rounds", "*.json"))
	if err != nil {
		return fmt.Errorf("stub: %w", err)
	}
	for _, p := range rounds {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("stub: %w", err)
		}
		if err := st.Put(ctx, outputKey(req.MatchID, "rounds/"+filepath.Base(p)), data); err != nil {
			return err
		}
	}
	if err := st.Put(ctx, outputKey(req.MatchID, match.FileName), matchJSON); err != nil {
		return err
	}
	res.matchJSON = matchJSON
	return nil
}

// outputKey is a key under parsed/{match_id}/ in the spec's R2 layout.
func outputKey(matchID, name string) string {
	return path.Join("parsed", matchID, name)
}

// classify maps a parse error to the HTTP status the Worker acts on, and to
// the summary line's result and level. 422 means the demo is bad and must
// not be retried; 5xx means try again.
func classify(err error) (status int, result string, level slog.Level) {
	switch {
	case err == nil:
		return http.StatusOK, "ok", slog.LevelInfo
	case errors.Is(err, errBadRequest):
		return http.StatusBadRequest, "bad_request", slog.LevelWarn
	case errors.Is(err, errPanic):
		return http.StatusInternalServerError, "panic", slog.LevelError
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The caller has gone, so the status is only for the log.
		return http.StatusServiceUnavailable, "canceled", slog.LevelWarn
	case errors.Is(err, store.ErrNotFound):
		return http.StatusUnprocessableEntity, "demo_not_found", slog.LevelWarn
	case errors.Is(err, demo.ErrBadDemo):
		return http.StatusUnprocessableEntity, "bad_demo", slog.LevelWarn
	}
	return http.StatusInternalServerError, "error", slog.LevelError
}

// writeBody sends a JSON response and reports whether it reached the
// connection.
func writeBody(w http.ResponseWriter, status int, body []byte) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err := w.Write(body)
	return err
}

// errorJSON is the body of an error response.
func errorJSON(msg string) []byte {
	body, _ := json.Marshal(map[string]string{"error": msg})
	return body
}
