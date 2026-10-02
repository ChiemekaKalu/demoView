# Plan

How the spec gets built, checkpoint by checkpoint. The spec says what to build. This file says in what order, and tracks status. Check items off as they land.

## M0: schema and fixtures

- [ ] **0.1 Spec gaps.** Decide the open items under [Spec gaps](#spec-gaps), then encode them in `schema/` (tighten the permissive spots) and the parser.
- [x] **0.2 Schema v1.** `schema/match.schema.json`, `schema/round.schema.json`, `validate.mjs` with cross-field checks, and deterministic synthetic samples (Mirage, 13–9, 4 round files). `npm test` passes.
- [x] **0.3 Reference parser.** `tools/oracle/`: its own module, demoinfocs v5.2.0 pinned. `oracle export <file> --out <dir> [--hz 16]` writes `match.json` plus round files in our schema, with `parser_version` `0.1.0-oracle+demoinfocs.5.2.0`.
  - On both demos, K, A and damage match the game's own counters exactly.
  - Nuke: 3.2 s, 182 MB peak.
- [x] **0.4 Fixture demos.** One matchmaking or Premier demo you played, one FACEIT or pro demo, and one with overtime. All three are now local.
  - *Played:* the user's 8 MM/Premier demos are local and gitignored. de_ancient (`match730_0038379968…_118`, 13-6 win) is the v0 candidate.
  - *Overtime:* `match730_003840038222499414100_1174486602_389`, de_inferno, 29 rounds, 13-16.
  - The `demoview-fixtures` bucket holds only the two approved public demos. Whether the user's demos go to the cloud is the user's call, because they carry other players' data. Don't upload them.
  - *Interim, approved by the user:* `parser/testdata/demos/faceit_nuke_2026.dem` (FACEIT, patch 14165, from demoinfocs' test-demo repo) and `valve_mm_mirage_2024.dem` (Valve MM, patch 13984, bundled with demoparser2). Both are gitignored.
- [ ] **0.5 Fixture files.** Export all 3 demos with 0.3. Exit (M0): the fixtures validate against the schema.
  - *Both interim demos are exported to `parser/testdata/reference/` (gitignored) and pass `--strict`.*
    - Mirage: 10 rounds, 0 warnings.
    - Nuke: 19 rounds, 2 warnings from kills during freeze time (see Spec gaps).
  - *All 10 local demos diff OK against the oracle,* the overtime match included.

## M1: v0 parser (done 2026-10-02)

Each checkpoint maps to a v0 stage in the spec and ends with a check against real demos.

- [x] **1.1 CLI skeleton.** `demo parse <file> --out <dir> [--events-only]`, plus a dev-only `demo inspect <file>`. Flags may follow the file. On failure: one JSON line on stderr, `{"error": "...", "code": "..."}`, with exit 1 / `bad_demo`, 2 / `usage` or 3 / `internal`. Exit 1 maps to the Cloud Run 422 and exit 3 to a 5xx.
- [x] **1.2 Frame loop** (stage 1). Both interim demos walk to EOF with no error, and the FileInfo frame sits at the header's offset. All 10 local demos walk clean.
- [x] **1.3 Decompress and decode** (stage 2). Snappy (klauspost/compress) plus `parser/pb` (game build 2000922). `inspect` prints the map, server, build and playback ticks, and these match both demos.
- [x] **1.4 Packet messages** (stage 3). `parser/bitread` plus `demo.MessageReader`. Every packet on both demos is consumed with at most 6 trailing bits. Mirage: 58k packets in 0.37 s. Nuke: 140k packets, 1.75M messages.
- [x] **1.5 Game events** (stage 4). `parser/events`. `player_death` matches demoparser2 row for row: Nuke 157, Mirage 73.
  - Nuke's 157 break down as 6 pre-match knife kills, 12 warmup, 138 live, and 1 post-match `world` suicide.
  - Mirage's 73 are 64 live plus 9 post-match `world` self-deaths.
  - Live window: after the last `begin_new_match`, up to `cs_win_panel_match`.
  - *Not yet wired into `parse.Run`.*
- [x] **1.6 Player identity** (stage 5). `parser/players` reads userinfo snapshots (StringTables and FullPacket; FullPacket tables are deltas) and the end-of-match player list (user message 375, present in both demos).
  - 10 humans per demo, whose names, Steam ids and final teams match demoparser2.
  - Bots can stand in for disconnected players and appear in kills.
  - *Per-kill resolution and per-round sides are checked in 1.7.*
- [x] **1.7 Match logic** (stage 6), and the full `match.json`. Exit: the scoreboard matches the in-game one for a match you played.
  - *Done on both interim demos.* `parser/matchlogic` records events while the demo streams, then builds players, rounds, kills and scoreboard.
  - Against the game's own counters: K/D/A, headshot kills and raw damage are exact for every player, and so is every round's winner, reason and end tick.
  - Kills match the oracle row for row. `demo parse` output passes the strict schema check.
  - A demo with no live rounds is a bad demo (exit 1, HTTP 422).
  - *Exit met (2026-10-02):* on all 8 of the user's own MM/Premier matches, K/A/D, HS kills and ADR equal the game's scoreboard counters at the win panel exactly, for 80/80 players. Those counters are what the in-game scoreboard reads. The counters were read with demoparser2 as a black box.
  - *Cross-check against the user's Leetify screenshot* of the Sep 4 de_ancient match: kills and deaths match all 10 players.
    - Leetify's assists are lower by exactly each player's flash assists (2, 2 and 1). The game counts flash assists in A; Leetify lists them separately.
    - Leetify's ADR is a whole number and 0–1.5 below ours, from its own damage calculation.
  - Choices to confirm are under Spec gaps → Match logic.
- [x] **1.8 `demo diff` and CI.** Exit: the diff passes on all 3 fixtures. This meets the v0 done-when and closes M1.
  - *Done:* package `parser/diff`. `Compare` checks match.json:
    - players by `steam_id` (bots by name);
    - rounds by `n`, all fields exact;
    - kills in order, with tick, attacker, victim, weapon, round, headshot and assister all exact;
    - K/D/A and HS% exact, and ADR within 1%.
  - *Done:* `CompareRound` checks round files: x/y within 1 unit, yaw within 1°, hp exact, plus grenades and events.
  - *Done:* the CI `diff` job. It pulls demos from `demoview-fixtures`, exports them with the oracle, then runs `demo diff`. It skips when the fixture secrets are absent. Deploy waits for every CI check.
  - *Done:* `demo diff <file.dem> --against <ref.json>`. It exits 0 when they agree, or 1 with code `mismatch` and the report on stdout. Both interim demos diff OK against the oracle.
  - The exit is met locally: `demo diff` passes on all 10 demos, including played MM, FACEIT and overtime. CI diffs the two public fixtures in `demoview-fixtures` once its secrets are set.

## M2 groundwork (pulled forward)

- [x] `worker/`: the four routes, queue consumer and DLQ, the D1 migration, and dev and prod wrangler config. 59 tests.
- [x] `parser/store`, `parser/cmd/server` and the Dockerfile.
  - `store`: R2 over S3 via aws-sdk-go-v2 s3 only, plus a local dir.
  - `cmd/server`: `POST /parse`, stub mode via `PARSER_STUB_DIR`, and one JSON log line per parse.
  - The Dockerfile's build context is the repo root. It's unverified until CI builds it.
- [x] `parser/parse`: `parse.Run`, shared by the CLI and the server.
- [x] Wrangler config points at the real Cloudflare account and D1 databases (user-approved), and `0001_init.sql` is applied to remote dev and prod D1.
- [x] Worker `GET /api/matches/:id/match.json`: streams it from R2, 404 until ready, max-age 300 plus an ETag.
- [x] The viewer is served from the same Worker. Assets come from `../web/dist` with an SPA fallback, and `/api/*` runs the Worker first, so the API needs no CORS. `npm run deploy:dev` and `deploy:prod` build the web app first.
- [x] CI workflows in `.github/`. They haven't run yet, because there's no remote.
  - **`ci.yml`:** 7 parallel jobs (parser, oracle, schema, worker, web, and a Docker build with no push) on PRs and non-main pushes.
  - **`deploy.yml`:** on main, pushes the image to Artifact Registry, then deploys dev, then prod. Each deploy runs Cloud Run, then D1 migrations, then the Worker, with `PARSER_URL` filled from the Cloud Run URL. It's being gated on CI, because the user commits straight to main.
  - **Setup:** the first-run steps and GCP/Cloudflare prerequisites are in `.github/README.md`.
- [x] Cloudflare dev and prod infra, done by the `cloud` session. Nothing is deployed yet.
  - D1 with the schema applied.
  - Queues and DLQs: `demoview-{env}-parse[-dlq]`.
  - R2 buckets `demoview-{env}` with CORS.
  - Per-bucket R2 S3 tokens, and Worker secrets set.
- [x] Parser on Cloudflare Containers.
  - **Stub test:** a 360 s request ran in full through the queue path.
  - **Worker:** `ParserContainer` binding instead of `PARSER_URL` (9c7175e). Falling back to Cloud Run is a one-liner.
  - **CI deploy:** reworked for Containers, no GCP (e047eea).
  - **Dev is live** with a container image built from HEAD 1b2f4bc. The image contract is unchanged: `$PORT` 8080, and `PARSE_SECRET` and `R2_*` come from the container's envVars.
- [x] The oracle defaults to 64 Hz (9ab7e1f). It writes compact round files, at most 1.5 MB raw (about 100 KB gzipped). Peak memory is 412 MB on Nuke.
- [x] **First real end-to-end parse on dev (2026-10-02).** The user's de_ancient match (166 MB) went from upload to ready in about 48 s; the parse took 31.6 s on attempt 1. The D1 scoreboard equals local `demo parse`. Round files are served as application/json with brotli: 503 KB becomes 56 KB on the wire at 64 Hz.
- [x] GitHub remote `ChiemekaKalu/demoView` (private). The first CI run passed all 7 checks, so the Docker build is verified.
- [x] **First green CI deploy (2026-10-02, run 36994672034).** All 7 checks passed, then dev and prod deployed from `main`. Prod is live. *M2 exit met.*
- [ ] *Optional:* set `FIXTURES_R2_ACCESS_KEY_ID` and `FIXTURES_R2_SECRET_ACCESS_KEY` (read-only on `demoview-fixtures`) so the CI diff job runs instead of skipping.

## M3 groundwork (pulled forward)

- [x] Reference round files in the Cloud Run pipeline.
  - **Image:** it carries an `oracle` binary, and `PARSER_ROUNDS=oracle` is set in the image.
  - **Flow:** after `parse.Run`, the server streams a second R2 GET of the demo into `oracle export -`, uploads its round files, then uploads `match.json` last.
  - **Consistency guard:** ours ships only when its players, rounds and kills agree with the oracle's. Otherwise the oracle's `match.json` ships, plus a warning, so the files always agree with each other. The log line records `match_source`.
  - **On the real demos** with the 1.7 work in the tree, ours agreed and shipped.
  - **Cost:** Nuke takes 3.7 s, with the oracle child at a 219 MB peak.

- [x] `web/`: upload, match and replay pages, built against `schema/samples` (fixtures mode: `VITE_FIXTURES=1` or id `sample`). Every spec replay bullet is in. Bundle 45.7 KB raw / 17.7 KB gzip. 50 vitest tests.
- [x] Mirage radar image at `web/public/radar/de_mirage.png`, from MurkyYT/cs2-map-icons @ ae7ed6a (1024×1024, extracted from the CS2 depot; the repo has no license, so these are Valve assets). Alignment verified by plotting real Mirage positions from the oracle export: trails follow the corridors, and spawns and bombsites match the overview file.

## M4 groundwork (pulled forward)

v1 ships on oracle round files, but entity decoding is the spec's hardest stage, so it starts early. Each piece is a standalone package, verified on the interim demos, and not wired into `parse.Run` until stage 3.

- [x] **Stage 1, classes and serializers** (`parser/entities`). Field layouts for every class on both demos: 211 and 218 classes, about 1.5k distinct fields.
  - Pawn position is `CBodyComponent` → `m_skeletonInstance.m_vecOrigin.m_cellX/Y/Z` + `m_vecX/Y/Z`. Also needed: `m_angEyeAngles`, `m_iHealth` and `m_lifeState`.
  - *Unverified until stage 3 reads real data:* `ClampHealth` zigzag, `minusone`, SimTime fields by name, pointer presence bits, and how a polymorphic pointer picks its type.
- [x] **Stage 2, string tables** (`parser/stringtables`). Packet create, update and clear-all messages match every snapshot byte for byte on both demos. `instancebaseline` is ready. *Open:* Mirage also has keys like `43:0`–`43:27` for `CCSGOViewModel`, with an unknown suffix, so look up baselines by the bare class id.
- [x] **Stage 3, entity updates** (1abbf6d, d455cbf).
  - Pawn x/y/yaw/hp agree with the oracle on **100% of frames on all 10 demos**. Every `svc_PacketEntities` is consumed exactly (1.18M), and Nuke's 351k full-packet values equal the delta-built state.
  - Nuke decodes in about 1.5 s on one CPU, at a 31 MB peak.
  - **Field paths:** a 40-op Huffman table; the tie-break rule was found empirically. 6 ops never occur in the 10 demos.
  - **Stage-1 unknowns resolved:** ClampHealth is zigzag, pointer presence is 1 bit, a polymorphic pointer is a UBitVar type index, and SimTime fields are varint ticks.
  - **For stage 4:** apply only the *first* FullPacket's entities, because later ones omit fields.
  - Found an oracle bug: a pre-round world death is filed under round 1. Reported to pipeline.
- [x] **Stages 4–5, extraction and round files** (ad573c1..0eaa37c).
  - `parse.Parse` with `Options{Rounds}`; packages `extract` and `roundfile`; the CLI writes and diffs round files.
  - Against the oracle on all 10 demos: players and kill events have zero mismatches. Grenades differ only where the oracle departs from plan.md: fire end (we end at `inferno_expire`, the flame-out; it ends at entity removal, about 13 s later), its `[0,0]` fire last point, and one undetonated decoy.
  - Byte-identical across runs.
  - An entity-stage error stops only that stage: `match.json` stays complete, and the CLI exits 1 with `unknown_entity`.
- [ ] **Pro-demo robustness (found 2026-10-02 by aiguy's bulk run).** 132 public pro GOTV demos (HF `skkwowee/chimera-cs2` @ 69b9cba, builds 9842–10847): no crashes, p95 1.6 s, max 61 MB. But:
  - Round segmentation collapses to 1 round on 113.
  - 2 are "no live rounds" bad_demo.
  - 9 have teams swapped (mostly build 9842).
  - A few have minor round-1 and ADR differences.
  - *Prod today:* the oracle guard ships the oracle's match.json for the collapsed and swapped cases, so they're correct. The 2 "no live rounds" demos fail with a 422.
  - **The swap-in default stays `oracle` until the pro sample passes.** Then: a pinned pro set in CI (pipeline), downloaded from HF at test time, never rehosted.
  - **Rule: no external demos on the user's machine.** Iterate only through aiguy's GitHub Actions research workflow in "parser" mode: push branch `parser/pro-rounds` (tell cloud first), then send aiguy the `parser_ref` directly.
  - *Fixed so far:*
    - 68a02cf: tournament GOTV fires no restart event in play, so `round_officially_ended` splits the rounds. This fixed the collapse (sweep 37051718863).
    - 53df3af: round 1 keeps the spawns and team changes before `begin_new_match`, and its start is clamped to the demo's first frame tick.
    - 0108c53: unknown `E*` enum fields decode as varints, and a projectile class missing from the build is skipped (build 9842: `EGrenadeThrowState`, `CFlashbangProjectile`).
    - 1dd244c: the CLI diff still compares match.json when the round files fail.
    - 76e0708: unknown types named like enums (`DecalMode_t`, any `…_t`) decode as varints. This covers 13 of the 21 `unknown_entity` failures, all `CWorld.m_nDecalMode`. A wrong guess between integer kinds still keeps the bitstream in step.
    - 4a3d389: sides are read at each round's freeze end, as the reference does. On build 9842 the halftime swap fires before the event that ends round 12.
    - ca49811: `demo inspect --events` prints game-event counts and a round-structure timeline (event names and ticks only, so it's safe to keep from any demo).
  - *Full-132 on 68a02cf (run 37054367353):*
    - 46 still collapse to 1 round: none of the four boundary events qualifies on them. The cause is unknown until the timelines come back.
    - 37 have a boundary or reason difference.
    - 21 fail with `unknown_entity`.
    - 14 have a clean match.json.
    - Parse p50 is 3.5 s, max 7.4 s and 219 MB.
  - *Repro on 1dd244c (run 37055026405):* 4 of the 10 have a clean match.json; the 9842 halftime and round-1 cases are left.
    - In the two round-1 cases, 9 or 10 of 10 players have no round-1 spawn. `begin_new_match` is at tick -1, so the recording probably starts after round 1's spawns.
    - The fix waits on the timelines rather than a guess.
  - *Not ours:* the 2 "no live rounds" demos also fail in the reference parser ("no live rounds in the demo"), so there's nothing to fall back to. Prod returns a 422, as it would with the reference parser alone.
  - *Now:* aiguy is running the `inspect --events` timelines on 11 demos (collapsed, round-1, no-live-rounds, plus checks), then the full 132 on ca49811.
  - `demo diff` shows only the first 40 mismatches, so sweep kind counts are from the truncated text.
- [ ] **Swap-in** (1587e55, de2ff27). The code is done and tested: `PARSER_ROUNDS=parser` (opt-in) ships our round files, falling back to the oracle on `entity_error` (6.5, tested) or `implausible_match`. `PARSER_SHADOW=oracle` diffs after a flushed response (6.6); a Mirage smoke run was a full match, 10 rounds and 0 mismatches.
  - *Remaining:* pipeline sets dev envVars (`PARSER_ROUNDS=parser`, `PARSER_SHADOW=oracle`); dev shadow lines come back clean; the pro sweep passes (parser/pro-rounds); then prod flips to `parser`. Never pass an empty `PARSER_ROUNDS` in prod.
- [x] **Narrative log groundwork** (`parser/eventlog`, build step 1; all six logging.md decisions approved by the user 2026-10-02). Envelope, Emitter, Nop, Spool and a deterministic finalizer, with facts, derived and notes as NDJSON plus a gzip with mtime 0. Floats and times are impossible by construction. Golden and run-twice tests. About 0.5 µs per record. Steps 2–6 (hooks, `--log`, outputs, schema, R2, viewer) are v1.2.

## M5: every map and safe reparse (v1.1)

The spec's v1.1 section is the source. Owners: **parser** is this session, **pipeline** is the Worker and viewer session.

- [x] **5.1 Maps route and loader** (pipeline, 091b2b5, c996015). `GET /api/maps/:name` serves `maps/radar_info` and radars. A map without a radar shows a plain background and a notice.
  - Two-floor maps (any `verticalsections` in the overview file) get a 404 until v1.2, so they replay on the plain background too.
  - The viewer keeps its bundled Mirage radar as the fallback, for fixtures mode with no backend.
- [x] **5.2 Radar-alignment check** (parser, `tools/align`). Bar: **97.5% per demo** (alpha > 0).
  - Measured: Mirage 99.46–99.64%; most misses are one stacked walkway the radar leaves transparent. Ancient 97.9–98.2%, because the radar hugs the walls. Inferno 99.4–99.98%. Overpass 99.68%.
  - The radars are aligned: the best offset is within 2 px. A 6 px shift drops every demo to 97.46% or below.
  - 99.5% would fail aligned demos. The bar comes from Mirage plus Ancient, so the spec wording needs updating (told cloud).
  - Local and dev only.
- [x] **5.3 Per-map diff entries** (parser). `TestReferenceDiff` runs one subtest per map and demo: Mirage 4, Ancient 2, Inferno 2, Overpass 1, Nuke 1. All OK.
- [x] **5.4 Duplicate upload** (pipeline, 25c96a6, 4498e20). `crypto.DigestStream` on confirm, indexed `demo_sha256`, existing id returned, duplicate object deleted.
  - The claim is one conditional UPDATE, so of two confirms of the same demo at once, only one parses. The new match row is deleted with the object.
  - Verified on dev and prod: a re-upload returns the original id with `duplicate: true`.
- [x] **5.5 Reparse route and cron** (pipeline). Keyed on the last attempted build, injected at deploy. Hourly, 5 per batch, paused at 80% of the container allowance. A failed reparse never takes a match off `ready`.
  - *Built:* the route (a37fc0d), the cron (9d5ead9), and container time per month in `container_usage` (1df36df).
  - `PARSER_BUILD` is 12 hex of a hash of the git trees of `parser/` and `tools/oracle/` (c7134a1), not the commit sha, so a web-only merge reparses nothing.
  - *Dev exit checks passed (2026-10-02):*
    - The 12:00 UTC batch moved all three dev matches, one of them a v1 match, onto new parses with nothing run by hand.
    - A poller over those 75 minutes made 9,228 requests and saw 0 404s and 0 mismatches between D1, `match.json` and the round files.
    - With the dev-only `FAULT=reject` flag (5994791; prod refuses it, 22b3884), two reparses failed and both matches stayed `ready` on their live parse, with `reparse_error` set.
  - Prod crons were turned on at the user's word (6e305d0).
  - *Prod (2026-10-02):* its one match was reparsed through the API. It went live on build 9f5be622def2 in 12.2 s and never left `ready`; its files serve at the new `?v=`. That was the last M5 exit check.
  - `POST /api/admin/cron/:job` (b64b6db) runs the sweeper, reparse batch or cleanup by hand, so checks don't wait for the clock.
- [x] **5.6 Safe swap.**
  - (parser) Write under a given `parsed/{id}/{parse_id}/` prefix, with `manifest.json` last. *Layout approved by the user (2026-10-02)*, along with the D1 columns and the reparse cron (hourly, 5 per batch, 80% cap, on in prod). It merges once CI passes. Contract proposed to pipeline: the Worker picks `parse_id`; `/parse` takes it as required; rounds, then match.json, then manifest.json last; `parsed_by` = `PARSER_BUILD` (the git sha).
  - (pipeline) Conditional D1 pointer swap, `?v=<parse_id>` URLs, 7-day cleanup by exact key. *Built:* 074f5b7, fff16cd and 9d5ead9. Parse ids are 9 base32 characters of time plus 7 random, one per attempt.
- [ ] **5.7 Self-recovery** (pipeline). A 30-minute stuck-match sweeper on `status_changed_at`, coded errors on the match page, an unknown-`schema_version` message, an upload rate limit, and an orphaned-demo lifecycle rule. The parser side is the error codes in its responses.
  - *Built:* the sweeper (9d5ead9), coded errors (2f91766, ed662e1), the `schema_version` message (ed662e1), and a 10-a-minute limit on uploads and reparses (856d8c5).
  - Daily cleanup also deletes uploads left unconfirmed after a day, with their demo.
  - *Left:* the R2 lifecycle rule for orphaned demos (cloud).
  - Each match's pipeline history is in D1 `match_events` (1df36df), shown on the match page (ed9700b).

## M6: trusted and hardened (v1.1)

- [x] **6.1 No Stop, bad demo** (parser, 27de943). The rule lives in `parse`. All 2,000 truncations of the public fixtures are `ErrBadDemo` with no panic, and all 10 local demos give identical output before and after.
- [x] **6.2 Fuzz targets** (parser). `bitread`, `FuzzFrames`, `FuzzApply` (string tables), `FuzzFieldPaths` and `FuzzApply` (entities, 1a5a988). 60 s runs crash-free, with 10-minute run lines in the doc comments.
  - Reading the code turned up two entity bugs: a dynamic-array write past its length, and an unbounded baseline cache, a memory-exhaustion risk from crafted demos. Both are being fixed in the M4 stage 4–5 work.
- [x] **6.3 Run-twice identity** (parser). Byte-identical `match.json` on all 10 local demos, from separate processes.
- [ ] **6.4 Budget** (parser). `TestBudget` passes events-only: the largest demo (336 MiB) parses in 0.96 s at 17 MiB with `GOMAXPROCS=1`.
  - *Full path (2026-10-02), largest fixture with GOMAXPROCS=1:* the oracle's 64 Hz export first peaked at 640 MiB, over budget.
    - pipeline's 23e232c brought it to **8.3 s and 270 MiB**, with byte-identical output, by compacting per-tick samples and freeing each round once built.
    - The server now runs the oracle with GOMEMLIMIT=400MiB (fe09822). So the full v1 path fits the budget.
  - *Our own full path:* 3.4 s and 108 MiB with round files, 0.58 s and 17 MiB events-only. **Within budget.**
- [ ] **6.5 Unknown entity data** (parser, after M4 stage 4). An unknown class, field or decoder is a coded entity-stage error. `match.json` still ships, round files fall back to the oracle, and `round_source=oracle` is logged. Exit: a synthetic unknown-field test.
- [x] **6.6 Shadow compare on dev** (parser and pipeline, after the M4 swap-in). Zero round-file mismatches against the oracle.
  - *Passed (2026-10-02):* dev ships our own round files (`PARSER_ROUNDS=parser`, e439244), with the shadow compare saved as `shadow.json` next to each parse (ad0f9d3). Container output never reaches `wrangler tail`.
  - All three dev matches were reparsed. Each `shadow.json` reads `result: match`, `round_source: parser`, `mismatches: 0`, over 19 + 10 + 19 rounds.
  - An independent diff of the deployed files against local oracle exports, with `parser/diff`, also found 0 mismatches.
  - The oracle re-run takes 15–63 s on the `basic` instance, after the response. Dev's `sleepAfter` is 2 minutes, so it finishes.
- [ ] **6.7 CI diff gates deploys** (pipeline). Needs the fixtures bucket secrets.
- [x] **6.8 Fault-flag recovery** (pipeline). 20 uploads each end `ready` or `failed` with a coded error, and none sits past 30 minutes. The flag is dev-only.
  - *Passed on dev (2026-10-02)* with `FAULT=fail`, so every attempt failed transiently. All 20 went through 3 attempts and the dead letter queue, and ended `failed` with `parse_failed: failed after 3 attempts: fault injected`.
  - The longest wait in `queued` or `parsing` was 0.5 minutes.
  - The flag was then deleted, and the 20 test matches, their demos and their history were removed.

## v1.2 (decided)

- The Steam/Valve auto-fetch (design below).
- Two-floor maps (Nuke and Vertigo): a z or level column, a layer toggle and a `schema_version` bump.
- The narrative log's output, schema, derived events and story pane, on top of the M4 `parser/eventlog` groundwork.

## Valve fetch (v1.2; design only)

Research done 2026-10-02. The approach still works for CS2: Leetify, csstats.gg and cs-demo-downloader all use it.

**Flow:**
1. **Discover.** An hourly Worker cron calls `ICSGOPlayers_730/GetNextMatchSharingCode/v1` in a loop, about one call per 1.5 s, until it gets `n/a`. The share-link box on the upload page also feeds this step. Each share code decodes locally to `matchid`, `outcomeid` and `token`; the code is base-57, with the first character least significant. New matches go to a fetch queue.
2. **Resolve.** A fetcher container behind a Durable Object starts only when there's work.
   - It runs Node with DoctorMcKay's `steam-user` and `globaloffensive`, the maintained option for CS2. Go's go-steam forks have no CS2 GC support.
   - It logs in the bot with its refresh token, plays app 730 and waits for the GC welcome.
   - Then `requestGame(code)` returns the demo URL, at `roundstatsall[last].map`. Requests go one at a time, with 30 s timeouts.
   - Afterwards it logs off and persists any renewed refresh token.
3. **Download.** `http://replayNNN.valve.net/730/…dem.bz2`, one at a time, streamed through bunzip2 into `demos/{match_id}.dem`. The match row is created, and the existing parse queue takes over.

**Checkpoints:**
- [ ] **V1.** Share-code decoder in `worker/`, tested against known vectors. A `fetch_jobs` D1 table holds code, matchid, status, attempts, url and error. Plus an API route to submit a share link, behind the upload token.
- [ ] **V2.** Discovery cron with the Web API. It handles 412 (a stale or wrong seed code) by surfacing "needs a fresh share code", and 403 for a revoked auth code.
- [ ] **V3.** The fetcher container: GC login and code-to-URL resolution. Includes a one-time local bootstrap script that turns the bot's username, password and Steam Guard code into a refresh token. The password is never stored.
- [ ] **V4.** Download, bunzip2 and R2, then hand off to the parse pipeline. Retries cover "no URL yet" and 404 for up to about 24 h. A 502 or 404 after about 30 days marks the match dead.
- [ ] **V5.** Viewer: the share-link box on `/` and fetch-job status.

**The user supplies:**
| Secret | What | Where |
|---|---|---|
| `STEAM_WEB_API_KEY` | Steam Web API key | steamcommunity.com/dev/apikey, from the user's main account. A fresh bot account is "limited" and can't get a key. |
| `STEAM_ID64` | The user's SteamID64 | Their Steam profile |
| `MATCH_AUTH_CODE` | Match-history authentication code (`XXXX-XXXXX-XXXX`) | help.steampowered.com/en/wizard/HelpWithGameIssue/?appid=730&issueid=128 |
| `SEED_SHARE_CODE` | A share code under 30 days old | Same wizard page, or in-game Watch → Your Matches |
| `STEAM_BOT_REFRESH_TOKEN` | The bot's refresh token, about 200 days | The V3 bootstrap script, run once locally. The bot is a new free account with the free CS2 license. |

**Risks:**
- **Steam Subscriber Agreement §4.C (Automation)** covers headless bots, so use a throwaway bot with nothing of value on it. The Web API part is officially documented.
- **30-day windows:** both the seed share code and the demos expire.
- **GC fragility:** `globaloffensive` hard-codes a hello version, and an issue reports empty `matches`.
- **Refresh-token expiry:** alert before its `exp`.
- **Throttling:** the replay CDN throttles bursts from data-center IPs, so download one file at a time.

## Decisions

- **v0 scope (decided 2026-10-02): infer from events.** Newer patches never fire `round_start`, `round_end`, `round_mvp` or `cs_win_panel_round`, though their descriptors remain. Patch 13984 (early 2024) still fires them, so v0 uses them when present and infers when absent. Both reference parsers rebuild them from `CCSGameRulesProxy` entity props. v0 stays events-only:
  - **Round boundaries:** `round_freeze_end`, `round_officially_ended` (none for the last round; use `cs_win_panel_match`), match start from the last `begin_new_match`, halftime from `announce_phase_end`.
  - **Winner and reason:** inferred from bomb events, eliminations and the round timer. Surrenders and backup restores can break this.
  - **Teams:** from `player_team`, `CS_UM_EndOfMatchAllPlayersData` when present, and the halftime swap.
  - **ADR:** allowed to be up to 1% off the in-game value. In practice it is exact: each hit counts as HP before minus `player_hurt.health`, because `dmg_health` is uncapped and rounded.
  - **Exact values** come with entity decoding (game rules proxy, `m_iTeamNum`), which goes at the front of M4. The diff test flags mismatches until then.
- **Public demos as interim fixtures (decided 2026-10-02):** yes, until the user's own demos arrive.
- **Wrangler account and D1 ids in git (approved by the user 2026-10-02, in the `terminal` session):** committed in `worker/wrangler.jsonc`.
- **Parser runtime: Cloudflare Containers, not Cloud Run (decided by the user 2026-10-02, in the `cloud` session).** $5/mo and simpler; Cloud Run only if the project grows. This deliberately departs from the spec, and the spec text changes after a stub test of a multi-minute request passes.
  - GCP setup is on hold.
  - Cloud Run pieces already built stay until the Containers design is confirmed: the deploy.yml steps and the `PARSER_URL` call.
  - The image and server (`$PORT`, env config) carry over.
- **Map data in R2 (decided by the user 2026-10-02):** only what the viewer needs: `maps/radars/*`, `maps/radar_info/*`, `maps/available.json` and `maps/manifest.json`, in both buckets, private. No thumbs or icons. *Done:* 100 objects (17.4 MB) per bucket, from cs2-map-icons @ ae7ed6a; every object re-downloaded and sha256-checked.
- **Pull the user's demos from Valve (scope change, decided by the user 2026-10-02).** The spec had "auto-fetching matches from Steam" out of scope through v1; the user pulled it in.
  - The login is a dedicated Steam bot account, never the user's main account.
  - Matches are found both ways: polling the user's match history (Steam Web API) and pasting a share link on the upload page.
  - It runs in the cloud, next to the parser.
  - It uses network APIs only (Steam Web API, Game Coordinator, Valve's replay servers), so the game-memory boundary still holds.
  - *Timing (decided by the user 2026-10-02): v1.2.* Build nothing for it before then: no infra, no secrets, no containers. The design is under [Valve fetch](#valve-fetch-v12-design-only).
- **Narrative logging:** the `cloud` session is running a separate design effort for structured, tick-stamped parser events. It will come as a proposal; don't build logging into the parser before then.

### Oracle conventions (built in `tools/oracle`; 1.7 matches them so the diff lines up)

- **idx order:** team A first. Within a team, SteamID64 ascending, then name. Team A is the team on CT at round 1's frame 0. `side_a` is the majority side of team A at each freeze end.
- **World deaths:** in these demos, `player_death` gives `attacker == userid` for world, fall and kick deaths. The attacker is set to null when the weapon is `world`, `worldspawn`, `trigger_hurt` or `planted_c4`. A console `kill` suicide becomes null too, because it looks identical.
- **Bots** who played a live round are listed with `steam_id` "0". On Nuke that's "Ava", who filled an empty slot, giving 11 players.
- **Weapon names:** the raw name without `weapon_`. Knife skins stay raw, e.g. `knife_widowmaker`.
- **ADR denominator:** all live rounds, the same for every player.
- **Molotovs that never ignite** (landed in smoke): detonate and end at the projectile's destroy.

## Spec gaps

Things the spec leaves open that the code has to decide. Each one has a proposed default. Once a gap is decided, encode it and delete it here.

### Scoreboard rules (verified against the game's counters on 2 real demos)

- **Kills:** enemy kills only. Suicides and world deaths do **not** subtract; that changed from CS:GO. Team kills are unverified.
- **Deaths:** every `player_death` victim between match start and `cs_win_panel_match`, including suicides.
  - **Needs a decision:** the game's end-of-demo death counter also counts the world deaths the server inflicts *after* `cs_win_panel_match`. That's +1 for 1 Nuke player and 9 of 10 Mirage players.
  - Whether the in-game scoreboard shows them depends on when it's read. The screenshot from your own match settles it.
  - Until then, stop at the win panel.
- **Assists:** the event's `assister` when on the other team from the victim. Flash assists count. No damage threshold.
- **HS%:** headshot kills / kills × 100, on a 0–100 scale with one decimal; 0 when there are no kills.
- **ADR:** enemy damage, where each hit counts as the victim's HP before minus `health` after (not `dmg_health`), team damage excluded, divided by all live rounds, one decimal. This matches the game's `m_iDamage` exactly.
- **Ticks:** the demo-frame tick clock, not `net_Tick`, which is offset.

### Schema (from the schema draft; the samples follow these defaults)

- **Round `reason`:** `elimination | bomb_exploded | bomb_defused | time_expired | surrender`. `winner` plus `side_a` already says which side won.
- **Null `attacker`:** null for world, fall and bomb deaths, and `attacker == victim` for suicides. *Built:* `match.Kill.Attacker` is `*int`.

### Match logic (built in 1.7; confirm)

- Bots are keyed by name, so a bot holding two slots is one player, `steam_id` "0".
- Names come from the end-of-match message when it lists the player, which is what the game's scoreboard shows.
- An assister on the victim's own side is dropped from the kill list, as the oracle does.
- Round `start_tick` comes from `round_start` or `round_prestart`. Round 1 is clamped to `begin_new_match`.
- Round timer: `mp_roundtime_defuse` on de_ maps, otherwise 1.92 min with a note. No fixture has a round that ran out of time.
- Overtime swaps: none entering overtime, then one at each overtime half. `player_team` gives them directly on modern demos. Verified: the overtime demo diffs OK against the oracle.
- Reason codes 12 (time) and 17/18 (surrender) follow the game's numbering. Only a surrender is covered by a fixture.
- Match-logic notes, i.e. what it had to guess, are returned by `Build` but not surfaced in the CLI or server yet.
- Team kills (they don't count and don't subtract) and coaches are untested.
- **`weapon`:** the kill event's name without `weapon_`, e.g. `ak47`, `m4a1_silencer`, `hegrenade`, `inferno`, `knife`, `world`. Open: do knife skins collapse to `knife`?
- **Players:** `idx` runs 0..N−1 in array order, team A first, and A is the team on CT in round 1. Include anyone who played a live round; leave out spectators, coaches and GOTV. Open: how bots appear (`steam_id "0"`?) and mid-match team switches.
- **Kill ownership:** a kill between one round's end and the next round's start belongs to the round that just ended. Kills are sorted by tick.
- **Surrender during freeze time:** set `freeze_end_tick = end_tick`.
- **Kills during freeze time (needs a decision):** Nuke has 2 bot deaths during tactical-pause freeze time. The round window starts at freeze end, so their kill frame clamps to 0 and the validator warns.
  - Options: accept the clamp, or start round files at the round start, which breaks `start_tick = freeze_end_tick`.
  - Proposed: accept the clamp.
- **Round file window:** `start_tick` = the round's `freeze_end_tick`. Frames run until the next round's start. Frame `i` is tick `start_tick + i·tick_rate/hz`. A kill's frame is `ceil((tick − start_tick)·hz/tick_rate)`.
- **`events[].kill`:** a 0-based index into `match.json` `kills`.
- **Event types:** `kill` only in v1. Adding bomb events needs new fields, which is a schema change.
- **Grenades:**
  - **types:** `smoke, flash, he, molotov, incendiary, decoy`.
  - **`end_frame`:** flash and HE end at detonation. Smoke ends when the cloud is gone. Molotov and incendiary end when the fire goes out. Decoy ends when it pops. Clamp to the last frame.
  - **`path`:** one `[x, y]` per frame from throw to detonation.
- **`yaw`:** integer degrees in [0, 360), with 0 = +x and counterclockwise.
- **Dead and absent players:** after death, x/y/yaw hold their death values with `hp` 0. A round file's `players` lists only those who spawned that round.
- **Files:** `NN` is at least two digits (`01.json`); the API route `/rounds/:n` stays unpadded. `hz` is read from the file, never assumed; v1 writes 64, one frame per tick (decided by the user 2026-10-02; demos carry entity updates every tick). `demo_sha256` is lowercase hex.
- **Stripping Steam ids** (an open question) would make `steam_id` nullable, which is a schema change.

### Parser CLI

- **CLI output.** The v0 scope says `demo parse match.dem > match.json`, but the CLI contract says `--out <dir>`. *Built:* the CLI contract.
- **Missing input file.** *Built:* exit 3 (`internal`). Alternative: treat it as `usage`.

### Worker and API (from the `worker/` scaffold; *Built* says what the code does today)

- **Status before upload.** *Built:* `uploaded` means "created, awaiting upload". Proposed: rename it to `pending`.
- **GET `/api/matches/:id` shape.** *Built:* `{id, status, error, map, score_a, score_b, parser_version, schema_version, created_at, parsed_at, players[], rounds[]}`, in `worker/README.md`. Proposed: copy it into the spec.
- **Kill feed source.** *Built:* `GET /api/matches/:id/match.json`, served from R2. Proposed: add it to the spec's API table.
- **Final score.** *Built:* the Worker counts round winners into `score_a`/`score_b`.
- **Upload auth.** *Built:* `Authorization: Bearer <token>` on both POSTs.
- **Caching vs reparse.** *Fixed in the viewer:* round and match.json URLs carry `?v=<parsed_at>`, so a reparse gets fresh cache entries. R2 keys are unchanged. *Still open:* a reparse that yields fewer rounds leaves stale `rounds/NN.json` behind, because the store has no delete. That's harmless, because the round list comes from D1.
- **Retries.** *Built:* `max_retries: 2` gives the spec's 3 attempts. The status returns to `queued` with the last error, and the DLQ sets `failed`.
- **Secrets.** The Worker also needs R2 S3 credentials to presign uploads. The bucket env var is `R2_BUCKET` on both sides.
- **CORS.** *Built:* the viewer is served by the same Worker, so the API needs no CORS. R2 buckets still need a CORS rule for the browser PUT.
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
- **Steam ids in public data.** *Decided by the user (2026-10-02): keep them.* They're semi-anonymous behind unlisted links. `match.json` already serves them; the summary GET omits them, which is fine.
- **Mirage constants (verified):** `pos_x` -3230, `pos_y` 1713, `scale` 5.0, on a 1024×1024 radar. Pixels: `px = (x - pos_x) / scale`, `py = (pos_y - y) / scale`.
