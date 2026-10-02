package matchlogic

import (
	"strings"

	"github.com/ChiemekaKalu/demoview/parser/eventlog"
	"github.com/ChiemekaKalu/demoview/parser/match"
)

// BombSite is a bomb planted at a site, A or B, as the entity stage saw it:
// the tick the planted bomb appeared.
type BombSite struct {
	Tick int32
	Site string
}

// built is everything Build worked out, for the narrative log.
type built struct {
	log   *Log
	t     timeline
	outs  []outcome
	s     sides
	ro    roster
	res   Result
	notes notes
	opt   Options
}

// story emits the narrative log (package eventlog) of a built match: a fact
// for each recorded event the log covers, then the records derived from the
// timeline and the scoring. Players are written by idx, and a participant
// match.json doesn't list as null. A record's round is 0 outside live
// rounds.
type story struct {
	built
	e     eventlog.Emitter
	facts []eventlog.Ref // [entry]: its fact, or the zero Ref
}

func tell(e eventlog.Emitter, b built) {
	st := story{built: b, e: e, facts: make([]eventlog.Ref, len(b.log.Entries))}
	if e.Enabled(eventlog.Fact) {
		st.emitFacts()
	}
	if e.Enabled(eventlog.Derived) {
		st.emitWindow()
		for r := range st.res.Rounds {
			st.emitRound(r, st.res.Rounds, st.outs[r])
			st.emitKills(r)
		}
	}
}

// player returns participant p's idx, or -1.
func (st *story) player(p int) int {
	if p == NoPlayer {
		return -1
	}
	return st.ro.idx[p]
}

// roundOf returns the round entry i is written with: from 1, or 0 outside
// live rounds.
func (st *story) roundOf(i int) int {
	if !st.t.live.contains(i) {
		return 0
	}
	r := roundOf(st.t.rounds, i)
	if st.log.Entries[i].Kind == PlayerDeath {
		r = roundOfDeath(st.log, st.t.rounds, i)
	}
	return r + 1
}

func (st *story) emitFacts() {
	for _, p := range st.ro.players {
		st.e.Emit(eventlog.Record{Layer: eventlog.Fact, Type: "player.registered", V: 1, Src: "userinfo",
			D: []eventlog.Attr{eventlog.Int("idx", p.Idx), eventlog.String("steam_id", p.SteamID), eventlog.String("name", p.Name)}})
	}
	starts := map[int]bool{}
	for _, sp := range st.t.rounds {
		starts[sp.start] = true
	}
	health := map[int]int{} // as the scoreboard counts it: full at a spawn and at a round's start
	for i, e := range st.log.Entries {
		if starts[i] {
			clear(health)
		}
		var typ string
		var d []eventlog.Attr
		switch e.Kind {
		case PlayerTeam:
			typ, d = "player.team", []eventlog.Attr{eventlog.Player("player", st.player(e.Player)),
				eventlog.String("from", sideName(e.OldSide)), eventlog.String("to", sideName(e.Side))}
		case PlayerSpawn:
			delete(health, e.Player)
			typ, d = "player.spawn", []eventlog.Attr{eventlog.Player("player", st.player(e.Player))}
		case PlayerDisconnect:
			typ, d = "player.disconnect", []eventlog.Attr{eventlog.Player("player", st.player(e.Player))}
		case RoundFreezeEnd:
			typ = "round.freeze_end"
		case PlayerHurt:
			before, ok := health[e.Player]
			if !ok {
				before = fullHealth
			}
			health[e.Player] = e.Health
			typ, d = "combat.hurt", []eventlog.Attr{eventlog.Player("attacker", st.player(e.Attacker)), eventlog.Player("victim", st.player(e.Player)),
				eventlog.String("weapon", strings.TrimPrefix(e.Weapon, "weapon_")), eventlog.Int("health", e.Health), eventlog.Int("damage", max(0, before-e.Health))}
		case PlayerDeath:
			attacker := e.Attacker
			if worldDeath(e) {
				attacker = NoPlayer
			}
			typ, d = "combat.death", []eventlog.Attr{eventlog.Player("attacker", st.player(attacker)), eventlog.Player("victim", st.player(e.Player)),
				eventlog.Player("assister", st.player(e.Assister)), eventlog.Bool("flash_assist", e.FlashAssist),
				eventlog.String("weapon", strings.TrimPrefix(e.Weapon, "weapon_")), eventlog.Bool("headshot", e.Headshot)}
		case BombPlanted:
			d = []eventlog.Attr{eventlog.Player("player", st.player(e.Player))}
			if site := st.siteAt(e.Tick); site != "" {
				d = append(d, eventlog.String("site", site))
			}
			typ = "bomb.plant"
		case BombBeginDefuse:
			typ, d = "bomb.defuse_start", []eventlog.Attr{eventlog.Player("player", st.player(e.Player)), eventlog.Bool("kit", e.Kit)}
		case BombDefused:
			typ, d = "bomb.defuse", []eventlog.Attr{eventlog.Player("player", st.player(e.Player))}
		case BombExploded:
			typ = "bomb.explode"
		case WinPanelMatch:
			typ = "match.end"
		default:
			continue
		}
		st.facts[i] = st.e.Emit(eventlog.Record{Layer: eventlog.Fact, Tick: e.Tick, Round: st.roundOf(i), Type: typ, V: 1, Src: "ge:" + e.Kind.String(), D: d})
	}
}

// siteAt returns the site of a bomb planted at tick: the entity stage's
// planted bomb that appeared within a second of it, or "".
func (st *story) siteAt(tick int32) string {
	for _, s := range st.opt.BombSites {
		if s.Tick >= tick && s.Tick-tick <= int32(max(st.opt.TickRate, 1)) {
			return s.Site
		}
	}
	return ""
}

// sideName names a side as player.team writes it.
func sideName(s Side) string {
	switch s {
	case T:
		return "T"
	case CT:
		return "CT"
	case Spectator:
		return "spec"
	}
	return "none"
}

// emitWindow says what part of the demo counted.
func (st *story) emitWindow() {
	w := st.t.live
	st.e.Emit(eventlog.Record{Layer: eventlog.Derived, Tick: tickAt(st.log, w.start), Type: "match.live_window", V: 1, Rule: "window@1",
		D: []eventlog.Attr{eventlog.Int("start_tick", int(tickAt(st.log, w.start))), eventlog.Int("end_tick", int(tickAt(st.log, w.end)))}})
}

// emitRound emits round r's begin, end and score, and its bomb timeline if
// a bomb was planted.
func (st *story) emitRound(r int, rounds []match.Round, o outcome) {
	sp, mr := st.t.rounds[r], rounds[r]
	var begin []eventlog.Ref
	if sp.freezeEnd >= 0 {
		begin = append(begin, st.facts[sp.freezeEnd])
	}
	st.derived(int32(mr.StartTick), r, "round.begin", "round@1", begin,
		eventlog.Int("n", mr.N), eventlog.Int("start_tick", mr.StartTick), eventlog.Int("freeze_end_tick", mr.FreezeEndTick),
		eventlog.Bool("replayed", sp.play != sp.start))

	st.emitBomb(r, sp)

	var end []eventlog.Ref // the facts on the tick the round ended
	for i := sp.start; i < sp.end; i++ {
		if e := st.log.Entries[i]; e.Tick == int32(mr.EndTick) && st.facts[i] != (eventlog.Ref{}) && e.Kind != PlayerHurt {
			end = append(end, st.facts[i])
		}
	}
	st.derived(int32(mr.EndTick), r, "round.end", "round@1", end,
		eventlog.Int("n", mr.N), eventlog.String("winner", mr.Winner), eventlog.String("side_a", mr.SideA),
		eventlog.String("reason", mr.Reason), eventlog.String("method", o.method))

	var a, b int
	for _, x := range rounds[:r+1] {
		if x.Winner == teamA.String() {
			a++
		} else {
			b++
		}
	}
	st.derived(int32(mr.EndTick), r, "round.score", "score@1", nil, eventlog.Int("n", mr.N), eventlog.Int("a", a), eventlog.Int("b", b))
}

// emitBomb emits round r's bomb timeline: who planted, where and when, each
// defuse attempt after it, and how it ended.
func (st *story) emitBomb(r int, sp span) {
	plant := -1
	var basis []eventlog.Ref
	var defuses [][]eventlog.Attr
	outcome, at := "none", int32(-1)
	for i := sp.start; i < sp.end; i++ {
		e := st.log.Entries[i]
		switch {
		case e.Kind == BombPlanted && plant < 0:
			plant = i
		case plant < 0:
			continue
		case e.Kind == BombBeginDefuse:
			defuses = append(defuses, []eventlog.Attr{eventlog.Player("player", st.player(e.Player)), eventlog.Int("tick", int(e.Tick)), eventlog.Bool("kit", e.Kit)})
		case e.Kind == BombDefused && at < 0:
			outcome, at = "defused", e.Tick
		case e.Kind == BombExploded && at < 0:
			outcome, at = "exploded", e.Tick
		default:
			continue
		}
		basis = append(basis, st.facts[i])
	}
	if plant < 0 {
		return
	}
	e := st.log.Entries[plant]
	d := []eventlog.Attr{eventlog.Player("planter", st.player(e.Player)), eventlog.Int("plant_tick", int(e.Tick))}
	if site := st.siteAt(e.Tick); site != "" {
		d = append(d, eventlog.String("site", site))
	}
	d = append(d, eventlog.Objects("defuses", defuses...), eventlog.String("outcome", outcome))
	if at >= 0 {
		d = append(d, eventlog.Int("outcome_tick", int(at)))
	}
	st.derived(e.Tick, r, "round.bomb", "bomb@1", basis, d...)
}

// derived emits a derived record of round r.
func (st *story) derived(tick int32, r int, typ, rule string, basis []eventlog.Ref, d ...eventlog.Attr) {
	st.e.Emit(eventlog.Record{Layer: eventlog.Derived, Tick: tick, Round: r + 1, Type: typ, V: 1, Rule: rule, Basis: basis, D: d})
}
