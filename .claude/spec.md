# demoView spec

<!-- Paste the full spec below this line. CLAUDE.md imports this file, so it is loaded into every Claude session. -->
CS2 Demo Analyzer: v0 to v1 Spec
Oct 1, 2026 · @chiemeka
Summary
A web app where you hand over a CS2 demo and get back a scoreboard and a scrubbable 2D replay of every round. The parser is written from scratch in Go. Everything else is cloud plumbing around it.
• v0 proves we can read the format. A CLI turns one demo into a correct scoreboard and kill list.
• v1 proves the pipeline. Upload a demo, it parses in the cloud, and a shareable link replays any round.
Four principles hold for both versions:
1. Prod runs off-machine. The browser uploads a file and draws a canvas. Storage, parsing, stats, and deploys all run in the cloud.
2. Precompute at parse time. The viewer only fetches finished files. A feature that needs computation becomes a new output of the parse job.
3. The schema is the contract. The parser writes it and everything else reads it, so the two halves can be built in parallel.
4. An open-source parser is the answer key. Every parser change is diffed against it on real demos.
Scope
v0 ships a local CLI and v1 ships the hosted product. Anything not listed here is out.
v0: kills from one demo
• Go CLI: demo parse match.dem > match.json
• Reads the file header and the outer message stream
• Decodes game events only: round start and end, kills, damage, bomb plant and defuse
• Resolves player ids in events to names and teams
• Outputs match metadata, a per-player scoreboard (K/D/A, ADR, headshot %), round results, and a kill list with round and tick
• A diff test compares the output with the reference parser
Done when the scoreboard matches the in-game one for a match you played, and the diff test passes on 3 different demos.
v1: watch a round in the browser
• Parser adds entity decoding: position, view direction, health, and alive state per player per tick
• Parser adds grenade throws and detonations
• Parser writes one replay file per round, downsampled to 16 Hz
• Upload goes straight to R2 through a presigned URL
• A queue and a Cloud Run service run the parse
• D1 holds matches, players, rounds, and kills
• Match page with scoreboard and round list
• 2D replay on a canvas: scrubber, play and pause, speed control, player dots with facing direction, kill markers, grenades
• One map supported
• Unlisted share links, no login
• CI deploys both the Worker and the parser container
Done when a link to a round you played opens on a phone or laptop and scrubs smoothly, and nothing ran on your machine after the upload.
Out of scope through v1
• Auto-fetching matches from Steam or FACEIT
• Accounts and private matches
• A custom rating, heatmaps, the lineup finder, win probability
• Live Game State Integration features
• More than one map
• POV demos and CS:GO-era demos (server-recorded CS2 demos only)
Architecture
Cloudflare is the front door and the storage, and Cloud Run does the parsing. The parser cannot run in a Worker, because a Worker is capped at 128 MB of memory (Workers limits).
1. The browser asks the Worker API for a match id and a presigned upload URL.
2. The browser uploads the demo straight to R2.
3. The browser confirms the upload and the Worker API enqueues a job.
4. The queue delivers the job to the consumer Worker.
5. The consumer calls the parser on Cloud Run and waits for it.
6. The parser streams the demo from R2 and writes the parsed files back.
7. The consumer writes the summary to D1 and marks the match ready.
8. The viewer reads through the Worker API, which serves the scoreboard from D1 and round files from R2.
Parser
The parser is one Go binary that reads a demo as a stream and writes files in the project schema. It runs the same way on a laptop and in Cloud Run.
CLI contract
demo parse <file.dem> --out <dir> [--events-only]
demo diff  <file.dem> --against <reference.json>
• parse writes match.json and, without --events-only, one rounds/NN.json per round
• Exit code 0 on success. On failure, a non-zero code and one JSON error line on stderr
• Reads the demo as a stream and never loads the whole file
• Targets: under 512 MB of memory and under 60 seconds for a full match on 1 vCPU
v0 stages
1. Header and frame loop. Check the file magic, then read frames: command, tick, size, payload.
2. Decompress and decode. Payloads are Snappy-compressed protobuf. Message definitions come from Valve's published .proto files.
3. Packet messages. Each packet frame holds a bitstream of inner messages. This needs a bit-level reader.
4. Game events. Read the event descriptor list once, then decode each event against it.
5. Player identity. Map the ids in events to names, Steam ids, and teams. Budget a full day for this.
6. Match logic. Drop warmup and knife rounds, track halves and overtime, and cap damage at the victim's remaining health for ADR.
v1 stages
1. Class info and serializers. Build the field layout for each entity class.
2. String tables and baselines. Entities start from a baseline state stored in a string table.
3. Entity updates. Decode create, update, and delete operations, including field paths and per-type field decoders.
4. Extraction. Keep only what the viewer needs: player position, view direction, health, alive state, team, and grenade projectiles.
5. Round files. Downsample to 16 Hz and write one file per round.
Stage 3 is the hardest part of the project. Everything else in v1 can be built before it is finished.
Validation
• The reference is an open-source parser such as demoinfocs-golang or demoparser. It is a test oracle and never a dependency of our parser.
• demo diff compares our output with the reference output for the same demo.
• Events must match exactly on tick, attacker, victim, and weapon.
• Positions must match within 1 game unit.
• The fixture set is at least 3 demos: one matchmaking, one FACEIT or pro, one with overtime.
• Fixture demos live in a storage bucket, not in git.
• CI runs the diff on every parser change.
Data model
The parser writes two kinds of JSON file. D1 holds a queryable copy of the summaries and R2 holds everything else.
match.json
One per match. v0 produces this file and nothing else.
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
• Teams are A and B for the whole match, because sides swap at half. Each round records which side team A played.
• Players are referenced by idx everywhere else.
rounds/NN.json
One per round, added in v1. Values are stored as columns so the viewer can index a frame directly.
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
• Positions are integers in game units. hp of 0 means dead.
• A long round is about 1,900 frames, so a file is a few hundred KB before compression.
D1 tables
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
R2 layout
demos/{match_id}.dem
parsed/{match_id}/match.json
parsed/{match_id}/rounds/{NN}.json
Versioning
• schema_version goes up on any breaking change to either file.
• parser_version is stored per match, so a cron job can find and reparse matches from older parsers.
• Raw demos are kept so a reparse never needs a new upload.
API
The Worker exposes four public routes and Cloud Run exposes one private route. Viewing is open to anyone with the link, and uploading needs a token.
Worker routes
Method
Path
What it does
POST
/api/matches
Creates a match row and returns its id and a presigned R2 upload URL. Requires the upload token.
POST
/api/matches/:id/uploaded
Confirms the object exists, rejects files over 1 GB, sets status to queued, and enqueues the job.
GET
/api/matches/:id
Returns status, scoreboard, and round list from D1.
GET
/api/matches/:id/rounds/:n
Streams the round file from R2 with long-lived cache headers.
• The upload token is one shared secret held by the two of you. It stops strangers from filling the bucket.
• The viewer polls GET /api/matches/:id every few seconds until status is ready or failed.
Queue consumer
1. Receives { match_id } and sets status to parsing.
2. Calls Cloud Run and waits for the response.
3. On 200, writes players, rounds, and kills to D1 and sets status to ready.
4. On 422, sets status to failed with the error and does not retry.
5. On any other failure, lets the queue retry. After 3 attempts the message goes to a dead letter queue and status becomes failed.
Cloud Run route
POST /parse
X-Parse-Secret: <shared secret>
{ "match_id": "...", "demo_key": "demos/....dem" }

200 -> match.json body (round files already written to R2)
422 -> { "error": "..." }   the demo is bad, do not retry
5xx -> transient, retry
• The service streams the demo from R2 over the S3 API, parses it, and writes parsed/{match_id}/... back to R2.
• Parsing is idempotent. A retry overwrites the same keys.
• The service never touches D1. The Worker owns the database.
• Settings to start: 2 GB memory, 1 vCPU, 10 minute timeout, concurrency 1, minimum instances 0.
Viewer
The viewer is a static TypeScript single-page app served from Cloudflare. It fetches finished files and draws them, and does no analysis of its own.
A plain Vite and Preact build is the starting point. Preact has React's component and hooks model at a fraction of the bundle size, which suits a viewer that is mostly one canvas. Next.js on Cloudflare works but adds an adapter layer this app does not need.
Pages
Route
Shows
/
Upload form: file picker, upload token field, progress bar
/m/:id
Parse status, then map, final score, scoreboard, and round list
/m/:id/r/:n
2D replay of one round
Replay behavior
• The background is the map's radar image. Three per-map constants (origin x, origin y, scale) convert game coordinates to pixels.
• Each player is a dot in team color with a name label and a wedge showing view direction.
• A dead player becomes an X at the spot where they died.
• Grenades draw their flight path, then their effect: a circle for a smoke's duration, a short pulse for a flash or HE, an area for a molotov.
• A kill feed beside the canvas stays in sync with playback.
• The scrubber marks each kill. Controls are play and pause, and 0.5x, 1x, 2x, and 4x speed.
• Space toggles play. Arrow keys step 5 seconds.
• ?t=42 in the URL opens the round at 42 seconds, so a specific moment can be shared.
Rendering
• Data is 16 Hz and the canvas draws at the display's refresh rate, interpolating between frames.
• Opening a round fetches one file. The next round is prefetched in the background.
• The canvas scales to the screen width and the scrubber works by touch.
Map choice
Pick one single-level map for v1, such as Mirage, Dust2, or Inferno. Nuke and Vertigo have two floors and need a second radar layer.
Dev and deploy
Development happens on a laptop and prod never depends on one. Every deploy goes through CI.
Repo layout
/parser   Go module: CLI, HTTP wrapper, Dockerfile
/worker   Cloudflare Worker: API, queue consumer, D1 migrations
/web      Viewer
/schema   JSON Schema for match.json and round files, plus sample files
/tools    Reference export script and diff helpers
Local development
• wrangler dev runs the Worker with D1 and Queues emulated locally.
• The parser runs in Docker from the same image that ships to Cloud Run.
• R2 uses a real dev bucket, even locally. The parser reads over the S3 API, and Wrangler's local R2 emulation is only reachable through Worker bindings.
• The viewer can be built against the sample files in /schema with no backend running.
Environments
• Two environments, dev and prod. Each has its own D1 database, R2 bucket, queue, and Cloud Run service.
• Three secrets per environment: the upload token, the parse secret, and the parser's R2 access key.
• Put the R2 bucket and the Cloud Run service in the same part of the world, for example western North America and us-west1.
CI
Trigger
Runs
Pull request
Go tests, parser diff test on fixtures, schema validation of parser output, TypeScript typecheck and tests
Merge to main
Build and push the parser image, deploy Cloud Run, apply D1 migrations, deploy the Worker, build and deploy the viewer
Observability
• The parser logs one JSON line per parse: match id, demo size, duration, peak memory, parser version, result.
• The Worker logs each status change for a match.
• Three numbers to watch: parse duration, parse failure rate, and queue backlog.
• No alerting in v1. A failed match shows its error on the match page.
Milestones
Four milestones on two parallel tracks, one for the parser and one for the platform and viewer. The schema and fixture files from M0 are what let the tracks run without blocking each other.
Milestone
Parser track
Platform track
Exit check
M0: contract (1 weekend, together)
Write schema v1. Export fixture files from 3 demos with the reference parser.
Create the repo, both cloud accounts, and the dev environment. Pick the map.
Fixture files validate against the schema.
M1: v0
Header, frames, game events, player identity, scoreboard.
Deploy the full pipeline with a stub parser that returns fixture data. CI deploys on merge.
v0 done-when is met, and a fake upload flows end to end in dev.
M2: build
Serializers, string tables, entity decoding.
Upload flow, match page, and replay viewer, all built against fixtures.
A fixture round replays correctly in prod.
M3: v1
Grenades, round files, position diff tests passing.
Swap the real parser into Cloud Run. Polish and mobile check.
v1 done-when is met.
Rough sizing is 1 to 2 weekends for M1 and 4 to 6 more for M2 and M3.
Fallback rule
If entity decoding is not passing the position diff 4 weeks after M1, ship v1 with round files generated by the reference parser. The from-scratch parser then continues as its own track and swaps in when it passes.
Working agreement
• Schema changes need both people to agree, and bump schema_version.
• Pair on entity decoding at least once, whoever owns the parser track. It is the part worth both people understanding.
• Each milestone ends with a short write-up in the README: what was built, what was hard, and what the numbers are.
Risks and open questions
The biggest risk is entity decoding taking far longer than planned, and the fallback rule exists for that. The rest are smaller.
Risk
Response
Entity decoding stalls
Fallback rule under Milestones. v1 ships on reference data.
A game update changes the demo format
Pin the .proto version, fail with a clear error on unknown messages, and add a recent demo to the fixtures after each major update.
Long demos exceed memory or the timeout
Stream the file, measure peak memory in M1, and raise Cloud Run limits if needed.
Cloud plumbing eats the time meant for the parser
The M1 platform track stops at a stub pipeline. No new infrastructure until the parser milestone is met.
Matchmaking demos stop being downloadable after a while
Download demos soon after playing and keep the raw files in R2.
Strangers upload junk
The upload token.


Open questions
[ ] Which map for v1? -> mirage
[ ] Which reference parser, and does its output cover every field in the schema?
[ ] Does Cloudflare Queues need the paid Workers plan on this account?
[ ] Is 16 Hz smooth enough for the replay, or should round files be 32 Hz?
[ ] Should a duplicate upload (same demo hash) reuse the existing match?
[ ] Demos include the other 8 players. Keep their Steam ids in public match data or strip them?
[ ] Is the repo public from day one?