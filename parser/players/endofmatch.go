package players

import (
	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"google.golang.org/protobuf/proto"
)

// MsgEndOfMatch is the packet message type of CS_UM_EndOfMatchAllPlayersData,
// the user message that lists every player at the end of the match. Body is a
// CCSUsrMsg_EndOfMatchAllPlayersData; decode it with DecodeEndOfMatch.
//
// Not every demo has one: it is reportedly missing from some community
// server demos.
const MsgEndOfMatch = demo.MsgType(pb.ECstrike15UserMessages_CS_UM_EndOfMatchAllPlayersData)

// EndOfMatchPlayer is one player from the end-of-match message.
type EndOfMatchPlayer struct {
	Slot      Slot
	SteamID64 uint64 // 0 for bots
	Name      string
	Team      int32 // team number at the end of the match: 2 = T, 3 = CT
	Bot       bool
}

// DecodeEndOfMatch decodes the body of a MsgEndOfMatch message, giving the
// players in message order. Team is the side each player finished on, after
// any halftime and overtime swaps. Malformed data, or a player without a
// slot in 0..255, gives an error wrapping demo.ErrBadDemo.
func DecodeEndOfMatch(body []byte) ([]EndOfMatchPlayer, error) {
	var m pb.CCSUsrMsg_EndOfMatchAllPlayersData
	if err := proto.Unmarshal(body, &m); err != nil {
		return nil, badDemof("end-of-match message: %v", err)
	}
	out := make([]EndOfMatchPlayer, 0, len(m.GetAllplayerdata()))
	for _, p := range m.GetAllplayerdata() {
		slot := p.GetSlot() // -1 when absent
		if slot < 0 || slot > 255 {
			return nil, badDemof("end-of-match player %q has slot %d", p.GetName(), slot)
		}
		out = append(out, EndOfMatchPlayer{
			Slot:      Slot(slot),
			SteamID64: p.GetXuid(),
			Name:      p.GetName(),
			Team:      p.GetTeamnumber(),
			Bot:       p.GetIsbot(),
		})
	}
	return out, nil
}
