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
| `POST /api/matches` | `201 { id, upload_url, upload_expires_in }`. PUT the demo to `upload_url` within `upload_expires_in` seconds. | 401 |
| `POST /api/matches/:id/uploaded` | `202 { id, status: "queued" }`, or `200 { id, status, duplicate: true }` when a match that hasn't failed already has the same demo (by SHA-256): `id` is that match, and the new upload and its match are deleted | 401, 404, 409 (not uploaded yet, or not in `uploaded` any more: the body has `status`), 413 (over 1 GB; the demo is deleted and the match fails) |
| `GET /api/matches/:id` | `200 { id, status, error, map, score_a, score_b, parser_version, schema_version, created_at, parsed_at, players, rounds }`, `no-store` | 404 |
| `GET /api/matches/:id/match.json` | `200` the parser's `match.json` (kills with assisters, in the order round files index them), cached 5 minutes with an ETag (304 on `If-None-Match`) | 404 (unknown match, not `ready` yet, or no file) |
| `GET /api/matches/:id/rounds/:n` | `200` round file, immutable for a year, with an ETag (304 on `If-None-Match`) | 404 |
| `GET /api/maps/:name` | `200 { name, pos_x, pos_y, scale }`, cached a day. A game position lands on the 1024-pixel radar at `((x - pos_x) / scale, (pos_y - y) / scale)`. | 404 (no radar, or a two-floor map) |
| `GET /api/maps/:name/radar.png` | `200` the radar image, cached a day, with an ETag | 404 |

`players` rows are `{ idx, name, team, kills, deaths, assists, adr, hs_pct }`, ordered by `idx`. Steam ids stay out until the spec's open question on them is settled. `rounds` rows are `{ n, winner, reason, side_a }`, ordered by `n`. Timestamps are Unix milliseconds.

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

A match is `uploaded` from creation until the upload is confirmed. While a retry is pending it is back in `queued`, with the last error in `error`. Every change is logged as one JSON line, `{"event":"match_status",...}`.

## Parser container

The queue consumer sends each parse to one container through its Durable Object (`PARSER`), one job at a time (`max_concurrency: 1`). The container gets `PARSE_SECRET` and the R2 credentials from the Worker's secrets at start, and sleeps 30 seconds after its last request, because idle time is billed. Its instance type is `basic` (1/4 vCPU, 1 GiB); the largest fixture peaks near 450 MB. It logs `container_start` and `container_stop`, where exit code 137 means it ran out of memory.

The account, D1 databases (migrations applied), R2 buckets, queues and Worker secrets (`UPLOAD_TOKEN`, `PARSE_SECRET`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`) exist in both environments, and each bucket's CORS policy allows `PUT` from the Worker's origin, because browsers upload straight to R2. New migrations go first: `wrangler d1 migrations apply DB --remote` (plus `--env prod`).

## Local development

`npm run dev` runs the API and both queue consumers with D1, R2 and Queues emulated locally. Run `npm run build:web` and `npm run migrate:local` first. Local D1 data is keyed by `database_id`, so migrate again if that changes. The parser container is off in `wrangler dev` (`dev.enable_containers`), since it needs Docker; set it to `true` with Docker running to parse locally. Presigned URLs always point at real R2, so for a local end-to-end run, put the demo with `wrangler r2 object put demoview-dev/demos/<id>.dem --local --file <demo>` instead.
