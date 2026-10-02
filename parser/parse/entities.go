package parse

import (
	"fmt"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/extract"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/stringtables"
)

// entityStage decodes entities and samples them once per tick for round
// files. The first error ends it; the parse goes on without it.
type entityStage struct {
	sers    *entities.Serializers
	state   *entities.State
	tables  stringtables.Tables
	rec     *extract.Recorder
	applied bool // an svc_PacketEntities has been applied
}

func (s *entityStage) sendTables(f demo.Frame) error {
	var m pb.CDemoSendTables
	if err := f.Unmarshal(&m); err != nil {
		return err
	}
	var err error
	s.sers, err = entities.DecodeSendTables(&m)
	return err
}

func (s *entityStage) classInfo(f demo.Frame) error {
	switch {
	case s.sers == nil:
		return fmt.Errorf("%w: class info before send tables", demo.ErrBadDemo)
	case s.state != nil:
		return fmt.Errorf("%w: a second class info", demo.ErrBadDemo)
	}
	var m pb.CDemoClassInfo
	if err := f.Unmarshal(&m); err != nil {
		return err
	}
	classes, err := entities.NewClasses(&m, s.sers)
	if err != nil {
		return err
	}
	if s.rec, err = extract.New(classes); err != nil {
		return err
	}
	s.state = entities.NewState(classes)
	return nil
}

// message applies a packet message that changes string tables or
// entities. A DEM_FullPacket repeats the state, so only one that comes
// first applies.
func (s *entityStage) message(m demo.Message, f demo.Frame) error {
	if _, err := s.tables.Apply(m); err != nil {
		return err
	}
	if m.Type != entities.MsgPacketEntities || f.Cmd == demo.CmdFullPacket && s.applied {
		return nil
	}
	if s.state == nil {
		return fmt.Errorf("%w: entities before class info", demo.ErrBadDemo)
	}
	var pe pb.CSVCMsg_PacketEntities
	if err := m.Unmarshal(&pe); err != nil {
		return err
	}
	s.applied = true
	return s.state.Apply(&pe, s.tables.Get(stringtables.InstanceBaseline))
}

// sampleTo samples every tick from the last one sampled through tick, so a
// tick takes the state after the first frame at it, or after the last
// frame before it if it has none. Sampling starts at the first frame tick
// once classes are known.
func (s *entityStage) sampleTo(tick int32) {
	if s.rec == nil || tick < 0 {
		return
	}
	next, ok := s.rec.Next()
	if !ok {
		next = tick
	}
	for ; next <= tick; next++ {
		s.rec.Sample(next, s.state)
	}
}
