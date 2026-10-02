package matchlogic

import (
	"math"

	"github.com/ChiemekaKalu/demoview/parser/match"
)

// fullHealth is a player's health at spawn.
const fullHealth = 100

// tally is one participant's counts for the scoreboard.
type tally struct {
	kills, headshots, deaths, assists int
	damage                            int // enemy damage, capped at the victim's health
}

// scoreboard computes one row per listed player, by the rules the game's own
// counters follow (verified on two demos):
//
//   - kills: enemy kills only; suicides, world deaths and team kills
//     neither count nor subtract;
//   - deaths: every death in the live window, suicides and world deaths
//     included;
//   - assists: the event's assister when on the other side from the
//     victim, flash assists included;
//   - hs_pct: headshot kills / kills * 100, one decimal, 0 with no kills;
//   - adr: enemy damage / rounds, one decimal. A hit's damage is the health
//     it took: the victim's health before it minus player_hurt's health
//     after it. dmg_health won't do: it isn't capped at the health left,
//     and it is rounded, so it can be 1 less than the health a hit takes.
//     Team and self damage don't count. Every player divides by all the
//     match's rounds, as the game does.
//
// Sides are the ones played in the round of the kill or hit.
func scoreboard(log *Log, w window, spans []span, s sides, ro roster) []match.ScoreRow {
	t := make([]tally, len(log.Players))
	for r, sp := range spans {
		side := s.of[r]
		health := map[int]int{} // health before the next hit, by participant; full if absent
		for i := sp.start; i < sp.end; i++ {
			e := log.Entries[i]
			switch e.Kind {
			case PlayerSpawn:
				delete(health, e.Player)
			case PlayerHurt:
				before, ok := health[e.Player]
				if !ok {
					before = fullHealth
				}
				health[e.Player] = e.Health
				if w.contains(i) && opponents(side, e.Attacker, e.Player) {
					t[e.Attacker].damage += max(0, before-e.Health)
				}
			case PlayerDeath:
				if !w.contains(i) || e.Player == NoPlayer {
					continue
				}
				t[e.Player].deaths++
				if !worldDeath(e) && opponents(side, e.Attacker, e.Player) {
					t[e.Attacker].kills++
					if e.Headshot {
						t[e.Attacker].headshots++
					}
				}
				if opponents(side, e.Assister, e.Player) {
					t[e.Assister].assists++
				}
			}
		}
	}

	rows := make([]match.ScoreRow, len(ro.players))
	for p, i := range ro.idx {
		if i < 0 {
			continue
		}
		c := t[p]
		row := match.ScoreRow{Player: i, Kills: c.kills, Deaths: c.deaths, Assists: c.assists}
		if c.kills > 0 {
			row.HSPct = oneDecimal(100 * float64(c.headshots) / float64(c.kills))
		}
		if len(spans) > 0 {
			row.ADR = oneDecimal(float64(c.damage) / float64(len(spans)))
		}
		rows[i] = row
	}
	return rows
}

// oneDecimal rounds x to one decimal place.
func oneDecimal(x float64) float64 { return math.Round(x*10) / 10 }
