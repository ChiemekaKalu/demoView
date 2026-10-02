# CS2 Demo Analyzer: v0 to v1 Spec

Oct 1, 2026 · @chiemeka · Updated Oct 2, 2026 for a solo build

## Summary

A web app where you hand over a CS2 demo and get back a scoreboard and a scrubbable 2D replay of every round. The parser is written from scratch in Go. Everything else is cloud plumbing around it.

- **v0 proves we can read the format.** A CLI turns one demo into a correct scoreboard and kill list.
- **v1 proves the pipeline.** Upload a demo, it parses in the cloud, and a shareable link replays any round.

Four principles hold for both versions:

1. **Prod runs off-machine.** The browser uploads a file and draws a canvas. Storage, parsing, stats, and deploys all run in the cloud.
2. **Precompute at parse time.** The viewer only fetches finished files. A feature that needs computation becomes a new output of the parse job.
3. **The schema is the contract.** The parser writes it and everything else reads it, so the pipeline and viewer can be built against fixture files before the parser is done.
4. **An open-source parser is the answer key.** Every parser change is diffed against it on real demos.

## Scope

v0 ships a local CLI and v1 ships the hosted product. Anything not listed here is out.

### v0: kills from one demo

- Go CLI: `demo parse match.dem > match.json`
- Reads the file header and the outer message stream
- Decodes game events only: round start and end, kills, damage, bomb plant and defuse
- Resolves player ids in events to names and teams
- Outputs match metadata, a per-player scoreboard (K/D/A, ADR, headshot %), round results, and a kill list with round and tick
- A diff test compares the output with the reference parser

**Done when** the scoreboard matches the in-game one for a match you played, and the diff test passes on 3 different demos.

### v1: watch a round in the browser

- Parser adds entity decoding: position, view direction, health, and alive state per player per tick
- Parser adds grenade throws and detonations
- Parser writes one replay file per round, at 64 Hz (every tick)
- Upload goes straight to R2 through a presigned URL
- A queue and a Cloudflare Container run the parse
- D1 holds matches, players, rounds, and kills
- Match page with scoreboard and round list
- 2D replay on a canvas: scrubber, play and pause, speed control, player dots with facing direction, kill markers, grenades
- One map supported
- Unlisted share links, no login
- CI deploys both the Worker and the parser container

**Done when** a link to a round you played opens on a desktop browser and scrubs smoothly, and nothing ran on your machine after the upload.

### Out of scope through v1

- Auto-fetching matches from Steam or FACEIT
- Accounts and private matches
- A custom rating, heatmaps, the lineup finder, win probability
- Live Game State Integration features
- More than one map
- POV demos and CS:GO-era demos (server-recorded CS2 demos only)

### v1.1: every map you play, and a pipeline that runs itself

v1.1 changes no `match.json` or round-file schema, so `schema_version` stays 1. It changes the R2 layout and adds D1 columns, which are spec changes, not schema-file changes.

- **More maps:**
  - Every single-level map with a radar under `maps/` in R2. The bar is the single-level maps in the local demos: Mirage, Ancient, Inferno and Overpass.
  - The viewer reads `pos_x`, `pos_y` and `scale` from `maps/radar_info/*` through `GET /api/maps/:name`, which serves the private R2 objects with long-lived cache headers.
  - A map without a radar still replays on a plain background, with a "no radar for this map" notice.
  - A radar-alignment check in `tools/` reports the share of alive player frames that land on a non-transparent radar pixel.
    - It runs locally and on dev, not in CI, because the radars are unlicensed private objects.
    - The pass bar is 97.5% per demo, set from the measured Mirage and Ancient baselines. Some radars hug the walls, and jump spots and boosts can sit off the radar.
- **Duplicate upload:**
  - On confirm, the Worker streams the R2 object through `crypto.DigestStream` and stores `demo_sha256` on the row. D1 indexes that column.
  - A hash that matches any match that hasn't failed returns that match's id, starts no parse, and deletes the duplicate object. Two quick uploads of the same demo never both parse.
- **Reparse, as the Versioning section promises:**
  - `POST /api/matches/:id/reparse`, behind the upload token.
  - The Worker knows the deployed parser build, injected at deploy from the same source as the image tag.
  - A cron requeues every match whose last *attempted* build (a D1 column) is older than the deployed one. So each match gets one try per build, and a failing match can't block the rest.
    - It keys on the build, never on `match.json`'s `parser_version`, which can be an oracle version.
    - It runs hourly, 5 matches a batch, and pauses at 80% of the monthly container allowance.
    - One `basic` instance at about 30 s a parse makes a batch about 3 minutes, so a new upload waits behind at most one batch.
  - A failed reparse never takes a match off `ready`. The match keeps its live parse and status. The failure goes into its own columns (`reparse_error`, `reparse_build`).
  - The sweeper and the dead-letter handler only touch matches that have never been `ready`.
- **Safe swap:**
  - A parse writes under `parsed/{id}/{parse_id}/` and writes `manifest.json` (counts and sha256s) last.
  - D1 moves its `live_parse_id` pointer only after that.
    - The swap is conditional (`UPDATE … WHERE live_parse_id = <the parse this one started from>`), so a race can't swap an older parse over a newer one.
    - All writes on success go in one D1 batch.
  - URLs pick the parse:
    - `/rounds/:n?v=<parse_id>`, and the same for `match.json`, serves that parse's prefix with an immutable cache header.
    - Without `v`, the route serves the live pointer with a short cache.
    - When a pruned version returns 404, the viewer refetches the summary once.
  - Cleanup deletes parses D1 no longer points to once they are 7 days old.
    - v1 files sit directly under `parsed/{id}/`, the parent of the new prefixes. Cleanup deletes those by exact key and never lists or deletes the whole `parsed/{id}/` prefix.
  - This fixes the stale `rounds/NN.json` that a reparse with fewer rounds leaves behind.
- **A pipeline that recovers by itself:**
  - A sweeper requeues or fails matches that have sat in `queued` or `parsing` for more than 30 minutes since their last status change. It uses a new `status_changed_at` column.
  - D1 `error` keeps the parser's error code, and the match page shows a message for each code.
  - The viewer shows a clear message for an unknown `schema_version`.
  - Uploads have a per-token rate limit, and an R2 lifecycle rule removes orphaned demos.
- **Trust gates for our own round files:**
  - The CI diff job runs and gates deploys.
  - Fuzz targets cover `bitread`, the frame loop, field paths and string tables. Corpora are generated at test time, so no demo bytes are committed.
  - Two parses in separate processes are byte-identical.
  - A `GOMAXPROCS=1` budget check on the largest fixture: under 512 MB and under 60 s.
  - A stream that ends without `DEM_Stop` is `bad_demo` ("truncated: no Stop frame").
  - An unknown class, field or decoder is a coded error in the entity stage only.
    - `match.json`, which comes from events, still ships.
    - Round files fall back to the oracle while the oracle is in the image.
    - So a CS2 update can degrade round files without taking parses down.
  - A dev-only shadow compare checks our round files against the oracle.
- **Narrative log:** v1.1 lands only the groundwork from M4, the `parser/eventlog` package and its tests. No v1.1 exit check depends on it. The log output, its schema and the story pane wait for v1.2.
- **Steam ids** stay public through `match.json`. `GET /api/matches/:id` keeps its current shape.
- The only input is still demo files. Nothing touches the game client, its memory or its processes.

**Done when** all four hold:
- A match you played on Ancient or Inferno replays on its own radar.
- Re-uploading a demo returns the same link.
- A parser build merged to `main` reparses every prod match with nothing run by hand and no link breaking.
- Our own round files have passed the trust gates.

### v1.2, decided

- The Steam/Valve auto-fetch. The design is in `plan.md`.
- Two-floor maps (Nuke, Vertigo), with a `z` or level column and a layer toggle. This is a `schema_version` bump.
- The narrative log's output, schema, derived events (opening kills, trades, clutches, buy types) and story pane, built on the M4 groundwork.
- Match Pulse: a calibrated win-probability line under each round's scrubber, with kill marks sized by swing and a "rounds worth watching" shelf. It comes from the Jev model on Workers AI (`.claude/ideas/jev/spec.md`) and runs on its own queue after a match is ready, writing a new `pulse.json` with its own schema. A self-grading loop scores every match against known outcomes, and a CI eval job tracks quality and drift.

### Out of scope through v1.1

- Any `match.json` or round-file schema change, or a `schema_version` bump
- Accounts, private matches, a match list, search and filters
- A custom rating, win probability, heatmaps, the lineup finder, and callout or zone labels
- External alerting, FACEIT or other sources, live Game State Integration, and anything that runs beside or touches the game client
- Mobile layout work (v1 is desktop-first)

## Architecture

Cloudflare runs everything: the Worker is the front door, R2 and D1 hold the data, and a Cloudflare Container does the parsing. The parser cannot run in a Worker, because a Worker is capped at 128 MB of memory ([Workers limits](https://developers.cloudflare.com/workers/platform/limits/)).

1. The browser asks the Worker API for a match id and a presigned upload URL.
2. The browser uploads the demo straight to R2.
3. The browser confirms the upload and the Worker API enqueues a job.
4. The queue delivers the job to the consumer Worker.
5. The consumer calls the parser container through its Durable Object and waits for it.
6. The parser streams the demo from R2 and writes the parsed files back.
7. The consumer writes the summary to D1 and marks the match ready.
8. The viewer reads through the Worker API, which serves the scoreboard from D1 and round files from R2.

## Parser

The parser is one Go binary that reads a demo as a stream and writes files in the project schema. It runs the same way on a laptop and in a Cloudflare Container.

### CLI contract

```sh
demo parse <file.dem> --out <dir> [--events-only]
demo diff  <file.dem> --against <reference.json>
```

- `parse` writes `match.json` and, without `--events-only`, one `rounds/NN.json` per round
- Exit code 0 on success. On failure, a non-zero code and one JSON error line on stderr
- Reads the demo as a stream and never loads the whole file
- Targets: under 512 MB of memory and under 60 seconds for a full match on 1 vCPU

### v0 stages

1. **Header and frame loop.** Check the file magic, then read frames: command, tick, size, payload.
2. **Decompress and decode.** Payloads are Snappy-compressed protobuf. Message definitions come from Valve's published `.proto` files.
3. **Packet messages.** Each packet frame holds a bitstream of inner messages. This needs a bit-level reader.
4. **Game events.** Read the event descriptor list once, then decode each event against it.
5. **Player identity.** Map the ids in events to names, Steam ids, and teams. Budget a full day for this.
6. **Match logic.** Drop warmup and knife rounds, track halves and overtime, and cap damage at the victim's remaining health for ADR.

### v1 stages

1. **Class info and serializers.** Build the field layout for each entity class.
2. **String tables and baselines.** Entities start from a baseline state stored in a string table.
3. **Entity updates.** Decode create, update, and delete operations, including field paths and per-type field decoders.
4. **Extraction.** Keep only what the viewer needs: player position, view direction, health, alive state, team, and grenade projectiles.
5. **Round files.** Sample every tick (64 Hz) and write one file per round.

Stage 3 is the hardest part of the project. Everything else in v1 can be built before it is finished.

### Validation

- The reference is an open-source parser such as demoinfocs-golang or demoparser. It is a test oracle and never a dependency of our parser.
- `demo diff` compares our output with the reference output for the same demo.
- Events must match exactly on tick, attacker, victim, and weapon.
- Positions must match within 1 game unit.
- The fixture set is at least 3 demos: one matchmaking, one FACEIT or pro, one with overtime.
- Fixture demos live in a storage bucket, not in git.
- CI runs the diff on every parser change.

## Data model

The parser writes two kinds of JSON file. D1 holds a queryable copy of the summaries and R2 holds everything else.

### `match.json`

One per match. v0 produces this file and nothing else.

```json
{
  "schema_version": 1,
  "parser_version": "0.1.0",
  "match": { "map": "de_mirage", "tick_rate": 64, "demo_sha256": "..." },
  "players": [
    { "idx": 0, "steam_id": "7656...", "name": "...", "team": "A" }
  ],
  "rounds": [
    { "n": 1, "start_tick": 0, "freeze_end_tick": 0, "end_tick": 0,
      "winner": "A", "reason": "bomb_defused", "side_a": "CT" }
  ],
  "kills": [
    { "round": 1, "tick": 0, "attacker": 0, "victim": 5, "assister": null,
      "weapon": "ak47", "headshot": true }
  ],
  "scoreboard": [
    { "player": 0, "kills": 0, "deaths": 0, "assists": 0, "adr": 0.0, "hs_pct": 0.0 }
  ]
}
```

- Teams are `A` and `B` for the whole match, because sides swap at half. Each round records which side team A played.
- Players are referenced by `idx` everywhere else.

### `rounds/NN.json`

One per round, added in v1. Values are stored as columns so the viewer can index a frame directly.

```json
{
  "schema_version": 1,
  "round": 7,
  "hz": 16,
  "start_tick": 0,
  "frames": 1800,
  "players": [
    { "idx": 0, "x": [], "y": [], "yaw": [], "hp": [] }
  ],
  "grenades": [
    { "type": "smoke", "thrower": 3, "throw_frame": 120,
      "detonate_frame": 180, "end_frame": 470, "path": [[0, 0]] }
  ],
  "events": [
    { "frame": 210, "type": "kill", "kill": 12 }
  ]
}
```

- Positions are integers in game units. `hp` of 0 means dead.
- A long round is about 7,600 frames at 64 Hz, so a file is about 2 MB before compression and a few hundred KB after.

### D1 tables

```sql
CREATE TABLE matches (
  id TEXT PRIMARY KEY,          -- random slug, used in the share link
  status TEXT NOT NULL,         -- uploaded | queued | parsing | ready | failed
  map TEXT, score_a INTEGER, score_b INTEGER,
  demo_key TEXT NOT NULL, demo_sha256 TEXT,
  parser_version TEXT, schema_version INTEGER,
  error TEXT, created_at INTEGER NOT NULL, parsed_at INTEGER
);
CREATE TABLE players (
  match_id TEXT, idx INTEGER, steam_id TEXT, name TEXT, team TEXT,
  kills INTEGER, deaths INTEGER, assists INTEGER, adr REAL, hs_pct REAL,
  PRIMARY KEY (match_id, idx)
);
CREATE TABLE rounds (
  match_id TEXT, n INTEGER, winner TEXT, reason TEXT, side_a TEXT,
  PRIMARY KEY (match_id, n)
);
CREATE TABLE kills (
  match_id TEXT, round INTEGER, tick INTEGER,
  attacker INTEGER, victim INTEGER, weapon TEXT, headshot INTEGER
);
```

### R2 layout

```text
demos/{match_id}.dem
parsed/{match_id}/match.json
parsed/{match_id}/rounds/{NN}.json
```

From v1.1, each parse writes under its own prefix and D1 points at the live one, so a reparse never overwrites what a viewer is reading:

```text
parsed/{match_id}/{parse_id}/manifest.json
parsed/{match_id}/{parse_id}/match.json
parsed/{match_id}/{parse_id}/rounds/{NN}.json
```

### Versioning

- `schema_version` goes up on any breaking change to either file.
- `parser_version` is stored per match, so a cron job can find and reparse matches from older parsers.
- Raw demos are kept so a reparse never needs a new upload.

## API

The Worker exposes four public routes, and the parser container exposes one private route that only the Worker can reach. Viewing is open to anyone with the link, and uploading needs a token.

### Worker routes

| Method | Path | What it does |
| ------ | ---- | ------------ |
| `POST` | `/api/matches` | Creates a match row and returns its id and a presigned R2 upload URL. Requires the upload token. |
| `POST` | `/api/matches/:id/uploaded` | Confirms the object exists, rejects files over 1 GB, sets status to `queued`, and enqueues the job. |
| `GET` | `/api/matches/:id` | Returns status, scoreboard, and round list from D1. |
| `GET` | `/api/matches/:id/rounds/:n` | Streams the round file from R2 with long-lived cache headers. |

- The upload token is one secret, held only by you. It stops strangers from filling the bucket.
- The viewer polls `GET /api/matches/:id` every few seconds until status is `ready` or `failed`.

### Queue consumer

1. Receives `{ match_id }` and sets status to `parsing`.
2. Calls the parser container and waits for the response.
3. On 200, writes players, rounds, and kills to D1 and sets status to `ready`.
4. On 422, sets status to `failed` with the error and does not retry.
5. On any other failure, lets the queue retry. After 3 attempts the message goes to a dead letter queue and status becomes `failed`.

### Parser container route

```http
POST /parse
X-Parse-Secret: <shared secret>

{ "match_id": "...", "demo_key": "demos/....dem" }
```

| Status | Body | Meaning |
| ------ | ---- | ------- |
| 200 | `match.json` | Success. Round files are already written to R2. |
| 422 | `{ "error": "..." }` | The demo is bad. Do not retry. |
| 5xx | | Transient. Retry. |

- The service streams the demo from R2 over the S3 API, parses it, and writes `parsed/{match_id}/...` back to R2.
- Parsing is idempotent. A retry overwrites the same keys.
- The service never touches D1. The Worker owns the database.
- Settings: the `basic` instance (1/4 vCPU, 1 GiB), one parse at a time (queue `max_concurrency` 1), a short `sleepAfter` so it scales to zero. A parse takes seconds.

## Viewer

The viewer is a static TypeScript single-page app served from Cloudflare. It fetches finished files and draws them, and does no analysis of its own.

A plain Vite and Preact build is the starting point. Preact has React's component and hooks model at a fraction of the bundle size, which suits a viewer that is mostly one canvas. Next.js on Cloudflare works but adds an adapter layer this app does not need.

### Pages

| Route | Shows |
| ----- | ----- |
| `/` | Upload form: file picker, upload token field, progress bar |
| `/m/:id` | Parse status, then map, final score, scoreboard, and round list |
| `/m/:id/r/:n` | 2D replay of one round |

### Replay behavior

- The background is the map's radar image. Three per-map constants (origin x, origin y, scale) convert game coordinates to pixels.
- Each player is a dot in team color with a name label and a wedge showing view direction.
- A dead player becomes an X at the spot where they died.
- Grenades draw their flight path, then their effect: a circle for a smoke's duration, a short pulse for a flash or HE, an area for a molotov.
- A kill feed beside the canvas stays in sync with playback.
- The scrubber marks each kill. Controls are play and pause, and 0.5x, 1x, 2x, and 4x speed.
- Space toggles play. Arrow keys step 5 seconds.
- `?t=42` in the URL opens the round at 42 seconds, so a specific moment can be shared.

### Rendering

- Data is 64 Hz (every tick) and the canvas draws at the display's refresh rate, interpolating between frames.
- Opening a round fetches one file. The next round is prefetched in the background.
- The viewer targets desktop browsers. The canvas scales to the window width, and a phone is not a requirement.

### Map choice

Pick one single-level map for v1, such as Mirage, Dust2, or Inferno. Nuke and Vertigo have two floors and need a second radar layer.

## Dev and deploy

Development happens on a laptop and prod never depends on one. Every deploy goes through CI.

### Repo layout

```text
/parser   Go module: CLI, HTTP wrapper, Dockerfile
/worker   Cloudflare Worker: API, queue consumer, D1 migrations
/web      Viewer
/schema   JSON Schema for match.json and round files, plus sample files
/tools    Reference export script and diff helpers
```

### Local development

- `wrangler dev` runs the Worker with D1 and Queues emulated locally.
- The parser runs in Docker from the same image that ships as the container.
- R2 uses a real dev bucket, even locally. The parser reads over the S3 API, and Wrangler's local R2 emulation is only reachable through Worker bindings.
- The viewer can be built against the sample files in `/schema` with no backend running.

### Environments

- Two environments, dev and prod. Each has its own D1 database, R2 bucket, queue, Worker and parser container.
- Three secrets per environment: the upload token, the parse secret, and the parser's R2 access key.
- The R2 buckets use the western North America location hint.

### CI

| Trigger | Runs |
| ------- | ---- |
| Pull request | Go tests, parser diff test on fixtures, schema validation of parser output, TypeScript typecheck and tests |
| Merge to `main` | Build the parser image, apply D1 migrations, then deploy the Worker with its viewer and parser container, dev first and then prod |

### Observability

- The parser logs one JSON line per parse: match id, demo size, duration, peak memory, parser version, result.
- The Worker logs each status change for a match.
- Three numbers to watch: parse duration, parse failure rate, and queue backlog.
- No alerting in v1. A failed match shows its error on the match page.

## Milestones

Five milestones in one sequence. The schema and fixture files from M0 let the pipeline and viewer be built before entity decoding exists.

| Milestone | Work | Exit check |
| --------- | ---- | ---------- |
| **M0: schema and fixtures** | Write schema v1. Export fixture files from 3 demos with the reference parser. Create the repo and pick the map. | Fixture files validate against the schema. |
| **M1: v0 parser** | Header, frames, game events, player identity, scoreboard. | v0 done-when is met. |
| **M2: cloud pipeline** | Create both cloud accounts and the dev environment. Deploy the full pipeline with a stub parser that returns fixture data. CI deploys on merge. | A fake upload flows end to end in dev, and a merge to `main` deploys it. |
| **M3: viewer** | Upload flow, match page, and replay viewer, all built against fixtures. Swap the stub for real parsing: your v0 parser writes `match.json` and the reference parser writes round files. Polish and a desktop browser check. | v1 done-when is met, with round files from the reference parser. |
| **M4: entity decoding** | Serializers, string tables, entity decoding, grenades, round files, position diff tests passing. Swap your parser in for round files. | The position diff passes on all fixtures, and prod round files come from your parser. |
| **M5: every map and safe reparse** | The maps route, the viewer's map loader and its no-radar notice, and the alignment tool in `tools/`, run locally and on dev. Duplicate reuse, hashing on confirm. The reparse route and cron, keyed on the last attempted build. A versioned prefix with a manifest, a conditional D1 pointer swap and versioned URLs. Batch D1 writes, a failed reparse that keeps the match `ready`, cleanup by exact key, the stuck-match sweeper on `status_changed_at`, coded errors, and the unknown-`schema_version` message. Needs M3, not M4. | Every single-level local demo scores at least 97.5% on the alignment check. Re-uploading a demo returns its match id and starts no parse, even when both uploads land within seconds. On dev, a parser build merged to `main` ends every match at the new build with nothing run by hand. A poller reading the match and its round files throughout sees no 404 and no mix of versions. A reparse with fewer rounds leaves no old round file reachable. A deliberately failing build leaves every match `ready` on its old parse. The same reparse then runs on prod inside the monthly container allowance. |
| **M6: trusted and hardened** | The CI diff required before deploy, fuzz targets, run-twice byte identity, and the `GOMAXPROCS=1` budget check. The no-`DEM_Stop` truncation rule, and an entity-stage unknown-field error with oracle fallback. A dev-only shadow compare once M4 swaps in our round files. The upload rate limit, the orphaned-demo lifecycle rule, and stuck-match recovery tested under a dev-only fault flag that deploy config refuses in prod. | Each fuzz target runs 10 minutes with no crash. Truncating each public fixture at 1,000 evenly spaced offsets always exits 1 with `bad_demo`, with no panic and no OOM. Two parses are byte-identical on all local demos. A synthetic unknown field gives a coded entity error, `match.json` still ships, and `round_source=oracle` is logged. The largest fixture stays under 512 MB and 60 s at `GOMAXPROCS=1`. The shadow compare reports zero round-file mismatches on dev. With the fault flag on, 20 uploads each end `ready` or `failed` with a coded error, and none sits in `queued` or `parsing` for 30 minutes past its last status change. |

Rough sizing, for one person driving coding agents: M0 takes a day, M1 a weekend, M2 a weekend, and M3 one to two weekends. That is about 3 to 4 weekends to v1 on reference data. M4 adds 2 to 3 more. For v1.1, M5 takes about 1.5 weekends and M6 about 1. M5 doesn't wait for M4, but M6's shadow compare does.

### Fallback rule

v1 ships with round files generated by the reference parser. This is the default path, not a fallback. Your from-scratch entity decoding is an upgrade in M4, and it swaps in when it passes the position diff.

## Risks and open questions

The biggest risk is entity decoding taking far longer than planned, and the fallback rule exists for that. The rest are smaller.

| Risk | Response |
| ---- | -------- |
| Entity decoding stalls | v1 already ships on reference data (Fallback rule). Your parser swaps in only when it passes. |
| Solo project loses momentum during entity decoding | M2 and M3 come before M4, so a working, shareable v1 exists before the hardest part starts. |
| A game update changes the demo format | Pin the `.proto` version, fail with a clear error on unknown messages, and add a recent demo to the fixtures after each major update. |
| Long demos exceed memory or the timeout | Stream the file, measure peak memory in M1, and raise the container instance size if needed. |
| Cloud plumbing eats the time meant for the parser | M1 finishes the v0 parser before any cloud work starts. M2 stops at a pipeline that runs on fixture data. |
| Matchmaking demos stop being downloadable after a while | Download demos soon after playing and keep the raw files in R2. |
| Strangers upload junk | The upload token. |

### Open questions

- [x] Which map for v1? Mirage.
- [ ] Which reference parser, and does its output cover every field in the schema?
- [ ] Does Cloudflare Queues need the paid Workers plan on this account?
- [x] Is 16 Hz smooth enough for the replay, or should round files be 32 Hz? 64 Hz, every tick: both fixture demos record entity updates on every tick, and the viewer targets desktop.
- [ ] Should a duplicate upload (same demo hash) reuse the existing match?
- [x] Demos include the other 8 players. Keep their Steam ids in public match data or strip them? Keep them: match data is semi-anonymous behind unlisted links.
- [ ] Is the repo public from day one?
