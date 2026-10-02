package entities_test

import (
	"path/filepath"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/stringtables"
)

// BenchmarkApply applies every svc_PacketEntities of the 2024 MM fixture,
// read ahead of time, to a new State. The baselines are the demo's last.
func BenchmarkApply(b *testing.B) {
	path := filepath.Join("..", "testdata", "demos", "valve_mm_mirage_2024.dem")
	if testing.Short() {
		b.Skip("reads a real demo")
	}
	var msgs []*pb.CSVCMsg_PacketEntities
	w := &walker{snapshot: func(demo.Frame, *pb.CSVCMsg_PacketEntities) {}}
	w.collect = func(m *pb.CSVCMsg_PacketEntities) { msgs = append(msgs, m) }
	w.run(b, path)
	baselines := w.tables.Get(stringtables.InstanceBaseline)
	bytes := 0
	for _, m := range msgs {
		bytes += len(m.GetEntityData())
	}
	b.SetBytes(int64(bytes))
	b.ReportAllocs()
	for b.Loop() {
		s := entities.NewState(w.classes)
		for _, m := range msgs {
			if err := s.Apply(m, baselines); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(msgs)), "ns/msg")
}
