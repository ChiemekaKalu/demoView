package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// oracle runs tools/oracle as a subprocess. Until our entity decoding passes
// the position diff, v1 ships the oracle's round files (the spec's fallback
// rule).
type oracle struct {
	argv []string // the command and any leading arguments
	env  []string // added to the minimal environment the oracle gets
}

// oracleOutput is one oracle export: its match.json and round files.
type oracleOutput struct {
	matchJSON []byte
	rounds    []outFile // rounds/NN.json, in name order
	peakMem   int64     // the subprocess's peak RSS, or 0 where unknown
	stderr    string    // the tail of its stderr
}

type outFile struct {
	name string
	data []byte
}

// export streams demo into `oracle export - --out <tmp>` and reads back what
// it wrote. The demo is never written to disk; the output is a few MB. The
// output's peakMem and stderr are set even when it fails.
func (o *oracle) export(ctx context.Context, demo io.Reader) (oracleOutput, error) {
	var out oracleOutput
	dir, err := os.MkdirTemp("", "oracle-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(dir)

	args := append(append([]string{}, o.argv[1:]...), "export", "-", "--out", dir)
	cmd := exec.CommandContext(ctx, o.argv[0], args...)
	cmd.Env = o.environ()
	cmd.Stdin = demo
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	out.peakMem = childPeakMemory(cmd.ProcessState)
	out.stderr = tail(strings.TrimSpace(stderr.String()), 500)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, fmt.Errorf("oracle: %v: %s", err, oracleError(stderr.Bytes()))
	}

	if out.matchJSON, err = os.ReadFile(filepath.Join(dir, "match.json")); err != nil {
		return out, fmt.Errorf("oracle: %w", err)
	}
	names, err := filepath.Glob(filepath.Join(dir, "rounds", "*.json"))
	if err != nil {
		return out, err
	}
	sort.Strings(names)
	for _, p := range names {
		data, err := os.ReadFile(p)
		if err != nil {
			return out, fmt.Errorf("oracle: %w", err)
		}
		out.rounds = append(out.rounds, outFile{name: filepath.Base(p), data: data})
	}
	return out, nil
}

// environ is all of the environment the oracle sees: enough to run, and
// none of the server's secrets.
func (o *oracle) environ() []string {
	return append([]string{
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
		"HOME=" + os.TempDir(),
		"GOMEMLIMIT=" + oracleMemLimit,
	}, o.env...)
}

// oracleMemLimit is a soft memory limit for the oracle, so its GC works
// harder before the parse outgrows the 512 MiB budget. It peaks near 270 MiB.
const oracleMemLimit = "400MiB"

// oracleError is the message from the oracle's JSON error line on stderr,
// or the tail of stderr when there is none.
func oracleError(stderr []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	last := lines[len(lines)-1]
	var e struct{ Error string }
	if json.Unmarshal([]byte(last), &e) == nil && e.Error != "" {
		return e.Error
	}
	return tail(last, 500)
}

// tail is the last n bytes of s.
func tail(s string, n int) string {
	return s[max(len(s)-n, 0):]
}

// guardView is the part of match.json that round files depend on: they
// index players by idx and kills by position.
type guardView struct {
	ParserVersion string `json:"parser_version"`
	Players       []struct {
		SteamID string `json:"steam_id"`
		Team    string `json:"team"`
	} `json:"players"`
	Rounds []struct {
		N             int `json:"n"`
		StartTick     int `json:"start_tick"`
		FreezeEndTick int `json:"freeze_end_tick"`
		EndTick       int `json:"end_tick"`
	} `json:"rounds"`
	Kills []struct {
		Round    int    `json:"round"`
		Tick     int    `json:"tick"`
		Attacker *int   `json:"attacker"`
		Victim   *int   `json:"victim"`
		Weapon   string `json:"weapon"`
	} `json:"kills"`
}

// firstMismatch compares two match.json files on everything round files
// depend on: players (Steam id and team, in idx order), rounds (n and
// ticks) and kills (round, tick, attacker and victim by Steam id, weapon).
// It returns "" when they agree, else a description of the first
// difference. Only JSON is compared, so the Go types may change freely.
func firstMismatch(ours, theirs []byte) string {
	var a, b guardView
	if err := json.Unmarshal(ours, &a); err != nil {
		return "ours is not valid match.json: " + err.Error()
	}
	if err := json.Unmarshal(theirs, &b); err != nil {
		return "oracle's is not valid match.json: " + err.Error()
	}
	if len(a.Players) != len(b.Players) {
		return fmt.Sprintf("players: %d vs %d", len(a.Players), len(b.Players))
	}
	for i := range a.Players {
		if a.Players[i] != b.Players[i] {
			return fmt.Sprintf("players[%d]: %+v vs %+v", i, a.Players[i], b.Players[i])
		}
	}
	if len(a.Rounds) != len(b.Rounds) {
		return fmt.Sprintf("rounds: %d vs %d", len(a.Rounds), len(b.Rounds))
	}
	for i := range a.Rounds {
		if a.Rounds[i] != b.Rounds[i] {
			return fmt.Sprintf("rounds[%d]: %+v vs %+v", i, a.Rounds[i], b.Rounds[i])
		}
	}
	if len(a.Kills) != len(b.Kills) {
		return fmt.Sprintf("kills: %d vs %d", len(a.Kills), len(b.Kills))
	}
	steam := func(v guardView, idx *int) string {
		switch {
		case idx == nil:
			return "-"
		case *idx < 0 || *idx >= len(v.Players):
			return fmt.Sprintf("idx %d out of range", *idx)
		}
		return v.Players[*idx].SteamID
	}
	for i := range a.Kills {
		ka, kb := a.Kills[i], b.Kills[i]
		if ka.Round != kb.Round || ka.Tick != kb.Tick || ka.Weapon != kb.Weapon ||
			steam(a, ka.Attacker) != steam(b, kb.Attacker) || steam(a, ka.Victim) != steam(b, kb.Victim) {
			return fmt.Sprintf("kills[%d]: round %d tick %d %s %s->%s vs round %d tick %d %s %s->%s", i,
				ka.Round, ka.Tick, ka.Weapon, steam(a, ka.Attacker), steam(a, ka.Victim),
				kb.Round, kb.Tick, kb.Weapon, steam(b, kb.Attacker), steam(b, kb.Victim))
		}
	}
	return ""
}
