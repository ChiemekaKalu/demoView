package entities_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/stringtables"
)

// deltaGaps are fields that the 2024 MM fixture's server left out of its
// deltas but put in its snapshots: players' per-round stats, and pawn
// fields that matter only to the player's own client. A prefix ending in
// "." covers everything under it.
var deltaGaps = map[string][]string{
	"valve_mm_mirage_2024.dem": {
		"CCSPlayerController m_pActionTrackingServices.m_perRoundStats.",
		"CCSPlayerPawn m_skybox3d.",
		"CCSPlayerPawn m_flTimeScale",
		"CCSPlayerPawn m_flGravityScale",
		"CCSPlayerPawn m_flFriction",
		"CCSPlayerPawn m_nNextThinkTick",
		"CCSPlayerPawn m_flNextSprayDecalTime",
		"CCSPlayerPawn m_pWeaponServices.m_flNextAttack",
		"CCSPlayerPawn m_pWeaponServices.m_hLastWeapon",
		"CCSPlayerPawn m_pMovementServices.m_flLastDuckTime",
		"CCSPlayerPawn m_pMovementServices.m_flCrouchTransitionStartTime",
		"CCSPlayerPawn m_pMovementServices.m_bDucking",
	},
}

// TestSnapshots checks the state that deltas build against each
// DEM_FullPacket after the first. A snapshot re-sends every entity from its
// baseline as of the packet before it, so it must have the same entities,
// and every value it carries must equal the state's. (Snapshots leave out
// some fields that deltas set, such as simulation times and match stats.
// Those aren't compared.)
func TestSnapshots(t *testing.T) {
	if testing.Short() {
		t.Skip("decodes whole demos")
	}
	for _, name := range realDemos {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "testdata", "demos", name)
			skipIfAbsent(t, path)
			w := &walker{}
			snapshots, values, gaps := 0, 0, 0
			w.snapshot = func(f demo.Frame, m *pb.CSVCMsg_PacketEntities) {
				bad, n, err := entities.CheckSnapshot(w.state, m, w.tables.Get(stringtables.InstanceBaseline))
				if err != nil {
					t.Fatalf("tick %d: %v", f.Tick, err)
				}
				for _, b := range bad {
					if isDeltaGap(name, b) {
						gaps++
						continue
					}
					t.Errorf("tick %d: entity %d %s %s: state %#v, snapshot %#v", f.Tick, b.Entity, b.Class, b.Field, b.State, b.Snapshot)
				}
				snapshots++
				values += n
			}
			w.run(t, path)
			if snapshots == 0 {
				t.Fatal("no snapshots after the first packet")
			}
			t.Logf("%d snapshots, %d values; %d differ, all in known delta gaps", snapshots, values, gaps)
		})
	}
}

func isDeltaGap(demo string, m entities.Mismatch) bool {
	name := m.Class + " " + m.Field
	for _, gap := range deltaGaps[demo] {
		if name == gap || strings.HasSuffix(gap, ".") && strings.HasPrefix(name, gap) {
			return true
		}
	}
	return false
}
