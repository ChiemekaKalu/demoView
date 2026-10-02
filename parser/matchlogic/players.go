package matchlogic

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/ChiemekaKalu/demoview/parser/match"
)

// roster is the match.json player list and the way to it from participants.
type roster struct {
	players []match.Player
	idx     []int // [participant]: their idx, or -1 if they aren't listed
}

// listPlayers lists everyone who played a live round, GOTV aside (it never
// plays). Bots are listed too, with steam_id "0"; a bot is identified by
// name, so a bot that leaves and rejoins is one player.
//
// The order is team A, then team B, each sorted by SteamID64 and then name,
// so bots come first in their team. idx is the position in that order.
func listPlayers(log *Log, s sides) roster {
	var listed []int
	for p := range log.Players {
		if s.team[p] != noTeam && playedAny(s.in, p) {
			listed = append(listed, p)
		}
	}
	slices.SortStableFunc(listed, func(a, b int) int {
		pa, pb := log.Players[a], log.Players[b]
		return cmp.Or(
			cmp.Compare(s.team[a], s.team[b]),
			cmp.Compare(pa.Info.SteamID64, pb.Info.SteamID64),
			cmp.Compare(pa.Name(), pb.Name()),
		)
	})
	ro := roster{idx: make([]int, len(log.Players))}
	for p := range ro.idx {
		ro.idx[p] = -1
	}
	for i, p := range listed {
		pp := log.Players[p]
		ro.idx[p] = i
		ro.players = append(ro.players, match.Player{
			Idx:     i,
			SteamID: strconv.FormatUint(pp.Info.SteamID64, 10),
			Name:    pp.Name(),
			Team:    s.team[p].String(),
		})
	}
	return ro
}

// spawned returns, for each round, the idx of every listed player with a
// player_spawn in it, ascending.
func spawned(log *Log, spans []span, ro roster) [][]int {
	out := make([][]int, len(spans))
	for r, sp := range spans {
		in := make([]bool, len(ro.players))
		for _, e := range log.Entries[sp.start:sp.end] {
			if e.Kind == PlayerSpawn && e.Player != NoPlayer && ro.idx[e.Player] >= 0 {
				in[ro.idx[e.Player]] = true
			}
		}
		for i, ok := range in {
			if ok {
				out[r] = append(out[r], i)
			}
		}
	}
	return out
}
