// Command oracle exports a CS2 demo to the project's schema v1 with
// demoinfocs-golang, as the reference our parser is diffed against.
//
// Usage:
//
//	oracle export <file.dem | -> --out <dir> [--hz 16]
//
// "-" reads the demo from stdin. It writes <dir>/match.json and
// <dir>/rounds/NN.json, then prints one JSON summary line on stdout. On
// failure it prints one JSON error line on stderr and exits 1.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/ChiemekaKalu/demoview/tools/oracle/export"
)

const usage = "usage: oracle export <file.dem | -> --out <dir> [--hz 16]"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		line, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintln(os.Stderr, string(line))
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "export" {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "output directory")
	hz := fs.Int("hz", export.DefaultHz, "round file sample rate")
	// Accept the file before or after the flags.
	var files []string
	rest := args[1:]
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return fmt.Errorf("%v; %s", err, usage)
		}
		rest = fs.Args()
		if len(rest) > 0 {
			files = append(files, rest[0])
			rest = rest[1:]
		}
	}
	if len(files) != 1 || *out == "" || *hz <= 0 {
		return errors.New(usage)
	}

	start := time.Now()
	// "-" reads the demo from stdin, so the parse server can stream it in
	// without a temporary copy. demoinfocs reads it front to back.
	in := os.Stdin
	if files[0] != "-" {
		f, err := os.Open(files[0])
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	res, err := export.Run(in, *hz)
	if err != nil {
		return err
	}
	if err := export.WriteDir(*out, res); err != nil {
		return err
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return json.NewEncoder(stdout).Encode(map[string]any{
		"map":         res.Match.Match.Map,
		"rounds":      len(res.Match.Rounds),
		"kills":       len(res.Match.Kills),
		"players":     len(res.Match.Players),
		"duration_ms": time.Since(start).Milliseconds(),
		"go_sys_mb":   mem.Sys >> 20, // memory obtained from the OS, an upper bound on the peak heap
		"stats":       res.Stats,
	})
}
