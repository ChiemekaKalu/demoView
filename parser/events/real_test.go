package events_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// realDemos are the interim fixture demos and the facts checked on them. They
// are not in git, so each one is skipped when absent.
var realDemos = []struct {
	file string
	// deaths is the player_death count, warmup included. demoparser2 0.42.0's
	// parse_event("player_death") gives the same count on both demos, and
	// the same tick, weapon and flags on every row.
	deaths int
	// fired events fire at least once, on top of the ones in alwaysFired.
	fired []string
	// unfired events are in the descriptor list but never fire.
	unfired []string
}{
	{file: "faceit_nuke_2026.dem", deaths: 157, unfired: []string{"round_end", "round_start"}},
	// Patch 13984 still fires the round events that later patches dropped.
	{file: "valve_mm_mirage_2024.dem", deaths: 73, fired: []string{"round_end", "round_start"}},
}

// alwaysFired are events that every demo fires at least once.
var alwaysFired = []string{"player_death", "player_hurt", "round_freeze_end", "round_officially_ended"}

// walk decodes every game event in the demo at path and calls fn with each.
func walk(t *testing.T, path string, fn func(*events.Decoder, events.Event)) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := demo.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	d := demo.NewDecoder(r)
	var mr demo.MessageReader
	var ev events.Decoder
	for {
		fr, err := d.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if fr.Cmd != demo.CmdPacket && fr.Cmd != demo.CmdSignonPacket && fr.Cmd != demo.CmdFullPacket {
			continue
		}
		if err := mr.Reset(fr); err != nil {
			t.Fatal(err)
		}
		for {
			m, err := mr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			switch m.Type {
			case demo.MsgType(pb.EBaseGameEvents_GE_Source1LegacyGameEventList):
				if err := ev.HandleList(m.Body); err != nil {
					t.Fatal(err)
				}
			case demo.MsgType(pb.EBaseGameEvents_GE_Source1LegacyGameEvent):
				e, err := ev.Decode(m.Body, fr.Tick)
				if err != nil {
					t.Fatalf("frame at offset %d: %v", fr.Offset, err)
				}
				fn(&ev, e)
			}
		}
	}
}

func TestRealDemos(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	for _, rd := range realDemos {
		t.Run(rd.file, func(t *testing.T) {
			path := filepath.Join("..", "testdata", "demos", rd.file)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				t.Skipf("%s not in testdata/demos", rd.file)
			}
			counts := map[string]int{}
			var dec *events.Decoder
			walk(t, path, func(d *events.Decoder, e events.Event) {
				dec = d
				counts[e.Name]++
				switch e.Name {
				case "player_death":
					checkKeys(t, e, []string{"userid", "attacker", "assister"}, []string{"assistedflash", "headshot"}, []string{"weapon"})
				case "player_hurt":
					checkKeys(t, e, []string{"userid", "attacker", "dmg_health", "health"}, nil, []string{"weapon"})
				}
			})
			if dec == nil {
				t.Fatal("no game events")
			}

			names := make([]string, 0, len(counts))
			total := 0
			for name, n := range counts {
				names = append(names, name)
				total += n
			}
			slices.Sort(names)
			var sb strings.Builder
			for _, name := range names {
				fmt.Fprintf(&sb, "\n  %-36s %6d", name, counts[name])
			}
			t.Logf("%d events of %d kinds:%s", total, len(counts), sb.String())
			if desc, ok := dec.Descriptor("player_death"); ok {
				t.Logf("player_death is id %d with keys %v", desc.ID, desc.Keys)
			}

			if got := counts["player_death"]; got != rd.deaths {
				t.Errorf("%d player_death events, want %d", got, rd.deaths)
			}
			for _, name := range slices.Concat(alwaysFired, rd.fired) {
				if counts[name] == 0 {
					t.Errorf("no %s events", name)
				}
			}
			for _, name := range rd.unfired {
				if _, ok := dec.Descriptor(name); !ok {
					t.Errorf("%s is not in the descriptor list", name)
				}
				if counts[name] != 0 {
					t.Errorf("%s fired %d times, want 0", name, counts[name])
				}
			}
		})
	}
}

// checkKeys reports keys missing from e: ints, bools and strings, by type.
func checkKeys(t *testing.T, e events.Event, ints, bools, strs []string) {
	t.Helper()
	for _, k := range ints {
		if _, ok := e.Int(k); !ok {
			t.Errorf("%s at tick %d: no int %s", e.Name, e.Tick, k)
		}
	}
	for _, k := range bools {
		if _, ok := e.Bool(k); !ok {
			t.Errorf("%s at tick %d: no bool %s", e.Name, e.Tick, k)
		}
	}
	for _, k := range strs {
		if _, ok := e.String(k); !ok {
			t.Errorf("%s at tick %d: no string %s", e.Name, e.Tick, k)
		}
	}
}
