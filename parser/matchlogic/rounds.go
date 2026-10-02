package matchlogic

// span is one round's stretch of Log.Entries: from the event that starts the
// round to the one that starts the next, or to the end of the match. The
// gap between a round's end and the next round's start belongs to the round
// that ended, and so do the kills in it.
type span struct {
	start, end int   // entries [start, end)
	startTick  int32 // match.json start_tick
	endTick    int32 // tick of the next round's start, or of the end of the match

	freezeEnd  int   // the round_freeze_end entry, or -1 (a surrender during freeze time)
	freezeTick int32 // its tick
	roundEnd   int   // the round_end entry, or -1 (newer demos)
	restart    int   // the round_officially_ended entry, or -1 (the last round)

	// presumed are participants taken to have spawned at the start, though
	// the demo has no spawn for them: it was recorded from after their
	// spawns (see presumeSpawns).
	presumed []int
}

// boundaryKind returns the event that starts a round in the live window w.
// round_prestart is the first event of a round restart: the halftime team
// swaps and the spawns of the new round all follow it, so splitting there
// keeps them in the round they belong to. round_start (older demos) and
// round_poststart fire on the same tick and stand in for it. Tournament GOTV
// demos fire none of these during play; there round_officially_ended, which
// fires as the next round restarts, marks the boundary, and failing that
// cs_pre_restart, which fires just before it.
//
// Every round has one freeze end, so a kind qualifies when w holds one for
// every round after the first (round 1's may come before begin_new_match).
// The first kind to qualify, in the order above, wins.
func boundaryKind(log *Log, w window) Kind {
	count := map[Kind]int{}
	for _, e := range log.Entries[w.start:w.end] {
		count[e.Kind]++
	}
	need := count[RoundFreezeEnd] - 1
	for _, k := range []Kind{RoundPrestart, RoundStart, RoundPoststart, RoundOfficiallyEnded, PreRestart} {
		if count[k] >= need {
			return k
		}
	}
	return RoundPrestart
}

// segmentRounds splits the live window into rounds.
//
// Round 1 starts at the restart that begin_new_match triggers, which comes
// just before it in the log. Every later restart inside the window
// starts a round. A restart followed by nothing that makes it a round (no
// freeze end, round_end or surrender before the next one) is folded into the
// round before it, unless no round follows it: the demo or the match ended
// before it was played, so it and what follows are left out.
//
// start_tick is round_start's tick when the demo fires it, and otherwise the
// restart's; the two are the same tick on every demo seen.
func segmentRounds(log *Log, w window, n *notes) []span {
	if w.start >= w.end {
		return nil
	}
	boundary := boundaryKind(log, w)
	// Round 1 starts at the restart before begin_new_match. With no restart
	// event there (tournament GOTV), it still takes the spawns and team
	// changes just before begin_new_match, which belong to it.
	first := w.start
backward:
	for i := w.start - 1; i >= 0; i-- {
		switch log.Entries[i].Kind {
		case boundary:
			first = i
			break backward
		case RoundFreezeEnd, RoundEnd, RoundOfficiallyEnded, PlayerDeath, PlayerHurt:
			break backward // reached the warmup or knife round's play
		case PlayerSpawn, PlayerTeam:
			first = i
		}
	}
	starts := []int{first}
	for i := w.start + 1; i < w.end; i++ {
		if log.Entries[i].Kind == boundary {
			starts = append(starts, restartAfter(log, i, w.end))
		}
	}

	end := func(j int) int {
		if j+1 < len(starts) {
			return starts[j+1]
		}
		return w.end
	}
	// A round the recording starts in the middle of comes first, with no
	// restart before it.
	isRound := func(j int, sp span) bool { return sp.played(log) || j == 0 && sp.midRound(log) }
	last := len(starts) - 1 // the last restart that starts a round
	for last >= 0 && !isRound(last, newSpan(log, starts[last], end(last))) {
		last--
	}
	if last >= 0 && last < len(starts)-1 {
		n.add("restart at tick %d has no freeze end or round end, and no round follows it; left out", tickAt(log, starts[last+1]))
	}

	var spans []span
	lead := -1 // start of restarts before the first real round
	for j, s := range starts[:last+1] {
		sp := newSpan(log, s, end(j))
		switch {
		case isRound(j, sp):
			if lead >= 0 {
				sp.start, lead = lead, -1
			}
			spans = append(spans, sp)
		case len(spans) > 0:
			n.add("restart at tick %d has no freeze end or round end; folded into round %d", sp.startTick, len(spans))
			spans[len(spans)-1].end = end(j)
			spans[len(spans)-1].endTick = sp.endTick
		default:
			if lead < 0 {
				lead = s
			}
		}
	}
	return spans
}

// restartAfter returns the restart that the boundary entry i announces. A
// cs_pre_restart comes a few ticks before the restart; that is the first
// spawn after it, unless a freeze end comes first. Any other boundary is its
// own restart.
func restartAfter(log *Log, i, end int) int {
	if log.Entries[i].Kind != PreRestart {
		return i
	}
	for j := i + 1; j < end; j++ {
		switch log.Entries[j].Kind {
		case PlayerSpawn:
			return j
		case RoundFreezeEnd:
			return i
		}
	}
	return i
}

// presumeSpawns gives round 1 the spawns a demo recorded from after them
// lacks, as a tournament recording that starts in round 1 does: when no
// restart comes before round 1's play (its freeze end, or its first death
// if the recording starts after that), everyone who spawns in round 2 is
// presumed to have spawned at the start of round 1. It reports whether it
// did.
func presumeSpawns(log *Log, spans []span) bool {
	if len(spans) < 2 {
		return false
	}
	play := spans[0].end
	for i := spans[0].start; i < spans[0].end; i++ {
		if k := log.Entries[i].Kind; k == RoundFreezeEnd || k == PlayerDeath {
			play = i
			break
		}
	}
	for _, e := range log.Entries[:play] {
		if restartKinds[e.Kind] {
			return false
		}
	}
	seen := map[int]bool{}
	for _, e := range log.Entries[spans[1].start:spans[1].end] {
		if e.Kind == PlayerSpawn && e.Player != NoPlayer && !seen[e.Player] {
			seen[e.Player] = true
			spans[0].presumed = append(spans[0].presumed, e.Player)
		}
	}
	return true
}

// newSpan describes the round in entries [start, end).
func newSpan(log *Log, start, end int) span {
	sp := span{
		start: start, end: end,
		startTick: log.Entries[start].Tick,
		endTick:   tickAt(log, end),
		freezeEnd: -1, roundEnd: -1, restart: -1,
	}
	sawStart := false
	for i := start; i < end; i++ {
		e := log.Entries[i]
		switch {
		case e.Kind == RoundStart && !sawStart:
			sp.startTick, sawStart = e.Tick, true
		case e.Kind == RoundFreezeEnd && sp.freezeEnd < 0:
			sp.freezeEnd, sp.freezeTick = i, e.Tick
		case e.Kind == RoundEnd && sp.roundEnd < 0:
			sp.roundEnd = i
		case e.Kind == RoundOfficiallyEnded && sp.restart < 0 && i > start:
			sp.restart = i
		}
	}
	// Where round_officially_ended is the boundary, the restart that ends
	// this round opens the next span.
	if sp.restart < 0 && end < len(log.Entries) && log.Entries[end].Kind == RoundOfficiallyEnded {
		sp.restart = end
	}
	return sp
}

// played reports whether anything makes the span a round: a freeze end, a
// round_end or a surrender.
func (sp span) played(log *Log) bool {
	return sp.freezeEnd >= 0 || sp.roundEnd >= 0 || has(log, sp.start, sp.end, Surrender)
}

// midRound reports whether the span is a round the recording starts in the
// middle of, after its freeze end: no restart opens it, and a death comes
// before the restart that ends it.
func (sp span) midRound(log *Log) bool {
	if restartKinds[log.Entries[sp.start].Kind] {
		return false
	}
	for _, e := range log.Entries[sp.start:sp.end] {
		if e.Kind == PlayerDeath && e.Tick < sp.endTick {
			return true
		}
	}
	return false
}

// tickAt returns the tick of entry i, or of the last entry if i is past the
// end.
func tickAt(log *Log, i int) int32 {
	switch {
	case i < len(log.Entries):
		return log.Entries[i].Tick
	case len(log.Entries) > 0:
		return log.Entries[len(log.Entries)-1].Tick
	}
	return 0
}

// has reports whether entries [start, end) include one of kind k.
func has(log *Log, start, end int, k Kind) bool {
	for i := start; i < end; i++ {
		if log.Entries[i].Kind == k {
			return true
		}
	}
	return false
}

// roundOf returns the round (an index into spans) that entry i falls in, or
// -1 if it is outside every round.
func roundOf(spans []span, i int) int {
	lo, hi := 0, len(spans)
	for lo < hi {
		mid := (lo + hi) / 2
		switch {
		case i < spans[mid].start:
			hi = mid
		case i >= spans[mid].end:
			lo = mid + 1
		default:
			return mid
		}
	}
	return -1
}
