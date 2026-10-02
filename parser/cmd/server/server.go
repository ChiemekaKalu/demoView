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
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/store"
)

// ParseFunc parses a streamed demo into match.json. Errors caused by a bad
// demo wrap demo.ErrBadDemo.
type ParseFunc func(ctx context.Context, r io.Reader) (match.File, error)

// server is the Cloud Run service. See the spec's "Cloud Run route".
type server struct {
	store  store.Store
	secret [sha256.Size]byte // hash of PARSE_SECRET
	parse  ParseFunc
	// stubDir, when set, holds match.json and rounds/*.json to copy instead
	// of parsing. M2 deploys the pipeline this way before the parser is done.
	stubDir string
	// oracle, when set, produces the round files (PARSER_ROUNDS=oracle).
	oracle *oracle

	logMu     sync.Mutex
	log       io.Writer
	resetPeak func()
	peakMem   func() int64
}

func newServer(st store.Store, secret string, parseFn ParseFunc, stubDir string, log io.Writer) *server {
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
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func (s *server) handleParse(w http.ResponseWriter, r *http.Request) {
	given := sha256.Sum256([]byte(r.Header.Get("X-Parse-Secret")))
	if subtle.ConstantTimeCompare(given[:], s.secret[:]) != 1 {
		s.writeLog(map[string]any{"severity": "WARNING", "message": "unauthorized parse request"})
		writeError(w, http.StatusUnauthorized, "missing or wrong X-Parse-Secret")
		return
	}

	var req parseRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be JSON: "+err.Error())
		return
	}
	if !matchIDPattern.MatchString(req.MatchID) || req.DemoKey == "" {
		writeError(w, http.StatusBadRequest, "body needs a match_id of [0-9a-z] and a demo_key")
		return
	}

	// Cloud Run runs one request per instance (concurrency 1), so the peak
	// measured from here belongs to this parse.
	s.resetPeak()
	start := time.Now()
	out, err := s.run(r.Context(), req)
	status, result := classify(err)
	entry := parseLog{
		Severity:         severity(status),
		Message:          "parse",
		MatchID:          req.MatchID,
		DemoKey:          req.DemoKey,
		DemoSize:         out.demoSize,
		DurationMS:       time.Since(start).Milliseconds(),
		PeakMemory:       s.peakMem(),
		ParserVersion:    parse.Version,
		Result:           result,
		Status:           status,
		Stub:             s.stubDir != "",
		OracleMS:         out.oracleMS,
		OraclePeakMemory: out.oraclePeak,
	}
	if err != nil {
		entry.Error = err.Error()
	} else {
		entry.MatchSource = out.source
		if v := parserVersionOf(out.matchJSON); v != "" {
			entry.ParserVersion = v // the shipped file's own
		}
	}
	s.writeLog(entry)

	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out.matchJSON)
}

// runResult is what one parse shipped, for the response and the log line.
type runResult struct {
	matchJSON  []byte
	demoSize   int64
	source     string // whose match.json shipped: "parser" or "oracle"
	oracleMS   int64
	oraclePeak int64
}

// run produces the parse output for req, writes it to the store and returns
// match.json. Every output key is overwritten, so a retry is safe. Round
// files are written first and match.json last, so its presence means the
// round files are there too.
func (s *server) run(ctx context.Context, req parseRequest) (runResult, error) {
	if s.stubDir != "" {
		return s.runStub(ctx, req)
	}
	rc, size, err := s.store.Open(ctx, req.DemoKey)
	if err != nil {
		return runResult{}, err
	}
	file, err := s.parse(ctx, rc)
	rc.Close()
	res := runResult{demoSize: size, source: "parser"}
	if err != nil {
		return res, err
	}
	var buf bytes.Buffer
	if err := match.Write(&buf, file); err != nil {
		return res, err
	}
	res.matchJSON = buf.Bytes()

	var rounds []outFile
	if s.oracle != nil {
		if rounds, err = s.oracleRounds(ctx, req, &res); err != nil {
			return res, err
		}
	}
	for _, f := range rounds {
		if err := s.store.Put(ctx, outputKey(req.MatchID, "rounds/"+f.name), f.data); err != nil {
			return res, err
		}
	}
	if err := s.store.Put(ctx, outputKey(req.MatchID, match.FileName), res.matchJSON); err != nil {
		return res, err
	}
	return res, nil
}

// oracleRounds streams the demo a second time into the oracle and returns
// its round files. Round files index match.json's players and kills, so
// ours ships only when the consistency guard finds it agrees with the
// oracle's; otherwise the oracle's match.json ships with them, and a
// warning names the first difference.
func (s *server) oracleRounds(ctx context.Context, req parseRequest, res *runResult) ([]outFile, error) {
	start := time.Now()
	rc, _, err := s.store.Open(ctx, req.DemoKey)
	if err != nil {
		return nil, err
	}
	out, err := s.oracle.export(ctx, rc)
	rc.Close()
	res.oracleMS = time.Since(start).Milliseconds()
	if err != nil {
		// Our parser accepted this demo, so the oracle failing is not the
		// demo's fault: a 500, which the queue retries.
		return nil, err
	}
	res.oraclePeak = out.peakMem
	if why := firstMismatch(res.matchJSON, out.matchJSON); why != "" {
		res.matchJSON, res.source = out.matchJSON, "oracle"
		s.writeLog(map[string]any{
			"severity": "WARNING",
			"message":  "match.json disagrees with the oracle's; shipping the oracle's",
			"match_id": req.MatchID,
			"mismatch": why,
		})
	}
	return out.rounds, nil
}

// runStub checks the demo exists, then copies the fixture files to the
// match's output keys as they are.
func (s *server) runStub(ctx context.Context, req parseRequest) (runResult, error) {
	size, err := s.store.Stat(ctx, req.DemoKey)
	if err != nil {
		return runResult{}, err
	}
	res := runResult{demoSize: size}
	matchJSON, err := os.ReadFile(filepath.Join(s.stubDir, match.FileName))
	if err != nil {
		return res, fmt.Errorf("stub: %w", err)
	}
	rounds, err := filepath.Glob(filepath.Join(s.stubDir, "rounds", "*.json"))
	if err != nil {
		return res, fmt.Errorf("stub: %w", err)
	}
	for _, p := range rounds {
		data, err := os.ReadFile(p)
		if err != nil {
			return res, fmt.Errorf("stub: %w", err)
		}
		if err := s.store.Put(ctx, outputKey(req.MatchID, "rounds/"+filepath.Base(p)), data); err != nil {
			return res, err
		}
	}
	if err := s.store.Put(ctx, outputKey(req.MatchID, match.FileName), matchJSON); err != nil {
		return res, err
	}
	res.matchJSON = matchJSON
	return res, nil
}

// outputKey is a key under parsed/{match_id}/ in the spec's R2 layout.
func outputKey(matchID, name string) string {
	return path.Join("parsed", matchID, name)
}

// classify maps a parse error to the HTTP status the Worker acts on: 422
// means the demo is bad and must not be retried, 5xx means try again.
func classify(err error) (status int, result string) {
	switch {
	case err == nil:
		return http.StatusOK, "ok"
	case errors.Is(err, store.ErrNotFound):
		return http.StatusUnprocessableEntity, "demo_not_found"
	case errors.Is(err, demo.ErrBadDemo):
		return http.StatusUnprocessableEntity, "bad_demo"
	}
	return http.StatusInternalServerError, "error"
}

// parseLog is the one JSON line logged per parse, per the spec's
// observability section. Cloud Logging reads severity and message.
type parseLog struct {
	Severity      string `json:"severity"`
	Message       string `json:"message"`
	MatchID       string `json:"match_id"`
	DemoKey       string `json:"demo_key"`
	DemoSize      int64  `json:"demo_size"`
	DurationMS    int64  `json:"duration_ms"`
	PeakMemory    int64  `json:"peak_memory_bytes"`
	ParserVersion string `json:"parser_version"`
	Result        string `json:"result"`
	Status        int    `json:"status"`
	Error         string `json:"error,omitempty"`
	Stub          bool   `json:"stub,omitempty"`
	// With PARSER_ROUNDS=oracle: whose match.json shipped, and the oracle
	// step's time and peak memory (Linux only).
	MatchSource      string `json:"match_source,omitempty"`
	OracleMS         int64  `json:"oracle_ms,omitempty"`
	OraclePeakMemory int64  `json:"oracle_peak_memory_bytes,omitempty"`
}

func severity(status int) string {
	switch {
	case status >= 500:
		return "ERROR"
	case status >= 400:
		return "WARNING"
	}
	return "INFO"
}

func (s *server) writeLog(v any) {
	line, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	s.log.Write(append(line, '\n'))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
