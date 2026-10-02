// Command demo parses CS2 demo files into the project's JSON schema.
//
// Usage:
//
//	demo parse <file.dem> --out <dir> [--events-only]
//	demo diff <file.dem> --against <reference.json>
//	demo inspect <file.dem> [--events]
//
// parse writes <dir>/match.json and, unless --events-only, one
// <dir>/rounds/NN.json per round. If the entity stage fails, match.json is
// still written and the error names why the round files aren't: code
// unknown_entity for a class, field or decoder this parser doesn't know,
// bad_demo otherwise. diff parses the demo and compares the result with a
// reference match.json, and with the reference's round files when a rounds
// directory sits beside it, printing the report on stdout. inspect prints
// frame, file header and packet message statistics, and with --events a
// round-structure timeline of game events; it is a development aid
// and not part of the CLI contract. Flags may come before or after the file.
// On failure demo writes one JSON line, {"error":"...","code":"..."}, to
// stderr and exits 1 (bad_demo, unknown_entity or mismatch), 2 (usage) or 3
// (internal).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/diff"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/roundfile"
)

const (
	parseSynopsis   = "demo parse <file.dem> --out <dir> [--events-only]"
	diffSynopsis    = "demo diff <file.dem> --against <reference.json>"
	inspectSynopsis = "demo inspect <file.dem> [--events]"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command line and returns the process exit code. A failure
// is reported on stderr as a single JSON line.
func run(args []string, stdout, stderr io.Writer) int {
	err := dispatch(args, stdout)
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	code, name := classify(err)
	enc := json.NewEncoder(stderr)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(errorLine{Error: err.Error(), Code: name})
	return code
}

// errorLine is the JSON error written to stderr on failure.
type errorLine struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// classify maps err to an exit code and the code string for its error line.
func classify(err error) (int, string) {
	var ue *usageError
	switch {
	case errors.Is(err, entities.ErrUnknown):
		return 1, "unknown_entity"
	case errors.Is(err, demo.ErrBadDemo):
		return 1, "bad_demo"
	case errors.Is(err, errMismatch):
		return 1, "mismatch"
	case errors.As(err, &ue):
		return 2, "usage"
	}
	return 3, "internal"
}

// usageError is a bad command line.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{fmt.Sprintf(format, args...)}
}

func dispatch(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return usagef("missing command; usage: %s | %s | %s", parseSynopsis, diffSynopsis, inspectSynopsis)
	}
	switch args[0] {
	case "parse":
		return cmdParse(args[1:], stdout)
	case "diff":
		return cmdDiff(args[1:], stdout)
	case "inspect":
		return cmdInspect(args[1:], stdout)
	case "help", "-h", "-help", "--help":
		_, err := fmt.Fprintf(stdout, "usage:\n  %s\n  %s\n  %s\n", parseSynopsis, diffSynopsis, inspectSynopsis)
		return err
	}
	return usagef("unknown command %q; usage: %s | %s | %s", args[0], parseSynopsis, diffSynopsis, inspectSynopsis)
}

func cmdParse(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	out := fs.String("out", "", "write output files to `dir`, creating it if missing")
	eventsOnly := fs.Bool("events-only", false, "write match.json only, no round files")
	pos, err := parseArgs(fs, parseSynopsis, args, stdout)
	if err != nil {
		return err
	}
	switch {
	case len(pos) != 1:
		return usagef("parse: want one demo file, got %d; usage: %s", len(pos), parseSynopsis)
	case *out == "":
		return usagef("parse: --out is required; usage: %s", parseSynopsis)
	}
	return parseDemo(pos[0], *out, *eventsOnly)
}

func cmdDiff(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	against := fs.String("against", "", "compare with the reference match.json at `path`")
	pos, err := parseArgs(fs, diffSynopsis, args, stdout)
	if err != nil {
		return err
	}
	switch {
	case len(pos) != 1:
		return usagef("diff: want one demo file, got %d; usage: %s", len(pos), diffSynopsis)
	case *against == "":
		return usagef("diff: --against is required; usage: %s", diffSynopsis)
	}
	return diffDemo(pos[0], *against, stdout)
}

// errMismatch means diff found differences; the report is on stdout.
var errMismatch = errors.New("output differs from the reference")

// diffDemo parses the demo at path and diffs it against the match.json at
// refPath, and against the round files in the rounds directory beside it,
// if there is one.
func diffDemo(path, refPath string, stdout io.Writer) error {
	ref, err := os.ReadFile(refPath)
	if err != nil {
		return err
	}
	refRounds := filepath.Join(filepath.Dir(refPath), roundfile.Dir)
	fi, err := os.Stat(refRounds)
	withRounds := err == nil && fi.IsDir()
	out, err := parseFile(path, withRounds)
	if err != nil {
		return err
	}
	var ours bytes.Buffer
	if err := match.Write(&ours, out.Match); err != nil {
		return err
	}
	report, err := diff.Compare(ours.Bytes(), ref)
	if err != nil {
		return err
	}
	switch {
	case out.RoundsErr != nil:
		// match.json still compares; the round files are missing.
		report.Mismatches = append(report.Mismatches, diff.Mismatch{Path: "round_files", Ours: out.RoundsErr.Error(), Ref: "present"})
	case withRounds:
		if err := diffRounds(&report, ours.Bytes(), ref, out.Rounds, refRounds); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout, report); err != nil {
		return err
	}
	if !report.OK() {
		return errMismatch
	}
	return nil
}

// diffRounds adds to r the differences between our round files and those
// in refDir. A round file on only one side is a difference too.
func diffRounds(r *diff.Report, ourMatch, refMatch []byte, rounds []roundfile.File, refDir string) error {
	names := map[string]bool{}
	for _, rf := range rounds {
		name := roundfile.Name(rf.Round)
		names[name] = true
		ref, err := os.ReadFile(filepath.Join(refDir, name))
		if errors.Is(err, os.ErrNotExist) {
			r.Mismatches = append(r.Mismatches, diff.Mismatch{Path: "rounds/" + name, Ours: "present", Ref: "absent"})
			continue
		}
		if err != nil {
			return err
		}
		var ours bytes.Buffer
		if err := roundfile.Write(&ours, rf); err != nil {
			return err
		}
		rr, err := diff.CompareRound(ourMatch, refMatch, ours.Bytes(), ref)
		if err != nil {
			return fmt.Errorf("rounds/%s: %w", name, err)
		}
		r.Mismatches = append(r.Mismatches, rr.Mismatches...)
	}
	refs, err := filepath.Glob(filepath.Join(refDir, "*.json"))
	if err != nil {
		return err
	}
	for _, p := range refs {
		if name := filepath.Base(p); !names[name] {
			r.Mismatches = append(r.Mismatches, diff.Mismatch{Path: "rounds/" + name, Ours: "absent", Ref: "present"})
		}
	}
	return nil
}

// parseDemo parses the demo at path and writes match.json and, unless
// eventsOnly, the round files to outDir. match.json is written even when
// the round files fail, and the error says why they did.
func parseDemo(path, outDir string, eventsOnly bool) error {
	out, err := parseFile(path, !eventsOnly)
	if err != nil {
		return err
	}
	if err := match.WriteFile(outDir, out.Match); err != nil {
		return err
	}
	for _, rf := range out.Rounds {
		if err := roundfile.WriteFile(outDir, rf); err != nil {
			return err
		}
	}
	return out.RoundsErr
}

func parseFile(path string, rounds bool) (parse.Output, error) {
	f, err := os.Open(path)
	if err != nil {
		return parse.Output{}, err
	}
	defer f.Close()
	out, _, err := parse.Parse(context.Background(), f, parse.Options{Rounds: rounds})
	return out, err
}

// parseArgs parses args against fs and returns the positional arguments.
// Unlike fs.Parse it accepts flags after positionals, as in
// "parse x.dem --out dir"; a "--" ends flag parsing. On -h it prints help to
// stdout and returns flag.ErrHelp. Other flag errors become usage errors.
func parseArgs(fs *flag.FlagSet, synopsis string, args []string, stdout io.Writer) ([]string, error) {
	fs.SetOutput(io.Discard) // errors are reported by run as JSON
	var pos []string
	for {
		err := fs.Parse(args)
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(stdout, "usage: %s\n", synopsis)
			fs.SetOutput(stdout)
			fs.PrintDefaults()
			return nil, flag.ErrHelp
		}
		if err != nil {
			return nil, usagef("%s: %v; usage: %s", fs.Name(), err, synopsis)
		}
		// fs.Parse stops at the first positional, or just after "--".
		rest := fs.Args()
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			return append(pos, rest...), nil
		}
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}
