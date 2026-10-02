# Plan

How the spec gets built, checkpoint by checkpoint. The spec says what to build. This file says in what order, and tracks status. Check items off as they land.

## M0: schema and fixtures

- [ ] **0.1 Spec gaps.** Decide the open items under [Spec gaps](#spec-gaps), then encode them in `schema/`.
- [ ] **0.2 Schema v1.** `schema/match.schema.json`, `schema/round.schema.json`, a validator with cross-field checks, and synthetic samples for the viewer. Exit: `npm test` in `schema/` passes.
- [ ] **0.3 Reference parser.** Pick one (a spec open question). Write the `tools/` export script that converts its output into our schema.
- [ ] **0.4 Fixture demos.** One matchmaking or Premier demo you played, one FACEIT or pro demo, and one with overtime. Keep local copies in `parser/testdata/demos/`; they move to a bucket once M2 creates R2.
- [ ] **0.5 Fixture files.** Export all 3 demos with 0.3. Exit (M0): the fixtures validate against the schema.

## M1: v0 parser

Each checkpoint maps to a v0 stage in the spec and ends with a check against real demos.

- [x] **1.1 CLI skeleton.** `demo parse <file> --out <dir> [--events-only]`, plus a dev-only `demo inspect <file>`. Flags may follow the file. On failure: one JSON line on stderr, `{"error": "...", "code": "..."}`, with exit 1 / `bad_demo`, 2 / `usage` or 3 / `internal`. Exit 1 maps to the Cloud Run 422 and exit 3 to a 5xx.
- [ ] **1.2 Frame loop** (stage 1). Exit: `demo inspect` walks all 3 fixture demos to EOF with no error. *Code done and tested on synthetic demos; waiting on real demos.*
- [ ] **1.3 Decompress and decode** (stage 2). Snappy, plus Valve's protos vendored and pinned in `parser/pb`. Exit: `inspect` prints the map name and playback ticks from FileHeader and FileInfo, and they match the demo. *Protos done (`parser/pb`, game build 2000922); decode in progress.*
- [ ] **1.4 Packet messages** (stage 3). `parser/bitread`, plus the inner message loop. Exit: every packet is consumed to its last bit on all fixtures, and `inspect` prints message counts by type. *`bitread` done; message loop in progress.*
- [ ] **1.5 Game events** (stage 4). Exit: the `player_death` count matches the reference kill count before any filtering.
- [ ] **1.6 Player identity** (stage 5). Exit: every kill's attacker, victim and assister resolve to a name, Steam id and team.
- [ ] **1.7 Match logic** (stage 6), and the full `match.json`. Exit: the scoreboard matches the in-game one for a match you played.
- [ ] **1.8 `demo diff` and CI.** Exit: the diff passes on all 3 fixtures. This meets the v0 done-when and closes M1.

Checkpoints 1.1 to 1.4 don't depend on M0, so they run in parallel with it.

## Spec gaps

Things the spec leaves open that the code has to decide. Each one has a proposed default. Once a gap is decided, encode it in `schema/` or the parser and delete it here.

### v0 scope: needs a decision

Research on real 2024–2026 demos found that **`round_start` and `round_end` stopped being fired around the Feb 2024 update**, and so did `round_mvp` and `cs_win_panel_round`. Their descriptors are still in the event list, so the list doesn't show it. Both reference parsers rebuild these events from `CCSGameRulesProxy` entity props.

- **Events alone give:** kills, damage, bomb plant/defuse/explode, `round_freeze_end`, `round_officially_ended` (none for the last round; use `cs_win_panel_match`), match start (the last `begin_new_match`), halftime (`announce_phase_end`) and `player_team` changes.
- **Events alone can't give reliably:**
  - round winner and reason (inferable from bomb events, eliminations and time, but not surrenders);
  - the warmup flag and knife rounds;
  - teams (`player_team` only fires on a change, and a Valve MM demo had none).
- **Proposed:**
  - Keep v0 events-only and infer the rest:
    - winner and reason from bomb events, eliminations and the round timer;
    - teams from `player_team`, plus `CS_UM_EndOfMatchAllPlayersData` when present, plus the halftime swap.
  - The diff test catches mismatches. Exact values arrive with entity decoding (game rules proxy and `m_iTeamNum` on controllers), which goes at the front of M4.
  - Pulling entity decoding into M1 isn't "minimal". Reading even a few props needs serializers, field paths and decoders, which would turn M1 into M4.
- **ADR with events only** comes out up to 1% below the in-game value. That's after capping `player_hurt.dmg_health` at the HP before the hit; `dmg_health` itself is uncapped. The exact value is the controller's `m_iDamage` counter, an entity prop. Proposed: allow ±1% on ADR in v0, and make it exact in M4.

### Scoreboard rules (verified against the game's own counters on 2 real demos)

These go into the schema docs and `match` logic.

- **Kills:** enemy kills only. Suicides and world deaths don't subtract. Team kills are unverified.
- **Deaths:** every `player_death` victim between match start and `cs_win_panel_match`, including suicides.
- **Assists:** the event's `assister` when on the other team from the victim. Flash assists count. Don't apply a damage threshold.
- **HS%:** headshot kills / kills × 100, so `hs_pct` is on a 0–100 scale.
- **Ticks:** use the demo-frame tick clock, not `net_Tick`, which is offset.

### Parser

- **CLI output.** The v0 scope says `demo parse match.dem > match.json`, but the CLI contract says `--out <dir>`. *Built:* the CLI contract.
- **Missing input file.** *Built:* exit 3 (`internal`), since the contract only defines "anything else". Alternative: treat it as `usage`.

### Worker and API

These are from the `worker/` scaffold. The *Built* note says what the code does today.

- **Status before upload.** The match row exists before the upload, but the first listed status is `uploaded`. *Built:* `uploaded` means "created, awaiting upload". Proposed: rename it to `pending`.
- **GET `/api/matches/:id` response shape.** The spec doesn't define it. *Built:* `{id, status, error, map, score_a, score_b, parser_version, schema_version, created_at, parsed_at, players[], rounds[]}`, documented in `worker/README.md`. Proposed: copy it into the spec, since `web/` codes against it.
- **Kill feed source.** Round files point at kills by index (`"kill": 12`), but D1 `kills` has no assister and no stable index, and GET returns only the scoreboard and rounds. Proposed: add `GET /api/matches/:id/match.json`, served from R2.
- **Final score.** `match.json` has no score field. *Built:* the Worker counts round winners into `score_a`/`score_b`. Alternative: the parser emits the score.
- **Upload auth.** *Built:* the token is required on both POSTs (the spec names only `POST /api/matches`), sent as `Authorization: Bearer <token>`.
- **Caching vs reparse.** Round files are cached `immutable` for a year, but a reparse overwrites the same R2 keys. Proposed: versioned keys (`parsed/{id}/v{parser_version}/...`) or `?v=` on round URLs.
- **Retries.** *Built:* `max_retries: 2` gives the spec's 3 attempts. While a retry is pending, the status goes back to `queued` with the last error. The DLQ consumer then sets `failed`.
- **Secrets.** Presigning uploads also needs R2 S3 credentials in the Worker (`R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`), beyond the spec's three secrets.
- **CORS.** Each R2 bucket needs a CORS rule for the browser PUT. API CORS depends on whether the viewer is served by the same Worker (static assets) or by Pages, which the spec doesn't say. Proposed: the same Worker, so no API CORS is needed.
- **Long subrequest.** The consumer waits up to 11 minutes for Cloud Run. M2 must confirm a multi-minute subrequest isn't cut off.
- **Small items.** Timestamps are Unix ms. 1 GB means 1024³ bytes. CI passes an explicit `--env` to `wrangler deploy`.

### Open questions (from the spec)

- **Reference parser.** Proposed: **demoinfocs-golang v5.2.0, pinned**, as the primary oracle. It's actively maintained and patched for every breaking CS2 update, and exposes every schema field, including grenade paths and smoke and molotov expiry. It lives in `tools/oracle/` with its own `go.mod`, so `parser/` can't import it. demoparser2 0.42.0 is a second opinion when the two disagree.
  - Watch out for these quirks when diffing:
    - demoparser2 duplicates `round_officially_ended`.
    - demoinfocs `InfernoExpired` is not the flame-out.
    - demoinfocs v6 changes the damage and health semantics.
- **Map constants (verified).** Mirage: `pos_x` -3230, `pos_y` 1713, `scale` 5.0, on a 1024×1024 radar. Pixels: `px = (x - pos_x) / scale`, `py = (pos_y - y) / scale`.

- **Queues pricing.** Cloudflare's limits page lists Queues on Workers Free, with 24 h retention. D1 Free allows 50 queries per invocation, so the consumer writes each table in one statement. This likely answers the spec question "no paid plan needed"; confirm on the account in M2.
- **Steam ids.** *Built:* stored in D1 but left out of the public GET until this is decided.
