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

	"google.golang.org/protobuf/proto"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/pb"
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
	sum := sha256.New()
	dr, err := demo.NewReader(io.TeeReader(r, sum))
	if err != nil {
		return match.File{}, err
	}
	var p parser
	if err := p.walk(ctx, demo.NewDecoder(dr)); err != nil {
		return match.File{}, err
	}
	return match.File{
		SchemaVersion: match.SchemaVersion,
		ParserVersion: Version,
		Match: match.Info{
			Map:        p.mapName,
			TickRate:   p.tickRate,
			DemoSHA256: hex.EncodeToString(sum.Sum(nil)),
		},
	}, nil
}

// parser holds what the stages extract while the demo streams past.
type parser struct {
	mapName  string // from the first FileHeader
	tickRate int    // from the first svc_ServerInfo

	// Later stages keep their state here, in the order they land: game
	// events, then string tables and player identity, then match logic.
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
		case demo.CmdPacket, demo.CmdSignonPacket, demo.CmdFullPacket:
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
		if err := p.message(m); err != nil {
			return err
		}
	}
}

// message hands one packet message to the stage that wants it. This is where
// later stages plug in: game events next (GE_Source1LegacyGameEventList, 205,
// and GE_Source1LegacyGameEvent, 207), then string tables.
func (p *parser) message(m demo.Message) error {
	switch m.Type {
	case demo.MsgType(pb.SVC_Messages_svc_ServerInfo):
		return p.serverInfo(m)
	}
	return nil
}

func (p *parser) serverInfo(m demo.Message) error {
	var info pb.CSVCMsg_ServerInfo
	if err := unmarshal(m, &info); err != nil {
		return err
	}
	if p.tickRate == 0 && info.GetTickInterval() > 0 {
		p.tickRate = int(math.Round(1 / float64(info.GetTickInterval())))
	}
	return nil
}

// unmarshalOpts matches package demo's: AllowPartial skips proto2's
// required-field check, but malformed wire data is still an error.
var unmarshalOpts = proto.UnmarshalOptions{AllowPartial: true}

// unmarshal decodes m's body into v. A malformed body is a bad demo.
func unmarshal(m demo.Message, v proto.Message) error {
	if err := unmarshalOpts.Unmarshal(m.Body, v); err != nil {
		return fmt.Errorf("%w: %s body: %v", demo.ErrBadDemo, m.Type, err)
	}
	return nil
}
