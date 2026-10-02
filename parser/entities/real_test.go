package entities_test

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/pb"
)

// realDemos are the interim fixture demos. They are not in git, so each one
// is skipped when absent.
var realDemos = []string{"faceit_nuke_2026.dem", "valve_mm_mirage_2024.dem"}

// projectiles are the grenade projectile classes the viewer draws.
var projectiles = []string{
	"CDecoyProjectile",
	"CFlashbangProjectile",
	"CHEGrenadeProjectile",
	"CMolotovProjectile",
	"CSmokeGrenadeProjectile",
}

// origin is the position of an entity with a body component, in the body
// component's serializer. Some versions of it send the offsets unquantized.
var origin = map[string][]entities.Kind{
	"m_skeletonInstance.m_vecOrigin.m_cellX": {entities.VarUint32},
	"m_skeletonInstance.m_vecOrigin.m_cellY": {entities.VarUint32},
	"m_skeletonInstance.m_vecOrigin.m_cellZ": {entities.VarUint32},
	"m_skeletonInstance.m_vecOrigin.m_vecX":  {entities.QuantizedFloat, entities.Float32},
	"m_skeletonInstance.m_vecOrigin.m_vecY":  {entities.QuantizedFloat, entities.Float32},
	"m_skeletonInstance.m_vecOrigin.m_vecZ":  {entities.QuantizedFloat, entities.Float32},
}

// pawnFields are the player pawn fields the viewer reads, besides origin.
var pawnFields = map[string][]entities.Kind{
	"m_angEyeAngles": {entities.QAnglePrecise},
	"m_iHealth":      {entities.VarInt32},
	"m_lifeState":    {entities.VarUint32},
	"m_iTeamNum":     {entities.VarUint32},
	"m_hController":  {entities.Handle},
}

// knownUnclassified are the element types the fixture demos use that have
// no decoder. Entity data never sends one in the local demos, and none is on
// anything the viewer reads.
var knownUnclassified = []string{"CTransform"}

func TestRealDemos(t *testing.T) {
	if testing.Short() {
		t.Skip("reads real demos")
	}
	for _, name := range realDemos {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "testdata", "demos", name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present", path)
			}
			sers, classes := load(t, path)

			for c := range classes.All() {
				if c.Serializer == nil || c.Serializer.Name != c.Name {
					t.Errorf("class %d %s has serializer %v", c.ID, c.Name, c.Serializer)
				}
			}
			for _, name := range append([]string{"CCSPlayerController", "CCSGameRulesProxy", "CCSPlayerPawn"}, projectiles...) {
				if classes.ByName(name) == nil {
					t.Fatalf("no class %s", name)
				}
			}

			pawn := classes.ByName("CCSPlayerPawn").Serializer
			checkFields(t, pawn, pawnFields)
			for _, name := range append([]string{"CCSPlayerPawn"}, projectiles...) {
				checkFields(t, body(t, classes.ByName(name).Serializer), origin)
			}

			for _, f := range sers.Unclassified() {
				if leaf := unknownLeaf(f); !slices.Contains(knownUnclassified, leaf.Type) {
					t.Errorf("field %s: no decoder for %s (encoder %q, var serializer %q)", f.FullName(), leaf.Type, leaf.Encoder, leaf.VarSerializer)
				}
			}

			logSummary(t, sers, classes)
			logFields(t, pawn)
		})
	}
}

// skipIfAbsent skips the test if the demo at path isn't present.
func skipIfAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
}

// load reads the demo at path up to its DEM_ClassInfo.
func load(t *testing.T, path string) (*entities.Serializers, *entities.Classes) {
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
	var sers *entities.Serializers
	for {
		fr, err := d.Next()
		if err == io.EOF {
			t.Fatal("no DEM_ClassInfo")
		}
		if err != nil {
			t.Fatal(err)
		}
		switch fr.Cmd {
		case demo.CmdSendTables:
			var st pb.CDemoSendTables
			if err := fr.Unmarshal(&st); err != nil {
				t.Fatal(err)
			}
			if sers, err = entities.DecodeSendTables(&st); err != nil {
				t.Fatal(err)
			}
		case demo.CmdClassInfo:
			if sers == nil {
				t.Fatal("DEM_ClassInfo before DEM_SendTables")
			}
			var ci pb.CDemoClassInfo
			if err := fr.Unmarshal(&ci); err != nil {
				t.Fatal(err)
			}
			classes, err := entities.NewClasses(&ci, sers)
			if err != nil {
				t.Fatal(err)
			}
			return sers, classes
		}
	}
}

// body returns the serializer of s's body component.
func body(t *testing.T, s *entities.Serializer) *entities.Serializer {
	t.Helper()
	i := s.FieldIndex("CBodyComponent")
	if i < 0 || s.Fields[i].Decoder.Kind != entities.Pointer {
		t.Fatalf("%s: no CBodyComponent pointer", s.Name)
	}
	return s.Fields[i].Serializer
}

// checkFields checks that s has the fields in want, each with one of its
// kinds.
func checkFields(t *testing.T, s *entities.Serializer, want map[string][]entities.Kind) {
	t.Helper()
	for name, kinds := range want {
		i := s.FieldIndex(name)
		if i < 0 {
			t.Errorf("%s/%d: no field %s", s.Name, s.Version, name)
			continue
		}
		if got := s.Fields[i].Decoder.Kind; !slices.Contains(kinds, got) {
			t.Errorf("%s/%d: %s is %s, want one of %v", s.Name, s.Version, name, got, kinds)
		}
	}
}

// unknownLeaf returns the first field in f's element chain with no decoder.
func unknownLeaf(f *entities.Field) *entities.Field {
	for f.Decoder.Kind != entities.Unknown {
		f = f.Elem
	}
	return f
}

func logSummary(t *testing.T, sers *entities.Serializers, classes *entities.Classes) {
	listed := 0
	distinct := map[*entities.Field]bool{}
	for s := range sers.All() {
		listed += len(s.Fields)
		for _, f := range s.Fields {
			distinct[f] = true
		}
	}
	kinds := map[string]int{}
	for f := range distinct {
		for ; f != nil; f = f.Elem {
			kinds[f.Decoder.String()]++
		}
	}
	t.Logf("%d classes (id bits %d), %d serializers, %d fields (%d listed)",
		classes.Len(), classes.IDBits(), sers.Len(), len(distinct), listed)

	var b strings.Builder
	for _, k := range slices.SortedFunc(maps.Keys(kinds), func(a, b string) int {
		return cmp.Or(kinds[b]-kinds[a], strings.Compare(a, b))
	}) {
		fmt.Fprintf(&b, "\n  %5d %s", kinds[k], k)
	}
	t.Logf("decoders, counting array elements:%s", b.String())

	b.Reset()
	for _, f := range sers.Unclassified() {
		leaf := unknownLeaf(f)
		fmt.Fprintf(&b, "\n  %s: %s (encoder %q, var serializer %q, bits %d)", f.FullName(), leaf.Type, leaf.Encoder, leaf.VarSerializer, leaf.BitCount)
	}
	t.Logf("unclassified:%s", b.String())
}

func logFields(t *testing.T, s *entities.Serializer) {
	var b strings.Builder
	for i, f := range s.Fields {
		fmt.Fprintf(&b, "\n  %3d %-48s %-44s %s", i, f.FullName(), f.Type, describe(f))
	}
	t.Logf("%s/%d fields:%s", s.Name, s.Version, b.String())
}
