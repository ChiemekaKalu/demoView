package matchlogic

import (
	"slices"

	"github.com/ChiemekaKalu/demoview/parser/eventlog"
)

// tradeWindow is how soon, in seconds, a kill must avenge a teammate's
// death to be a trade.
const tradeWindow = 5

// emitKills emits round r's kill stories: the opening kill, trades,
// multi-kills of three or more, and clutches. A kill here is a death in the
// live window by an enemy, as the scoreboard counts it.
func (st *story) emitKills(r int) {
	side := st.s.of[r]
	type kill struct {
		i                int
		attacker, victim int // participants
	}
	var kills []kill
	for i := st.t.live.start; i < st.t.live.end; i++ {
		e := st.log.Entries[i]
		if e.Kind != PlayerDeath || worldDeath(e) || roundOfDeath(st.log, st.t.rounds, i) != r || !opponents(side, e.Attacker, e.Player) {
			continue
		}
		kills = append(kills, kill{i, e.Attacker, e.Player})
	}
	tick := func(k kill) int32 { return st.log.Entries[k.i].Tick }

	if len(kills) > 0 {
		k := kills[0]
		st.derived(tick(k), r, "kill.opening", "opening@1", []eventlog.Ref{st.facts[k.i]},
			eventlog.Player("attacker", st.player(k.attacker)), eventlog.Player("victim", st.player(k.victim)))
	}

	window := int32(tradeWindow * max(st.opt.TickRate, 1))
	for j, k := range kills {
		for _, prev := range slices.Backward(kills[:j]) {
			if tick(k)-tick(prev) > window {
				break
			}
			if prev.attacker == k.victim && prev.victim != k.attacker && side[prev.victim].Playing() && side[prev.victim] == side[k.attacker] {
				st.derived(tick(k), r, "kill.trade", "trade@1", []eventlog.Ref{st.facts[prev.i], st.facts[k.i]},
					eventlog.Player("attacker", st.player(k.attacker)), eventlog.Player("victim", st.player(k.victim)),
					eventlog.Player("avenged", st.player(prev.victim)))
				break
			}
		}
	}

	byPlayer := map[int][]kill{}
	var order []int // players in the order of their first kill
	for _, k := range kills {
		if byPlayer[k.attacker] == nil {
			order = append(order, k.attacker)
		}
		byPlayer[k.attacker] = append(byPlayer[k.attacker], k)
	}
	for _, p := range order {
		if ks := byPlayer[p]; len(ks) >= 3 {
			basis := make([]eventlog.Ref, len(ks))
			for n, k := range ks {
				basis[n] = st.facts[k.i]
			}
			st.derived(tick(ks[len(ks)-1]), r, "round.multikill", "multikill@1", basis,
				eventlog.Player("player", st.player(p)), eventlog.Int("kills", len(ks)))
		}
	}

	st.emitClutches(r)
}

// emitClutches emits, for each side of round r that gets down to its last
// living player while the other side still has someone alive, that player
// and how many they faced at that moment, and whether their side won.
// Living players are tracked as inferOutcome tracks them.
func (st *story) emitClutches(r int) {
	sp, side := st.t.rounds[r], st.s.of[r]
	alive := map[int]bool{}
	var living [CT + 1]int
	spawn := func(p int) {
		if p != NoPlayer && side[p].Playing() && !alive[p] {
			alive[p] = true
			living[side[p]]++
		}
	}
	if sp.play == sp.start {
		for _, p := range sp.presumed {
			spawn(p)
		}
	}
	done := map[Side]bool{}
	for i := sp.play; i < sp.end; i++ {
		e := st.log.Entries[i]
		switch e.Kind {
		case PlayerSpawn:
			spawn(e.Player)
			continue
		case PlayerDeath, PlayerDisconnect:
			if e.Player == NoPlayer || !alive[e.Player] || e.Kind == PlayerDeath && sp.onRestart(st.log, e) {
				continue
			}
			alive[e.Player] = false
			living[side[e.Player]]--
		default:
			continue
		}
		for _, sd := range []Side{T, CT} {
			if done[sd] || living[sd] != 1 || living[sd.Other()] < 1 {
				continue
			}
			done[sd] = true
			last := NoPlayer
			for p, a := range alive {
				if a && side[p] == sd && (last == NoPlayer || p < last) {
					last = p
				}
			}
			st.derived(e.Tick, r, "round.clutch", "clutch@1", []eventlog.Ref{st.facts[i]},
				eventlog.Player("player", st.player(last)), eventlog.Int("against", living[sd.Other()]),
				eventlog.Bool("won", st.outs[r].winner == sd))
		}
	}
}
