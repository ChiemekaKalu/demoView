package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	"github.com/ChiemekaKalu/demoview/parser/roundfile"
	"github.com/ChiemekaKalu/demoview/parser/store"
)

// ParseFunc parses a streamed demo as parse.Parse does: match.json, and
// round files when opt.Rounds is set, with stats on how far it got even
// when it fails. Errors caused by a bad demo wrap demo.ErrBadDemo.
type ParseFunc func(ctx context.Context, r io.Reader, opt parse.Options) (parse.Output, parse.Stats, error)

// server is the parse service. See the spec's "Cloud Run route", which
// holds wherever the container runs.
type server struct {
	store  store.Store
	secret [sha256.Size]byte // hash of PARSE_SECRET
	parse  ParseFunc
	// stubDir, when set, holds match.json and rounds/*.json to copy instead
	// of parsing. M2 deploys the pipeline this way before the parser is done.
	stubDir string
	// parserRounds ships our own round files (PARSER_ROUNDS=parser).
	parserRounds bool
	// oracle, when set, produces the round files (PARSER_ROUNDS=oracle). With
	// parserRounds, it stands in for ours when they can't be trusted.
	oracle *oracle
	// shadow compares each parse that ships our round files with the
	// oracle's output, after the response (PARSER_SHADOW=oracle).
	shadow bool
	// build identifies the parser build (PARSER_BUILD), which reparses key on.
	build string

	log       *slog.Logger
	inFlight  atomic.Int64 // parses and shadow compares running now
	resetPeak func() bool
	peakMem   func() int64
}

func newServer(st store.Store, secret string, parseFn ParseFunc, stubDir string, log *slog.Logger) *server {
	return &server{
		store:     st,
		secret:    sha256.Sum256([]byte(secret)),
		parse:     parseFn,
		stubDir:   stubDir,
		build:     "dev",
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

// parseRequest is the body of POST /parse. The Worker picks a new ParseID for
// every attempt, and the output goes under parsed/{match_id}/{parse_id}/.
type parseRequest struct {
	MatchID string `json:"match_id"`
	DemoKey string `json:"demo_key"`
	ParseID string `json:"parse_id"`
}

// prefix is where the parse's files go: parsed/{match_id}/{parse_id}/.
func (req parseRequest) prefix() string {
	return path.Join("parsed", req.MatchID, req.ParseID) + "/"
}

// idPattern matches the Worker's match and parse ids. Both become part of
// output keys, so nothing else is accepted.
var idPattern = regexp.MustCompile(`^[0-9a-z]{1,64}$`)

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
		writeBody(w, http.StatusUnauthorized, errorJSON("unauthorized", "missing or wrong X-Parse-Secret"))
		return
	}

	start := time.Now()
	req, err := decodeRequest(w, r)
	attempt, _ := strconv.Atoi(r.Header.Get("X-Attempt"))
	log := slog.New(s.log.Handler().WithAttrs(nonzero(
		slog.String("match_id", req.MatchID),
		slog.String("parse_id", req.ParseID),
		slog.String("demo_key", req.DemoKey),
		slog.Int("attempt", attempt),
		slog.String("request_id", r.Header.Get("X-Request-Id")),
	)))
	var res runResult
	if err == nil {
		// The Worker treats a 200 without its parse id as an old instance.
		w.Header().Set("X-Parse-Id", req.ParseID)
		res, err = s.track(r.Context(), log, req)
	}
	status, result, level := classify(err)
	log.LogAttrs(r.Context(), level, "parse", s.summary(start, status, result, err, res)...)

	body := res.matchJSON
	switch {
	case status >= 500:
		// The details stay in the log line: the Worker shows this text on
		// the public match page, and store errors name the account.
		body = errorJSON(result, fmt.Sprintf("parser %s (see parser logs for match %s)", result, req.MatchID))
	case err != nil:
		body = errorJSON(result, err.Error())
	default:
		w.Header().Set("X-Manifest-Sha256", res.manifestSHA)
	}
	if err := writeBody(w, status, body); err != nil {
		log.Warn("response write failed", "status", status, "bytes", len(body), "error", err)
	}
	if s.shadow && err == nil && res.roundSource == "parser" {
		s.shadowCompare(r.Context(), log, req, res)
	}
}

// decodeRequest reads and checks the body of POST /parse. Its errors wrap
// errBadRequest.
func decodeRequest(w http.ResponseWriter, r *http.Request) (parseRequest, error) {
	var req parseRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		return req, fmt.Errorf("%w: body must be JSON: %v", errBadRequest, err)
	}
	if !idPattern.MatchString(req.MatchID) || !idPattern.MatchString(req.ParseID) || req.DemoKey == "" {
		return req, fmt.Errorf("%w: body needs a match_id and a parse_id of [0-9a-z], and a demo_key", errBadRequest)
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
	matchJSON   []byte
	source      string // whose match.json shipped: "parser", "oracle" or "stub"
	roundSource string // whose round files shipped: "parser", "oracle", "stub" or "none"
	fallback    string // why the configured round source didn't ship, or ""
	rounds      []outFile
	manifestSHA string
	demoSize    int64
	parse       parse.Stats
	store       storeStats
	stack       string // where the parse panicked

	inFlight  int64  // parses and shadow compares running as this one ended, itself included
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

// run produces the parse output for req, writes it under the parse's prefix
// and fills in res. Round files go first, then match.json, then the manifest.
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
	res.demoSize, res.source, res.roundSource = size, "parser", "none"
	out, stats, err := s.parse(ctx, rc, parse.Options{Rounds: s.parserRounds})
	res.parse = stats
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := match.Write(&buf, out.Match); err != nil {
		return err
	}
	res.matchJSON = buf.Bytes()

	if res.rounds, err = s.roundFiles(ctx, st, req, res, out); err != nil {
		return err
	}
	return s.write(ctx, st, req, res, res.rounds)
}

// roundFiles returns the round files to ship and sets res.roundSource.
// PARSER_ROUNDS=oracle ships the oracle's. PARSER_ROUNDS=parser ships ours
// unless fallbackReason finds a reason not to, which a warning names; then
// the oracle's ship if there is an oracle, and none if not.
//
// When the oracle fails on a demo our parser accepted in oracle mode, as it
// does on a recording that never finishes a match, ours ship instead if
// they pass fallbackReason, as "oracle_failed": there is nothing to check
// them against, and a partial match beats none. That takes one more parse,
// with round files. Otherwise the oracle's error stands.
func (s *server) roundFiles(ctx context.Context, st store.Store, req parseRequest, res *runResult, out parse.Output) ([]outFile, error) {
	if s.parserRounds {
		reason := fallbackReason(out, s.oracle != nil)
		if reason == "" {
			res.roundSource = "parser"
			return encodeRounds(out.Rounds)
		}
		res.fallback = reason
		source := "none"
		if s.oracle != nil {
			source = "oracle"
		}
		attrs := []any{"match_id", req.MatchID, "parse_id", req.ParseID, "fallback_reason", reason,
			"round_source", source, "live_rounds", len(out.Match.Rounds), "kills", len(out.Match.Kills)}
		if out.RoundsErr != nil {
			attrs = append(attrs, "entity_error", out.RoundsErr.Error())
		}
		s.log.Warn("round file fallback", attrs...)
	}
	if s.oracle == nil {
		return nil, nil
	}
	rounds, err := s.oracleRounds(ctx, st, req, res)
	switch {
	case err == nil:
		res.roundSource = "oracle"
		return rounds, nil
	case res.fallback != "" || ctx.Err() != nil:
		return nil, err
	}
	full, perr := s.parseWithRounds(ctx, st, req.DemoKey)
	if perr != nil || fallbackReason(full, true) != "" {
		return nil, err
	}
	res.fallback, res.roundSource = "oracle_failed", "parser"
	s.log.Warn("oracle failed; shipping our files", "match_id", req.MatchID, "parse_id", req.ParseID,
		"fallback_reason", res.fallback, "error", err.Error())
	return encodeRounds(full.Rounds)
}

// parseWithRounds parses the demo at key again, with round files.
func (s *server) parseWithRounds(ctx context.Context, st store.Store, key string) (parse.Output, error) {
	rc, _, err := st.Open(ctx, key)
	if err != nil {
		return parse.Output{}, err
	}
	defer rc.Close()
	out, _, err := s.parse(ctx, rc, parse.Options{Rounds: true})
	return out, err
}

// fallbackReason says why our round files must not ship, or "" if they
// may: "entity_error" when the entity stage failed, or, when an oracle can
// stand in, "implausible_match" when match logic found fewer than 2 live
// rounds or no kills. Every real match has more, and a round segmentation
// bug would look like that.
func fallbackReason(out parse.Output, haveOracle bool) string {
	switch {
	case out.RoundsErr != nil:
		return "entity_error"
	case haveOracle && (len(out.Match.Rounds) < 2 || len(out.Match.Kills) == 0):
		return "implausible_match"
	}
	return ""
}

// encodeRounds encodes our round files as rounds/NN.json.
func encodeRounds(files []roundfile.File) ([]outFile, error) {
	out := make([]outFile, len(files))
	for i, f := range files {
		var buf bytes.Buffer
		if err := roundfile.Write(&buf, f); err != nil {
			return nil, err
		}
		out[i] = outFile{name: roundfile.Name(f.Round), data: buf.Bytes()}
	}
	return out, nil
}

// write puts the round files and match.json under the parse's prefix, then
// the manifest listing them, which marks the parse complete.
func (s *server) write(ctx context.Context, st store.Store, req parseRequest, res *runResult, rounds []outFile) error {
	out := &output{st: st, prefix: req.prefix()}
	for _, f := range rounds {
		if err := out.put(ctx, "rounds/"+f.name, f.data); err != nil {
			return err
		}
	}
	if err := out.put(ctx, match.FileName, res.matchJSON); err != nil {
		return err
	}
	facts := factsOf(res.matchJSON)
	sum, err := out.commit(ctx, manifest{
		ParseID:       req.ParseID,
		MatchID:       req.MatchID,
		ParsedBy:      s.build,
		ParserVersion: facts.ParserVersion,
		SchemaVersion: facts.SchemaVersion,
		MatchSource:   res.source,
		RoundSource:   res.roundSource,
		Fallback:      res.fallback,
	})
	res.manifestSHA = sum
	return err
}

// oracleRounds streams the demo a second time into the oracle and returns
// its round files. Round files index match.json's players and kills, so
// ours ships only when the consistency guard finds it agrees with the
// oracle's; otherwise the oracle's match.json ships with them, and a
// warning names the first difference.
func (s *server) oracleRounds(ctx context.Context, st store.Store, req parseRequest, res *runResult) ([]outFile, error) {
	start := time.Now()
	out, err := s.runOracle(ctx, st, req.DemoKey)
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

// runOracle streams the demo at key from st into the oracle.
func (s *server) runOracle(ctx context.Context, st store.Store, key string) (oracleOutput, error) {
	rc, _, err := st.Open(ctx, key)
	if err != nil {
		return oracleOutput{}, err
	}
	defer rc.Close()
	return s.oracle.export(ctx, rc)
}

// runStub checks the demo exists, then writes the fixture files as they are,
// with a manifest, so dev exercises the same swap as a real parse.
func (s *server) runStub(ctx context.Context, st store.Store, req parseRequest, res *runResult) error {
	size, err := st.Stat(ctx, req.DemoKey)
	if err != nil {
		return err
	}
	res.demoSize, res.source, res.roundSource = size, "stub", "stub"
	if res.matchJSON, err = os.ReadFile(filepath.Join(s.stubDir, match.FileName)); err != nil {
		return fmt.Errorf("stub: %w", err)
	}
	paths, err := filepath.Glob(filepath.Join(s.stubDir, "rounds", "*.json"))
	if err != nil {
		return fmt.Errorf("stub: %w", err)
	}
	rounds := make([]outFile, len(paths))
	for i, p := range paths {
		if rounds[i].data, err = os.ReadFile(p); err != nil {
			return fmt.Errorf("stub: %w", err)
		}
		rounds[i].name = filepath.Base(p)
	}
	return s.write(ctx, st, req, res, rounds)
}

// output writes one parse's files under its prefix and lists each for the
// manifest.
type output struct {
	st     store.Store
	prefix string
	files  []manifestFile
}

func (o *output) put(ctx context.Context, name string, data []byte) error {
	key := o.prefix + name
	if err := o.st.Put(ctx, key, data); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	o.files = append(o.files, manifestFile{Key: key, Bytes: len(data), SHA256: hex.EncodeToString(sum[:])})
	return nil
}

// commit writes manifest.json, listing every file put so far, and returns
// its sha256. A prefix without a manifest is an unfinished parse.
func (o *output) commit(ctx context.Context, m manifest) (string, error) {
	m.Files = o.files
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	if err := o.st.Put(ctx, o.prefix+"manifest.json", data); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// manifest is manifest.json. It holds no wall-clock time, so the same demo,
// build and parse id always give the same bytes.
type manifest struct {
	ParseID       string         `json:"parse_id"`
	MatchID       string         `json:"match_id"`
	ParsedBy      string         `json:"parsed_by"`      // PARSER_BUILD
	ParserVersion string         `json:"parser_version"` // the shipped match.json's
	SchemaVersion int            `json:"schema_version"`
	MatchSource   string         `json:"match_source"`
	RoundSource   string         `json:"round_source"`
	Fallback      string         `json:"fallback_reason,omitempty"` // why RoundSource isn't the configured one
	Files         []manifestFile `json:"files"`                     // in write order
}

type manifestFile struct {
	Key    string `json:"key"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
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

// writeBody sends a JSON response, with its length and flushed, so the
// client has all of it while the handler goes on to a shadow compare. It
// reports whether the response reached the connection.
func writeBody(w http.ResponseWriter, status int, body []byte) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return err
	}
	return http.NewResponseController(w).Flush()
}

// errorJSON is the body of an error response. code is classify's result,
// which the Worker stores and the match page explains.
func errorJSON(code, msg string) []byte {
	body, _ := json.Marshal(struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}{code, msg})
	return body
}
