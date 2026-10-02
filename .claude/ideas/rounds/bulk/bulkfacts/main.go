// bulkfacts turns one CS2 demo into name-free per-round facts for the
// round-dynamics study: winner and reason, each side's economy at freeze end,
// every kill with the alive counts after it, the bomb timeline, utility thrown,
// enemy blindness and damage. Players appear only as a per-demo index.
// Research tooling on the research/bulk-demos branch; it is never merged.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

type econ struct {
	Equip   int `json:"equip"`
	Money   int `json:"money"`
	Rifles  int `json:"rifles"`
	AWPs    int `json:"awps"`
	Armor   int `json:"armor"`
	Helmets int `json:"helmets"`
	Kits    int `json:"kits"`
	N       int `json:"n"`
}

type kill struct {
	T      float64 `json:"t"`
	KS     string  `json:"ks"`
	VS     string  `json:"vs"`
	K      int     `json:"k"`
	V      int     `json:"v"`
	W      string  `json:"w"`
	HS     bool    `json:"hs,omitempty"`
	WB     bool    `json:"wb,omitempty"`
	NS     bool    `json:"ns,omitempty"`
	SM     bool    `json:"sm,omitempty"`
	BL     bool    `json:"bl,omitempty"`
	FA     bool    `json:"fa,omitempty"`
	KP     string  `json:"kp,omitempty"`
	VP     string  `json:"vp,omitempty"`
	D      int     `json:"d"`
	Alive  [2]int  `json:"a"`
	Post   bool    `json:"post,omitempty"`
}

type bomb struct {
	T     float64 `json:"t"`
	Site  string  `json:"site,omitempty"`
	P     int     `json:"p"`
	Alive [2]int  `json:"a"`
	Kit   bool    `json:"kit,omitempty"`
}

type round struct {
	N         int                       `json:"n"`
	Start     int                       `json:"start"`
	FreezeEnd int                       `json:"freeze_end"`
	End       int                       `json:"end"`
	Winner    string                    `json:"winner"`
	Reason    string                    `json:"reason"`
	ScoreCT   int                       `json:"score_ct"`
	ScoreT    int                       `json:"score_t"`
	CTIsA     bool                      `json:"ct_is_a"`
	Econ      map[string]econ           `json:"econ"`
	Kills     []kill                    `json:"kills"`
	Plant     *bomb                     `json:"plant"`
	Defuse    *bomb                     `json:"defuse"`
	Explode   *float64                  `json:"explode"`
	Nades     map[string]map[string]int `json:"nades"`
	BlindS    map[string]float64        `json:"blind_s"`
	Dmg       map[string]int            `json:"dmg"`
}

type out struct {
	V       int     `json:"v"`
	Map     string  `json:"map"`
	Build   int32   `json:"build"`
	Players int     `json:"players"`
	Rounds  []round `json:"rounds"`
	ParseMS int64   `json:"parse_ms"`
	Error   string  `json:"error,omitempty"`
}

var reasons = map[events.RoundEndReason]string{
	events.RoundEndReasonTargetBombed:        "bomb_exploded",
	events.RoundEndReasonBombDefused:         "bomb_defused",
	events.RoundEndReasonCTWin:               "elimination",
	events.RoundEndReasonTerroristsWin:       "elimination",
	events.RoundEndReasonTargetSaved:         "time_expired",
	events.RoundEndReasonTerroristsSurrender: "surrender",
	events.RoundEndReasonCTSurrender:         "surrender",
}

var nadeKind = map[common.EquipmentType]string{
	common.EqSmoke: "smoke", common.EqFlash: "flash", common.EqHE: "he",
	common.EqMolotov: "molotov", common.EqIncendiary: "molotov", common.EqDecoy: "decoy",
}

func side(t common.Team) string {
	switch t {
	case common.TeamTerrorists:
		return "T"
	case common.TeamCounterTerrorists:
		return "CT"
	}
	return ""
}

func r1(v float64) float64 { return math.Round(v*10) / 10 }

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: bulkfacts <file.dem>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	defer f.Close()

	t0 := time.Now()
	p := dem.NewParser(f)
	defer p.Close()
	gs := p.GameState()
	res := out{V: 1}
	idx := map[uint64]int{}
	pid := func(pl *common.Player) int {
		if pl == nil {
			return -1
		}
		key := pl.SteamID64
		if key == 0 {
			key = uint64(pl.UserID) | 1<<62 // bots: stable within a demo, never a real id
		}
		if i, ok := idx[key]; ok {
			return i
		}
		idx[key] = len(idx)
		return idx[key]
	}

	var cur *round
	var teamA map[uint64]bool
	alive := map[string]int{}
	over := false
	live := func() bool { return gs.IsMatchStarted() && !gs.IsWarmupPeriod() && !over }
	sec := func() float64 {
		if cur == nil || cur.FreezeEnd < 0 {
			return 0
		}
		return r1(float64(gs.IngameTick()-cur.FreezeEnd) / 64)
	}
	inRound := func() bool { return cur != nil && cur.FreezeEnd >= 0 && cur.End < 0 }

	p.RegisterNetMessageHandler(func(m *msg.CDemoFileHeader) {
		if res.Map == "" {
			res.Map = m.GetMapName()
			res.Build = m.GetBuildNum()
		}
	})
	p.RegisterEventHandler(func(events.MatchStart) {
		res.Rounds, cur, teamA, over = nil, nil, nil, false
	})
	p.RegisterEventHandler(func(events.AnnouncementWinPanelMatch) { over = true })
	p.RegisterEventHandler(func(events.RoundStart) {
		if !live() {
			cur = nil
			return
		}
		res.Rounds = append(res.Rounds, round{
			N: len(res.Rounds) + 1, Start: gs.IngameTick(), FreezeEnd: -1, End: -1,
			Econ: map[string]econ{}, Nades: map[string]map[string]int{"CT": {}, "T": {}},
			BlindS: map[string]float64{"CT": 0, "T": 0}, Dmg: map[string]int{"CT": 0, "T": 0},
		})
		cur = &res.Rounds[len(res.Rounds)-1]
	})
	p.RegisterEventHandler(func(events.RoundFreezetimeEnd) {
		if cur == nil || cur.FreezeEnd >= 0 || !live() {
			return
		}
		cur.FreezeEnd = gs.IngameTick()
		cur.ScoreCT = gs.TeamCounterTerrorists().Score()
		cur.ScoreT = gs.TeamTerrorists().Score()
		alive["CT"], alive["T"] = 0, 0
		ct := map[uint64]bool{}
		for _, pl := range gs.Participants().Playing() {
			s := side(pl.Team)
			if s == "" {
				continue
			}
			pid(pl)
			if s == "CT" {
				ct[pl.SteamID64] = true
			}
			if pl.IsAlive() {
				alive[s]++
			}
			e := cur.Econ[s]
			e.N++
			e.Equip += pl.EquipmentValueCurrent()
			e.Money += pl.Money()
			if pl.Armor() > 0 {
				e.Armor++
			}
			if pl.HasHelmet() {
				e.Helmets++
			}
			if pl.HasDefuseKit() {
				e.Kits++
			}
			for _, w := range pl.Weapons() {
				if w.Type == common.EqAWP {
					e.AWPs++
				}
				if w.Class() == common.EqClassRifle {
					e.Rifles++
				}
			}
			cur.Econ[s] = e
		}
		if teamA == nil {
			teamA = ct
		}
		overlap := 0
		for id := range ct {
			if teamA[id] {
				overlap++
			}
		}
		cur.CTIsA = overlap*2 >= len(ct)
	})
	p.RegisterEventHandler(func(e events.RoundEnd) {
		if cur == nil || cur.End >= 0 {
			return
		}
		cur.End = gs.IngameTick()
		cur.Winner = side(e.Winner)
		cur.Reason = reasons[e.Reason]
		if cur.FreezeEnd < 0 {
			cur.FreezeEnd = cur.End
		}
	})
	p.RegisterEventHandler(func(e events.Kill) {
		if cur == nil || cur.FreezeEnd < 0 || e.Victim == nil || !live() {
			return
		}
		k := kill{T: sec(), VS: side(e.Victim.Team), V: pid(e.Victim), K: -1, HS: e.IsHeadshot,
			WB: e.IsWallBang(), NS: e.NoScope, SM: e.ThroughSmoke, BL: e.AttackerBlind, FA: e.AssistedFlash,
			VP: e.Victim.LastPlaceName(), D: int(e.Distance), Post: cur.End >= 0}
		if e.Weapon != nil {
			k.W = e.Weapon.String()
		}
		if e.Killer != nil {
			k.K, k.KS, k.KP = pid(e.Killer), side(e.Killer.Team), e.Killer.LastPlaceName()
		}
		if !k.Post && alive[k.VS] > 0 {
			alive[k.VS]--
		}
		k.Alive = [2]int{alive["CT"], alive["T"]}
		cur.Kills = append(cur.Kills, k)
	})
	p.RegisterEventHandler(func(e events.BombPlanted) {
		if inRound() && cur.Plant == nil {
			site := ""
			if e.Site == events.BombsiteA || e.Site == events.BombsiteB {
				site = string(rune(e.Site))
			}
			cur.Plant = &bomb{T: sec(), Site: site, P: pid(e.Player), Alive: [2]int{alive["CT"], alive["T"]}}
		}
	})
	kits := map[int]bool{}
	p.RegisterEventHandler(func(e events.BombDefuseStart) {
		if inRound() {
			kits[pid(e.Player)] = e.HasKit
		}
	})
	p.RegisterEventHandler(func(e events.BombDefused) {
		if inRound() {
			id := pid(e.Player)
			cur.Defuse = &bomb{T: sec(), P: id, Alive: [2]int{alive["CT"], alive["T"]}, Kit: kits[id]}
		}
	})
	p.RegisterEventHandler(func(events.BombExplode) {
		if inRound() {
			t := sec()
			cur.Explode = &t
		}
	})
	p.RegisterEventHandler(func(e events.GrenadeProjectileThrow) {
		if !inRound() || e.Projectile == nil || e.Projectile.Thrower == nil || e.Projectile.WeaponInstance == nil {
			return
		}
		if k, ok := nadeKind[e.Projectile.WeaponInstance.Type]; ok {
			if s := side(e.Projectile.Thrower.Team); s != "" {
				cur.Nades[s][k]++
			}
		}
	})
	p.RegisterEventHandler(func(e events.PlayerFlashed) {
		if !inRound() || e.Player == nil || e.Attacker == nil || e.Player.Team == e.Attacker.Team || !e.Player.IsAlive() {
			return
		}
		if s := side(e.Attacker.Team); s != "" {
			cur.BlindS[s] = r1(cur.BlindS[s] + e.FlashDuration().Seconds())
		}
	})
	p.RegisterEventHandler(func(e events.PlayerHurt) {
		if !inRound() || e.Player == nil || e.Attacker == nil || e.Player.Team == e.Attacker.Team {
			return
		}
		if s := side(e.Attacker.Team); s != "" {
			cur.Dmg[s] += e.HealthDamageTaken
		}
	})

	err = p.ParseToEnd()
	res.ParseMS = time.Since(t0).Milliseconds()
	res.Players = len(idx)
	if err != nil {
		res.Error = err.Error()
	}
	// Drop rounds that never started or ended: a half-recorded last round
	// would read as a result.
	kept := res.Rounds[:0]
	for _, r := range res.Rounds {
		if r.FreezeEnd >= 0 && r.End >= 0 && r.Winner != "" {
			kept = append(kept, r)
		}
	}
	res.Rounds = kept
	if err := json.NewEncoder(os.Stdout).Encode(res); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	if res.Error != "" {
		os.Exit(1)
	}
}
