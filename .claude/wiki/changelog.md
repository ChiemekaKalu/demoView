# Changelog

What moved, newest first. This is a digest, so `git log` has the full detail. Each entry says which part of the wiki it touches.

## 2026-10-02, late morning: a local off switch

- `ddbb480` adds `tools/local/stop-local.ps1`, which lists and (with `-Force`) stops the local dev servers, including the learning center on port 5180. It is scheduled once for about 11:03 Pacific, so restart the site afterwards if you want it up. Wiki: [infra](infra.md#local-dev).

## 2026-10-02, midday: entities decode, and the pipeline looks after itself

- **M4 stage 3 is done** (`1abbf6d`, `d455cbf`, `f771df1`): pawn position, view angle and health agree with the oracle on 100% of frames on all 10 demos, every entity packet is consumed exactly, and the largest demo decodes in about 1.5 s. The comparison found an oracle bug, fixed in `df7dced`. Fuzzing field paths and entity updates is in (`1a5a988`), and it found two entity bugs, being fixed. Wiki: [parser](parser.md), [testing](testing.md).
- **Budget closed:** the server caps the oracle at 400 MiB (`fe09822`), and the full path measures 270 MiB at one CPU. Wiki: [parser](parser.md).
- **Pipeline self-care** (`9d5ead9`, `1df36df`, `5994791`, `22b3884`): a stuck-match sweeper, an hourly reparse batch, a daily cleanup, and per-match history in D1 shown on the match page (`ed9700b`). **Crons are on in dev only** until the exit checks pass, and CI refuses to deploy prod with the dev-only fault flag set. Wiki: [worker](worker.md), [reparse runbook](runbook-reparse.md), [stuck-match runbook](runbook-stuck-matches.md).
- **Limits:** uploads and reparses are capped at 10 a minute, and the viewer says so (`856d8c5`, `94dba79`). A reparse with fewer rounds is tested to hide the old extras (`16c8041`).
- The experiment spend cap for Match Pulse is $12.50 of the $25 loaded. Wiki: [decisions](decisions.md).

## 2026-10-02, morning: safe swap and hardening land

- **Safe swap, Worker side:** every attempt writes under its own prefix and the match goes live by one conditional pointer swap (`074f5b7`, migration `0003`). A reparse route exists (`a37fc0d`), the viewer asks for files by parse id (`fff16cd`), and CI passes the parser's build id to the deploy (`c7134a1`). The `attempted_build` column is now named. Wiki: [R2 layout](r2-layout.md), [reparse runbook](runbook-reparse.md), [worker](worker.md), [big-picture](big-picture.md).
- **M6, parser side:** demos with no Stop frame are rejected (`27de943`), the frame loop and string tables are fuzzed (`00e3150`), and truncation, run-twice, budget and per-map diff tests exist (`3c86e04`). The full path was 640 MiB over budget because the oracle held its whole timeline. Compacting its samples (`23e232c`) took it to about 270 to 320 MiB. Wiki: [parser](parser.md), [testing](testing.md).
- **Narrative log groundwork is done** (`06ca637`, `b72dfce`). Wiki: [parser](parser.md).
- **Match Pulse approved for v1.2** (`b002e11`, `158d192`): a win-probability line under each round. Wiki: [decisions](decisions.md).
- The persisting step in the tour, the quiz about commit markers and the system map's R2 entry changed to match: it's the manifest that is written last now.

## 2026-10-02, small hours: M5 starts landing

- **Maps:** the Worker serves each map's constants and radar (`091b2b5`), and the viewer loads them through the API (`c996015`). The alignment tool is in (`2c3b41d`, `f0c3e2d`), with a bar of **97.5% per demo**, which the spec adopted (`1d7ce68`): Ancient's walls-hugging radar scores about 98%, so the first-drafted 99.5% would have failed aligned demos. Wiki: [maps](maps.md), [testing](testing.md).
- **Duplicate upload:** confirm hashes the object as a stream, and a match that hasn't failed with the same demo is returned instead of parsed again (`25c96a6`, with an index in migration 0002). The web app opens the existing match (`4498e20`). Wiki: [worker](worker.md).
- **Coded errors:** the Worker stores a code with every failed match's error (`2f91766`), and the match page explains failures and unreadable data formats (`ed662e1`). Wiki: [stuck-match runbook](runbook-stuck-matches.md).
- **Parser side of safe swap** (`1a02084`): `POST /parse` takes a `parse_id`, writes under `parsed/{id}/{parse_id}/` with the manifest last, and every response and log line carries ids and codes. Wiki: [R2 layout](r2-layout.md).
- **Narrative log design approved in full** (`196517a`, `c58fa42`). Output stays v1.2. Wiki: [decisions](decisions.md).

## 2026-10-02, later at night: v1.1 sign-offs

- The user approved the versioned R2 layout, its D1 columns and the reparse cron (hourly, 5 per batch, 80% cap, on in prod), and confirmed that their own demos stay local (`4e06e12`). The "pending sign-off" banner is gone from [R2 layout](r2-layout.md), and [decisions](decisions.md), the [reparse runbook](runbook-reparse.md) and the [README](README.md) say so. Nothing in v1.1 is built yet.

## 2026-10-02, night: v1.1 is final, and four new pages

- **v1.1 is final in the spec** (`6d603ba`): M5 "every map and safe reparse" and M6 "trusted and hardened", with v1.2 as the Valve fetch, two-floor maps and the narrative log build. It adds D1 columns and changes the R2 layout, but `schema_version` stays 1. Wiki: [decisions](decisions.md), [README](README.md).
- **New pages:** [maps](maps.md), [R2 layout](r2-layout.md) (marked pending the user's sign-off on the versioned layout), [reparse runbook](runbook-reparse.md) and [stuck-match runbook](runbook-stuck-matches.md). Each says plainly which parts are specified and not yet built. The learning center lists them under a new "Operations" group.
- **Fix:** an earlier edit had dropped the README's system diagram, its "Where to start" table and the Status heading. They are restored, with rows for the new pages.

## 2026-10-02, evening: scope decisions and viewer polish

- **Decided by the user** (`74fc4c0`, `6e71214`): keep Steam ids in public match data. v1.1 is duplicate-upload reuse, a reparse path and more single-level maps. The Valve fetch and two-floor maps move to v1.2. The narrative log gets groundwork only (`parser/eventlog`, no output). Wiki: [decisions](decisions.md), [schema](schema.md), [worker](worker.md), [parser](parser.md).
- **Viewer polish** from the pipeline session: player names avoid overlapping (`eb51eb1`), playback text updates ten times a second (`17c2e4f`), uploads cancel when you leave (`c277646`), unused helpers removed (`295e73e`), and the data layer's caching and URL versioning are tested (`06b3e02`). Wiki: [viewer](viewer.md).
- Docs commits: `7890f65` and `a49509a` put this wiki and its learning center in git.
- In progress, untracked: more of `parser/entities` (field paths, quantized values, state, tables, value decoders) for M4 stage 3.

## 2026-10-02, later: M1 and M2 close, prod is live

- **M1 is closed** (`3bd6548`): on all 8 of the user's matches, K/A/D, headshot kills and ADR equal the game's scoreboard counters for 80 of 80 players. A third-party stats site differs only by listing flash assists separately and rounding ADR. The post-win-panel death question is settled. Wiki: [match-logic](match-logic.md), [README](README.md).
- **M2's exit is met** (`1244395`): run `36994672034` was green, deploying dev then prod (version `bd466d33`) with both parser container apps ready. CI is pinned to `ubuntu-24.04` (`68070ae`). Wiki: [infra](infra.md).
- **The spec now says Containers everywhere** (`2811946`, `8c863b2`). The parser route is private, only the Worker can reach it, and `X-Parse-Secret` stays. The instance is `basic` with `sleepAfter` 30 s and one instance at most. The "deliberate deviation" wording is gone from the wiki and the quiz. Wiki: [infra](infra.md), [decisions](decisions.md).
- **M4 stage 1 is done** (`bf09f8f`, `5b49b48`): field layouts for 211 and 218 classes. Stage 3 started (`f4bbc25`). Wiki: [parser](parser.md).
- **Renames:** `ide` is now `parser` and `terminal` is now `pipeline`, which also owns `web/` and the M3 viewer. Wiki: [README](README.md), [decisions](decisions.md).

## 2026-10-02, 03:10: checkpoints close out

- `0d3d900`: the plan records the fixtures and the first end-to-end parse. Checkpoint 0.4 is done (a played match, a FACEIT demo and an overtime match are all local), and all 10 local demos diff OK. The overtime swap assumption is now verified. Still open for 1.7: a screenshot of the final scoreboard of a played match. Wiki: [match-logic](match-logic.md), [testing](testing.md).
- The same plan entry says the D1 scoreboard from the cloud parse equals local `demo parse`, and the first CI run verified the Docker build. The CI deploy waits on a `CLOUDFLARE_API_TOKEN` the user is creating. Wiki: [infra](infra.md), [README](README.md).
- `eb77edf`: the CI migration step passes `--env=<name>`, as the deploy step does. `42d255e`: the Dockerfile's unverified note is gone.
- Untracked and in progress: `parser/entities/` (M4 stage 1: classes, decoder, serializers).

## 2026-10-02, later still: Containers live, M4 begins, 64 Hz lands

- **First end-to-end parse on dev passed:** upload to ready in about 48 s, with a 31.6 s parse. The repo is pushed to a private GitHub repo and the first CI run passed all 7 checks. `demo diff` passes locally on all 10 demos, including an overtime one. The shared fixtures bucket holds the two public demos only. Wiki: [infra](infra.md), [testing](testing.md), [README](README.md).
- **Parser on Containers is real.** A 360 s stub request ran through the queue path, the stub was removed (`62af2c3`), the Worker got a `ParserContainer` binding in place of `PARSER_URL` (`9c7175e`), and CI deploys without GCP (`e047eea`). Dev runs the container. Wiki: [infra](infra.md), [worker](worker.md).
- **The oracle writes 64 Hz** by default (`9ab7e1f`), with compact files. Wiki: [schema](schema.md), [testing](testing.md).
- **M4 groundwork:** in-packet string tables decode and match every snapshot (`c9e9bd7`), `parse.RunStats` (`52f8113`), and the server logs every parse without leaking secrets (`14ec9f0`). Wiki: [parser](parser.md).
- 8 of the user's own MM/Premier demos are in the local demos folder, gitignored.
- The learning center was restyled in a quieter register, taking its type, palette and window panels from chiemeka.dev.

## 2026-10-02, later: 64 Hz, Valve fetch, logging audit, and a learning suite

- **Round files at 64 Hz.** The user went 16, then 32 (`9507fd0`), then 64, every tick (`f73feb7`). The oracle and regenerated samples were still at 32 in the working tree when I looked. Wiki: [schema](schema.md), [viewer](viewer.md), [decisions](decisions.md).
- **Scope change: pull the user's own demos from Valve** (`72dd18c`, `4fb647d`). `CLAUDE.md`'s boundary now allows demos fetched through network APIs, and still forbids anything touching a game process. Timing pending. Wiki: [decisions](decisions.md).
- `41ddb26`, `14e7130`: the Worker logs every parse attempt, dead letter and rejected confirm, and tracing is on. A logging audit exists with items for `terminal` (W1 to W17) and `ide` (P1 to P19), uncommitted. Wiki: [decisions](decisions.md#logging-audit), [infra](infra.md).
- Map data upload: 100 objects (17.4 MB) per bucket, hash-verified.
- A private GitHub repo is now `origin`. Nothing is pushed. Wiki: [infra](infra.md).
- Valve demo fetch is decided as post-v1 (`6c9355e`). The spec now targets desktop browsers (`d56ca5a`). Dev runs the full observability block, and `crane` is installed for pushing images without Docker. Wiki: [decisions](decisions.md), [viewer](viewer.md), [infra](infra.md).
- **New:** the wiki runs as a local learning center (`.claude/wiki/site/`): a guided Tour, a clickable system Map, seven Labs, the Pages, and a Study tab with progress, notes, quizzes and flashcards.

## 2026-10-02, 02:29 to 02:30: demo diff, exact ADR, dev site live

- `0d283b4`: **`demo diff`** is committed. Both interim demos diff OK against the oracle. Wiki: [parser](parser.md), [testing](testing.md).
- `22bb104`: a demo with no live rounds is rejected as a bad demo (exit 1, HTTP 422), and tests share a synthetic one-round 1v1 (`parsetest.Match`).
- `696f2fa`: plan marks match logic and `demo diff` done on the interim demos. **ADR is exact**, not within 1%: HP before minus HP after matches `m_iDamage`. Wiki: [match-logic](match-logic.md), [decisions](decisions.md).
- `51015a0`: invocation logs at 100% sampling in every env. The dev site is live at `demoview-api-dev.chiemeka-kalu.workers.dev`, prod isn't deployed, and uploads fail into the DLQ until `PARSER_URL` is real. Wiki: [infra](infra.md).
- Coordination: `cloud` is the coordinating session. Directory ownership now is `ide` for `parser/` and `schema/`, `terminal` for `worker/`, `cloud` for infra and the `.claude` coordination docs, and this session for `.claude/wiki/`. That's who edits what, not history. Peers send me one line per notable commit.

## 2026-10-02, 02:24 to 02:25: match logic lands, Containers stub appears

- **Match logic is committed.** `fa3d8a9` (infer rounds, sides, kills and scoreboard) and `671e08e` (`parse.Run` builds the full `match.json`). Checkpoint 1.7 code is in. Wiki: [match-logic](match-logic.md), [parser](parser.md).
- `4c5ae64`: `match.Kill.Attacker` is now `*int`, which closes the schema mismatch listed in the plan.
- `6e5885c`: `Message.Unmarshal` and named CS user messages, needed for the end-of-match list.
- `d8fca91`: **Containers stub** in `worker/container-stub/`. It holds a request open for minutes to see if the Containers runtime can work. Wiki: [infra](infra.md#parser-runtime).
- **New proposal:** `.claude/logging.md`, the narrative log. It awaits the user's decisions. Wiki: [decisions](decisions.md#narrative-log).
- `ee9ed6e`: the stub container sleeps after 30 s, the cost fix from the logging proposal.
- The `demo diff` subcommand was still in the working tree at this point. It landed in `0d283b4`.

## 2026-10-02, 02:05 to 02:23: oracle in the pipeline, CI, diff

- The server now ships the oracle's round files, behind the consistency guard (`89ffa22`, `26bad74`, `247c339`). The oracle reads stdin so the demo never touches disk.
- CI: seven parallel jobs, a deploy workflow gated on CI, and a `diff` job against fixtures (`79ca1ea`, `6538308`, `329beb3`, `bcd87ae`).
- `parser/diff` package (`3dfed21`). Plan edits record the Containers decision and the Cloudflare infra.
- Wiki: [testing](testing.md), [infra](infra.md).

## 2026-10-02, 01:44 to 02:04: events, identity, oracle, viewer

- Game events decoded, matching demoparser2 row for row: Nuke 157 deaths, Mirage 73 (`d4572aa`). Player identity from userinfo (`983428f`).
- **Oracle** built (`8950fa5`).
- **Viewer** built against fixtures: data layer, replay, upload and match pages (`55ce4c9`, `d057d06`, `e69a850`). Mirage radar added.
- Worker serves `match.json` and the viewer itself (`a99e30d`, `f89c2e6`). Viewer URLs are versioned by `parsed_at` so a reparse never serves stale files (`17066f3`).
- Wiki: [viewer](viewer.md), [demo-format](demo-format.md).

## 2026-10-02, 01:29 to 01:43: decode, schema, server

- Frames decompress and decode, packet messages are iterated (`d85430c`, `4269624`).
- **Schema v1** with validator and samples (`cf94346`).
- `parser/store`, the `/parse` service and Dockerfile (`442b977`, `606968a`, `d14f873`).
- `parse.Run` becomes the single path for the CLI and server (`3983f89`).
- Wiki: [schema](schema.md), [parser](parser.md).

## 2026-10-02, 00:19 to 00:42: day one

- Spec updated for a solo build (`b3f3afb`). Protos vendored, streaming frame reader and CLI skeleton (`035cfe2`, `7ce6862`, `0191ae4`).
- Worker scaffolded: migration, routes, queue consumer with DLQ (`2b18ccd` to `0b4cea0`).
- Bit reader (`70657e2`). The game-memory boundary recorded (`5c8324f`).
- Plan with M0/M1 checkpoints and spec gaps (`35715f7`).

## Wiki log

- **2026-10-02:** first version of every page, built from the repo at `28d20f7`. Second pass brought it up to `696f2fa`.

## Not yet noticed by this wiki

Nothing outstanding. If a session landed something between my passes, it shows up in the next entry.
