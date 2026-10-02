package parse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/parse/parsetest"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

var (
	header = parsetest.Marshal(&pb.CDemoFileHeader{
		DemoFileStamp: proto.String("PBDEMS2\x00"),
		MapName:       proto.String("de_mirage"),
	})
	serverInfo = parsetest.Msg{
		Type: uint32(pb.SVC_Messages_svc_ServerInfo),
		Body: parsetest.Marshal(&pb.CSVCMsg_ServerInfo{TickInterval: proto.Float32(1.0 / 64)}),
	}
	tick = parsetest.Msg{
		Type: uint32(pb.NET_Messages_net_Tick),
		Body: parsetest.Marshal(&pb.CNETMsg_Tick{Tick: proto.Uint32(5)}),
	}
)

// goodDemo has a FileHeader, a compressed SignonPacket with ServerInfo, a
// Packet, Stop and a FileInfo.
var goodDemo = parsetest.Build(
	parsetest.Frame{Cmd: demo.CmdFileHeader, Payload: header},
	parsetest.Frame{Cmd: demo.CmdSignonPacket, Tick: -1, Compress: true, Payload: parsetest.Packet(serverInfo, tick)},
	parsetest.Frame{Cmd: demo.CmdPacket, Tick: 5, Payload: parsetest.Packet(tick)},
	parsetest.Frame{Cmd: demo.CmdStop, Tick: 6},
	parsetest.Frame{Cmd: demo.CmdFileInfo, Tick: 6, Payload: parsetest.Marshal(&pb.CDemoFileInfo{PlaybackTicks: proto.Int32(6)})},
)

func TestRun(t *testing.T) {
	data := parsetest.Match()
	got, err := Run(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	want := match.File{
		SchemaVersion: match.SchemaVersion,
		ParserVersion: Version,
		Match:         match.Info{Map: "de_mirage", TickRate: 64, DemoSHA256: hex.EncodeToString(sum[:])},
	}
	if got.SchemaVersion != want.SchemaVersion || got.ParserVersion != want.ParserVersion || got.Match != want.Match {
		t.Errorf("Run = %+v, want %+v", got, want)
	}
}

func TestRunWithoutHeaderOrServerInfo(t *testing.T) {
	got, err := Run(context.Background(), bytes.NewReader(parsetest.Build(parsetest.MatchFrames()...)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Match.Map != "" || got.Match.TickRate != 0 {
		t.Errorf("Match = %+v, want no map or tick rate", got.Match)
	}
}

func TestRunBadDemo(t *testing.T) {
	badInfo := parsetest.Msg{Type: serverInfo.Type, Body: []byte{0x6A, 0x09, 'x'}} // field 13 length-delimited, truncated
	tests := map[string][]byte{
		"bad magic":        []byte("HL2DEMO\x00 not a cs2 demo"),
		"short preamble":   []byte("PBDEMS2"),
		"truncated frame":  goodDemo[:len(goodDemo)-3],
		"bad header proto": parsetest.Build(parsetest.Frame{Cmd: demo.CmdFileHeader, Payload: []byte{0x2A, 0x09, 'd'}}),
		"bad server info":  parsetest.Build(parsetest.Frame{Cmd: demo.CmdSignonPacket, Payload: parsetest.Packet(badInfo)}),
	}
	for name, data := range tests {
		_, err := Run(context.Background(), bytes.NewReader(data))
		if !errors.Is(err, demo.ErrBadDemo) {
			t.Errorf("%s: error = %v, want ErrBadDemo", name, err)
		}
	}
}

// failingReader returns its data, then err instead of io.EOF.
type failingReader struct {
	r   io.Reader
	err error
}

func (f *failingReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF {
		return n, f.err
	}
	return n, err
}

func TestRunReaderErrorIsNotBadDemo(t *testing.T) {
	errNet := errors.New("connection reset")
	_, err := Run(context.Background(), &failingReader{bytes.NewReader(goodDemo[:40]), errNet})
	if !errors.Is(err, errNet) || errors.Is(err, demo.ErrBadDemo) {
		t.Errorf("error = %v, want the reader's error and not ErrBadDemo", err)
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, bytes.NewReader(goodDemo)); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// realDemos are facts about the fixture demos, from the game's own counters
// and round results (read with demoparser2) and the reference exporter. A
// demo in testdata/demos that isn't listed is checked for invariants only.
var realDemos = map[string]struct {
	rounds, kills, players int
	scoreA, scoreB         int
	reasons                map[string]int
	notes                  int
}{
	// Rounds 1 to 12 with team A on CT, then T. Two bots of the same name
	// stood in for players who dropped, one on each side; they count as
	// one player, which gets a note.
	"faceit_nuke_2026.dem": {19, 138, 11, 13, 6, map[string]int{"elimination": 16, "bomb_defused": 2, "bomb_exploded": 1}, 1},
	// Round 10 is a CT surrender during freeze time.
	"valve_mm_mirage_2024.dem": {10, 64, 10, 2, 8, map[string]int{"elimination": 6, "bomb_defused": 1, "bomb_exploded": 2, "surrender": 1}, 0},
}

// TestRealDemos parses whatever demos are in testdata/demos and checks that
// match.json holds together. They are not in git, so it skips when there
// are none.
func TestRealDemos(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	paths, _ := filepath.Glob(filepath.Join("..", "testdata", "demos", "*.dem"))
	if len(paths) == 0 {
		t.Skip("no demos in testdata/demos")
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			f, err := os.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			start := time.Now()
			got, notes, err := run(context.Background(), f)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("map=%s tick_rate=%d sha256=%s in %v", got.Match.Map, got.Match.TickRate, got.Match.DemoSHA256, time.Since(start))
			if !strings.HasPrefix(got.Match.Map, "de_") || got.Match.TickRate != 64 {
				t.Errorf("Match = %+v, want a de_ map at 64 ticks", got.Match)
			}
			for _, n := range notes {
				t.Logf("note: %s", n)
			}
			checkMatch(t, got)

			want, ok := realDemos[filepath.Base(p)]
			if !ok {
				return
			}
			if len(notes) != want.notes {
				t.Errorf("%d notes, want %d", len(notes), want.notes)
			}
			score := map[string]int{}
			reasons := map[string]int{}
			for _, r := range got.Rounds {
				score[r.Winner]++
				reasons[r.Reason]++
			}
			if len(got.Rounds) != want.rounds || len(got.Kills) != want.kills || len(got.Players) != want.players {
				t.Errorf("%d rounds, %d kills, %d players; want %d, %d, %d",
					len(got.Rounds), len(got.Kills), len(got.Players), want.rounds, want.kills, want.players)
			}
			if score["A"] != want.scoreA || score["B"] != want.scoreB {
				t.Errorf("score %d-%d, want %d-%d", score["A"], score["B"], want.scoreA, want.scoreB)
			}
			if !maps.Equal(reasons, want.reasons) {
				t.Errorf("reasons %v, want %v", reasons, want.reasons)
			}
		})
	}
}

// checkMatch checks the rules match.json must keep: the schema's shapes and
// enums, and the conventions the validator warns about.
func checkMatch(t *testing.T, m match.File) {
	t.Helper()
	if len(m.Players) == 0 || len(m.Rounds) == 0 {
		t.Fatalf("%d players and %d rounds, want some of each", len(m.Players), len(m.Rounds))
	}

	team := make([]string, len(m.Players))
	for i, p := range m.Players {
		team[i] = p.Team
		if p.Idx != i || (p.Team != "A" && p.Team != "B") || p.SteamID == "" || strings.Trim(p.SteamID, "0123456789") != "" {
			t.Errorf("players[%d] = idx %d, team %q, steam_id %q", i, p.Idx, p.Team, p.SteamID)
		}
		if i > 0 && m.Players[i-1].Team == "B" && p.Team == "A" {
			t.Errorf("players[%d] is on team A after a team B player", i)
		}
	}
	valid := func(idx *int) bool { return idx == nil || (*idx >= 0 && *idx < len(m.Players)) }

	reasons := []string{"elimination", "bomb_exploded", "bomb_defused", "time_expired", "surrender"}
	for i, r := range m.Rounds {
		if r.N != i+1 || !(r.StartTick <= r.FreezeEndTick && r.FreezeEndTick <= r.EndTick) {
			t.Errorf("rounds[%d] = n %d, ticks %d, %d, %d", i, r.N, r.StartTick, r.FreezeEndTick, r.EndTick)
		}
		if i+1 < len(m.Rounds) && m.Rounds[i+1].StartTick < r.EndTick {
			t.Errorf("round %d ends at %d, after round %d starts", r.N, r.EndTick, r.N+1)
		}
		if (r.Winner != "A" && r.Winner != "B") || (r.SideA != "CT" && r.SideA != "T") || !slices.Contains(reasons, r.Reason) {
			t.Errorf("round %d: winner %q, side_a %q, reason %q", r.N, r.Winner, r.SideA, r.Reason)
		}
	}
	if m.Rounds[0].SideA != "CT" {
		t.Errorf("team A plays %s in round 1, want CT", m.Rounds[0].SideA)
	}

	// The scoreboard must agree with the kill list, counted with match
	// teams.
	var kills, deaths, assists, hs = make([]int, len(m.Players)), make([]int, len(m.Players)), make([]int, len(m.Players)), make([]int, len(m.Players))
	for i, k := range m.Kills {
		if !valid(k.Attacker) || !valid(&k.Victim) || !valid(k.Assister) || k.Weapon == "" || strings.HasPrefix(k.Weapon, "weapon_") {
			t.Fatalf("kills[%d] = %+v", i, k)
		}
		if i > 0 && k.Tick < m.Kills[i-1].Tick {
			t.Errorf("kills[%d] at tick %d is before the kill before it", i, k.Tick)
		}
		if k.Round < 1 || k.Round > len(m.Rounds) {
			t.Fatalf("kills[%d] is in round %d", i, k.Round)
		}
		r := m.Rounds[k.Round-1]
		if k.Tick < r.StartTick || (k.Round < len(m.Rounds) && k.Tick >= m.Rounds[k.Round].StartTick) {
			t.Errorf("kills[%d] at tick %d is outside round %d", i, k.Tick, k.Round)
		}
		deaths[k.Victim]++
		if k.Assister != nil {
			assists[*k.Assister]++
		}
		if a := k.Attacker; a != nil && *a != k.Victim && team[*a] != team[k.Victim] {
			kills[*a]++
			if k.Headshot {
				hs[*a]++
			}
		}
	}
	if len(m.Scoreboard) != len(m.Players) {
		t.Fatalf("%d scoreboard rows for %d players", len(m.Scoreboard), len(m.Players))
	}
	for i, row := range m.Scoreboard {
		hsPct := 0.0
		if kills[i] > 0 {
			hsPct = math.Round(1000*float64(hs[i])/float64(kills[i])) / 10
		}
		if row.Player != i || row.Kills != kills[i] || row.Deaths != deaths[i] || row.Assists != assists[i] || row.HSPct != hsPct || row.ADR < 0 {
			t.Errorf("scoreboard[%d] = %+v; the kill list gives %d kills, %d deaths, %d assists, hs_pct %.1f",
				i, row, kills[i], deaths[i], assists[i], hsPct)
		}
	}
}
