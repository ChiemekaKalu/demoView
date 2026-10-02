package matchlogic

import (
	"slices"
	"strings"

	"github.com/ChiemekaKalu/demoview/parser/match"
)

// worldWeapons are the weapons of deaths no player caused: the world (a
// disconnect, a fall, a kick), map damage and the bomb. player_death names
// the victim as the attacker for these, so they look like suicides; a
// console "kill" looks the same and is counted as a world death too.
var worldWeapons = map[string]bool{
	"world":        true,
	"worldspawn":   true,
	"trigger_hurt": true,
	"planted_c4":   true,
}

// worldDeath reports whether a PlayerDeath entry has no attacker: none named,
// or a world weapon.
func worldDeath(e Entry) bool {
	return e.Attacker == NoPlayer || worldWeapons[e.Weapon]
}

// listKills lists every death in the live window, sorted by tick, in the
// round whose span holds it: a kill after one round's end and before the
// next one's start belongs to the round that ended.
//
// attacker is null for a world death and equals victim for a suicide.
// assister is the event's assister when they get the assist, that is when
// they are on the other side from the victim (see scoreboard), and null
// otherwise. weapon is the event's, without any "weapon_" prefix.
func listKills(log *Log, w window, spans []span, s sides, ro roster, n *notes) []match.Kill {
	var kills []match.Kill
	for i := w.start; i < w.end; i++ {
		e := log.Entries[i]
		if e.Kind != PlayerDeath {
			continue
		}
		r := roundOfDeath(log, spans, i)
		victim := listedIdx(ro, e.Player)
		if r < 0 || victim == nil {
			n.add("death at tick %d is outside every round or has no listed victim; left out", e.Tick)
			continue
		}
		k := match.Kill{
			Round:    r + 1,
			Tick:     int(e.Tick),
			Victim:   *victim,
			Weapon:   strings.TrimPrefix(e.Weapon, "weapon_"),
			Headshot: e.Headshot,
		}
		if !worldDeath(e) {
			k.Attacker = listedIdx(ro, e.Attacker)
		}
		if opponents(s.of[r], e.Assister, e.Player) {
			k.Assister = listedIdx(ro, e.Assister)
		}
		if k.Weapon == "" {
			k.Weapon = "unknown"
		}
		kills = append(kills, k)
	}
	slices.SortStableFunc(kills, func(a, b match.Kill) int { return a.Tick - b.Tick })
	return kills
}

// opponents reports whether participants a and b played on opposite sides,
// given the sides of a round. A participant is never their own opponent.
func opponents(side []Side, a, b int) bool {
	return a != NoPlayer && b != NoPlayer && a != b && side[a].Playing() && side[b].Playing() && side[a] != side[b]
}

// listedIdx returns participant p's idx, or nil if p is NoPlayer or not
// listed.
func listedIdx(ro roster, p int) *int {
	if p == NoPlayer || ro.idx[p] < 0 {
		return nil
	}
	i := ro.idx[p]
	return &i
}
