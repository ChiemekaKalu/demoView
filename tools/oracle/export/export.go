// Package export turns a CS2 demo into the project's schema v1 files using
// demoinfocs-golang. It is the test oracle for our own parser, and per the
// spec's fallback rule it also produces the round files that ship until the
// parser's entity decoding passes.
//
// Conventions follow .claude/plan.md ("Scoreboard rules" and "Schema").
// Where demoinfocs reads an exact game-state value (round winner and reason,
// teams, the damage counter behind ADR) the oracle uses it.
package export

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

// Version is the parser_version stamped on oracle output: our schema,
// produced by demoinfocs v5.2.0.
const Version = "0.1.0-oracle+demoinfocs.5.2.0"

// DefaultHz is the round file sample rate, 64 Hz: a frame for every tick of
// a 64-tick demo.
const DefaultHz = 64

// Result is everything one export produces.
type Result struct {
	Match  Match
	Rounds []RoundFile // in round order, one per match round
	Stats  Stats
}

// Run parses the demo in r and returns its match.json and round files. hz is
// the round file sample rate; it must divide the demo's tick rate.
func Run(r io.Reader, hz int) (*Result, error) {
	sum := sha256.New()
	tee := io.TeeReader(r, sum)
	p := dem.NewParser(tee)
	defer p.Close()

	x := newExporter(p, hz)
	if err := p.ParseToEnd(); err != nil {
		return nil, fmt.Errorf("demoinfocs: %w", err)
	}
	// demoinfocs stops at the Stop command; hash whatever follows it too.
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return nil, err
	}
	return x.result(hex.EncodeToString(sum.Sum(nil)))
}

// exporter collects what the files need while demoinfocs streams the demo.
type exporter struct {
	p   dem.Parser
	gs  dem.GameState
	hz  int
	err error // first fatal problem found during parsing

	mapName  string
	lastTick int

	// Everything below is reset at each begin_new_match, which drops the
	// warmup and any knife round played before it.
	players   map[string]*playerRec // by playerKey
	rounds    []*roundRec
	cur       *roundRec // the latest live round, open until the next starts
	rec       *recording
	kills     []*killRec
	damage    map[string]int // last m_iDamage seen per player
	matchOver bool           // cs_win_panel_match fired

	// Counts of things the oracle had to guess at.
	stats Stats
}

// Stats counts the places an export fell back on a guess. All zero is the
// expected result; anything else deserves a look.
type Stats struct {
	UnknownWeapons      int // kills with no raw weapon name ("unknown" written)
	UnknownGrenades     int // projectiles of an unrecognised type, skipped
	MolotovsWithoutFire int // molotovs that never caught fire, e.g. landed in a smoke
	KillFramesBefore    int // kills before their round file's start, clamped to 0
}

type playerRec struct {
	key     string
	steamID uint64
	name    string
}

type roundRec struct {
	n         int
	start     int
	freezeEnd int // -1 until known
	end       int // -1 until known
	winner    common.Team
	reason    events.RoundEndReason
	rec       *recording // the round file's samples
}

type killRec struct {
	round        *roundRec
	tick         int
	attacker     string // player key, "" for none
	victim       string
	assister     string
	victimUserID int
	weapon       string // raw player_death weapon without "weapon_"; "" until seen
	headshot     bool
	// The victim at the moment of death, which the round file holds after.
	deathX, deathY, deathYaw int
	// Sides at the moment of death, for players with no samples.
	attackerSide, victimSide common.Team
}

func newExporter(p dem.Parser, hz int) *exporter {
	x := &exporter{p: p, gs: p.GameState(), hz: hz}
	x.reset()

	p.RegisterNetMessageHandler(func(m *msg.CDemoFileHeader) {
		if x.mapName == "" {
			x.mapName = m.GetMapName()
		}
	})
	p.RegisterEventHandler(func(events.MatchStart) { x.reset() })
	p.RegisterEventHandler(x.onRoundStart)
	p.RegisterEventHandler(x.onFreezeEnd)
	p.RegisterEventHandler(x.onRoundEnd)
	p.RegisterEventHandler(x.onKill)
	p.RegisterEventHandler(x.onGameEvent)
	p.RegisterEventHandler(func(events.AnnouncementWinPanelMatch) {
		x.captureDamage()
		x.matchOver = true
	})
	p.RegisterEventHandler(x.onFrameDone)
	x.registerGrenades()
	return x
}

func (x *exporter) reset() {
	x.players = map[string]*playerRec{}
	x.rounds = nil
	x.cur = nil
	x.rec = nil
	x.kills = nil
	x.damage = map[string]int{}
	x.matchOver = false
}

func (x *exporter) tick() int { return x.gs.IngameTick() }

// live reports whether the game is in a scored round: after the match has
// started and outside warmup.
func (x *exporter) live() bool {
	return x.gs.IsMatchStarted() && !x.gs.IsWarmupPeriod() && !x.matchOver
}

func (x *exporter) onRoundStart(events.RoundStart) {
	t := x.tick()
	x.closeRecording(t)
	if !x.live() {
		return
	}
	x.cur = &roundRec{n: len(x.rounds) + 1, start: t, freezeEnd: -1, end: -1}
	x.rounds = append(x.rounds, x.cur)
}

func (x *exporter) onFreezeEnd(events.RoundFreezetimeEnd) {
	if x.cur == nil || x.cur.freezeEnd >= 0 || x.cur.end >= 0 || !x.live() {
		return
	}
	x.cur.freezeEnd = x.tick()
	x.startRecording(x.cur)
}

func (x *exporter) onRoundEnd(e events.RoundEnd) {
	if x.cur == nil || x.cur.end >= 0 {
		return
	}
	r := x.cur
	r.end = x.tick()
	r.winner = e.Winner
	r.reason = e.Reason
	// A round can end during freeze time, by surrender. Its file then starts
	// at the end.
	if r.freezeEnd < 0 {
		r.freezeEnd = r.end
		x.startRecording(r)
	}
	x.captureDamage()
}

func (x *exporter) onKill(e events.Kill) {
	if x.cur == nil || x.matchOver || e.Victim == nil || x.gs.IsWarmupPeriod() {
		return
	}
	k := &killRec{
		round:        x.cur,
		tick:         x.tick(),
		victim:       x.seePlayer(e.Victim),
		victimUserID: e.Victim.UserID,
		headshot:     e.IsHeadshot,
		victimSide:   e.Victim.Team,
	}
	if e.Killer != nil {
		k.attacker = x.seePlayer(e.Killer)
		k.attackerSide = e.Killer.Team
	}
	if e.Assister != nil {
		k.assister = x.seePlayer(e.Assister)
	}
	pos := e.Victim.Position()
	k.deathX, k.deathY = roundInt(pos.X), roundInt(pos.Y)
	k.deathYaw = yawDegrees(e.Victim.ViewDirectionX())
	x.kills = append(x.kills, k)
}

// onGameEvent picks the raw weapon name off player_death. demoinfocs maps it
// to an equipment type, which loses knife and skin names. It dispatches the
// Kill first and the raw event right after it.
func (x *exporter) onGameEvent(e events.GenericGameEvent) {
	if e.Name != "player_death" || len(x.kills) == 0 {
		return
	}
	weapon := strings.TrimPrefix(e.Data["weapon"].GetValString(), "weapon_")
	uid := int(e.Data["userid"].GetValShort())
	if uid <= math.MaxUint16 {
		uid &= 0xff // as demoinfocs resolves user ids
	}
	for i := len(x.kills) - 1; i >= 0; i-- {
		k := x.kills[i]
		if k.tick != x.tick() {
			break
		}
		if k.weapon == "" && k.victimUserID == uid {
			k.weapon = weapon
			if worldWeapons[weapon] {
				k.attacker = "" // no player killed them
			}
			return
		}
	}
}

// worldWeapons are the player_death weapons for deaths no player caused:
// falls, map hazards and the bomb. CS2 sets the attacker to the victim on
// these, which would otherwise read as a suicide.
var worldWeapons = map[string]bool{
	"world":        true,
	"worldspawn":   true,
	"trigger_hurt": true,
	"planted_c4":   true,
}

func (x *exporter) onFrameDone(events.FrameDone) {
	t := x.tick()
	x.lastTick = t
	if x.rec != nil {
		x.rec.sample(x, t)
	}
}

// seePlayer registers p and returns its key.
func (x *exporter) seePlayer(p *common.Player) string {
	key := playerKey(p)
	if pr := x.players[key]; pr != nil {
		pr.name = p.Name
	} else {
		x.players[key] = &playerRec{key: key, steamID: p.SteamID64, name: p.Name}
	}
	return key
}

// playerKey identifies a player across reconnects: the Steam id, or the
// name for bots, which have none.
func playerKey(p *common.Player) string {
	if p.SteamID64 != 0 {
		return fmt.Sprint(p.SteamID64)
	}
	return "bot:" + p.Name
}

// captureDamage records each player's damage counter, the game's own value
// behind ADR: enemy damage, capped at the victim's health, team damage
// excluded. It is read while the controller still exists, since players
// may leave before the end.
func (x *exporter) captureDamage() {
	for _, p := range x.gs.Participants().All() {
		if p == nil || p.Entity == nil || (p.Team != common.TeamTerrorists && p.Team != common.TeamCounterTerrorists) {
			continue
		}
		x.damage[x.seePlayer(p)] = p.TotalDamage()
	}
}

// finish closes the last round's window at the end of the demo.
func (x *exporter) finish() {
	x.closeRecording(x.lastTick + 1)
	x.captureDamage()
}

func (x *exporter) result(sha string) (*Result, error) {
	x.finish()
	if x.err != nil {
		return nil, x.err
	}
	// Drop a final round the demo cut off before it ended.
	for len(x.rounds) > 0 && x.rounds[len(x.rounds)-1].end < 0 {
		last := x.rounds[len(x.rounds)-1]
		x.rounds = x.rounds[:len(x.rounds)-1]
		kept := x.kills[:0]
		for _, k := range x.kills {
			if k.round != last {
				kept = append(kept, k)
			}
		}
		x.kills = kept
	}
	if len(x.rounds) == 0 {
		return nil, errors.New("no live rounds in the demo")
	}
	tickRate := int(math.Round(x.p.TickRate()))
	if tickRate <= 0 || tickRate%x.hz != 0 {
		return nil, fmt.Errorf("tick rate %d is not a multiple of %d Hz", tickRate, x.hz)
	}
	return x.build(sha, tickRate)
}

func roundInt(v float64) int { return int(math.Round(v)) }

// yawDegrees maps a yaw in degrees to an integer in [0, 360), 0 = +x,
// counterclockwise, as Source measures it.
func yawDegrees(yaw float32) int {
	d := int(math.Round(float64(yaw))) % 360
	if d < 0 {
		d += 360
	}
	return d
}

// ceilDiv is ceil(a / b) for b > 0.
func ceilDiv(a, b int) int {
	if a <= 0 {
		return -((-a) / b)
	}
	return (a + b - 1) / b
}
