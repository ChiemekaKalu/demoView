// Package eventlog keeps the narrative log of a parse: a tick-stamped,
// deterministic record of what a demo said and what the parser made of it.
//
// A log has three layers, each its own NDJSON file (see Layer). Every line
// is one record:
//
//	{"seq":412,"tick":18230,"round":7,"type":"combat.death","v":1,"src":"ge:player_death","d":{"attacker":3,"victim":5,"weapon":"ak47","headshot":true}}
//	{"seq":88,"tick":18230,"round":7,"type":"kill.opening","v":1,"rule":"opening@1","basis":[412],"d":{"attacker":3,"victim":5}}
//
// seq numbers a layer's records from 0 in file order, which is by tick, then
// Record.Rank, then emit order, and never map order. A derived record or a
// note cites the facts it rests on by their seq, in basis.
//
// Hooks hand records to an Emitter while the demo streams. A Spool holds
// them, because rounds and player idx are known only once match logic has
// run. Spool.Finalize then orders, numbers and resolves them, and Gzip packs
// each file. The same records, emitted in the same order, give the same
// bytes.
//
// Records hold no floats, wall-clock times or rendered sentences. A payload
// is a list of Attrs, which only this package's constructors make, and none
// of them takes a float or a time. Payload strings are names and
// identifiers; wording is left to the narrator that renders the log.
package eventlog

import "strconv"

// Layer is one of a log's three files.
type Layer uint8

// The layers. Each is written to its own file and numbered on its own.
const (
	Fact    Layer = iota // what the demo said, with identity resolved and nothing else
	Derived              // interpretations, each citing the facts it rests on
	Note                 // the parser's own decisions, and the anomalies it met
)

var layerNames = [...]string{Fact: "fact", Derived: "derived", Note: "note"}

// String returns "fact", "derived", "note", or "Layer(N)".
func (l Layer) String() string {
	if l.valid() {
		return layerNames[l]
	}
	return "Layer(" + strconv.Itoa(int(l)) + ")"
}

func (l Layer) valid() bool { return l <= Note }

// Record is one log record as a hook emits it. Finalize gives it its seq and
// resolves its round and players.
type Record struct {
	Layer Layer
	Tick  int32 // demo-frame tick

	// Round is the round as the emitter knows it, or a provisional key that
	// Resolver.Round turns into the round.
	Round int

	// Rank orders records of the same tick, lowest first: for example, by
	// the stage that emits them. Records of the same tick and rank keep
	// their emit order. Rank is not written.
	Rank int

	Type string // what the record says, as a dotted name, e.g. "combat.death"
	V    int    // the version of Type's payload, from 1

	// Src says where a fact came from, e.g. "ge:player_death". Every fact
	// has one, and nothing else does.
	Src string

	// Rule names the rule, with its version, that made a derived record,
	// which needs one, or a note, which may have one: e.g. "opening@1".
	Rule string

	// Basis cites the facts a derived record or a note rests on. A derived
	// record always writes it; a note, only if it isn't empty.
	Basis []Ref

	D []Attr // the payload, a JSON object whose fields keep this order
}

// Ref refers to an emitted record, so that a later record can cite it in
// its Basis. The zero Ref refers to nothing. A Ref means something only to
// the Spool that returned it.
type Ref struct {
	layer Layer
	n     int32 // the record's place in its layer's emit order, from 1
}

// Emitter takes log records. Hooks check Enabled before building a record,
// so that a log nobody keeps costs nothing.
type Emitter interface {
	// Enabled reports whether records of layer l are kept.
	Enabled(l Layer) bool

	// Emit takes r and returns a Ref that later records can cite.
	Emit(r Record) Ref
}

// Nop is an Emitter that keeps nothing.
type Nop struct{}

// Enabled returns false.
func (Nop) Enabled(Layer) bool { return false }

// Emit discards r and returns the zero Ref.
func (Nop) Emit(Record) Ref { return Ref{} }

var (
	_ Emitter = Nop{}
	_ Emitter = (*Spool)(nil)
)
