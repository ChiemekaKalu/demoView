package matchlogic

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// notes collects what Build had to guess or found inconsistent, for
// whoever debugs a parse. They don't change the output.
type notes []string

func (n *notes) add(format string, args ...any) {
	*n = append(*n, fmt.Sprintf(format, args...))
}

// Defaults for cvars a demo doesn't carry: CS2 competitive, MR12 with MR3
// overtime.
const (
	defaultMaxRounds   = 24
	defaultOTMaxRounds = 6
	defaultRoundTime   = 1.92 // minutes
)

// cvar returns the value cvar name had just before entry i: the last ConVar
// entry for it.
func cvar(log *Log, name string, i int) (string, bool) {
	for j := min(i, len(log.Entries)) - 1; j >= 0; j-- {
		if e := log.Entries[j]; e.Kind == ConVar && e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

// cvarFloat is cvar parsed as a number.
func cvarFloat(log *Log, name string, i int) (float64, bool) {
	v, ok := cvar(log, name, i)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return f, err == nil
}

// cvarBool is cvar read as a boolean: "true" or a nonzero number.
func cvarBool(log *Log, name string, i int) (val, ok bool) {
	v, ok := cvar(log, name, i)
	if !ok {
		return false, false
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f != 0, true
	}
	return strings.EqualFold(v, "true"), true
}

// halftimes returns, for each round, whether sides swap after it.
//
// Every round the timeline marks as a halftime (an announce_phase_end in
// it) swaps sides at the next restart. A demo without one falls back to the
// schedule in the cvars (see scheduledSwap). When both exist and disagree,
// the event wins and a note says so.
func halftimes(log *Log, spans []span, w window, n *notes) []bool {
	swap := make([]bool, len(spans))
	announced := false
	for r := range spans[:max(len(spans)-1, 0)] {
		if spans[r].halftime {
			swap[r], announced = true, true
		}
	}

	sched := readSchedule(log, w.end)
	for r := range swap[:max(len(swap)-1, 0)] {
		want := sched.swapAfter(r + 1)
		switch {
		case !announced:
			swap[r] = want
		case swap[r] != want:
			n.add("halftime after round %d is %v by announce_phase_end but %v by the cvars (%s)", r+1, swap[r], want, sched)
		}
	}
	if !announced && len(spans) > sched.maxRounds/2 && sched.assumed != "" {
		n.add("no announce_phase_end; halftimes follow %s, assumed for %s", sched, sched.assumed)
	}
	return swap
}

// schedule is the halftime schedule the cvars give.
type schedule struct {
	maxRounds int    // mp_maxrounds: regulation rounds
	otRounds  int    // mp_overtime_maxrounds: rounds per overtime
	halftime  bool   // mp_halftime
	assumed   string // the cvars that were missing and took their default, if any
}

func (s schedule) String() string {
	return fmt.Sprintf("mp_maxrounds %d, mp_overtime_maxrounds %d, mp_halftime %v", s.maxRounds, s.otRounds, s.halftime)
}

// readSchedule reads the schedule in force at entry i.
func readSchedule(log *Log, i int) schedule {
	s := schedule{maxRounds: defaultMaxRounds, otRounds: defaultOTMaxRounds, halftime: true}
	var missing []string
	if v, ok := cvarFloat(log, "mp_maxrounds", i); ok && v > 0 {
		s.maxRounds = int(v)
	} else {
		missing = append(missing, "mp_maxrounds")
	}
	if v, ok := cvarFloat(log, "mp_overtime_maxrounds", i); ok && v > 0 {
		s.otRounds = int(v)
	} else {
		missing = append(missing, "mp_overtime_maxrounds")
	}
	if v, ok := cvarBool(log, "mp_halftime", i); ok {
		s.halftime = v
	} else {
		missing = append(missing, "mp_halftime")
	}
	s.assumed = strings.Join(missing, ", ")
	return s
}

// swapAfter reports whether sides swap after round n (from 1). Regulation
// swaps at its half. Overtime is played in blocks of otRounds; sides stay
// as they are into each block and swap at its half (the Valve and ESL rule).
func (s schedule) swapAfter(n int) bool {
	if !s.halftime || s.maxRounds < 2 {
		return false
	}
	if n <= s.maxRounds {
		return n == s.maxRounds/2
	}
	if s.otRounds < 2 {
		return false
	}
	into := (n - s.maxRounds - 1) % s.otRounds // 0-based position in its overtime block
	return into == s.otRounds/2-1
}

// roundTimeTicks returns how long round play lasts after freeze time, in
// ticks, from the cvars in force at entry i: mp_roundtime_defuse on a bomb
// map (de_*) when it is set, mp_roundtime otherwise. The game keeps the
// round time in whole seconds.
func roundTimeTicks(log *Log, i int, mapName string, tickRate int, n *notes) int32 {
	minutes, ok := 0.0, false
	if strings.HasPrefix(mapName, "de_") {
		minutes, ok = cvarFloat(log, "mp_roundtime_defuse", i)
		ok = ok && minutes > 0
	}
	if !ok {
		minutes, ok = cvarFloat(log, "mp_roundtime", i)
		ok = ok && minutes > 0
	}
	if !ok {
		minutes = defaultRoundTime
		n.add("no mp_roundtime or mp_roundtime_defuse; the round timer assumes %.2f minutes", minutes)
	}
	return int32(math.Floor(minutes*60)) * int32(tickRate)
}
