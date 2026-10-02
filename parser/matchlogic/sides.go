package matchlogic

// team is a team for the whole match. Team A is the team on CT in round 1.
type team int8

const (
	noTeam team = iota
	teamA
	teamB
)

// String returns "A", "B", or "" for noTeam.
func (t team) String() string {
	switch t {
	case teamA:
		return "A"
	case teamB:
		return "B"
	}
	return ""
}

// sides says who played which side in each round.
type sides struct {
	in    [][]bool // [round][participant]: they played the round
	of    [][]Side // [round][participant]: the side they played, NoSide if they didn't play it
	sideA []Side   // [round]: the side team A played
	team  []team   // [participant]: their team, noTeam if they played no round
}

// assignSides works out every participant's side in every round they played,
// their team, and the side team A played in each round.
//
//  1. A participant plays a round if they spawn in it, or kill, die, assist,
//     hurt or get hurt in it.
//  2. Their observed side in a round comes from their player_team events,
//     read at the end of the round. Without events, or if the events end on
//     a different side than the end-of-match message gives, it is the
//     end-of-match side carried back through the halftimes.
//  3. Team A is whoever was observed on CT in round 1. The side team A
//     played in a later round is the side most of them were observed on,
//     or the previous round's side, swapped at a halftime, when none of
//     them was.
//  4. A participant's team is the one whose side they were observed on in
//     most of their rounds (the earliest decides a tie). Someone never
//     observed takes the team opposite the players they fought.
//  5. The side played in a round is the observed side, or else the one
//     their team played.
func assignSides(log *Log, spans []span, swap []bool, n *notes) sides {
	s := sides{in: participation(log, spans)}
	obs := observe(log, spans, s.in, swap, n)
	s.sideA = sideOfTeamA(spans, s.in, obs, swap, n)
	s.team = teams(log, spans, s.in, obs, s.sideA, n)
	s.of = make([][]Side, len(spans))
	for r := range spans {
		s.of[r] = make([]Side, len(log.Players))
		for p, played := range s.in[r] {
			switch {
			case !played:
			case obs[r][p].Playing():
				s.of[r][p] = obs[r][p]
			case s.team[p] == teamA:
				s.of[r][p] = s.sideA[r]
			case s.team[p] == teamB:
				s.of[r][p] = s.sideA[r].Other()
			}
		}
	}
	checkSides(log, spans, s, n)
	return s
}

// participation returns [round][participant]: whether they played the round.
func participation(log *Log, spans []span) [][]bool {
	in := make([][]bool, len(spans))
	for r, sp := range spans {
		in[r] = make([]bool, len(log.Players))
		mark := func(p int) {
			if p != NoPlayer && !log.Players[p].Info.HLTV {
				in[r][p] = true
			}
		}
		for _, e := range log.Entries[sp.start:sp.end] {
			switch e.Kind {
			case PlayerSpawn, BombPlanted, BombDefused:
				mark(e.Player)
			case PlayerDeath:
				mark(e.Player)
				if !worldDeath(e) {
					mark(e.Attacker)
				}
				mark(e.Assister)
			case PlayerHurt:
				mark(e.Player)
				mark(e.Attacker)
			}
		}
	}
	return in
}

// observe returns [round][participant]: the side each participant was seen
// on in each round they played, or NoSide.
func observe(log *Log, spans []span, in [][]bool, swap []bool, n *notes) [][]Side {
	obs := make([][]Side, len(spans))
	for r := range obs {
		obs[r] = make([]Side, len(log.Players))
	}
	if len(spans) == 0 {
		return obs
	}
	changes := make([][]int, len(log.Players)) // each participant's player_team entries
	for i, e := range log.Entries {
		if e.Kind == PlayerTeam && e.Player != NoPlayer {
			changes[e.Player] = append(changes[e.Player], i)
		}
	}
	last := len(spans) - 1
	for p, pp := range log.Players {
		useEvents := len(changes[p]) > 0
		if final := sideAfter(log, changes[p], spans[last].end); useEvents && pp.FinalSide.Playing() && final != pp.FinalSide {
			n.add("%s ends on %v by player_team but on %v by the end-of-match message; using the latter", pp.Name(), final, pp.FinalSide)
			useEvents = false
		}
		side := pp.FinalSide // carried back from the end when there are no events
		for r := last; r >= 0; r-- {
			if r < last && swap[r] {
				side = side.Other()
			}
			if !in[r][p] {
				continue
			}
			if useEvents {
				obs[r][p] = sideAfter(log, changes[p], spans[r].end)
			} else if side.Playing() {
				obs[r][p] = side
			}
		}
	}
	return obs
}

// sideAfter returns the side a participant is on just before entry i, from
// their player_team entries: the last playing side they changed to, or the
// side they changed from in their first change. NoSide if neither is T or
// CT. Changes to unassigned or spectator, as on a disconnect, don't count.
func sideAfter(log *Log, changes []int, i int) Side {
	s := NoSide
	if len(changes) > 0 {
		s = log.Entries[changes[0]].OldSide
	}
	for _, j := range changes {
		if j >= i {
			break
		}
		if side := log.Entries[j].Side; side.Playing() {
			s = side
		}
	}
	if !s.Playing() {
		return NoSide
	}
	return s
}

// sideOfTeamA returns the side team A played in each round.
func sideOfTeamA(spans []span, in [][]bool, obs [][]Side, swap []bool, n *notes) []Side {
	sideA := make([]Side, len(spans))
	if len(spans) == 0 {
		return sideA
	}
	var core []int // team A: those seen on CT in round 1
	for p, s := range obs[0] {
		if in[0][p] && s == CT {
			core = append(core, p)
		}
	}
	if len(core) == 0 {
		n.add("nobody is known to be on CT in round 1; team A's side follows the halftimes alone")
	}
	for r := range spans {
		expected := CT
		if r > 0 {
			expected = sideA[r-1]
			if swap[r-1] {
				expected = expected.Other()
			}
		}
		votes := map[Side]int{}
		for _, p := range core {
			if in[r][p] && obs[r][p].Playing() {
				votes[obs[r][p]]++
			}
		}
		switch {
		case votes[CT] > votes[T]:
			sideA[r] = CT
		case votes[T] > votes[CT]:
			sideA[r] = T
		default:
			sideA[r] = expected
		}
		if sideA[r] != expected {
			n.add("round %d: team A plays %v by player_team, but %v by the halftimes", r+1, sideA[r], expected)
		}
	}
	return sideA
}

// teams returns each participant's team.
func teams(log *Log, spans []span, in [][]bool, obs [][]Side, sideA []Side, n *notes) []team {
	out := make([]team, len(log.Players))
	for p := range log.Players {
		votes, first := map[team]int{}, noTeam
		for r := range spans {
			if !in[r][p] || !obs[r][p].Playing() {
				continue
			}
			t := teamB
			if obs[r][p] == sideA[r] {
				t = teamA
			}
			votes[t]++
			if first == noTeam {
				first = t
			}
		}
		switch {
		case votes[teamA] > votes[teamB]:
			out[p] = teamA
		case votes[teamB] > votes[teamA]:
			out[p] = teamB
		default:
			out[p] = first
		}
		if votes[teamA] > 0 && votes[teamB] > 0 {
			n.add("%s played for both teams (rounds: %d with A, %d with B); listed on %v", log.Players[p].Name(), votes[teamA], votes[teamB], out[p])
		}
	}

	// Anyone who played but was never observed: opposite the players they
	// fought.
	for p := range log.Players {
		if out[p] != noTeam || !playedAny(in, p) {
			continue
		}
		votes := map[team]int{}
		for _, sp := range spans {
			for _, e := range log.Entries[sp.start:sp.end] {
				if e.Kind != PlayerDeath || worldDeath(e) || e.Attacker == e.Player || e.Attacker == NoPlayer {
					continue
				}
				switch p {
				case e.Attacker:
					votes[out[e.Player]]++
				case e.Player:
					votes[out[e.Attacker]]++
				}
			}
		}
		out[p] = teamB
		if votes[teamB] > votes[teamA] {
			out[p] = teamA
		}
		n.add("%s has no known side; put on team %v, opposite the players they fought (%d A, %d B)",
			log.Players[p].Name(), out[p], votes[teamA], votes[teamB])
	}
	return out
}

// playedAny reports whether participant p played any round.
func playedAny(in [][]bool, p int) bool {
	for r := range in {
		if in[r][p] {
			return true
		}
	}
	return false
}

// checkSides notes signs that the sides are wrong: kills between players on
// the same side, and bombs planted by CTs or defused by Ts.
func checkSides(log *Log, spans []span, s sides, n *notes) {
	same := 0
	for r, sp := range spans {
		for _, e := range log.Entries[sp.start:sp.end] {
			switch e.Kind {
			case PlayerDeath:
				if !worldDeath(e) && e.Attacker != NoPlayer && e.Attacker != e.Player && s.of[r][e.Attacker] == s.of[r][e.Player] {
					same++
				}
			case BombPlanted:
				if e.Player != NoPlayer && s.of[r][e.Player] != T {
					n.add("round %d: bomb planted by %s, who is on %v", r+1, log.Players[e.Player].Name(), s.of[r][e.Player])
				}
			case BombDefused:
				if e.Player != NoPlayer && s.of[r][e.Player] != CT {
					n.add("round %d: bomb defused by %s, who is on %v", r+1, log.Players[e.Player].Name(), s.of[r][e.Player])
				}
			}
		}
	}
	if same > 0 {
		n.add("%d kills between players on the same side (team kills, or wrong sides)", same)
	}
}
