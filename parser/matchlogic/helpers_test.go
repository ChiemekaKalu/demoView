package matchlogic

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ChiemekaKalu/demoview/parser/players"
)

// testLog builds a synthetic Log entry by entry. Its methods return it, so
// calls chain.
type testLog struct{ Log }

// human adds a human who finished the match on side final (NoSide if the
// end-of-match message doesn't list them) and returns their index. Steam
// ids are fake: 1000 plus the index.
func (l *testLog) human(name string, final Side) int {
	p := len(l.Players)
	l.Players = append(l.Players, Participant{
		Slot:      players.Slot(p),
		Info:      players.Info{Name: name, SteamID64: 1000 + uint64(p), UserID: int32(p)},
		FinalSide: final,
	})
	return p
}

// bot adds a bot and returns its index.
func (l *testLog) bot(name string, final Side) int {
	p := len(l.Players)
	l.Players = append(l.Players, Participant{
		Slot:      players.Slot(p),
		Info:      players.Info{Name: name, UserID: int32(p), Fake: true},
		FinalSide: final,
	})
	return p
}

// gotv adds GOTV and returns its index.
func (l *testLog) gotv() int {
	p := len(l.Players)
	l.Players = append(l.Players, Participant{
		Slot: players.Slot(p),
		Info: players.Info{Name: "GOTV", UserID: int32(p), Fake: true, HLTV: true},
	})
	return p
}

// entry returns an entry naming nobody.
func entry(tick int32, k Kind) Entry {
	return Entry{Kind: k, Tick: tick, Player: NoPlayer, Attacker: NoPlayer, Assister: NoPlayer}
}

func (l *testLog) add(e Entry) *testLog {
	l.Entries = append(l.Entries, e)
	return l
}

// ev adds an event with no players, such as round_freeze_end.
func (l *testLog) ev(tick int32, k Kind) *testLog { return l.add(entry(tick, k)) }

func (l *testLog) cvar(tick int32, name, value string) *testLog {
	e := entry(tick, ConVar)
	e.Name, e.Value = name, value
	return l.add(e)
}

// round adds a restart at tick with everyone in ps spawning, and the end of
// freeze time at freezeEnd.
func (l *testLog) round(tick, freezeEnd int32, ps ...int) *testLog {
	l.ev(tick, RoundPrestart)
	l.spawn(tick, ps...)
	return l.ev(freezeEnd, RoundFreezeEnd)
}

func (l *testLog) spawn(tick int32, ps ...int) *testLog {
	for _, p := range ps {
		e := entry(tick, PlayerSpawn)
		e.Player = p
		l.add(e)
	}
	return l
}

func (l *testLog) team(tick int32, p int, from, to Side) *testLog {
	e := entry(tick, PlayerTeam)
	e.Player, e.OldSide, e.Side = p, from, to
	return l.add(e)
}

// killOpt sets an optional field of a kill.
type killOpt func(*Entry)

func headshot(e *Entry)       { e.Headshot = true }
func assist(p int) killOpt    { return func(e *Entry) { e.Assister = p } }
func flash(p int) killOpt     { return func(e *Entry) { e.Assister, e.FlashAssist = p, true } }
func weapon(w string) killOpt { return func(e *Entry) { e.Weapon = w } }

// kill adds a player_death of victim by attacker with an ak47, unless an
// option says otherwise.
func (l *testLog) kill(tick int32, attacker, victim int, opts ...killOpt) *testLog {
	e := entry(tick, PlayerDeath)
	e.Attacker, e.Player, e.Weapon = attacker, victim, "ak47"
	for _, o := range opts {
		o(&e)
	}
	return l.add(e)
}

// worldDeath adds the death the game reports when a player disconnects or
// falls: the victim as attacker, weapon "world".
func (l *testLog) world(tick int32, victim int) *testLog {
	return l.kill(tick, victim, victim, weapon("world"))
}

func (l *testLog) hurt(tick int32, attacker, victim, health, dmg int) *testLog {
	e := entry(tick, PlayerHurt)
	e.Attacker, e.Player, e.Health, e.Damage = attacker, victim, health, dmg
	return l.add(e)
}

func (l *testLog) disconnect(tick int32, p int) *testLog {
	e := entry(tick, PlayerDisconnect)
	e.Player = p
	return l.add(e)
}

func (l *testLog) bomb(tick int32, k Kind, p int) *testLog {
	e := entry(tick, k)
	e.Player = p
	return l.add(e)
}

func (l *testLog) roundEnd(tick int32, winner Side, reason int) *testLog {
	e := entry(tick, RoundEnd)
	e.Side, e.Reason = winner, reason
	return l.add(e)
}

func (l *testLog) surrender(tick int32, gaveUp Side) *testLog {
	e := entry(tick, Surrender)
	e.Side = gaveUp
	return l.add(e)
}

// twoVsTwo returns a log with players a and b, who finish on CT, and c and
// d, who finish on T, and the cvars of a short match with no halftime.
func twoVsTwo() (l *testLog, a, b, c, d int) {
	l = &testLog{}
	a, b = l.human("a", CT), l.human("b", CT)
	c, d = l.human("c", T), l.human("d", T)
	l.cvar(0, "mp_maxrounds", "24").cvar(0, "mp_roundtime_defuse", "1.92")
	return l, a, b, c, d
}

// mustBuild runs Build on de_test at 64 ticks and fails on any note not in
// allowed. The test matches are short, so it ignores the note that a match
// ended before either team won; TestCheckFinish covers that one.
func mustBuild(t *testing.T, l *testLog, allowed ...string) Result {
	t.Helper()
	res := Build(&l.Log, Options{Map: "de_test", TickRate: 64})
	for _, n := range res.Notes {
		if !slices.Contains(allowed, n) && !strings.HasPrefix(n, "the match ended ") {
			t.Errorf("unexpected note: %s", n)
		}
	}
	return res
}

// roundsText renders rounds compactly for comparisons:
// "n:start-freeze-end winner side_a reason".
func roundsText(res Result) []string {
	var out []string
	for _, r := range res.Rounds {
		out = append(out, fmt.Sprintf("%d:%d-%d-%d %s %s %s", r.N, r.StartTick, r.FreezeEndTick, r.EndTick, r.Winner, r.SideA, r.Reason))
	}
	return out
}
