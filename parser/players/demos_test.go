package players

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// demoScan is what one pass over a demo found.
type demoScan struct {
	table        Table
	snapshots    int                // string-table snapshots applied
	withUserinfo int                // snapshots that carry the userinfo table
	lastUserinfo int32              // tick of the last one
	endOfMatch   []EndOfMatchPlayer // the last end-of-match message
	eomCount     int                // end-of-match messages seen
	eomTick      int32
	slotErrors   []string // userinfo entries whose userid's low byte isn't their key
}

// scanDemo walks every frame, applies every string-table snapshot and decodes
// the end-of-match user message.
func scanDemo(t *testing.T, r io.Reader) *demoScan {
	t.Helper()
	dr, err := demo.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	d := demo.NewDecoder(dr)
	var mr demo.MessageReader
	s := &demoScan{}
	apply := func(f demo.Frame, st *pb.CDemoStringTables) {
		if err := s.table.ApplyStringTables(st); err != nil {
			t.Fatalf("%s frame at offset %d: %v", f.Cmd, f.Offset, err)
		}
		s.snapshots++
		for _, tbl := range st.GetTables() {
			if tbl.GetTableName() == "userinfo" {
				s.withUserinfo++
				s.lastUserinfo = f.Tick
			}
		}
		for slot, info := range s.table.All() {
			if SlotOf(info.UserID) != slot {
				s.slotErrors = append(s.slotErrors, info.Name+" in slot "+strconv.Itoa(int(slot))+" has userid "+strconv.Itoa(int(info.UserID)))
			}
		}
	}
	for {
		f, err := d.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch f.Cmd {
		case demo.CmdStringTables:
			var st pb.CDemoStringTables
			if err := f.Unmarshal(&st); err != nil {
				t.Fatal(err)
			}
			apply(f, &st)
		case demo.CmdFullPacket:
			var fp pb.CDemoFullPacket
			if err := f.Unmarshal(&fp); err != nil {
				t.Fatal(err)
			}
			apply(f, fp.GetStringTable())
		}
		if f.Cmd != demo.CmdPacket && f.Cmd != demo.CmdSignonPacket && f.Cmd != demo.CmdFullPacket {
			continue
		}
		if err := mr.Reset(f); err != nil {
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
			if m.Type != MsgEndOfMatch {
				continue
			}
			players, err := DecodeEndOfMatch(m.Body)
			if err != nil {
				t.Fatalf("tick %d: %v", f.Tick, err)
			}
			s.endOfMatch, s.eomTick = players, f.Tick
			s.eomCount++
		}
	}
	return s
}

func TestDemoPlayers(t *testing.T) {
	tests := []struct {
		file          string
		snapshots     int // 1 DEM_StringTables + one per DEM_FullPacket
		withUserinfo  int // full packets carry only the tables that changed
		hasEndOfMatch bool
	}{
		{"faceit_nuke_2026.dem", 38, 8, true},
		{"valve_mm_mirage_2024.dem", 17, 4, true},
	}
	for _, tt := range tests {
		t.Run(strings.TrimSuffix(tt.file, ".dem"), func(t *testing.T) {
			f, err := os.Open(filepath.Join("..", "testdata", "demos", tt.file))
			if errors.Is(err, fs.ErrNotExist) {
				t.Skipf("fixture not present: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			s := scanDemo(t, f)

			team := map[Slot]int32{}
			for _, p := range s.endOfMatch {
				team[p.Slot] = p.Team
			}
			t.Logf("%d string-table snapshots, %d with userinfo (last at tick %d); end-of-match messages: %d (tick %d)",
				s.snapshots, s.withUserinfo, s.lastUserinfo, s.eomCount, s.eomTick)
			t.Logf("%4s  %-20s %-17s %6s %-9s %s", "slot", "name", "steamid64", "userid", "kind", "eom team")
			for slot, i := range s.table.All() {
				kind := "human"
				switch {
				case i.HLTV:
					kind = "gotv"
				case i.Fake:
					kind = "bot"
				}
				eom := "-"
				if tm, ok := team[slot]; ok {
					eom = strconv.Itoa(int(tm))
				}
				t.Logf("%4d  %-20s %-17d %6d %-9s %s", slot, i.Name, i.SteamID64, i.UserID, kind, eom)
			}

			if s.snapshots != tt.snapshots || s.withUserinfo != tt.withUserinfo {
				t.Errorf("snapshots = %d, %d with userinfo; want %d, %d", s.snapshots, s.withUserinfo, tt.snapshots, tt.withUserinfo)
			}
			for _, e := range s.slotErrors {
				t.Errorf("userid low byte is not the slot: %s", e)
			}

			humans, hltv := 0, 0
			for slot, i := range s.table.All() {
				if i.HLTV {
					hltv++
					if !i.Fake || i.SteamID64 != 0 {
						t.Errorf("slot %d: GOTV %+v, want Fake and SteamID64 0", slot, i)
					}
					continue
				}
				if i.Fake {
					continue
				}
				humans++
				id := strconv.FormatUint(i.SteamID64, 10)
				if len(id) != 17 || !strings.HasPrefix(id, "7656119") {
					t.Errorf("slot %d (%s): SteamID64 %s is not a 17-digit 7656119... id", slot, i.Name, id)
				}
				if i.Name == "" {
					t.Errorf("slot %d: empty name", slot)
				}
			}
			if humans != 10 {
				t.Errorf("humans = %d, want 10", humans)
			}
			if hltv != 1 {
				t.Errorf("GOTV entries = %d, want 1", hltv)
			}

			if got := s.eomCount > 0; got != tt.hasEndOfMatch {
				t.Fatalf("has end-of-match message = %v, want %v", got, tt.hasEndOfMatch)
			}
			if !tt.hasEndOfMatch {
				return
			}
			if s.eomCount != 1 {
				t.Errorf("end-of-match messages = %d, want 1", s.eomCount)
			}
			// Every end-of-match player is in the table under the same slot,
			// name and Steam id, and the teams split 5 and 5.
			perTeam := map[int32]int{}
			for _, p := range s.endOfMatch {
				perTeam[p.Team]++
				i, ok := s.table.Get(p.Slot)
				if !ok {
					t.Errorf("end-of-match player %+v: slot not in table", p)
					continue
				}
				if i.Name != p.Name || i.SteamID64 != p.SteamID64 || i.Bot() != p.Bot {
					t.Errorf("end-of-match player %+v, table has %+v", p, i)
				}
			}
			if len(s.endOfMatch) != 10 || perTeam[2] != 5 || perTeam[3] != 5 {
				t.Errorf("end-of-match: %d players, teams %v; want 5 on team 2 and 5 on team 3", len(s.endOfMatch), perTeam)
			}
		})
	}
}
