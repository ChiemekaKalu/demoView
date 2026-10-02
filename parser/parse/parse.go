// Package parse turns a demo stream into match.json. It is the one parse
// path, shared by the CLI and the Cloud Run server, so both accept and reject
// exactly the same demos.
package parse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/matchlogic"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/players"
)

// Version is the parser version recorded in every match.json.
const Version = "0.1.0"

// checkEvery is how many frames pass between checks for cancellation.
const checkEvery = 4096

// Run reads the demo from r to the end and returns its match.json. It
// decodes every frame and every packet message, so a malformed demo fails
// with an error wrapping demo.ErrBadDemo. Errors from r are returned as they
// are, and so is ctx's error if it is cancelled.
func Run(ctx context.Context, r io.Reader) (match.File, error) {
	f, _, err := run(ctx, r)
	return f, err
}

// run is Run, plus the match logic's notes on what it had to guess.
func run(ctx context.Context, r io.Reader) (match.File, []string, error) {
	sum := sha256.New()
	dr, err := demo.NewReader(io.TeeReader(r, sum))
	if err != nil {
		return match.File{}, nil, err
	}
	p := parser{rec: matchlogic.NewRecorder()}
	if err := p.walk(ctx, demo.NewDecoder(dr)); err != nil {
		return match.File{}, nil, err
	}
	res := matchlogic.Build(p.rec.Log(), matchlogic.Options{Map: p.mapName, TickRate: p.tickRate})
	if len(res.Rounds) == 0 {
		return match.File{}, res.Notes, fmt.Errorf("%w: no live rounds", demo.ErrBadDemo)
	}
	return match.File{
		SchemaVersion: match.SchemaVersion,
		ParserVersion: Version,
		Match: match.Info{
			Map:        p.mapName,
			TickRate:   p.tickRate,
			DemoSHA256: hex.EncodeToString(sum.Sum(nil)),
		},
		Players:    res.Players,
		Rounds:     res.Rounds,
		Kills:      res.Kills,
		Scoreboard: res.Scoreboard,
	}, res.Notes, nil
}

// parser holds what the stages extract while the demo streams past.
type parser struct {
	mapName  string // from the first FileHeader
	tickRate int    // from the first svc_ServerInfo

	events events.Decoder       // game event descriptors
	rec    *matchlogic.Recorder // the events, players and cvars match logic needs
}

// walk feeds every frame from d, and every message in its packets, to the
// stages.
func (p *parser) walk(ctx context.Context, d *demo.Decoder) error {
	var mr demo.MessageReader
	for n := 0; ; n++ {
		if n%checkEvery == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		f, err := d.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch f.Cmd {
		case demo.CmdFileHeader:
			err = p.fileHeader(f)
		case demo.CmdFileInfo:
			err = f.Unmarshal(new(pb.CDemoFileInfo))
		case demo.CmdStringTables:
			err = p.stringTables(f)
		case demo.CmdFullPacket:
			err = p.fullPacket(&mr, f)
		case demo.CmdPacket, demo.CmdSignonPacket:
			err = p.packet(&mr, f)
		}
		if err != nil {
			return err
		}
	}
}

func (p *parser) fileHeader(f demo.Frame) error {
	var h pb.CDemoFileHeader
	if err := f.Unmarshal(&h); err != nil {
		return err
	}
	if p.mapName == "" {
		p.mapName = h.GetMapName()
	}
	return nil
}

// stringTables applies a DEM_StringTables snapshot: player identity.
func (p *parser) stringTables(f demo.Frame) error {
	var st pb.CDemoStringTables
	if err := f.Unmarshal(&st); err != nil {
		return err
	}
	return frameErr(f, p.rec.StringTables(&st))
}

// fullPacket applies a DEM_FullPacket's string tables, then reads its
// messages.
func (p *parser) fullPacket(mr *demo.MessageReader, f demo.Frame) error {
	var fp pb.CDemoFullPacket
	if err := f.Unmarshal(&fp); err != nil {
		return err
	}
	if err := frameErr(f, p.rec.StringTables(fp.GetStringTable())); err != nil {
		return err
	}
	return p.packet(mr, f)
}

func (p *parser) packet(mr *demo.MessageReader, f demo.Frame) error {
	if err := mr.Reset(f); err != nil {
		return err
	}
	for {
		m, err := mr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := p.message(m, f.Tick); err != nil {
			return frameErr(f, err)
		}
	}
}

// frameErr adds the position of frame f to an error from handling its
// contents, such as a game event that doesn't decode. nil stays nil.
func frameErr(f demo.Frame, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s frame at offset %d, tick %d: %w", f.Cmd, f.Offset, f.Tick, err)
}

// message hands one packet message to the stage that wants it. tick is the
// tick of the frame that carried it.
func (p *parser) message(m demo.Message, tick int32) error {
	switch m.Type {
	case demo.MsgType(pb.SVC_Messages_svc_ServerInfo):
		return p.serverInfo(m)
	case demo.MsgType(pb.EBaseGameEvents_GE_Source1LegacyGameEventList):
		return p.events.HandleList(m.Body)
	case demo.MsgType(pb.EBaseGameEvents_GE_Source1LegacyGameEvent):
		e, err := p.events.Decode(m.Body, tick)
		if err != nil {
			return err
		}
		p.rec.Event(e)
	case demo.MsgType(pb.NET_Messages_net_SetConVar):
		var c pb.CNETMsg_SetConVar
		if err := m.Unmarshal(&c); err != nil {
			return err
		}
		p.rec.ConVars(tick, c.GetConvars())
	case demo.MsgType(pb.ECstrike15UserMessages_CS_UM_VotePass):
		var v pb.CCSUsrMsg_VotePass
		if err := m.Unmarshal(&v); err != nil {
			return err
		}
		p.rec.VotePass(tick, &v)
	case players.MsgEndOfMatch:
		list, err := players.DecodeEndOfMatch(m.Body)
		if err != nil {
			return err
		}
		p.rec.EndOfMatch(list)
	}
	return nil
}

func (p *parser) serverInfo(m demo.Message) error {
	var info pb.CSVCMsg_ServerInfo
	if err := m.Unmarshal(&info); err != nil {
		return err
	}
	if p.tickRate == 0 && info.GetTickInterval() > 0 {
		p.tickRate = int(math.Round(1 / float64(info.GetTickInterval())))
	}
	return nil
}
