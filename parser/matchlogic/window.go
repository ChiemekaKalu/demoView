package matchlogic

// window is the live part of the match as a range of Log.Entries: from the
// last begin_new_match before the match ends, up to cs_win_panel_match.
// That leaves out warmup, a knife round, and the deaths a server inflicts
// after the match. Kills and damage count only inside it.
type window struct {
	start int // the begin_new_match entry, or 0 if there is none
	end   int // the cs_win_panel_match entry, or len(Entries) if there is none
}

// contains reports whether entry i is live.
func (w window) contains(i int) bool { return i >= w.start && i < w.end }

// liveWindow finds the live window. The match ends at the last
// cs_win_panel_match, and starts at the last begin_new_match before it, so
// a server that starts a new match after the end doesn't hide the one that
// was played.
func liveWindow(log *Log, n *notes) window {
	w := window{start: -1, end: len(log.Entries)}
	for i := len(log.Entries) - 1; i >= 0; i-- {
		if log.Entries[i].Kind == WinPanelMatch {
			w.end = i
			break
		}
	}
	for i := w.end - 1; i >= 0; i-- {
		if log.Entries[i].Kind == BeginNewMatch {
			w.start = i
			break
		}
	}
	if w.end == len(log.Entries) {
		n.add("no cs_win_panel_match: the match runs to the end of the demo")
	}
	if w.start < 0 {
		w.start = 0
		n.add("no begin_new_match: the match runs from the start of the demo, warmup included")
	}
	return w
}
