# worker

The Cloudflare Worker: the public API, the parse queue consumer, the parser's container (`src/container.ts`, built from `parser/Dockerfile`), and the host for the viewer's static files. It owns D1. See the API, Queue consumer and Data model sections of `.claude/spec.md`.

```sh
npm ci
npm test              # vitest inside workerd, with fake secrets and a stubbed parser
npm run typecheck
npm run lint
npm run types         # regenerate worker-configuration.d.ts after editing wrangler.jsonc
npm run dev           # wrangler dev; needs a .dev.vars (copy .dev.vars.example) and a web build
npm run build:web     # build the viewer into ../web/dist
npm run deploy:dev    # build the viewer, then deploy dev (needs Docker for the parser image)
npm run deploy:prod   # build the viewer, then deploy prod (needs Docker for the parser image)
```

Deploys normally run in CI (`.github/workflows/deploy.yml`), which has Docker.

If npm 10.9 has to resolve the tree without `package-lock.json`, it crashes on Vite 8's optional peers (`Cannot read properties of null (reading 'edgesOut')`). Regenerate the lockfile with `npx npm@11 install` instead.

## API

Uploads need `Authorization: Bearer <UPLOAD_TOKEN>`. Reads are open. Errors are `{ "error": "..." }`.

| Route | Success | Errors |
| ----- | ------- | ------ |
| `POST /api/matches` | `201 { id, upload_url, upload_expires_in }`. PUT the demo to `upload_url` within `upload_expires_in` seconds. | 401, 429 (over 10 uploads and reparses a minute; `Retry-After: 60`) |
| `POST /api/matches/:id/uploaded` | `202 { id, status: "queued" }`, or `200 { id, status, duplicate: true }` when a match that hasn't failed already has the same demo (by SHA-256): `id` is that match, and the new upload and its match are deleted | 401, 404, 409 (not uploaded yet, or not in `uploaded` any more: the body has `status`), 413 (over 1 GB; the demo is deleted and the match fails) |
| `POST /api/matches/:id/reparse` | `202 { id, status }`. A `ready` match gets a reparse job and stays `ready` on its live parse whatever happens; a `failed` one goes back to `queued` | 401, 404, 409 (any other status, or the demo is no longer stored), 429 |
| `GET /api/matches/:id` | `200 { id, status, error, map, score_a, score_b, parser_version, schema_version, created_at, parsed_at, parse_id, players, rounds }`, `no-store` | 404 |
| `GET /api/matches/:id/events` | `200 { events }`, the match's pipeline history oldest first: `{ seq, ts, kind, msg, attempt, build, parse_id }` rows, where `kind` is `status`, `attempt`, `reparse` or `cleanup`. `no-store` | 404 |
| `GET /api/matches/:id/match.json` | `200` the parser's `match.json` (kills with assisters, in the order round files index them). With `?v=<parse_id>`, that parse's, immutable for a year; without it, the live parse's, cached 5 minutes. Both with an ETag (304 on `If-None-Match`) | 404 (unknown match, not `ready` yet, or no file) |
| `GET /api/matches/:id/rounds/:n` | `200` round file, cached as `match.json` is | 404 |
| `POST /api/admin/cron/:job` | `200 { job, ran: true }` after running `sweep`, `reparse` or `cleanup` now, as its cron would | 401, 404 (no such job) |
| `GET /api/maps/:name` | `200 { name, pos_x, pos_y, scale }`, cached a day. A game position lands on the 1024-pixel radar at `((x - pos_x) / scale, (pos_y - y) / scale)`. | 404 (no radar, or a two-floor map) |
| `GET /api/maps/:name/radar.png` | `200` the radar image, cached a day, with an ETag | 404 |

`players` rows are `{ idx, name, team, kills, deaths, assists, adr, hs_pct }`, ordered by `idx`. Steam ids stay out; `match.json` has them. `rounds` rows are `{ n, winner, reason, side_a }`, ordered by `n`. Timestamps are Unix milliseconds.

A failed match's `error` starts with a code, `"<code>: <detail>"`: `bad_demo` or `demo_not_found` from the parser, or `too_large`, `bad_output`, `parse_failed` or `stuck` from the Worker. Errors from before codes, and the last error of a pending retry, have none.

## Parses

Every parse attempt writes under its own prefix, `parsed/{id}/{parse_id}/`, with `manifest.json` last. Once the parser answers 200, the consumer points the match's `live_parse_id` at it and replaces its D1 rows in one batch, but only if the pointer hasn't moved since the attempt started, so a slower parse can never replace a newer one. The viewer asks for files with `?v=<parse_id>` from the summary. Matches parsed before parse ids have a null `live_parse_id`, and their files sit straight under `parsed/{id}/`.

A reparse job (`{ match_id, reparse: true }`) parses a `ready` match again without leaving `ready`. If it fails, the match keeps its live parse, and `reparse_error` and `reparse_build` say why and with which build. `attempted_build` is the build of the latest attempt: `PARSER_BUILD`, which CI sets at deploy from the git trees of `parser/` and `tools/oracle/`, plus `PARSER_ROUNDS` when it's set (e.g. `9f5be622def2+parser`). So switching whose round files ship reparses every match, the same as a code change.

## Scheduled jobs

Three crons (`triggers` in `wrangler.jsonc`, `src/cron.ts`), in both environments.

- **Every 10 minutes, the sweeper.** A match 30 minutes in `queued` since its last status change has lost its queue message, so it is queued again. One 30 minutes in `parsing` has nothing left to finish it, so it fails with `stuck`. Matches in any other status are never touched.
- **Hourly, the reparse batch.** Up to 5 `ready` matches whose `attempted_build` isn't the deployed `PARSER_BUILD` get reparse jobs, oldest first, so each match is tried once per build. It waits while anything is `queued` or `parsing`, so an upload waits behind at most one batch. It stops for the month once `container_usage` reaches 80% of the 25 instance-hours the plan includes, and does nothing on a deploy without a build id (`dev`).
- **Daily at 04:30 UTC, cleanup.**
  - It deletes the prefixes of parses that aren't live and started over 7 days ago, found through the attempts in `match_events`.
  - A reparsed v1 match's files go once its live parse is 7 days old. They are deleted by key (`match.json`, plus what is under `rounds/`), never by listing `parsed/{id}/`, which holds every parse.
  - Uploads still unconfirmed after a day go too, with their demo.

## Viewer

The same Worker serves the viewer from `../web/dist` (`assets` in `wrangler.jsonc`), so the API is same-origin and needs no CORS. Build the viewer before `wrangler dev` or a deploy; the `deploy:*` scripts do it for you.

- `/api/*` always runs the Worker (`run_worker_first`), even for a browser navigation.
- Any other path that isn't a file gets `index.html` (`single-page-application`), so `/m/:id` and `/m/:id/r/:n` load directly.
- The build includes `samples/` (from `schema/samples`), so `/m/sample` works in every environment.

Tests don't need the viewer build.

## Match status

```text
uploaded --confirm--> queued --consumer--> parsing --200--> ready
                         ^                    |
                         +---- transient -----+--422 or bad output--> failed
                                              |
                         3 attempts used: dead letter queue --> failed
```

A match is `uploaded` from creation until the upload is confirmed. While a retry is pending it is back in `queued`, with the last error in `error`. A match that reached `ready` stays there. Every change is logged as one JSON line, `{"event":"match_status",...}`, and stamps `status_changed_at`.

The same changes, every parse attempt (its build, parse id, outcome and time), reparse requests and cleanups also go into D1's `match_events`, about ten rows per match, which outlast the 7 days Workers Logs keep. Each parse call's time, plus the container's 30 idle seconds, adds to `container_usage` for the month.

## Parser container

The queue consumer sends each parse to one container through its Durable Object (`PARSER`), one job at a time (`max_concurrency: 1`). The container gets `PARSE_SECRET` and the R2 credentials from the Worker's secrets at start, and `PARSER_BUILD`, `PARSER_ROUNDS` and `PARSER_SHADOW` from its vars. Dev ships our own round files (`PARSER_ROUNDS=parser`) and compares each parse's with the oracle's (`PARSER_SHADOW=oracle`, logged as `event=shadow`). Prod ships the oracle's. An empty value is never passed on, since it would override the image's `PARSER_ROUNDS=oracle`. The container sleeps 30 seconds after its last request, because idle time is billed, or 2 minutes on dev, so the compare that runs after each response isn't cut short. Its instance type is `basic` (1/4 vCPU, 1 GiB); the largest fixture peaks near 450 MB. It logs `container_start` and `container_stop`, where exit code 137 means it ran out of memory.

The account, D1 databases (migrations applied), R2 buckets, queues and Worker secrets (`UPLOAD_TOKEN`, `PARSE_SECRET`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`) exist in both environments, and each bucket's CORS policy allows `PUT` from the Worker's origin, because browsers upload straight to R2. New migrations go first: `wrangler d1 migrations apply DB --remote` (plus `--env prod`).

On dev only, the `FAULT` secret stands in for a broken parser build in the recovery checks: `reject` answers every parse as a bad demo, and `fail` as a transient failure. The prod deploy refuses to run while prod has it set.

## Local development

`npm run dev` runs the API and both queue consumers with D1, R2 and Queues emulated locally. Run `npm run build:web` and `npm run migrate:local` first. Local D1 data is keyed by `database_id`, so migrate again if that changes. The parser container is off in `wrangler dev` (`dev.enable_containers`), since it needs Docker; set it to `true` with Docker running to parse locally. Presigned URLs always point at real R2, so for a local end-to-end run, put the demo with `wrangler r2 object put demoview-dev/demos/<id>.dem --local --file <demo>` instead.
