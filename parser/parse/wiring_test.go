package parse

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse/parsetest"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// TestRunMatch checks that Run feeds events, string tables, cvars and the
// end-of-match message to match logic: a one-round 1v1.
func TestRunMatch(t *testing.T) {
	got, notes, err := run(context.Background(), bytes.NewReader(parsetest.Match()))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if !strings.HasPrefix(n, "the match ended ") { // a one-round match ends early
			t.Errorf("note: %s", n)
		}
	}
	if len(got.Players) != 2 || got.Players[0].Name != "CT player" || got.Players[0].Team != "A" || got.Players[1].SteamID != "1001" {
		t.Errorf("players %+v", got.Players)
	}
	want := match.Round{N: 1, StartTick: 100, FreezeEndTick: 200, EndTick: 300, Winner: "A", Reason: "elimination", SideA: "CT"}
	if len(got.Rounds) != 1 || got.Rounds[0] != want {
		t.Errorf("rounds %+v", got.Rounds)
	}
	if len(got.Kills) != 1 || *got.Kills[0].Attacker != 0 || got.Kills[0].Victim != 1 || got.Scoreboard[0].Kills != 1 {
		t.Errorf("kills %+v, scoreboard %+v", got.Kills, got.Scoreboard)
	}
}

// An event that doesn't decode is a bad demo, and the error says which
// frame holds it.
func TestRunBadEventNamesFrame(t *testing.T) {
	unknown := parsetest.Msg{
		Type: uint32(pb.EBaseGameEvents_GE_Source1LegacyGameEvent),
		Body: parsetest.Marshal(&pb.CMsgSource1LegacyGameEvent{Eventid: proto.Int32(99)}),
	}
	data := parsetest.Build(
		parsetest.Frame{Cmd: demo.CmdSignonPacket, Tick: -1, Payload: parsetest.Packet(parsetest.EventList())},
		parsetest.Frame{Cmd: demo.CmdPacket, Tick: 7, Payload: parsetest.Packet(unknown)},
	)
	_, err := Run(context.Background(), bytes.NewReader(data))
	if !errors.Is(err, events.ErrUnknownEvent) || !strings.Contains(err.Error(), "Packet frame at offset ") || !strings.Contains(err.Error(), "tick 7") {
		t.Errorf("error = %v, want ErrUnknownEvent naming the frame", err)
	}
}

// A demo with no live round has nothing to show, so it is a bad demo.
func TestRunNoLiveRounds(t *testing.T) {
	data := parsetest.Build(parsetest.Frame{Cmd: demo.CmdSignonPacket, Tick: -1, Payload: parsetest.Packet(parsetest.EventList())})
	if _, err := Run(context.Background(), bytes.NewReader(data)); !errors.Is(err, demo.ErrBadDemo) {
		t.Errorf("error = %v, want ErrBadDemo", err)
	}
}
