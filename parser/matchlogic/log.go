// Package matchlogic turns a demo's game events into the players, rounds,
// kills and scoreboard of match.json.
//
// It works in two phases. While the demo streams past, a Recorder
// (record.go) keeps the few thousand events that matter in a compact Log,
// resolving each player slot to a stable participant as it goes. After the
// walk, Build (build.go) computes the match from the Log in separate steps,
// one file each:
//
//   - window.go: the live part of the match, from the last begin_new_match
//     to cs_win_panel_match.
//   - rounds.go: where each round starts and stops in the log.
//   - settings.go: the cvars: halftimes and the round timer.
//   - sides.go: which side every participant played in every round, which
//     team (A or B) they belong to, and the side team A played.
//   - outcome.go: each round's end tick, winner and reason, from round_end
//     when the demo has it and inferred from bomb events, eliminations,
//     surrender votes and the round timer when it doesn't.
//   - players.go, kills.go, scoreboard.go: the match.json lists.
//
// Newer demos never fire round_start, round_end or cs_win_panel_round, so
// everything here is written to work from the events that remain, and uses
// the old ones only where they exist.
package matchlogic

import (
	"strconv"

	"github.com/ChiemekaKalu/demoview/parser/players"
)

// Side is a team number as the game numbers them. Only T and CT play.
type Side int8

// Sides.
const (
	NoSide    Side = 0 // unassigned, or not known
	Spectator Side = 1
	T         Side = 2
	CT        Side = 3
)

// Playing reports whether s is T or CT.
func (s Side) Playing() bool { return s == T || s == CT }

// Other returns the opposing side: CT for T and T for CT. Other sides are
// returned as they are.
func (s Side) Other() Side {
	switch s {
	case T:
		return CT
	case CT:
		return T
	}
	return s
}

// String returns "T", "CT", or "Side(N)".
func (s Side) String() string {
	switch s {
	case T:
		return "T"
	case CT:
		return "CT"
	}
	return "Side(" + strconv.Itoa(int(s)) + ")"
}

// Kind is what a log entry records. Most kinds are the game event of the
// same name.
type Kind uint8

// Entry kinds.
const (
	BeginNewMatch        Kind = iota + 1 // begin_new_match: the match (re)starts and stats reset
	RoundPrestart                        // round_prestart: the first event of a round restart
	RoundStart                           // round_start: older demos only
	RoundPoststart                       // round_poststart
	RoundFreezeEnd                       // round_freeze_end
	RoundEnd                             // round_end, older demos only: Side won for Reason
	RoundOfficiallyEnded                 // round_officially_ended: fires as the next round restarts
	AnnouncePhaseEnd                     // announce_phase_end: halftime
	WinPanelMatch                        // cs_win_panel_match: the match is over
	PlayerTeam                           // player_team: Player moves from OldSide to Side
	PlayerSpawn                          // player_spawn
	PlayerDisconnect                     // player_disconnect
	PlayerDeath                          // player_death: Player killed by Attacker
	PlayerHurt                           // player_hurt: Player hurt by Attacker
	BombPlanted                          // bomb_planted by Player
	BombDefused                          // bomb_defused by Player
	BombExploded                         // bomb_exploded
	Surrender                            // a passed surrender vote (CS_UM_VotePass): Side gave up
	ConVar                               // a cvar's value (net_SetConVar or server_cvar): Name = Value
)

var kindNames = [...]string{
	BeginNewMatch:        "begin_new_match",
	RoundPrestart:        "round_prestart",
	RoundStart:           "round_start",
	RoundPoststart:       "round_poststart",
	RoundFreezeEnd:       "round_freeze_end",
	RoundEnd:             "round_end",
	RoundOfficiallyEnded: "round_officially_ended",
	AnnouncePhaseEnd:     "announce_phase_end",
	WinPanelMatch:        "cs_win_panel_match",
	PlayerTeam:           "player_team",
	PlayerSpawn:          "player_spawn",
	PlayerDisconnect:     "player_disconnect",
	PlayerDeath:          "player_death",
	PlayerHurt:           "player_hurt",
	BombPlanted:          "bomb_planted",
	BombDefused:          "bomb_defused",
	BombExploded:         "bomb_exploded",
	Surrender:            "surrender",
	ConVar:               "convar",
}

// String returns the kind's event name, or "Kind(N)".
func (k Kind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// NoPlayer is the value of an Entry's player fields when the event names
// nobody.
const NoPlayer = -1

// Entry is one recorded event. Which fields are set depends on Kind; the
// rest are zero, or NoPlayer for the player fields.
type Entry struct {
	Kind Kind
	Tick int32 // demo-frame tick

	// Player is the event's subject, an index into Log.Players: the victim
	// of a death or a hit, or the player who spawns, changes team,
	// disconnects, plants or defuses.
	Player int
	// Attacker and Assister are indexes into Log.Players. A world death has
	// the victim as its Attacker; see worldWeapons.
	Attacker, Assister int

	// Side is the new team of a PlayerTeam, the winner of a RoundEnd, and
	// the side that gave up for a Surrender. OldSide is a PlayerTeam's old
	// team.
	Side, OldSide Side
	Reason        int // RoundEnd: the game's reason code

	Health int // PlayerHurt: the victim's health after the hit, 0 if it was fatal
	Damage int // PlayerHurt: dmg_health, which is not capped at the health the victim had

	Headshot    bool   // PlayerDeath
	FlashAssist bool   // PlayerDeath: the assister flashed the victim
	Weapon      string // PlayerDeath: as the event names it, e.g. "ak47" or "world"

	Name, Value string // ConVar
}

// Participant is one person or bot the demo names, as a stable identity. A
// human is identified by SteamID64, so a reconnect, even to another slot, is
// the same participant. A bot is identified by name.
type Participant struct {
	Slot players.Slot // the slot it occupied last
	Info players.Info // the latest userinfo or player_connect data

	// FinalSide and FinalName come from the end-of-match message: the side
	// the participant finished on, and the name the game's scoreboard
	// shows. NoSide and "" if the message doesn't list them.
	FinalSide Side
	FinalName string
}

// Name returns the participant's name: the end-of-match name when there is
// one, which is what the game's scoreboard shows, and otherwise the latest
// userinfo name.
func (p Participant) Name() string {
	if p.FinalName != "" {
		return p.FinalName
	}
	return p.Info.Name
}

// Log is what a Recorder keeps of a demo: the events match logic needs, in
// demo order, and the participants they refer to.
type Log struct {
	Entries []Entry
	Players []Participant
}
