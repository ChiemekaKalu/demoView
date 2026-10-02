package matchlogic

import (
	"slices"

	"github.com/ChiemekaKalu/demoview/parser/eventlog"
	"github.com/ChiemekaKalu/demoview/parser/match"
)

// Options are what Build needs to know besides the log.
type Options struct {
	Map      string // map name; bomb maps (de_*) time rounds with mp_roundtime_defuse
	TickRate int    // ticks per second
	// FirstTick is the demo's first frame tick past the signon. No round
	// starts before it.
	FirstTick int32

	// Log, if set, gets the match's narrative log (see story). BombSites
	// are the sites the entity stage saw bombs planted at, if it ran.
	Log       eventlog.Emitter
	BombSites []BombSite
}

// Result is the part of match.json that match logic fills in.
type Result struct {
	Players    []match.Player
	Rounds     []match.Round
	Kills      []match.Kill
	Scoreboard []match.ScoreRow
	// Spawned lists, for each round, the idx of every player who spawned
	// in it, in ascending order: the players its round file shows.
	Spawned [][]int

	// Notes say what Build had to guess, or found inconsistent, in plain
	// words. They don't change the output; they are for whoever debugs a
	// parse.
	Notes []string
}

// Build computes the match from a recorded log. A log without a single
// round gives an empty Result, with a note.
func Build(log *Log, opt Options) Result {
	var n notes
	t := normalize(log, opt.FirstTick, &n)
	w, spans := t.live, t.rounds
	if len(spans) == 0 {
		n.add("no rounds in the match")
		return Result{Notes: n}
	}
	swap := halftimes(log, spans, w, &n)
	s := assignSides(log, spans, swap, &n)
	outs := decideRounds(log, spans, s, opt, &n)
	ro := listPlayers(log, s)
	rounds := listRounds(spans, outs, s, &n)
	checkFinish(rounds, readSchedule(log, w.end), &n)
	res := Result{
		Players:    ro.players,
		Rounds:     rounds,
		Kills:      listKills(log, w, spans, s, ro, &n),
		Scoreboard: scoreboard(log, w, spans, s, ro),
		Spawned:    spawned(log, spans, ro),
		Notes:      dedupe(n),
	}
	if opt.Log != nil {
		tell(opt.Log, built{log: log, t: t, outs: outs, s: s, ro: ro, res: res, notes: n, opt: opt})
	}
	return res
}

// checkFinish notes a match that ended in regulation before either team won
// it, unless its last round is a surrender: the events don't show why it
// ended, and it may have been a surrender they don't record.
func checkFinish(rounds []match.Round, sched schedule, n *notes) {
	score := map[string]int{}
	for _, r := range rounds {
		score[r.Winner]++
	}
	last := rounds[len(rounds)-1]
	if len(rounds) < sched.maxRounds && max(score["A"], score["B"]) <= sched.maxRounds/2 && last.Reason != reasonSurrender {
		n.add("the match ended %d-%d, before either team won; a surrender or abandon the events don't show?", score["A"], score["B"])
	}
}

// listRounds turns spans and outcomes into match.json rounds. A round that
// ended in freeze time, by surrender, has freeze_end_tick = end_tick.
func listRounds(spans []span, outs []outcome, s sides, n *notes) []match.Round {
	rounds := make([]match.Round, len(spans))
	for r, sp := range spans {
		o := outs[r]
		start, end := sp.startTick, o.endTick
		freeze := end
		if sp.freezeEnd >= 0 {
			freeze = min(sp.freezeTick, end)
		}
		if freeze < start {
			n.add("round %d: ends at tick %d, before it starts at %d; clamped", r+1, end, start)
			freeze, end = start, max(end, start)
		}
		winner := teamB
		if o.winner == s.sideA[r] {
			winner = teamA
		}
		rounds[r] = match.Round{
			N:             r + 1,
			StartTick:     int(start),
			FreezeEndTick: int(freeze),
			EndTick:       int(end),
			Winner:        winner.String(),
			Reason:        o.reason,
			SideA:         s.sideA[r].String(),
		}
	}
	return rounds
}

// dedupe drops repeated notes, keeping the first of each.
func dedupe(n notes) []string {
	var out []string
	for _, s := range n {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
