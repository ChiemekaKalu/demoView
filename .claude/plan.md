# Plan

How the spec gets built, checkpoint by checkpoint. The spec says what to build. This file says in what order, and tracks status. Check items off as they land.

## M0: schema and fixtures

- [ ] **0.1 Spec gaps.** Decide the open items under [Spec gaps](#spec-gaps), then encode them in `schema/` (tighten the permissive spots) and the parser.
- [x] **0.2 Schema v1.** `schema/match.schema.json`, `schema/round.schema.json`, `validate.mjs` with cross-field checks, and deterministic synthetic samples (Mirage, 13–9, 4 round files). `npm test` passes.
- [ ] **0.3 Reference parser.** Proposed: demoinfocs v5.2.0 in `tools/oracle/` (see Open questions). Write the export script that converts its output into our schema.
- [ ] **0.4 Fixture demos.** Need one matchmaking or Premier demo you played (with a screenshot of the final scoreboard), one FACEIT or pro demo, and one with overtime.
  - *Interim, approved by the user:* `parser/testdata/demos/faceit_nuke_2026.dem` (FACEIT, patch 14165, from demoinfocs' test-demo repo) and `valve_mm_mirage_2024.dem` (Valve MM, patch 13984, bundled with demoparser2). Both are gitignored.
- [ ] **0.5 Fixture files.** Export all 3 demos with 0.3. Exit (M0): the fixtures validate against the schema.

## M1: v0 parser

Each checkpoint maps to a v0 stage in the spec and ends with a check against real demos.

- [x] **1.1 CLI skeleton.** `demo parse <file> --out <dir> [--events-only]`, plus a dev-only `demo inspect <file>`. Flags may follow the file. On failure: one JSON line on stderr, `{"error": "...", "code": "..."}`, with exit 1 / `bad_demo`, 2 / `usage` or 3 / `internal`. Exit 1 maps to the Cloud Run 422 and exit 3 to a 5xx.
- [x] **1.2 Frame loop** (stage 1). Both interim demos walk to EOF with no error, and the FileInfo frame sits at the header's offset. *Re-check on the third fixture.*
- [x] **1.3 Decompress and decode** (stage 2). Snappy (klauspost/compress) plus `parser/pb` (game build 2000922). `inspect` prints the map, server, build and playback ticks, and these match both demos.
- [x] **1.4 Packet messages** (stage 3). `parser/bitread` plus `demo.MessageReader`. Every packet on both demos is consumed with at most 6 trailing bits. Mirage: 58k packets in 0.37 s. Nuke: 140k packets, 1.75M messages.
- [ ] **1.5 Game events** (stage 4). Exit: the `player_death` count matches the reference kill count before any filtering.
- [ ] **1.6 Player identity** (stage 5). Exit: every kill's attacker, victim and assister resolve to a name, Steam id and team.
- [ ] **1.7 Match logic** (stage 6), and the full `match.json`. Exit: the scoreboard matches the in-game one for a match you played.
- [ ] **1.8 `demo diff` and CI.** Exit: the diff passes on all 3 fixtures. This meets the v0 done-when and closes M1.

## M2 groundwork (pulled forward)

- [x] `worker/`: the four routes, queue consumer and DLQ, the D1 migration, and dev and prod wrangler config. 59 tests.
- [ ] `parser/store` (R2 over S3, plus a local dir), `parser/cmd/server` (`POST /parse`, stub mode via `PARSER_STUB_DIR`, one JSON log line per parse), and the Dockerfile. Being built by the `terminal` session.
- [ ] `parser/parse`: a shared library, so the CLI and the server decode the same way.

## Decisions

- **v0 scope (decided 2026-10-02): infer from events.** Since about Feb 2024, demos never fire `round_start`, `round_end`, `round_mvp` or `cs_win_panel_round`, though their descriptors remain. Both reference parsers rebuild them from `CCSGameRulesProxy` entity props. v0 stays events-only:
  - **Round boundaries:** `round_freeze_end`, `round_officially_ended` (none for the last round; use `cs_win_panel_match`), match start from the last `begin_new_match`, halftime from `announce_phase_end`.
  - **Winner and reason:** inferred from bomb events, eliminations and the round timer. Surrenders and backup restores can break this.
  - **Teams:** from `player_team`, `CS_UM_EndOfMatchAllPlayersData` when present, and the halftime swap.
  - **ADR:** allowed to be up to 1% off the in-game value. `player_hurt.dmg_health` is uncapped, so cap it at the HP before the hit. The exact value is the controller's `m_iDamage`.
  - **Exact values** come with entity decoding (game rules proxy, `m_iTeamNum`), which goes at the front of M4. The diff test flags mismatches until then.
- **Public demos as interim fixtures (decided 2026-10-02):** yes, until the user's own demos arrive.

## Spec gaps

Things the spec leaves open that the code has to decide. Each one has a proposed default. Once a gap is decided, encode it and delete it here.

### Scoreboard rules (verified against the game's counters on 2 real demos)

- **Kills:** enemy kills only. Suicides and world deaths do **not** subtract; that changed from CS:GO. Team kills are unverified.
- **Deaths:** every `player_death` victim between match start and `cs_win_panel_match`, including suicides.
- **Assists:** the event's `assister` when on the other team from the victim. Flash assists count. No damage threshold.
- **HS%:** headshot kills / kills × 100, on a 0–100 scale with one decimal; 0 when there are no kills.
- **ADR:** enemy damage capped at the victim's HP before the hit, team damage excluded, divided by rounds played, one decimal.
- **Ticks:** the demo-frame tick clock, not `net_Tick`, which is offset.

### Schema (from the schema draft; the samples follow these defaults)

- **Round `reason`:** `elimination | bomb_exploded | bomb_defused | time_expired | surrender`. `winner` plus `side_a` already says which side won.
- **Null `attacker`:** null for world, fall and bomb deaths, and `attacker == victim` for suicides. *Mismatch:* Go's `match.Kill.Attacker` is a plain `int`, so it should become `*int`.
- **`weapon`:** the kill event's name without `weapon_`, e.g. `ak47`, `m4a1_silencer`, `hegrenade`, `inferno`, `knife`, `world`. Open: do knife skins collapse to `knife`?
- **Players:** `idx` runs 0..N−1 in array order, team A first, and A is the team on CT in round 1. Include anyone who played a live round; leave out spectators, coaches and GOTV. Open: how bots appear (`steam_id "0"`?) and mid-match team switches.
- **Kill ownership:** a kill between one round's end and the next round's start belongs to the round that just ended. Kills are sorted by tick.
- **Surrender during freeze time:** set `freeze_end_tick = end_tick`.
- **Round file window:** `start_tick` = the round's `freeze_end_tick`. Frames run until the next round's start. Frame `i` is tick `start_tick + i·tick_rate/hz`. A kill's frame is `ceil((tick − start_tick)·hz/tick_rate)`.
- **`events[].kill`:** a 0-based index into `match.json` `kills`.
- **Event types:** `kill` only in v1. Adding bomb events needs new fields, which is a schema change.
- **Grenades:**
  - **types:** `smoke, flash, he, molotov, incendiary, decoy`.
  - **`end_frame`:** flash and HE end at detonation. Smoke ends when the cloud is gone. Molotov and incendiary end when the fire goes out. Decoy ends when it pops. Clamp to the last frame.
  - **`path`:** one `[x, y]` per frame from throw to detonation.
- **`yaw`:** integer degrees in [0, 360), with 0 = +x and counterclockwise.
- **Dead and absent players:** after death, x/y/yaw hold their death values with `hp` 0. A round file's `players` lists only those who spawned that round.
- **Files:** `NN` is at least two digits (`01.json`); the API route `/rounds/:n` stays unpadded. `hz` is read from the file, never assumed. `demo_sha256` is lowercase hex.
- **Stripping Steam ids** (an open question) would make `steam_id` nullable, which is a schema change.

### Parser CLI

- **CLI output.** The v0 scope says `demo parse match.dem > match.json`, but the CLI contract says `--out <dir>`. *Built:* the CLI contract.
- **Missing input file.** *Built:* exit 3 (`internal`). Alternative: treat it as `usage`.

### Worker and API (from the `worker/` scaffold; *Built* says what the code does today)

- **Status before upload.** *Built:* `uploaded` means "created, awaiting upload". Proposed: rename it to `pending`.
- **GET `/api/matches/:id` shape.** *Built:* `{id, status, error, map, score_a, score_b, parser_version, schema_version, created_at, parsed_at, players[], rounds[]}`, in `worker/README.md`. Proposed: copy it into the spec.
- **Kill feed source.** Round files reference kills by index, but D1 `kills` has no assister and no stable index. Proposed: add `GET /api/matches/:id/match.json`, served from R2.
- **Final score.** *Built:* the Worker counts round winners into `score_a`/`score_b`.
- **Upload auth.** *Built:* `Authorization: Bearer <token>` on both POSTs.
- **Caching vs reparse.** Round files are cached `immutable`, but a reparse overwrites the same keys. A reparse that yields fewer rounds also leaves stale `rounds/NN.json` behind, because the store has no delete. Proposed: versioned keys (`parsed/{id}/v{parser_version}/...`).
- **Retries.** *Built:* `max_retries: 2` gives the spec's 3 attempts. The status returns to `queued` with the last error, and the DLQ sets `failed`.
- **Secrets.** The Worker also needs R2 S3 credentials to presign uploads. The bucket env var is `R2_BUCKET` on both sides.
- **CORS.** R2 buckets need a CORS rule for the browser PUT. Proposed: serve the viewer from the same Worker, so no API CORS is needed.
- **Long subrequest.** The consumer waits up to 11 minutes for Cloud Run; confirm in M2.
- **Stub mode.** HEAD can't tell a missing bucket from a missing key, so a misconfigured bucket gives a 422 instead of a 500.
- **Docker.** The build context is the repo root (`docker build -f parser/Dockerfile .`) so that `schema/samples` lands in the image as `/fixtures`. It needs BuildKit and is unverified until CI builds it.
- **Small items.** Timestamps are Unix ms. 1 GB means 1024³ bytes. CI passes an explicit `--env` to `wrangler deploy`.

### Open questions (from the spec)

- **Reference parser.** Proposed: **demoinfocs-golang v5.2.0, pinned**, in `tools/oracle/` with its own `go.mod`, so `parser/` can't import it. It's actively maintained, patched for every breaking CS2 update, and exposes every schema field. demoparser2 0.42.0 is a second opinion.
  - When diffing, watch for:
    - demoparser2 duplicates `round_officially_ended`.
    - demoinfocs `InfernoExpired` is not the flame-out.
    - demoinfocs v6 changes the damage and health semantics.
- **Queues pricing.** Cloudflare lists Queues on Workers Free, with 24 h retention. Confirm on the account in M2.
- **Steam ids in public data.** *Built:* stored in D1, left out of the public GET.
- **Mirage constants (verified):** `pos_x` -3230, `pos_y` 1713, `scale` 5.0, on a 1024×1024 radar. Pixels: `px = (x - pos_x) / scale`, `py = (pos_y - y) / scale`.
