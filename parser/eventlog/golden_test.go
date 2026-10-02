package eventlog_test

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/eventlog"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// The provisional player keys of the golden match: the order the walk meets
// participants in.
const (
	alice = iota
	bob
	cedric
	gotv
)

// goldenResolver maps the golden match's provisional keys. Its rounds are
// keyed by the restarts seen: the first is the knife round, which isn't
// live. GOTV isn't in match.json.
var goldenResolver = eventlog.Resolver{
	Round:  func(_ int32, restarts int) int { return []int{0, 0, 1}[restarts] },
	Player: func(p int) int { return []int{1, 0, 2, -1}[p] },
}

// golden emits a short synthetic match: settings, four participants, a
// knife round, and one live round of a hit, a flash, two kills and a plant.
// Some records come out of tick order, as they can from a real walk.
func golden(em eventlog.Emitter) {
	fact := func(tick int32, round, rank int, typ, src string, d ...eventlog.Attr) eventlog.Ref {
		return em.Emit(eventlog.Record{Layer: eventlog.Fact, Tick: tick, Round: round, Rank: rank, Type: typ, V: 1, Src: src, D: d})
	}
	derived := func(tick int32, typ, rule string, basis []eventlog.Ref, d ...eventlog.Attr) {
		em.Emit(eventlog.Record{Layer: eventlog.Derived, Tick: tick, Round: 2, Type: typ, V: 1, Rule: rule, Basis: basis, D: d})
	}
	refs := func(r ...eventlog.Ref) []eventlog.Ref { return r }
	p, str := eventlog.Player, eventlog.String

	fact(0, 0, 0, "match.settings", "cvar", str("map", "de_mirage"), eventlog.Int("max_rounds", 24), eventlog.Int("tick_rate", 64))
	for key, name := range []string{"alice", `bob "the builder"`, "BOT Cédric\xff", "GOTV"} {
		fact(10, 0, 0, "player.registered", "st:userinfo", p("player", key), str("name", name), eventlog.Bool("bot", key == cedric))
	}
	knife := fact(100, 1, 0, "round.freeze_end", "ge:round_freeze_end")
	freeze := fact(900, 2, 0, "round.freeze_end", "ge:round_freeze_end")
	blind := fact(1500, 2, 1, "player.blind", "ent:flash", p("player", bob), p("attacker", cedric), eventlog.Int("ms", 1820))
	fact(1500, 2, 0, "combat.hurt", "ge:player_hurt", p("attacker", alice), p("victim", bob), str("weapon", "ak47"), eventlog.Int("damage", 27), eventlog.Int("health", 73))
	first := fact(1620, 2, 0, "combat.death", "ge:player_death", p("attacker", alice), p("victim", bob), p("assister", -1), str("weapon", "ak47"), eventlog.Bool("headshot", true), eventlog.Strings("flags", "through_smoke"))
	second := fact(1700, 2, 0, "combat.death", "ge:player_death", p("attacker", alice), p("victim", cedric), p("assister", -1), str("weapon", "ak47"), eventlog.Bool("headshot", false), eventlog.Strings("flags"))
	fact(1650, 2, 0, "bomb.plant", "ge:bomb_planted", p("player", alice), str("site", "B"))

	derived(900, "round.begin", "round@1", refs(freeze), eventlog.Object("sides", str("a", "T"), str("b", "CT")))
	derived(1500, "flash.team", "teamflash@1", refs(blind), p("thrower", cedric), p("victim", bob))
	derived(1620, "kill.opening", "opening@1", refs(first), p("attacker", alice), p("victim", bob))
	derived(1700, "kill.multi", "multi@1", refs(first, second), p("player", alice), eventlog.Int("kills", 2), eventlog.Players("victims", bob, cedric))
	derived(1700, "round.end", "round_end@1", refs(second), str("winner", "A"), str("method", "elimination"),
		eventlog.Objects("alive", []eventlog.Attr{p("player", alice), eventlog.Int("health", 100)}))

	em.Emit(eventlog.Record{Layer: eventlog.Note, Tick: 0, Type: "match.no_win_panel", V: 1})
	em.Emit(eventlog.Record{Layer: eventlog.Note, Tick: 100, Round: 1, Type: "round.not_live", V: 1, Rule: "window@1", Basis: refs(knife), D: []eventlog.Attr{str("reason", "knife")}})
}

func TestGolden(t *testing.T) {
	s := eventlog.NewSpool(eventlog.DefaultLimit)
	golden(s)
	log := finalize(t, s, goldenResolver)
	var sums bytes.Buffer
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"facts.ndjson", log.Facts},
		{"derived.ndjson", log.Derived},
		{"notes.ndjson", log.Notes},
	} {
		compareGolden(t, f.name, f.data)
		fmt.Fprintf(&sums, "%x  %s.gz\n", sha256.Sum256(eventlog.Gzip(f.data)), f.name)
	}
	// The gzip bytes depend on compress/flate too, so a Go release could
	// change them; the NDJSON must not change.
	compareGolden(t, "gzip.sha256", sums.Bytes())
}

func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file (go test -update rewrites it):\n got %s\nwant %s", name, got, want)
	}
}

// TestRunTwice feeds the same records to separate Spools and checks they
// give the same bytes, and that finalizing again changes nothing.
func TestRunTwice(t *testing.T) {
	for _, c := range []struct {
		name  string
		limit int
		emit  func(eventlog.Emitter)
		res   eventlog.Resolver
	}{
		{"golden", eventlog.DefaultLimit, golden, goldenResolver},
		{"synthetic", eventlog.DefaultLimit, func(em eventlog.Emitter) { synthetic(em, 5000) }, syntheticResolver},
		{"truncated", 1000, func(em eventlog.Emitter) { synthetic(em, 5000) }, syntheticResolver},
	} {
		t.Run(c.name, func(t *testing.T) {
			var logs [2]eventlog.Log
			var s *eventlog.Spool
			for i := range logs {
				s = eventlog.NewSpool(c.limit)
				c.emit(s)
				logs[i] = finalize(t, s, c.res)
			}
			equalLogs(t, logs[1], logs[0])
			equalLogs(t, finalize(t, s, c.res), logs[0])
			for _, f := range [][2][]byte{
				{logs[0].Facts, logs[1].Facts},
				{logs[0].Derived, logs[1].Derived},
				{logs[0].Notes, logs[1].Notes},
			} {
				if !bytes.Equal(eventlog.Gzip(f[0]), eventlog.Gzip(f[1])) {
					t.Error("gzip bytes differ")
				}
			}
		})
	}
}

var syntheticResolver = eventlog.Resolver{
	Round:  func(_ int32, r int) int { return r + 1 },
	Player: func(p int) int { return 9 - p },
}

// synthetic emits n records shaped like a real match's: mostly hits and
// deaths, some a little out of tick order, a derived trade every tenth
// record citing the last two deaths, and a note every fiftieth.
func synthetic(em eventlog.Emitter, n int) {
	rng := rand.New(rand.NewPCG(1, 2))
	var prev, last eventlog.Ref
	var tick int32
	for i := range n {
		tick += int32(rng.IntN(48))
		t := tick - int32(rng.IntN(16))
		round := int(tick / 8000)
		switch {
		case i%50 == 49:
			em.Emit(eventlog.Record{Layer: eventlog.Note, Tick: t, Round: round, Type: "round.end_inferred", V: 1, Rule: "timer@1",
				D: []eventlog.Attr{eventlog.String("method", "timer")}})
		case i%10 == 9 && prev != (eventlog.Ref{}):
			em.Emit(eventlog.Record{Layer: eventlog.Derived, Tick: t, Round: round, Type: "kill.trade", V: 1, Rule: "trade@1",
				Basis: []eventlog.Ref{prev, last},
				D:     []eventlog.Attr{eventlog.Player("killer", rng.IntN(10)), eventlog.Player("traded", rng.IntN(10))}})
		case rng.IntN(4) == 0:
			prev, last = last, em.Emit(eventlog.Record{Layer: eventlog.Fact, Tick: t, Round: round, Type: "combat.death", V: 1, Src: "ge:player_death",
				D: []eventlog.Attr{
					eventlog.Player("attacker", rng.IntN(10)), eventlog.Player("victim", rng.IntN(10)), eventlog.Player("assister", rng.IntN(11)-1),
					eventlog.String("weapon", "ak47"), eventlog.Bool("headshot", rng.IntN(2) == 0),
				}})
		default:
			em.Emit(eventlog.Record{Layer: eventlog.Fact, Tick: t, Round: round, Rank: rng.IntN(2), Type: "combat.hurt", V: 1, Src: "ge:player_hurt",
				D: []eventlog.Attr{
					eventlog.Player("attacker", rng.IntN(10)), eventlog.Player("victim", rng.IntN(10)),
					eventlog.String("weapon", "m4a1_silencer"), eventlog.Int("damage", rng.IntN(100)), eventlog.Int("health", rng.IntN(100)),
				}})
		}
	}
}

// recorder is a Spool that keeps a copy of the records it takes. A fresh
// Spool given the same records returns the same Refs, so they can be
// replayed.
type recorder struct {
	*eventlog.Spool
	records []eventlog.Record
}

func (r *recorder) Emit(rec eventlog.Record) eventlog.Ref {
	r.records = append(r.records, rec)
	return r.Spool.Emit(rec)
}

// BenchmarkSpool emits and finalizes 20k synthetic records per op, about a
// long match's worth of game events. Building the records isn't timed.
// held-B is the heap the Spool holds before Finalize.
func BenchmarkSpool(b *testing.B) {
	rec := recorder{Spool: eventlog.NewSpool(eventlog.DefaultLimit)}
	synthetic(&rec, 20_000)
	replay := func() *eventlog.Spool {
		s := eventlog.NewSpool(eventlog.DefaultLimit)
		for _, r := range rec.records {
			s.Emit(r)
		}
		return s
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	s := replay()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(s)

	for b.Loop() {
		finalize(b, replay(), syntheticResolver)
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(rec.records)), "ns/event")
	b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc), "held-B")
}
