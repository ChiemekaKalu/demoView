package main

import (
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

	"github.com/ChiemekaKalu/demoview/parser/demo"
)

// cmdInspect prints frame statistics for a demo. It is a development aid, not
// part of the CLI contract, so its output format may change freely.
func cmdInspect(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	pos, err := parseArgs(fs, inspectSynopsis, args, stdout)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("inspect: want one demo file, got %d; usage: %s", len(pos), inspectSynopsis)
	}

	f, err := os.Open(pos[0])
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := demo.NewReader(f)
	if err != nil {
		return err
	}

	type stats struct {
		frames, compressed int
		bytes              int64
	}
	var (
		total               stats
		byCmd               = map[demo.Command]*stats{}
		firstTick, lastTick int32
		ticked              bool
		fileInfoOffset      int64 = -1
		framesAfterStop     int
		seenStop            bool
	)
	for {
		fr, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		s := byCmd[fr.Cmd]
		if s == nil {
			s = new(stats)
			byCmd[fr.Cmd] = s
		}
		for _, s := range []*stats{s, &total} {
			s.frames++
			if fr.Compressed {
				s.compressed++
			}
			s.bytes += int64(len(fr.Payload))
		}
		if fr.Tick >= 0 {
			if !ticked {
				firstTick, ticked = fr.Tick, true
			}
			lastTick = fr.Tick
		}
		if fr.Cmd == demo.CmdFileInfo && fileInfoOffset < 0 {
			fileInfoOffset = fr.Offset
		}
		if seenStop {
			framesAfterStop++
		}
		seenStop = seenStop || fr.Cmd == demo.CmdStop
	}

	h := r.Header()
	fmt.Fprintf(stdout, "file:     %s\n", pos[0])
	fmt.Fprintf(stdout, "header:   fileinfo_offset=%d spawngroups_offset=%d\n", h.FileInfoOffset, h.SpawnGroupsOffset)
	if fileInfoOffset >= 0 {
		fmt.Fprintf(stdout, "fileinfo: frame at offset %d\n", fileInfoOffset)
	}
	if ticked {
		fmt.Fprintf(stdout, "ticks:    first=%d last=%d (ignoring tick -1)\n", firstTick, lastTick)
	} else {
		fmt.Fprintf(stdout, "ticks:    none\n")
	}
	if seenStop {
		fmt.Fprintf(stdout, "stop:     %d frames after Stop\n", framesAfterStop)
	} else {
		fmt.Fprintf(stdout, "stop:     no Stop frame\n")
	}
	fmt.Fprintf(stdout, "\n%-20s %8s %10s %14s\n", "command", "frames", "compressed", "payload_bytes")
	for _, c := range slices.Sorted(maps.Keys(byCmd)) {
		s := byCmd[c]
		fmt.Fprintf(stdout, "%-20s %8d %10d %14d\n", c, s.frames, s.compressed, s.bytes)
	}
	fmt.Fprintf(stdout, "%-20s %8d %10d %14d\n", "total", total.frames, total.compressed, total.bytes)
	return nil
}
