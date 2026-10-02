package matchlogic

import (
	"strconv"
	"strings"

	"github.com/ChiemekaKalu/demoview/parser/events"
	"github.com/ChiemekaKalu/demoview/parser/pb"
	"github.com/ChiemekaKalu/demoview/parser/players"
)

// Recorder builds a Log while a demo streams past. Feed it every game event,
// string-table snapshot, end-of-match message, vote result and cvar message,
// in demo order; then call Log. The zero value is not ready; use
// NewRecorder.
//
// Events name players by slot. The Recorder resolves each slot to a
// Participant when the event arrives, using the userinfo it has seen so far,
// so a slot that changes hands mid-match still attributes every event to the
// right person.
type Recorder struct {
	log    Log
	table  players.Table
	slots  [256]int       // participant in each slot, or NoPlayer
	byKey  map[string]int // participant by identity key
	placed [256]bool      // slots[s] is a placeholder: events named the slot before any userinfo did
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder {
	r := &Recorder{byKey: map[string]int{}}
	for i := range r.slots {
		r.slots[i] = NoPlayer
	}
	return r
}

// Log returns the log recorded so far. It shares memory with the Recorder.
func (r *Recorder) Log() *Log { return &r.log }

// cvarsUsed are the cvars that match logic reads.
var cvarsUsed = map[string]bool{
	"mp_maxrounds":           true,
	"mp_overtime_maxrounds":  true,
	"mp_halftime":            true,
	"mp_roundtime":           true,
	"mp_roundtime_defuse":    true,
	"mp_round_restart_delay": true,
}

// Event records e if match logic uses it, and ignores it otherwise. A
// player_connect updates who is in the slot it names.
func (r *Recorder) Event(e events.Event) {
	kind, ok := eventKinds[e.Name]
	if !ok {
		switch e.Name {
		case "player_connect":
			r.playerConnect(e)
		case "server_cvar":
			name, _ := e.String("cvarname")
			value, _ := e.String("cvarvalue")
			r.ConVar(e.Tick, name, value)
		}
		return
	}
	en := Entry{Kind: kind, Tick: e.Tick, Player: NoPlayer, Attacker: NoPlayer, Assister: NoPlayer}
	switch kind {
	case PlayerTeam:
		en.Player = r.player(e, "userid")
		en.Side = side(e, "team")
		en.OldSide = side(e, "oldteam")
	case PlayerSpawn, PlayerDisconnect, BombPlanted, BombDefused:
		en.Player = r.player(e, "userid")
	case BombBeginDefuse:
		en.Player = r.player(e, "userid")
		en.Kit, _ = e.Bool("haskit")
	case PlayerDeath:
		en.Player = r.player(e, "userid")
		en.Attacker = r.player(e, "attacker")
		en.Assister = r.player(e, "assister")
		en.Headshot, _ = e.Bool("headshot")
		en.FlashAssist, _ = e.Bool("assistedflash")
		en.Weapon, _ = e.String("weapon")
	case PlayerHurt:
		en.Player = r.player(e, "userid")
		en.Attacker = r.player(e, "attacker")
		en.Health, _ = e.Int("health")
		en.Damage, _ = e.Int("dmg_health")
		en.Weapon, _ = e.String("weapon")
	case RoundEnd:
		en.Side = side(e, "winner")
		en.Reason, _ = e.Int("reason")
	}
	r.log.Entries = append(r.log.Entries, en)
}

// eventKinds maps the game events that become entries to their kind.
var eventKinds = map[string]Kind{
	"begin_new_match":        BeginNewMatch,
	"round_prestart":         RoundPrestart,
	"round_start":            RoundStart,
	"round_poststart":        RoundPoststart,
	"round_freeze_end":       RoundFreezeEnd,
	"round_end":              RoundEnd,
	"round_officially_ended": RoundOfficiallyEnded,
	"cs_pre_restart":         PreRestart,
	"announce_phase_end":     AnnouncePhaseEnd,
	"cs_win_panel_match":     WinPanelMatch,
	"player_team":            PlayerTeam,
	"player_spawn":           PlayerSpawn,
	"player_disconnect":      PlayerDisconnect,
	"player_death":           PlayerDeath,
	"player_hurt":            PlayerHurt,
	"bomb_planted":           BombPlanted,
	"bomb_begindefuse":       BombBeginDefuse,
	"bomb_defused":           BombDefused,
	"bomb_exploded":          BombExploded,
}

// side reads a team number key.
func side(e events.Event, key string) Side {
	v, _ := e.Int(key)
	return Side(v)
}

// player resolves the userid in key to a participant, or NoPlayer for the
// game's "no player" (65535) or a missing key.
func (r *Recorder) player(e events.Event, key string) int {
	userid, ok := e.Int(key)
	if !ok || userid < 0 || userid == 0xFFFF {
		return NoPlayer
	}
	slot := players.SlotOf(int32(userid))
	if p := r.slots[slot]; p != NoPlayer {
		return p
	}
	// No userinfo has named this slot yet. Hold the events on a placeholder
	// that the slot's first userinfo fills in.
	p := len(r.log.Players)
	r.log.Players = append(r.log.Players, Participant{Slot: slot})
	r.slots[slot], r.placed[slot] = p, true
	return p
}

// playerConnect applies a player_connect event: someone new, or someone
// back, in a slot.
func (r *Recorder) playerConnect(e events.Event) {
	userid, ok := e.Int("userid")
	if !ok || userid < 0 || userid == 0xFFFF {
		return
	}
	info := players.Info{UserID: int32(userid)}
	info.Name, _ = e.String("name")
	info.SteamID64, _ = e.Uint64("xuid")
	info.Fake, _ = e.Bool("bot")
	slot := players.SlotOf(info.UserID)
	r.table.Set(slot, info)
	r.occupy(slot, info)
}

// StringTables applies a string-table snapshot (DEM_StringTables, or a
// DEM_FullPacket's string_table); only userinfo matters. Its error, from
// players.Table.ApplyStringTables, wraps demo.ErrBadDemo.
func (r *Recorder) StringTables(st *pb.CDemoStringTables) error {
	if err := r.table.ApplyStringTables(st); err != nil {
		return err
	}
	for slot, info := range r.table.All() {
		r.occupy(slot, info)
	}
	return nil
}

// occupy records that info is the player in slot from now on.
func (r *Recorder) occupy(slot players.Slot, info players.Info) {
	key := identity(info)
	p, known := r.byKey[key]
	switch {
	case known:
	case r.placed[slot]:
		p = r.slots[slot] // fill in the placeholder
	default:
		p = len(r.log.Players)
		r.log.Players = append(r.log.Players, Participant{})
	}
	r.byKey[key] = p
	r.slots[slot], r.placed[slot] = p, false
	pp := &r.log.Players[p]
	pp.Slot, pp.Info = slot, info
}

// identity returns the key that identifies the participant info describes:
// the SteamID64 for a human, the name for a bot or GOTV.
func identity(info players.Info) string {
	switch {
	case info.SteamID64 != 0:
		return "steam:" + strconv.FormatUint(info.SteamID64, 10)
	case info.HLTV:
		return "gotv:" + info.Name
	}
	return "bot:" + info.Name
}

// EndOfMatch applies the end-of-match player list (players.DecodeEndOfMatch):
// each player's final side and scoreboard name. A player is matched by
// SteamID64, or by name for a bot. One the Recorder hasn't seen yet takes
// over the slot the message gives.
func (r *Recorder) EndOfMatch(list []players.EndOfMatchPlayer) {
	for _, ep := range list {
		info := players.Info{Name: ep.Name, SteamID64: ep.SteamID64, UserID: int32(ep.Slot), Fake: ep.Bot}
		p, ok := r.byKey[identity(info)]
		if !ok {
			r.occupy(ep.Slot, info)
			p = r.slots[ep.Slot]
		}
		r.log.Players[p].FinalSide = Side(ep.Team)
		r.log.Players[p].FinalName = ep.Name
	}
}

// surrenderVote is the start of a passed surrender vote's display string,
// e.g. "#SFUI_vote_passed_surrender_q".
const surrenderVote = "#SFUI_vote_passed_surrender"

// VotePass records a passed surrender vote. Other votes, such as timeouts,
// are ignored. The vote's team is the side that gave up.
func (r *Recorder) VotePass(tick int32, v *pb.CCSUsrMsg_VotePass) {
	if !strings.HasPrefix(v.GetDispStr(), surrenderVote) {
		return
	}
	r.log.Entries = append(r.log.Entries, Entry{
		Kind: Surrender, Tick: tick, Side: Side(v.GetTeam()),
		Player: NoPlayer, Attacker: NoPlayer, Assister: NoPlayer,
	})
}

// ConVars records the cvars in a net_SetConVar message that match logic
// reads.
func (r *Recorder) ConVars(tick int32, cvars *pb.CMsg_CVars) {
	for _, cv := range cvars.GetCvars() {
		r.ConVar(tick, cv.GetName(), cv.GetValue())
	}
}

// ConVar records a cvar's value if match logic reads it.
func (r *Recorder) ConVar(tick int32, name, value string) {
	if !cvarsUsed[name] {
		return
	}
	r.log.Entries = append(r.log.Entries, Entry{
		Kind: ConVar, Tick: tick, Name: name, Value: value,
		Player: NoPlayer, Attacker: NoPlayer, Assister: NoPlayer,
	})
}
