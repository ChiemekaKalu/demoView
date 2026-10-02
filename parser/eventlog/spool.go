package eventlog

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
)

// DefaultLimit is the Spool limit for a real parse: about 15 times the game
// events of the longest fixture demo, and some 30 MB in a full layer.
const DefaultLimit = 250_000

// Spool is an Emitter that holds records in memory until Finalize, because
// a record's round and players are known only once match logic has run.
//
// It holds at most limit records per layer: the first ones emitted. If it
// drops any, Finalize adds one note, ahead of the other notes of its tick:
//
//	{"seq":S,"tick":T,"round":R,"type":"log.truncated","v":1,"d":{"limit":N,"dropped":{"fact":F,"derived":D,"note":O}}}
//
// T and R are the tick and round of the earliest record dropped. A basis
// that cites a dropped fact leaves it out.
//
// A Spool is not safe for concurrent use.
type Spool struct {
	limit  int
	layers [3]layer
	names  []name         // the types and origins of the records held
	ids    map[name]int32 // each name's index in names
	err    error          // the first bad record; the Spool takes nothing after it

	truncated bool
	dropTick  int32 // the earliest dropped record's tick
	dropRound int   // and its round
}

// name is a record's type and origin: a fact's src, or the rule. Entries
// refer to names by index, so they hold no pointers for the garbage
// collector to trace.
type name struct{ typ, origin string }

// truncation is names[0], the truncation note's.
var truncation = name{typ: "log.truncated"}

// layer holds one layer's records. Each entry's payload, players and basis
// follow the previous entry's in data, holes and basis.
type layer struct {
	entries []entry
	data    []byte  // payloads as JSON, with a hole wherever a player goes
	holes   []hole  // players, in data order
	basis   []int32 // cited facts, as indexes into the fact layer's entries
	emitted int32   // records emitted, held or dropped
}

type entry struct {
	name                        int32 // index into Spool.names
	tick, round, rank, v        int32
	dataEnd, holesEnd, basisEnd int32 // where its share of data, holes and basis ends
}

// hole is a player whose idx Finalize writes at data[off].
type hole struct{ off, key int32 }

// NewSpool returns an empty Spool that holds up to limit records per layer.
func NewSpool(limit int) *Spool {
	return &Spool{limit: limit, names: []name{truncation}, ids: map[name]int32{truncation: 0}}
}

// Enabled reports whether l is a layer: a Spool keeps all three.
func (s *Spool) Enabled(l Layer) bool { return l.valid() }

// Emit holds r, or counts it as dropped if its layer is full. A record that
// breaks the rules of Record and Attr makes Finalize fail, and the Spool
// ignores every Emit after it.
func (s *Spool) Emit(r Record) Ref {
	if s.err != nil {
		return Ref{}
	}
	ref, err := s.emit(&r)
	if err != nil {
		s.err = fmt.Errorf("eventlog: %s %q at tick %d: %w", r.Layer, r.Type, r.Tick, err)
		return Ref{}
	}
	return ref
}

func (s *Spool) emit(r *Record) (Ref, error) {
	if err := s.check(r); err != nil {
		return Ref{}, err
	}
	l := &s.layers[r.Layer]
	l.emitted++
	ref := Ref{layer: r.Layer, n: l.emitted}
	if len(l.entries) >= s.limit {
		if !s.truncated || r.Tick < s.dropTick {
			s.truncated, s.dropTick, s.dropRound = true, r.Tick, r.Round
		}
		return ref, nil
	}
	return ref, l.add(r, s.name(r), int32(len(s.layers[Fact].entries)))
}

func (s *Spool) check(r *Record) error {
	switch {
	case !r.Layer.valid():
		return errors.New("no such layer")
	case r.Type == "":
		return errors.New("no type")
	case r.V < 1:
		return errors.New("v must be at least 1")
	case !fits32(r.V) || !fits32(r.Round) || !fits32(r.Rank):
		return errors.New("v, round and rank must fit in 32 bits")
	case (r.Layer == Fact) != (r.Src != ""):
		return errors.New("every fact has a src, and nothing else does")
	case r.Layer == Fact && (r.Rule != "" || len(r.Basis) > 0):
		return errors.New("a fact has no rule or basis")
	case r.Layer == Derived && r.Rule == "":
		return errors.New("a derived record needs a rule")
	}
	for _, ref := range r.Basis {
		if ref.layer != Fact || ref.n < 1 || ref.n > s.layers[Fact].emitted {
			return errors.New("dangling basis: cite facts by the Ref this Spool returned")
		}
	}
	return nil
}

// name returns the index of r's type and origin in s.names, adding them if
// they are new.
func (s *Spool) name(r *Record) int32 {
	n := name{typ: r.Type, origin: r.Src}
	if r.Layer != Fact {
		n.origin = r.Rule
	}
	id, ok := s.ids[n]
	if !ok {
		id = int32(len(s.names))
		s.names = append(s.names, n)
		s.ids[n] = id
	}
	return id
}

// add appends r, whose name is names[id], to l. facts is the number of
// facts held: a cited fact past it was dropped.
func (l *layer) add(r *Record, id, facts int32) error {
	if err := l.appendObject(r.D); err != nil {
		return err
	}
	for _, ref := range r.Basis {
		if ref.n <= facts {
			l.basis = append(l.basis, ref.n-1)
		}
	}
	if !fits32(len(l.data)) || !fits32(len(l.basis)) {
		return errors.New("the layer has outgrown 2 GiB")
	}
	l.entries = append(l.entries, entry{
		name: id, tick: r.Tick, round: int32(r.Round), rank: int32(r.Rank), v: int32(r.V),
		dataEnd: int32(len(l.data)), holesEnd: int32(len(l.holes)), basisEnd: int32(len(l.basis)),
	})
	return nil
}

// Resolver supplies, at Finalize, what only match logic knows. A nil field
// leaves values as they were emitted.
type Resolver struct {
	// Round returns a record's round, 0 outside live rounds, from its tick
	// and the Round it was emitted with.
	Round func(tick int32, round int) int

	// Player returns the idx of the player with a provisional key, or a
	// negative number for one that match.json doesn't list, which is
	// written as null.
	Player func(key int) int
}

func (res Resolver) round(tick int32, round int) int {
	if res.Round == nil {
		return round
	}
	return res.Round(tick, round)
}

func (res Resolver) appendPlayer(b []byte, key int32) []byte {
	idx := int(key)
	if res.Player != nil {
		idx = res.Player(idx)
	}
	if idx < 0 {
		return append(b, "null"...)
	}
	return strconv.AppendInt(b, int64(idx), 10)
}

// Log is a finalized log: each layer as NDJSON, one record per line.
type Log struct {
	Facts, Derived, Notes []byte
}

// Finalize returns the log. Each layer's records are sorted by tick, then
// Rank, then emit order, numbered from 0, and have their rounds, players and
// bases resolved. Finalize fails if any Emit met a bad record. It doesn't
// change the Spool, so calling it again gives the same log.
func (s *Spool) Finalize(res Resolver) (Log, error) {
	if s.err != nil {
		return Log{}, s.err
	}
	f := finalizer{Spool: s, res: res}
	var log Log
	log.Facts = f.layer(Fact) // first: it numbers the facts that bases cite
	log.Derived = f.layer(Derived)
	log.Notes = f.layer(Note)
	return log, nil
}

// finalizer writes a Spool's layers.
type finalizer struct {
	*Spool
	res     Resolver
	factSeq []int // each held fact's seq, once the fact layer is written
}

// sortKey places entry i of a layer, or the truncation note if i is -1.
type sortKey struct{ tick, rank, i int32 }

// layer returns layer l as NDJSON.
func (f *finalizer) layer(l Layer) []byte {
	ly := &f.layers[l]
	keys := make([]sortKey, len(ly.entries), len(ly.entries)+1)
	for i, e := range ly.entries {
		keys[i] = sortKey{tick: e.tick, rank: e.rank, i: int32(i)}
	}
	var marker *layer
	if l == Note && f.truncated {
		marker = f.marker()
		keys = append(keys, sortKey{tick: f.dropTick, rank: math.MinInt32, i: -1})
	}
	slices.SortFunc(keys, func(a, b sortKey) int {
		return cmp.Or(cmp.Compare(a.tick, b.tick), cmp.Compare(a.rank, b.rank), cmp.Compare(a.i, b.i))
	})
	if l == Fact {
		f.factSeq = make([]int, len(keys))
		for seq, k := range keys {
			f.factSeq[k.i] = seq
		}
	}
	// About the NDJSON's length, so that the buffer rarely regrows.
	b := make([]byte, 0, len(ly.data)+4*len(ly.holes)+8*len(ly.basis)+100*len(keys))
	for seq, k := range keys {
		if k.i < 0 {
			b = f.appendRecord(b, l, marker, 0, seq)
		} else {
			b = f.appendRecord(b, l, ly, int(k.i), seq)
		}
	}
	return b
}

// marker returns a layer holding just the truncation note.
func (f *finalizer) marker() *layer {
	var n [3]int32
	for l := range f.layers {
		n[l] = f.layers[l].emitted - int32(len(f.layers[l].entries))
	}
	d := fmt.Appendf(nil, `{"limit":%d,"dropped":{"fact":%d,"derived":%d,"note":%d}}`, f.limit, n[Fact], n[Derived], n[Note])
	return &layer{
		entries: []entry{{name: 0, tick: f.dropTick, round: int32(f.dropRound), v: 1, dataEnd: int32(len(d))}},
		data:    d,
	}
}

// appendRecord appends entry i of ly, which holds layer l, as line seq.
func (f *finalizer) appendRecord(b []byte, l Layer, ly *layer, i, seq int) []byte {
	e := &ly.entries[i]
	n := f.names[e.name]
	var data0, holes0, basis0 int32
	if i > 0 {
		p := &ly.entries[i-1]
		data0, holes0, basis0 = p.dataEnd, p.holesEnd, p.basisEnd
	}
	b = append(b, `{"seq":`...)
	b = strconv.AppendInt(b, int64(seq), 10)
	b = append(b, `,"tick":`...)
	b = strconv.AppendInt(b, int64(e.tick), 10)
	b = append(b, `,"round":`...)
	b = strconv.AppendInt(b, int64(f.res.round(e.tick, int(e.round))), 10)
	b = append(b, `,"type":`...)
	b = appendString(b, n.typ)
	b = append(b, `,"v":`...)
	b = strconv.AppendInt(b, int64(e.v), 10)
	if n.origin != "" {
		if l == Fact {
			b = append(b, `,"src":`...)
		} else {
			b = append(b, `,"rule":`...)
		}
		b = appendString(b, n.origin)
	}
	// A derived record always has a basis; a note only when it cites facts.
	if basis := ly.basis[basis0:e.basisEnd]; l == Derived || len(basis) > 0 {
		b = append(b, `,"basis":[`...)
		for j, fact := range basis {
			if j > 0 {
				b = append(b, ',')
			}
			b = strconv.AppendInt(b, int64(f.factSeq[fact]), 10)
		}
		b = append(b, ']')
	}
	b = append(b, `,"d":`...)
	pos := data0
	for _, h := range ly.holes[holes0:e.holesEnd] {
		b = append(b, ly.data[pos:h.off]...)
		b = f.res.appendPlayer(b, h.key)
		pos = h.off
	}
	b = append(b, ly.data[pos:e.dataEnd]...)
	return append(b, "}\n"...)
}

// Gzip compresses one of a Log's files. The gzip header has no file name and
// a zero modification time, so equal input gives equal output from the same
// Go release.
func Gzip(ndjson []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf) // its zero Header has no name and no mtime
	zw.Write(ndjson)           // writes to a bytes.Buffer don't fail
	zw.Close()
	return buf.Bytes()
}

func fits32(n int) bool { return n == int(int32(n)) }
