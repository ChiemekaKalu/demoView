# Pickup: parser session

Written 2026-10-02, about 17:00 PT, for the next parser session. Read CLAUDE.md, then `.claude/plan.md` (M4 and the v1.2 section), then this.

## Where things stand

- **M4 is done.** Since 21:07 UTC on 2026-10-02, prod ships round files from our own parser (`PARSER_ROUNDS=parser`).
  - Prod's parse takes 7.2 s, against 12.7 s with the oracle.
  - Dev's `shadow.json` verdicts showed 0 mismatches. M6 6.4, 6.5 and 6.6 are checked off; only 6.7 (the CI gate on fixture diffs) is open.
- **The pro gate passed** on 6782d49 (research run 37062846092): 0 blocking of 132 public pro demos, 115 clean. The bar:
  - No demo with a reference may differ in round count, boundaries, winner, reason, side_a, team or kills.
  - Allowed: ADR-only diffs, the reference's `unknown` weapons, and the 2 demos it can't parse.
- **The timeline stage**, `parser/matchlogic/timeline.go`, holds every demo quirk of how rounds are marked; scoring reads only its output. Its doc comment lists the quirks:
  - which events mark a restart: `round_officially_ended`, or `cs_pre_restart` 19 ticks before the spawns;
  - recordings that start in round 1, with presumed spawns;
  - backup restores, which replay their round;
  - restarts the demo ends in;
  - deaths on a restart's tick, which count in the round before.
- **The narrative log's parser side is built** (36a0a69, 8706d20, cc4c6b3), to the proposed record set at the end of `.claude/logging.md`.
  - `narrative.go` emits the facts and the round, score and bomb records; `narrative_kills.go` emits opening kills, trades, multi-kills and clutches.
  - `demo parse` writes `<out>/log/` unless `--log off`. The server doesn't write the log yet.
- **Other work landed this session:**
  - `shadow.json` (ad0f9d3);
  - the `oracle_failed` fallback (a40a665);
  - the Match Pulse schema (9b0251d, edf0090);
  - `demo inspect --events`, a name-free event timeline (ca49811).

## In flight

- Branch `parser/pro-rounds` is at 02dfe09, behind main. It's only a research ref now; push a fresh one when you need a sweep.
- aiguy's research workflow is on `research/bulk-demos`, and every sweep keeps `inspect --events` timelines.
- The ADR diagnostic, `tools/adr/adr_rounds.py`, is committed but hasn't run yet. The user has since approved it (see below).

## Next, in order

1. **Coded notes.** Give each `notes.add` call in matchlogic (about 30 sites) a stable snake_case code and named fields, and emit them to the log's notes layer as `note.<code>`.
   - `Result.Notes` must stay byte-identical, since tests compare the strings.
   - Participants become idx in the log. No floats.
   - A subagent was dispatched for this but hit a usage limit before editing anything, so nothing is half-done.
2. **The log in R2 and its schema.** Both are decided; see "Decided by the user".
   - `cmd/server` sets `parse.Options{Log: true}` and writes the three files under `parsed/{id}/{parse_id}/log/` before `manifest.json`, which lists them.
   - Write `schema/log.schema.json` (`log_version` 1) to the record set in `.claude/logging.md`, with a sample and validator support like pulse's.
   - Update logging.md's layout section to the per-parse key, and tell pipeline the `round.bomb` shape for Pulse's state_version 2.
3. **The ADR follow-up.** The run is approved; see "Decided by the user". Push a branch with `tools/adr/adr_rounds.py` (it's on main) and give aiguy the ref. 5 full recordings have unexplained 2–11% ADR gaps: spirit-vs-faze-m3-vertigo, the-mongolz-vs-vitality-m2-overpass, faze-vs-mouz-m2-overpass, natus-vincere-vs-vitality-m2-anubis and mouz-vs-natus-vincere-m1-dust2.
   - The other 6 ADR-only demos are partial or restored recordings, so they're explained.
   - Prod already ships our ADR, so this blocks nothing.

## Gotchas

- **Stage new files before a pathspec commit.** `git commit -- dir/` skips untracked files; that broke main once (d4c824e, fixed by 2e3d668). Check `git status --short` afterwards.
- **No external demos on the user's machine.** Pro and public demos run only through aiguy's research workflow: push a branch (tell cloud first), then send aiguy the ref. The user's own demos stay local and never go to cloud fixtures.
- **Never route around another session's refusal.** If a peer was denied something, it goes back to the user.
- **Guards before any parser commit, from `parser/`:**
  - `gofmt -l .`, `go vet ./...` and `go test ./...`. These include `TestReferenceDiff` and `TestReferenceRounds` on the 10 local demos.
  - Byte-identical `demo parse --log off` output on `testdata/demos/*.dem` for behavior-neutral changes.
  - `tools/adr/scoreboard_check.py <demo.exe> match730_*.dem` must print ALL MATCH. Run it in a venv with `pip install demoparser2`.
- **`demo diff` shows only the first 40 mismatches,** so sweep kind counts are lower bounds.
- **Commits:** Conventional Commits, pathspecs, no Co-Authored-By trailer. cloud owns pushes to main; tell it before any branch push.

## Decided by the user (2026-10-02, relayed by cloud)

1. The narrative log goes under the per-parse prefix, `parsed/{id}/{parse_id}/log/`, listed in that parse's manifest. This replaces logging.md's `parsed/{id}/log/{parser_version}/`.
2. `schema/log.schema.json` is approved, written to the proposed record set at the end of `.claude/logging.md`.
3. `tools/adr/adr_rounds.py` may run in aiguy's research CI.

## Needs the user

- Nothing open from this session.
