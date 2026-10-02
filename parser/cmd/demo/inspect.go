package main

import (
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// cmdInspect prints statistics for a demo: frames by command, the file
// header and info, and packet messages by type. With --events it adds game
// events by name and a timeline of the ones that shape rounds; neither names
// a player, so the output is safe to keep from any demo. It is a development
// aid, not part of the CLI contract, so its output format may change freely.
func cmdInspect(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	withEvents := fs.Bool("events", false, "also print game events by name and a round-structure timeline")
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
	in := inspection{
		byCmd:          map[demo.Command]*frameStats{},
		byType:         map[demo.MsgType]*msgStats{},
		fileInfoOffset: -1,
	}
	if *withEvents {
		in.byEvent = map[string]int{}
	}
	if err := in.read(demo.NewDecoder(r)); err != nil {
		return err
	}
	in.print(stdout, pos[0], r.Header())
	return nil
}

// frameStats counts frames of one command, or of all commands.
type frameStats struct {
	frames, compressed int
	bytes, maxBytes    int // decompressed payload sizes
}

// msgStats counts packet messages of one type.
type msgStats struct{ count, bytes int }

// inspection accumulates what inspect reports.
type inspection struct {
	total           frameStats
	byCmd           map[demo.Command]*frameStats
	firstTick       int32
	lastTick        int32
	ticked          bool
	fileInfoOffset  int64
	framesAfterStop int
	seenStop        bool

	header *pb.CDemoFileHeader // the first FileHeader, or nil
	info   *pb.CDemoFileInfo   // the first FileInfo, or nil

	packets     int
	messages    int
	maxTrailing int // most padding bits left after a packet's last message
	byType      map[demo.MsgType]*msgStats

	events   events.Decoder
	byEvent  map[string]int // game events by name; nil without --events
	timeline []timelineEntry
}

// timelineEntry is a run of one game event on one tick.
type timelineEntry struct {
	tick  int32
	name  string
	count int
}

// read consumes every frame from d.
func (in *inspection) read(d *demo.Decoder) error {
	var mr demo.MessageReader
	for {
		f, err := d.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		in.frame(f)
		switch f.Cmd {
		case demo.CmdFileHeader:
			h := new(pb.CDemoFileHeader)
			err = f.Unmarshal(h)
			if in.header == nil {
				in.header = h
			}
		case demo.CmdFileInfo:
			fi := new(pb.CDemoFileInfo)
			err = f.Unmarshal(fi)
			if in.info == nil {
				in.info = fi
			}
		case demo.CmdPacket, demo.CmdSignonPacket, demo.CmdFullPacket:
			err = in.packet(&mr, f)
		}
		if err != nil {
			return err
		}
	}
}

// frame records the frame-level statistics for f.
func (in *inspection) frame(f demo.Frame) {
	s := in.byCmd[f.Cmd]
	if s == nil {
		s = new(frameStats)
		in.byCmd[f.Cmd] = s
	}
	for _, s := range []*frameStats{s, &in.total} {
		s.frames++
		if f.Compressed {
			s.compressed++
		}
		s.bytes += len(f.Payload)
		s.maxBytes = max(s.maxBytes, len(f.Payload))
	}
	if f.Tick >= 0 {
		if !in.ticked {
			in.firstTick, in.ticked = f.Tick, true
		}
		in.lastTick = f.Tick
	}
	if f.Cmd == demo.CmdFileInfo && in.fileInfoOffset < 0 {
		in.fileInfoOffset = f.Offset
	}
	if in.seenStop {
		in.framesAfterStop++
	}
	in.seenStop = in.seenStop || f.Cmd == demo.CmdStop
}

// packet counts the messages in the packet frame f.
func (in *inspection) packet(mr *demo.MessageReader, f demo.Frame) error {
	if err := mr.Reset(f); err != nil {
		return err
	}
	for {
		m, err := mr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		s := in.byType[m.Type]
		if s == nil {
			s = new(msgStats)
			in.byType[m.Type] = s
		}
		s.count++
		s.bytes += len(m.Body)
		in.messages++
		if in.byEvent != nil {
			if err := in.gameEvent(m, f.Tick); err != nil {
				return err
			}
		}
	}
	in.packets++
	in.maxTrailing = max(in.maxTrailing, mr.TrailingBits())
	return nil
}

// gameEvent counts m if it is a game event, and adds it to the timeline if
// it shapes rounds.
func (in *inspection) gameEvent(m demo.Message, tick int32) error {
	switch m.Type {
	case demo.MsgType(pb.EBaseGameEvents_GE_Source1LegacyGameEventList):
		return in.events.HandleList(m.Body)
	case demo.MsgType(pb.EBaseGameEvents_GE_Source1LegacyGameEvent):
		e, err := in.events.Decode(m.Body, tick)
		if err != nil {
			return err
		}
		in.byEvent[e.Name]++
		if !onTimeline(e.Name) {
			return nil
		}
		if n := len(in.timeline); n > 0 && in.timeline[n-1].tick == tick && in.timeline[n-1].name == e.Name {
			in.timeline[n-1].count++
			return nil
		}
		in.timeline = append(in.timeline, timelineEntry{tick, e.Name, 1})
	}
	return nil
}

// onTimeline reports whether the event named name shapes rounds: one that
// names a round, match, phase or freeze time, or a spawn, team change,
// death or bomb event.
func onTimeline(name string) bool {
	for _, s := range []string{"round", "match", "phase", "freeze", "restart", "warmup", "player_spawn", "player_team", "player_death", "bomb_"} {
		if strings.Contains(name, s) {
			return true
		}
	}
	return false
}

func (in *inspection) print(w io.Writer, path string, h demo.Header) {
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }
	p("file:     %s\n", path)
	p("header:   fileinfo_offset=%d spawngroups_offset=%d\n", h.FileInfoOffset, h.SpawnGroupsOffset)
	if in.fileInfoOffset >= 0 {
		p("fileinfo: frame at offset %d\n", in.fileInfoOffset)
	}
	if in.ticked {
		p("ticks:    first=%d last=%d (ignoring tick -1)\n", in.firstTick, in.lastTick)
	} else {
		p("ticks:    none\n")
	}
	if in.seenStop {
		p("stop:     %d frames after Stop\n", in.framesAfterStop)
	} else {
		p("stop:     no Stop frame\n")
	}

	if hd := in.header; hd != nil {
		p("map:      %s\n", hd.GetMapName())
		p("server:   %s\n", hd.GetServerName())
		p("client:   %s\n", hd.GetClientName())
		p("demo:     stamp=%q version=%q guid=%q\n", hd.GetDemoFileStamp(), hd.GetDemoVersionName(), hd.GetDemoVersionGuid())
		p("network:  protocol=%d build=%d (patch_version, build_num)\n", hd.GetPatchVersion(), hd.GetBuildNum())
	} else {
		p("map:      unknown (no FileHeader)\n")
	}
	if fi := in.info; fi != nil {
		p("playback: ticks=%d frames=%d time=%.3fs\n", fi.GetPlaybackTicks(), fi.GetPlaybackFrames(), fi.GetPlaybackTime())
	} else {
		p("playback: unknown (no FileInfo)\n")
	}
	p("packets:  %d holding %d messages, max trailing bits %d\n", in.packets, in.messages, in.maxTrailing)

	p("\n%-20s %8s %10s %14s %12s\n", "command", "frames", "compressed", "bytes", "max_bytes")
	for _, c := range slices.Sorted(maps.Keys(in.byCmd)) {
		s := in.byCmd[c]
		p("%-20s %8d %10d %14d %12d\n", c, s.frames, s.compressed, s.bytes, s.maxBytes)
	}
	s := in.total
	p("%-20s %8d %10d %14d %12d\n", "total", s.frames, s.compressed, s.bytes, s.maxBytes)

	if len(in.byType) > 0 {
		p("\n%6s %-32s %10s %14s\n", "id", "message", "count", "bytes")
		for _, t := range slices.Sorted(maps.Keys(in.byType)) {
			s := in.byType[t]
			p("%6d %-32s %10d %14d\n", uint32(t), t, s.count, s.bytes)
		}
	}

	if in.byEvent == nil {
		return
	}
	p("\n%8s %s\n", "count", "game event")
	for _, name := range slices.Sorted(maps.Keys(in.byEvent)) {
		p("%8d %s\n", in.byEvent[name], name)
	}
	p("\n%8s %s\n", "tick", "round-structure event (xN: N on one tick)")
	for _, e := range in.timeline {
		if e.count > 1 {
			p("%8d %s x%d\n", e.tick, e.name, e.count)
		} else {
			p("%8d %s\n", e.tick, e.name)
		}
	}
}
