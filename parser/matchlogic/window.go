package matchlogic

// window is the live part of the match as a range of Log.Entries: from the
// begin_new_match that starts the match, up to cs_win_panel_match. That
// leaves out warmup, a knife round, and the deaths a server inflicts after
// the match. Kills and damage count only inside it.
type window struct {
	start int // the begin_new_match entry, or where the match's rounds start if there is none
	end   int // the cs_win_panel_match entry, or len(Entries) if there is none
}

// contains reports whether entry i is live.
func (w window) contains(i int) bool { return i >= w.start && i < w.end }

// restartKinds are the events of a round restart.
var restartKinds = map[Kind]bool{
	BeginNewMatch:        true,
	RoundPrestart:        true,
	RoundStart:           true,
	RoundPoststart:       true,
	RoundOfficiallyEnded: true,
	PreRestart:           true,
}

// liveWindow finds the live window. The match ends at the last
// cs_win_panel_match. It starts at the last begin_new_match before that
// with a freeze end after it, so neither a new match started after the end
// nor a restart at the end of a demo, with no round played after it, hides
// the match that was played. A demo with no such begin_new_match, such as a
// tournament recording that starts in round 1, starts where startWithout
// says.
func liveWindow(log *Log, n *notes) window {
	w := window{start: -1, end: len(log.Entries)}
	for i := len(log.Entries) - 1; i >= 0; i-- {
		if log.Entries[i].Kind == WinPanelMatch {
			w.end = i
			break
		}
	}
	if w.end == len(log.Entries) {
		n.add("no cs_win_panel_match: the match runs to the end of the demo")
	}

	played, lastBegin := false, -1 // a freeze end comes after entry i; the last begin_new_match
	for i := w.end - 1; i >= 0 && w.start < 0; i-- {
		switch log.Entries[i].Kind {
		case RoundFreezeEnd:
			played = true
		case BeginNewMatch:
			if lastBegin < 0 {
				lastBegin = i
			}
			if played {
				w.start = i
			}
		}
	}
	switch {
	case w.start >= 0:
	case lastBegin >= 0 && !played:
		w.start = lastBegin // no round at all
	default:
		w.start = startWithout(log, w.end)
		n.add("no begin_new_match before a round: the match starts at tick %d", tickAt(log, w.start))
	}
	return w
}

// startWithout returns where a match with no begin_new_match starts: at the
// last restart before the first freeze end, which leaves out a restore that
// kills everyone as it restarts, or at the first entry if a death comes
// before that restart's tick, as when the recording starts in the middle of
// round 1.
func startWithout(log *Log, end int) int {
	restart := -1
	for i := range end {
		if k := log.Entries[i].Kind; k == RoundFreezeEnd {
			break
		} else if restartKinds[k] {
			restart = i
		}
	}
	if restart < 0 {
		return 0
	}
	for _, e := range log.Entries[:restart] {
		if e.Kind == PlayerDeath && e.Tick < log.Entries[restart].Tick {
			return 0
		}
	}
	return restart
}
