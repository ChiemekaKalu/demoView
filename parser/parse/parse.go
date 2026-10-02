// Package parse turns a demo stream into match.json. It is the one parse
// path, shared by the CLI and the Cloud Run server, so both accept and reject
// exactly the same demos.
package parse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

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
		Match:         match.Info{DemoSHA256: hex.EncodeToString(sum.Sum(nil))},
	}, nil
}

// parser holds what the stages extract while the demo streams past. Later
// stages keep their state here, in the order they land: game events, then
// string tables and player identity, then match logic.
type parser struct{}

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
			err = f.Unmarshal(new(pb.CDemoFileHeader))
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
func (p *parser) message(demo.Message) error {
	return nil
}
