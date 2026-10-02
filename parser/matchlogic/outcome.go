package matchlogic

// outcome is how a round ended.
type outcome struct {
	endTick int32
	winner  Side
	reason  string // a match.json reason
}

// Round end reasons in match.json.
const (
	reasonElimination  = "elimination"
	reasonBombExploded = "bomb_exploded"
	reasonBombDefused  = "bomb_defused"
	reasonTimeExpired  = "time_expired"
	reasonSurrender    = "surrender"
)

// roundEndReasons maps the game's round_end reason codes to match.json
// reasons. 1, 7, 8, 9 and 18 are confirmed on a real demo, together with the
// message each comes with. 12 and 17 follow the same numbering of the
// game's reasons and are not yet seen in a demo.
var roundEndReasons = map[int]string{
	1:  reasonBombExploded, // #SFUI_Notice_Target_Bombed
	7:  reasonBombDefused,  // #SFUI_Notice_Bomb_Defused
	8:  reasonElimination,  // #SFUI_Notice_CTs_Win: every T dead
	9:  reasonElimination,  // #SFUI_Notice_Terrorists_Win: every CT dead
	12: reasonTimeExpired,  // #SFUI_Notice_Target_Saved
	17: reasonSurrender,    // #SFUI_Notice_Terrorists_Surrender
	18: reasonSurrender,    // #SFUI_Notice_CTs_Surrender
}

// defaultRestartDelay is mp_round_restart_delay's default in seconds: the
// time from a round's end to round_officially_ended.
const defaultRestartDelay = 7

// decideRounds returns how each round ended. A round with round_end takes
// its tick, winner and reason from it; the inference runs anyway, and a
// note says where the two disagree. A round without one is inferred, and
// guessed if nothing decided it (see guessOutcome).
func decideRounds(log *Log, spans []span, s sides, opt Options, n *notes) []outcome {
	out := make([]outcome, len(spans))
	for r, sp := range spans {
		at := sp.start
		if sp.freezeEnd >= 0 {
			at = sp.freezeEnd
		}
		roundTicks := roundTimeTicks(log, at, opt.Map, opt.TickRate, n)
		inferred, ok := inferOutcome(log, sp, s.of[r], roundTicks)
		switch {
		case sp.roundEnd >= 0:
			out[r] = fromRoundEnd(log.Entries[sp.roundEnd], inferred, ok, r, n)
		case ok:
			out[r] = inferred
		default:
			out[r] = guessOutcome(log, sp, opt.TickRate)
			n.add("round %d: nothing decided it; guessed %v %s at tick %d", r+1, out[r].winner, out[r].reason, out[r].endTick)
		}
	}
	return out
}

// fromRoundEnd reads a round_end entry. An unknown reason code, or a winner
// that isn't T or CT, falls back to the inferred outcome.
func fromRoundEnd(e Entry, inferred outcome, ok bool, r int, n *notes) outcome {
	o := outcome{endTick: e.Tick, winner: e.Side, reason: roundEndReasons[e.Reason]}
	if o.reason == "" {
		o.reason = reasonElimination
		if ok {
			o.reason = inferred.reason
		}
		n.add("round %d: round_end reason %d is not mapped; using %s", r+1, e.Reason, o.reason)
	}
	if !o.winner.Playing() {
		o.winner = inferred.winner
		n.add("round %d: round_end winner %d is not T or CT; using %v", r+1, e.Side, o.winner)
	}
	switch {
	case !ok:
		n.add("round %d: round_end says %v %s at tick %d, but no event decided it", r+1, o.winner, o.reason, o.endTick)
	case inferred != o:
		n.add("round %d: round_end says %v %s at tick %d, inference says %v %s at tick %d",
			r+1, o.winner, o.reason, o.endTick, inferred.winner, inferred.reason, inferred.endTick)
	}
	return o
}

// inferOutcome finds the event that decided the round, as the game decides
// it:
//
//   - the bomb explodes: T win, bomb_exploded;
//   - the bomb is defused: CT win, bomb_defused;
//   - a surrender vote passes: the other side wins, surrender;
//   - the last living CT dies or leaves: T win, elimination;
//   - the last living T dies or leaves with the bomb not planted: CT win,
//     elimination;
//   - the round timer, roundTicks after freeze end, runs out with the bomb
//     not planted: CT win, time_expired.
//
// Living players are tracked from spawns, presumed or recorded, deaths and
// disconnects, with the sides the round was played on. ok is false if nothing decided the round.
func inferOutcome(log *Log, sp span, side []Side, roundTicks int32) (o outcome, ok bool) {
	alive := make([]bool, len(side))
	var living, spawned [CT + 1]int // by side
	spawn := func(p int) {
		if p != NoPlayer && side[p].Playing() && !alive[p] {
			alive[p] = true
			living[side[p]]++
			spawned[side[p]]++
		}
	}
	for _, p := range sp.presumed {
		spawn(p)
	}
	planted := false
	expiry := int32(-1) // tick the round timer runs out, once freeze time ends
	for i := sp.start; i < sp.end; i++ {
		e := log.Entries[i]
		if expiry >= 0 && !planted && e.Tick > expiry {
			return outcome{expiry, CT, reasonTimeExpired}, true
		}
		switch e.Kind {
		case RoundFreezeEnd:
			if expiry < 0 && roundTicks > 0 {
				expiry = e.Tick + roundTicks
			}
		case PlayerSpawn:
			spawn(e.Player)
		case PlayerDeath, PlayerDisconnect:
			p := e.Player
			if p == NoPlayer || !alive[p] {
				continue
			}
			alive[p] = false
			living[side[p]]--
			switch {
			case spawned[CT] > 0 && living[CT] == 0:
				return outcome{e.Tick, T, reasonElimination}, true
			case spawned[T] > 0 && living[T] == 0 && !planted:
				return outcome{e.Tick, CT, reasonElimination}, true
			}
		case BombPlanted:
			planted = true
		case BombDefused:
			return outcome{e.Tick, CT, reasonBombDefused}, true
		case BombExploded:
			return outcome{e.Tick, T, reasonBombExploded}, true
		case Surrender:
			if e.Side.Playing() {
				return outcome{e.Tick, e.Side.Other(), reasonSurrender}, true
			}
		}
	}
	if expiry >= 0 && !planted && expiry < sp.endTick {
		return outcome{expiry, CT, reasonTimeExpired}, true
	}
	return outcome{}, false
}

// guessOutcome is the outcome of a round nothing decided: a planted bomb
// whose explosion the demo didn't record is a T win, anything else a CT win
// on time. The round ends the restart delay before round_officially_ended,
// or at the end of the match for the last round.
func guessOutcome(log *Log, sp span, tickRate int) outcome {
	o := outcome{endTick: sp.endTick, winner: CT, reason: reasonTimeExpired}
	if has(log, sp.start, sp.end, BombPlanted) {
		o.winner, o.reason = T, reasonBombExploded
	}
	if sp.restart >= 0 {
		delay := float64(defaultRestartDelay)
		if v, ok := cvarFloat(log, "mp_round_restart_delay", sp.restart); ok && v >= 0 {
			delay = v
		}
		o.endTick = log.Entries[sp.restart].Tick - int32(delay*float64(tickRate))
	}
	return o
}
