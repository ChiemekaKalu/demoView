package eventlog_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/eventlog"
)

func fact(tick int32, typ string, d ...eventlog.Attr) eventlog.Record {
	return eventlog.Record{Layer: eventlog.Fact, Tick: tick, Type: typ, V: 1, Src: "test", D: d}
}

func derived(tick int32, typ string, basis ...eventlog.Ref) eventlog.Record {
	return eventlog.Record{Layer: eventlog.Derived, Tick: tick, Type: typ, V: 1, Rule: "test@1", Basis: basis}
}

func note(tick int32, typ string) eventlog.Record {
	return eventlog.Record{Layer: eventlog.Note, Tick: tick, Type: typ, V: 1}
}

func finalize(t testing.TB, s *eventlog.Spool, res eventlog.Resolver) eventlog.Log {
	t.Helper()
	log, err := s.Finalize(res)
	if err != nil {
		t.Fatal(err)
	}
	return log
}

// line is the part of a record that the tests read back.
type line struct {
	Seq, Tick int
	Type      string
	Basis     []int
	D         json.RawMessage
}

func lines(t *testing.T, ndjson []byte) []line {
	t.Helper()
	var out []line
	for l := range bytes.Lines(ndjson) {
		var ln line
		if err := json.Unmarshal(l, &ln); err != nil {
			t.Fatalf("%v in %s", err, l)
		}
		out = append(out, ln)
	}
	for i, ln := range out {
		if ln.Seq != i {
			t.Errorf("line %d has seq %d", i, ln.Seq)
		}
	}
	return out
}

func types(ls []line) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Type)
	}
	return out
}

func TestOrder(t *testing.T) {
	s := eventlog.NewSpool(10)
	for _, r := range []struct {
		tick int32
		rank int
		typ  string
	}{
		{20, 0, "a"}, {10, 1, "b"}, {10, 0, "c"}, {5, 0, "d"}, {10, 0, "e"}, {10, -1, "f"},
	} {
		rec := fact(r.tick, r.typ)
		rec.Rank = r.rank
		s.Emit(rec)
	}
	s.Emit(derived(30, "x"))
	s.Emit(derived(3, "y"))
	s.Emit(note(1, "z"))
	log := finalize(t, s, eventlog.Resolver{})

	// By tick, then rank, then emit order; each layer numbered from 0.
	for _, c := range []struct {
		layer []byte
		want  []string
	}{
		{log.Facts, []string{"d", "f", "c", "e", "b", "a"}},
		{log.Derived, []string{"y", "x"}},
		{log.Notes, []string{"z"}},
	} {
		if got := types(lines(t, c.layer)); !slices.Equal(got, c.want) {
			t.Errorf("order %v, want %v", got, c.want)
		}
	}
}

func TestBasis(t *testing.T) {
	s := eventlog.NewSpool(10)
	late := s.Emit(fact(30, "late"))
	early := s.Emit(fact(10, "early"))
	mid := s.Emit(fact(20, "mid"))
	s.Emit(derived(40, "cites", late, early))
	s.Emit(derived(50, "cites nothing"))
	n := note(40, "notes")
	n.Basis = []eventlog.Ref{mid}
	s.Emit(n)
	s.Emit(note(50, "notes nothing"))
	log := finalize(t, s, eventlog.Resolver{})

	if got := lines(t, log.Derived)[0].Basis; !slices.Equal(got, []int{2, 0}) {
		t.Errorf("derived basis %v, want [2 0]: the facts' seqs, in the order cited", got)
	}
	if got := lines(t, log.Notes)[0].Basis; !slices.Equal(got, []int{1}) {
		t.Errorf("note basis %v, want [1]", got)
	}
	// A derived record always has a basis; a note only when it cites facts.
	for _, c := range []struct {
		layer []byte
		want  string
	}{
		{log.Derived, `{"seq":1,"tick":50,"round":0,"type":"cites nothing","v":1,"rule":"test@1","basis":[],"d":{}}`},
		{log.Notes, `{"seq":1,"tick":50,"round":0,"type":"notes nothing","v":1,"d":{}}`},
	} {
		if !bytes.Contains(c.layer, []byte(c.want+"\n")) {
			t.Errorf("got\n%s\nwant a line\n%s", c.layer, c.want)
		}
	}
}

func TestTruncation(t *testing.T) {
	s := eventlog.NewSpool(2)
	one := s.Emit(fact(10, "f.one"))
	two := s.Emit(fact(20, "f.two"))
	dropped := s.Emit(fact(40, "f.dropped"))
	earliest := fact(30, "f.earliest")
	earliest.Round = 3
	s.Emit(earliest)
	s.Emit(derived(40, "d.one", one, dropped, two))
	s.Emit(note(30, "n.kept"))
	s.Emit(note(50, "n.later"))
	s.Emit(note(30, "n.dropped"))
	log := finalize(t, s, eventlog.Resolver{})

	// The first records of each layer stay. The note marks the earliest
	// record dropped, ahead of the notes of its tick, and the dropped fact
	// leaves the basis.
	want := eventlog.Log{
		Facts: []byte(`{"seq":0,"tick":10,"round":0,"type":"f.one","v":1,"src":"test","d":{}}
{"seq":1,"tick":20,"round":0,"type":"f.two","v":1,"src":"test","d":{}}
`),
		Derived: []byte(`{"seq":0,"tick":40,"round":0,"type":"d.one","v":1,"rule":"test@1","basis":[0,1],"d":{}}
`),
		Notes: []byte(`{"seq":0,"tick":30,"round":3,"type":"log.truncated","v":1,"d":{"limit":2,"dropped":{"fact":2,"derived":0,"note":1}}}
{"seq":1,"tick":30,"round":0,"type":"n.kept","v":1,"d":{}}
{"seq":2,"tick":50,"round":0,"type":"n.later","v":1,"d":{}}
`),
	}
	equalLogs(t, log, want)
}

func equalLogs(t *testing.T, got, want eventlog.Log) {
	t.Helper()
	for _, c := range []struct {
		name      string
		got, want []byte
	}{
		{"facts", got.Facts, want.Facts},
		{"derived", got.Derived, want.Derived},
		{"notes", got.Notes, want.Notes},
	} {
		if !bytes.Equal(c.got, c.want) {
			t.Errorf("%s:\n got %s\nwant %s", c.name, c.got, c.want)
		}
	}
}

func TestPayload(t *testing.T) {
	d := []eventlog.Attr{
		eventlog.Int("i", -7),
		eventlog.String("s", "ak47"),
		eventlog.Bool("t", true),
		eventlog.Bool("f", false),
		eventlog.Player("p", 2),
		eventlog.Player("none", -1),
		eventlog.Player("unlisted", 3),
		eventlog.Object("o", eventlog.Int("a", 1), eventlog.Object("empty")),
		eventlog.Ints("is", 1, 2),
		eventlog.Ints("no_ints"),
		eventlog.Strings("ss", "x", "y"),
		eventlog.Players("ps", 0, -1, 2),
		eventlog.Objects("os", []eventlog.Attr{eventlog.Int("k", 1)}, nil),
	}
	for _, c := range []struct {
		name string
		res  eventlog.Resolver
		want string
	}{
		{"resolved", eventlog.Resolver{Player: func(p int) int { return []int{5, 6, 9, -1}[p] }},
			`{"i":-7,"s":"ak47","t":true,"f":false,"p":9,"none":null,"unlisted":null,"o":{"a":1,"empty":{}},"is":[1,2],"no_ints":[],"ss":["x","y"],"ps":[5,null,9],"os":[{"k":1},{}]}`},
		{"as emitted", eventlog.Resolver{},
			`{"i":-7,"s":"ak47","t":true,"f":false,"p":2,"none":null,"unlisted":3,"o":{"a":1,"empty":{}},"is":[1,2],"no_ints":[],"ss":["x","y"],"ps":[0,null,2],"os":[{"k":1},{}]}`},
	} {
		s := eventlog.NewSpool(1)
		s.Emit(fact(0, "all", d...))
		if got := string(lines(t, finalize(t, s, c.res).Facts)[0].D); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func TestStrings(t *testing.T) {
	for _, c := range []struct{ in, json, want string }{
		{"ak47", `"ak47"`, "ak47"},
		{`say "hi" \o/`, `"say \"hi\" \\o/"`, `say "hi" \o/`},
		{"\t\n\r\x00\x1f\x7f", `"\t\n\r\u0000\u001f` + "\x7f\"", "\t\n\r\x00\x1f\x7f"},
		{"Cédric ✓ 🙂", `"Cédric ✓ 🙂"`, "Cédric ✓ 🙂"},
		{"bad \xff\xfe utf8", `"bad \ufffd\ufffd utf8"`, "bad \uFFFD\uFFFD utf8"},
	} {
		s := eventlog.NewSpool(1)
		s.Emit(fact(0, "s", eventlog.String("s", c.in)))
		raw := lines(t, finalize(t, s, eventlog.Resolver{}).Facts)[0].D
		if want := `{"s":` + c.json + `}`; string(raw) != want {
			t.Errorf("%q encodes as %s, want %s", c.in, raw, want)
		}
		var got struct{ S string }
		if err := json.Unmarshal(raw, &got); err != nil || got.S != c.want {
			t.Errorf("%q decodes as %q (%v), want %q", c.in, got.S, err, c.want)
		}
	}
}

func TestBadRecords(t *testing.T) {
	// Refs from another Spool, which holds four facts and a derived record.
	other := eventlog.NewSpool(10)
	var foreign eventlog.Ref
	for range 4 {
		foreign = other.Emit(fact(0, "other"))
	}
	citesDerived := other.Emit(derived(0, "other", foreign))

	for _, c := range []struct {
		name string
		rec  eventlog.Record
		want string
	}{
		{"no layer", eventlog.Record{Layer: 3, Type: "x", V: 1}, "layer"},
		{"no type", fact(0, ""), "type"},
		{"no version", eventlog.Record{Layer: eventlog.Fact, Type: "x", Src: "test"}, "v must"},
		{"fact without src", eventlog.Record{Layer: eventlog.Fact, Type: "x", V: 1}, "src"},
		{"fact with rule", eventlog.Record{Layer: eventlog.Fact, Type: "x", V: 1, Src: "test", Rule: "r@1"}, "rule"},
		{"fact with basis", eventlog.Record{Layer: eventlog.Fact, Type: "x", V: 1, Src: "test", Basis: []eventlog.Ref{foreign}}, "basis"},
		{"derived without rule", eventlog.Record{Layer: eventlog.Derived, Type: "x", V: 1}, "rule"},
		{"derived with src", eventlog.Record{Layer: eventlog.Derived, Type: "x", V: 1, Rule: "r@1", Src: "test"}, "src"},
		{"note with src", eventlog.Record{Layer: eventlog.Note, Type: "x", V: 1, Src: "test"}, "src"},
		{"duplicate key", fact(0, "x", eventlog.Int("a", 1), eventlog.Int("a", 2)), "twice"},
		{"nested duplicate key", fact(0, "x", eventlog.Objects("o", []eventlog.Attr{eventlog.Int("a", 1), eventlog.Bool("a", true)})), "twice"},
		{"zero Attr", fact(0, "x", eventlog.Attr{}), "zero Attr"},
		{"zero Ref", derived(0, "x", eventlog.Ref{}), "dangling basis"},
		{"Nop's Ref", derived(0, "x", eventlog.Nop{}.Emit(fact(0, "x"))), "dangling basis"},
		{"citing a derived record", derived(0, "x", citesDerived), "dangling basis"},
		{"another Spool's Ref", derived(0, "x", foreign), "dangling basis"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := eventlog.NewSpool(10)
			s.Emit(derived(0, "ok", s.Emit(fact(0, "ok"))))
			if got := s.Emit(c.rec); got != (eventlog.Ref{}) {
				t.Errorf("Emit returned %v, want the zero Ref", got)
			}
			s.Emit(fact(0, "after")) // ignored: the first error stands
			_, err := s.Finalize(eventlog.Resolver{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got error %v, want one about %q", err, c.want)
			}
		})
	}
}

func TestEnabled(t *testing.T) {
	s := eventlog.NewSpool(1)
	for _, l := range []eventlog.Layer{eventlog.Fact, eventlog.Derived, eventlog.Note} {
		if (eventlog.Nop{}).Enabled(l) {
			t.Errorf("Nop keeps %s", l)
		}
		if !s.Enabled(l) {
			t.Errorf("Spool doesn't keep %s", l)
		}
	}
	if s.Enabled(3) {
		t.Error("Spool keeps Layer(3)")
	}
	if got := (eventlog.Nop{}).Emit(fact(0, "x")); got != (eventlog.Ref{}) {
		t.Errorf("Nop.Emit returned %v, want the zero Ref", got)
	}
}

func TestGzip(t *testing.T) {
	in := []byte("{\"seq\":0}\n")
	gz := eventlog.Gzip(in)
	if !bytes.Equal(gz, eventlog.Gzip(in)) {
		t.Error("Gzip differs between runs")
	}
	if flags, mtime := gz[3], gz[4:8]; flags != 0 || !bytes.Equal(mtime, []byte{0, 0, 0, 0}) {
		t.Errorf("header flags %#x, mtime % x; want no name and a zero mtime", flags, mtime)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := io.ReadAll(zr); err != nil || !bytes.Equal(out, in) {
		t.Errorf("round trip gives %q, %v", out, err)
	}
}

// TestNoFloatsOrTimes checks the package's exported API: nothing takes or
// holds a float, a complex number, a time or an interface value. Attr's
// fields are unexported, so that leaves no way to put one in a record.
func TestNoFloatsOrTimes(t *testing.T) {
	banned := map[string]bool{"float32": true, "float64": true, "complex64": true, "complex128": true, "any": true}
	fset := token.NewFileSet()
	check := func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if banned[x.Name] {
					t.Errorf("%s: %s in the exported API", fset.Position(x.Pos()), x.Name)
				}
			case *ast.SelectorExpr:
				if pkg, ok := x.X.(*ast.Ident); ok && pkg.Name == "time" {
					t.Errorf("%s: time.%s in the exported API", fset.Position(x.Pos()), x.Sel.Name)
				}
			case *ast.InterfaceType:
				t.Errorf("%s: an interface value in the exported API", fset.Position(x.Pos()))
			}
			return true
		})
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() {
					check(d.Type)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ts.Name.IsExported() {
						continue
					}
					switch typ := ts.Type.(type) {
					case *ast.StructType:
						for _, field := range typ.Fields.List {
							if len(field.Names) == 0 || field.Names[0].IsExported() {
								check(field.Type)
							}
						}
					case *ast.InterfaceType:
						for _, m := range typ.Methods.List {
							check(m.Type)
						}
					default:
						check(typ)
					}
				}
			}
		}
	}
}
