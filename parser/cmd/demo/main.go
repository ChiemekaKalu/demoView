// Command demo parses CS2 demo files into the project's JSON schema.
//
// Usage:
//
//	demo parse <file.dem> --out <dir> [--events-only]
//	demo inspect <file.dem>
//
// parse writes <dir>/match.json. inspect prints frame, file header and packet
// message statistics; it is a development aid and not part of the CLI
// contract. Flags may come before or
// after the file. On failure demo writes one JSON line,
// {"error":"...","code":"..."}, to stderr and exits 1 (bad_demo), 2 (usage)
// or 3 (internal).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse"
)

const (
	parseSynopsis   = "demo parse <file.dem> --out <dir> [--events-only]"
	inspectSynopsis = "demo inspect <file.dem>"
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
	case errors.Is(err, demo.ErrBadDemo):
		return 1, "bad_demo"
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
		return usagef("missing command; usage: %s | %s", parseSynopsis, inspectSynopsis)
	}
	switch args[0] {
	case "parse":
		return cmdParse(args[1:], stdout)
	case "inspect":
		return cmdInspect(args[1:], stdout)
	case "help", "-h", "-help", "--help":
		_, err := fmt.Fprintf(stdout, "usage:\n  %s\n  %s\n", parseSynopsis, inspectSynopsis)
		return err
	}
	return usagef("unknown command %q; usage: %s | %s", args[0], parseSynopsis, inspectSynopsis)
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

// parseDemo parses the demo at path with parse.Run and writes match.json to
// outDir. eventsOnly has no effect yet, because round files are not produced.
func parseDemo(path, outDir string, eventsOnly bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	file, err := parse.Run(context.Background(), f)
	if err != nil {
		return err
	}
	return match.WriteFile(outDir, file)
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
