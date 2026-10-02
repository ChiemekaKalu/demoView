// Package parse turns a demo stream into match.json and, optionally, round
// files. It is the one parse path, shared by the CLI and the server, so both
// accept and reject exactly the same demos.
package parse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/entities"
	"github.com/ChiemekaKalu/demoview/parser/eventlog"
	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/match"
	"github.com/ChiemekaKalu/demoview/parser/matchlogic"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/players"
	"github.com/ChiemekaKalu/demoview/parser/roundfile"
	"github.com/ChiemekaKalu/demoview/parser/stringtables"
)

// Version is the parser version recorded in every match.json.
const Version = "0.1.0"

// checkEvery is how many frames pass between checks for cancellation.
const checkEvery = 4096

// ErrNoStop means the demo ended cleanly between frames, but before its Stop
// frame: an upload or recording that was cut short. It wraps
// demo.ErrBadDemo.
var ErrNoStop = fmt.Errorf("%w: truncated: no Stop frame", demo.ErrBadDemo)

// ErrNoEntities is the round-file error of a demo with no entity classes.
// It wraps demo.ErrBadDemo.
var ErrNoEntities = fmt.Errorf("%w: entities: no class info", demo.ErrBadDemo)

// Stats says how far a parse got and where its time went.
type Stats struct {
	BytesRead int64         // demo bytes read from the input
	Frames    int           // frames read
	LastTick  int32         // the highest frame tick reached
	Walk      time.Duration // reading and decoding the frames
	Build     time.Duration // match logic, once the frames are done
	Rounds    time.Duration // slicing round files, once match logic is done
	Notes     []string      // what match logic had to guess or found inconsistent
}

// Options say what a parse produces besides match.json.
type Options struct {
	// Rounds decodes entities, sampling them every tick, to build round
	// files. Without it a parse reads game events only, which is much
	// cheaper.
	Rounds bool
	// Log keeps the parse's narrative log (package eventlog). Bomb sites
	// are in it only with Rounds, since they come from entities.
	Log bool
}

// Output is what a parse produced.
type Output struct {
	Match match.File
	// Rounds holds one file per match round, in order, when Options.Rounds
	// is set and the entity stage succeeded.
	Rounds []roundfile.File
	// RoundsErr says why Rounds is empty when Options.Rounds is set: the
	// error that ended the entity stage. It wraps entities.ErrUnknown for a
	// class, field or decoder this parser doesn't know, and otherwise
	// demo.ErrBadDemo. Match is good either way, since it comes from game
	// events, so a caller can ship it and take round files from elsewhere.
	RoundsErr error
	// Log is the narrative log when Options.Log is set.
	Log *eventlog.Log
}

// Run reads the demo from r to the end and returns its match.json. It
// decodes every frame and every packet message, so a malformed demo fails
// with an error wrapping demo.ErrBadDemo, and so does one without a Stop
// frame (ErrNoStop). Errors from r are returned as they are, and so is ctx's
// error if it is cancelled.
func Run(ctx context.Context, r io.Reader) (match.File, error) {
	f, _, err := RunStats(ctx, r)
	return f, err
}

// RunStats is Run, plus Stats, which are filled in as far as the parse got
// when it fails.
func RunStats(ctx context.Context, r io.Reader) (match.File, Stats, error) {
	out, st, err := Parse(ctx, r, Options{})
	return out.Match, st, err
}

// Parse is RunStats with options. Its error is about match.json, as
// RunStats's is; an error in the entity stage goes in Output.RoundsErr.
func Parse(ctx context.Context, r io.Reader, opt Options) (Output, Stats, error) {
	var st Stats
	sum := sha256.New()
	p := parser{rec: matchlogic.NewRecorder(), firstTick: -1}
	if opt.Rounds {
		p.ent = &entityStage{}
	}
	start := time.Now()
	err := p.walk(ctx, io.TeeReader(r, io.MultiWriter(sum, counter{&st.BytesRead})), &st)
	st.Walk = time.Since(start)
	if err != nil {
		return Output{}, st, err
	}

	start = time.Now()
	mo := matchlogic.Options{Map: p.mapName, TickRate: p.tickRate, FirstTick: p.firstTick}
	var spool *eventlog.Spool
	if opt.Log {
		spool = eventlog.NewSpool(eventlog.DefaultLimit)
		mo.Log = spool
		if p.ent != nil && p.ent.rec != nil && p.entErr == nil {
			for _, pl := range p.ent.rec.Plants() {
				mo.BombSites = append(mo.BombSites, matchlogic.BombSite{Tick: pl.Tick, Site: pl.Site})
			}
		}
	}
	res := matchlogic.Build(p.rec.Log(), mo)
	st.Build, st.Notes = time.Since(start), res.Notes
	if len(res.Rounds) == 0 {
		return Output{}, st, fmt.Errorf("%w: no live rounds", demo.ErrBadDemo)
	}
	var lg *eventlog.Log
	if spool != nil {
		l, err := spool.Finalize(eventlog.Resolver{})
		if err != nil {
			return Output{}, st, fmt.Errorf("narrative log: %w", err)
		}
		lg = &l
	}
	out := Output{Match: match.File{
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
	}, Log: lg}
	switch {
	case !opt.Rounds:
	case p.entErr != nil:
		out.RoundsErr = p.entErr
	case p.ent.rec == nil:
		out.RoundsErr = ErrNoEntities
	default:
		start = time.Now()
		out.Rounds = p.ent.rec.Rounds(&out.Match, res.Spawned, st.LastTick)
		st.Rounds = time.Since(start)
	}
	return out, st, nil
}

// counter adds the length of everything written to it to *n.
type counter struct{ n *int64 }

func (c counter) Write(p []byte) (int, error) {
	*c.n += int64(len(p))
	return len(p), nil
}

// parser holds what the stages extract while the demo streams past.
type parser struct {
	mapName   string // from the first FileHeader
	tickRate  int    // from the first svc_ServerInfo
	firstTick int32  // the first frame tick past the signon's -1, or -1

	events events.Decoder       // game event descriptors
	rec    *matchlogic.Recorder // the events, players and cvars match logic needs

	ent    *entityStage // nil when off, or once it fails
	entErr error        // why it failed
}

// entity runs step, a part of the entity stage, on frame f if the stage is
// on.
func (p *parser) entity(f demo.Frame, step func(demo.Frame) error) {
	if p.ent == nil {
		return
	}
	if err := step(f); err != nil {
		p.entityFailed(f, err)
	}
}

// entityFailed ends the entity stage with err, from frame f.
func (p *parser) entityFailed(f demo.Frame, err error) {
	p.ent, p.entErr = nil, frameErr(f, err)
}

// walk feeds every frame of the demo in r, and every message in its
// packets, to the stages, counting frames and ticks in st. It reads past
// Stop to the end of the input, but the input must hold a Stop.
func (p *parser) walk(ctx context.Context, r io.Reader, st *Stats) error {
	dr, err := demo.NewReader(r)
	if err != nil {
		return err
	}
	d := demo.NewDecoder(dr)
	var mr demo.MessageReader
	stopped := false
	for n := 0; ; n++ {
		if n%checkEvery == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		f, err := d.Next()
		if err == io.EOF {
			if !stopped {
				return ErrNoStop
			}
			return nil
		}
		if err != nil {
			return err
		}
		st.Frames++
		st.LastTick = max(st.LastTick, f.Tick)
		if p.firstTick < 0 && f.Tick >= 0 {
			p.firstTick = f.Tick
		}
		if p.ent != nil {
			p.ent.sampleTo(f.Tick - 1)
		}
		switch f.Cmd {
		case demo.CmdStop:
			stopped = true
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
		case demo.CmdSendTables:
			p.entity(f, p.ent.sendTables)
		case demo.CmdClassInfo:
			p.entity(f, p.ent.classInfo)
		}
		if err != nil {
			return err
		}
		if p.ent != nil {
			p.ent.sampleTo(f.Tick)
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

// stringTables applies a DEM_StringTables snapshot: player identity, and
// entity baselines.
func (p *parser) stringTables(f demo.Frame) error {
	var st pb.CDemoStringTables
	if err := f.Unmarshal(&st); err != nil {
		return err
	}
	if p.ent != nil {
		p.ent.tables.ApplySnapshot(&st)
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
		if err := p.message(m, f); err != nil {
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

// message hands one packet message, from frame f, to the stage that wants
// it.
func (p *parser) message(m demo.Message, f demo.Frame) error {
	tick := f.Tick
	switch m.Type {
	case stringtables.MsgCreate, stringtables.MsgUpdate, stringtables.MsgClearAll, entities.MsgPacketEntities:
		if p.ent != nil {
			if err := p.ent.message(m, f); err != nil {
				p.entityFailed(f, err)
			}
		}
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
		if p.ent != nil && p.ent.rec != nil {
			p.ent.rec.Event(e)
		}
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
