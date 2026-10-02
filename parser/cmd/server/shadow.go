package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/ChiemekaKalu/demoview/parser/diff"
)

// shadowTimeout bounds a shadow compare. It runs after the response, so no
// caller's deadline does.
const shadowTimeout = 10 * time.Minute

// shadowExamples is how many mismatches a shadow line shows in full.
const shadowExamples = 5

// shadowCompare streams the demo into the oracle again, compares the
// oracle's match.json and round files with the ones that shipped, and logs
// one "shadow" line. It also writes the verdict to shadow.json beside the
// parse's files, so it outlasts the logs: after manifest.json and not listed
// in it, so the manifest still marks the parse complete. It runs once the
// response has gone, under its own timeout, and counts as in flight, so a
// shutdown drain waits for it.
func (s *server) shadowCompare(ctx context.Context, log *slog.Logger, req parseRequest, res runResult) {
	s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shadowTimeout)
	defer cancel()

	start := time.Now()
	ref, err := s.runOracle(ctx, s.store, req.DemoKey)
	oracleMS := time.Since(start).Milliseconds()
	var report diff.Report
	compared := 0
	if err == nil {
		report, compared, err = shadowDiff(res.matchJSON, res.rounds, ref)
	}

	v := shadowVerdict{
		Result:         "match",
		RoundSource:    res.roundSource,
		RoundsCompared: compared,
		Mismatches:     len(report.Mismatches),
		ByCategory:     map[string]int{},
		First:          []string{},
		OracleMS:       oracleMS,
	}
	level := slog.LevelInfo
	switch {
	case err != nil:
		v.Result, v.Error, level = "error", err.Error(), slog.LevelWarn
	case !report.OK():
		v.Result, level = "mismatch", slog.LevelWarn
	}
	for _, m := range report.Mismatches {
		v.ByCategory[indexes.ReplaceAllString(m.Path, "")]++
		if len(v.First) < shadowExamples {
			v.First = append(v.First, fmt.Sprintf("%s: ours %s, oracle %s", m.Path, m.Ours, m.Ref))
		}
	}
	attrs := []slog.Attr{
		slog.String("event", "shadow"),
		slog.String("result", v.Result),
		slog.Int("rounds_compared", v.RoundsCompared),
		slog.Int("mismatches", v.Mismatches),
		slog.Any("by_category", v.ByCategory),
		slog.Any("first", v.First),
		slog.Int64("oracle_ms", v.OracleMS),
	}
	if v.Error != "" {
		attrs = append(attrs, slog.String("error", v.Error))
	}
	log.LogAttrs(ctx, level, "shadow", attrs...)

	data, err := json.Marshal(v)
	if err == nil {
		err = s.store.Put(ctx, req.prefix()+"shadow.json", data)
	}
	if err != nil {
		log.Warn("shadow write failed", "error", err)
	}
}

// shadowVerdict is shadow.json, and what the shadow line logs.
type shadowVerdict struct {
	Result         string         `json:"result"` // "match", "mismatch" or "error"
	RoundSource    string         `json:"round_source"`
	RoundsCompared int            `json:"rounds_compared"`
	Mismatches     int            `json:"mismatches"`
	ByCategory     map[string]int `json:"by_category"` // mismatches by path, indexes removed
	First          []string       `json:"first"`       // the first few mismatches
	OracleMS       int64          `json:"oracle_ms"`
	Error          string         `json:"error,omitempty"`
}

// indexes matches the indexes in a diff path, such as "[3]" or "[n=2]".
// Without them, a path names a category of mismatch.
var indexes = regexp.MustCompile(`\[[^\]]*\]`)

// shadowDiff diffs the match.json and round files that shipped against the
// oracle's, pairing round files by name; one on only one side is a
// mismatch. It also returns how many round files both sides had.
func shadowDiff(matchJSON []byte, rounds []outFile, ref oracleOutput) (diff.Report, int, error) {
	report, err := diff.Compare(matchJSON, ref.matchJSON)
	if err != nil {
		return report, 0, err
	}
	theirs := make(map[string][]byte, len(ref.rounds))
	for _, f := range ref.rounds {
		theirs[f.name] = f.data
	}
	compared := 0
	for _, f := range rounds {
		data, ok := theirs[f.name]
		if !ok {
			report.Mismatches = append(report.Mismatches, diff.Mismatch{Path: "round_files[" + f.name + "]", Ours: "present", Ref: "absent"})
			continue
		}
		delete(theirs, f.name)
		rr, err := diff.CompareRound(matchJSON, ref.matchJSON, f.data, data)
		if err != nil {
			return report, compared, fmt.Errorf("rounds/%s: %w", f.name, err)
		}
		report.Mismatches = append(report.Mismatches, rr.Mismatches...)
		compared++
	}
	for _, f := range ref.rounds {
		if _, ok := theirs[f.name]; ok {
			report.Mismatches = append(report.Mismatches, diff.Mismatch{Path: "round_files[" + f.name + "]", Ours: "absent", Ref: "present"})
		}
	}
	return report, compared, nil
}
