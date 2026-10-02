package matchlogic

// The timeline stage turns a demo's log into its match in one standard form
// before anything is scored: the live window, and the rounds in it, each a
// span from the restart that starts it to the next one, with its freeze
// end. How a demo marks these varies with the game build and with how it
// was recorded, and every such quirk is handled in this stage:
//
//   - which event marks a restart: round_prestart, round_start or
//     round_poststart; round_officially_ended in tournament GOTV; or
//     cs_pre_restart, a few ticks before the spawns, where nothing else
//     fires each round (boundaryKind, restartAfter);
//   - where the match starts: the last begin_new_match with a round after
//     it, or without one, the first round's restart or the start of the
//     demo (liveWindow);
//   - a recording that starts inside round 1, in its freeze time or after
//     it, with none of its spawns (midRound, presumeSpawns);
//   - restarts that start no round: one folded into a round, and the ones a
//     demo ends in (segmentRounds);
//   - backup restores, which kill and respawn everyone at once and replay
//     the round they happen in: no new round, the replay decides it, and
//     their announce_phase_end is no halftime (restoreTicks);
//   - round 1's start_tick, which can't come before begin_new_match or the
//     demo's first tick.
//
// The scoring stages (sides, outcomes, kills, the scoreboard) read only the
// timeline and the entries inside its rounds. How each round ended is
// theirs to decide: only older demos fire round_end.

// timeline is the match in its standard form.
type timeline struct {
	live   window // the entries that count
	rounds []span
}

// normalize builds the timeline of log. firstTick is the demo's first frame
// tick past the signon.
func normalize(log *Log, firstTick int32, n *notes) timeline {
	t := timeline{live: liveWindow(log, n)}
	t.rounds = segmentRounds(log, t.live, n)
	if len(t.rounds) == 0 {
		return t
	}
	first := &t.rounds[0]
	if s := t.live.start; log.Entries[s].Kind == BeginNewMatch {
		first.startTick = max(first.startTick, log.Entries[s].Tick)
	}
	first.startTick = max(first.startTick, firstTick)
	if presumeSpawns(log, t.rounds) {
		first.startTick = firstTick // the recording starts inside round 1
	}
	return t
}
