# worker

The Cloudflare Worker: the public API and the parse queue consumer. It owns D1. See the API, Queue consumer and Data model sections of `.claude/spec.md`.

```sh
npm ci
npm test              # vitest inside workerd, with fake secrets and a stubbed parser
npm run typecheck
npm run lint
npm run types         # regenerate worker-configuration.d.ts after editing wrangler.jsonc
npm run dev           # wrangler dev; needs a .dev.vars (copy .dev.vars.example)
```

If npm 10.9 has to resolve the tree without `package-lock.json`, it crashes on Vite 8's optional peers (`Cannot read properties of null (reading 'edgesOut')`). Regenerate the lockfile with `npx npm@11 install` instead.

## API

Uploads need `Authorization: Bearer <UPLOAD_TOKEN>`. Reads are open. Errors are `{ "error": "..." }`.

| Route | Success | Errors |
| ----- | ------- | ------ |
| `POST /api/matches` | `201 { id, upload_url, upload_expires_in }`. PUT the demo to `upload_url` within `upload_expires_in` seconds. | 401 |
| `POST /api/matches/:id/uploaded` | `202 { id, status: "queued" }` | 401, 404, 409 (not uploaded yet, or not in `uploaded` any more: the body has `status`), 413 (over 1 GB; the demo is deleted and the match fails) |
| `GET /api/matches/:id` | `200 { id, status, error, map, score_a, score_b, parser_version, schema_version, created_at, parsed_at, players, rounds }`, `no-store` | 404 |
| `GET /api/matches/:id/rounds/:n` | `200` round file, immutable for a year, with an ETag (304 on `If-None-Match`) | 404 |

`players` rows are `{ idx, name, team, kills, deaths, assists, adr, hs_pct }`, ordered by `idx`. Steam ids stay out until the spec's open question on them is settled. `rounds` rows are `{ n, winner, reason, side_a }`, ordered by `n`. Timestamps are Unix milliseconds.

## Match status

```text
uploaded --confirm--> queued --consumer--> parsing --200--> ready
                         ^                    |
                         +---- transient -----+--422 or bad output--> failed
                                              |
                         3 attempts used: dead letter queue --> failed
```

A match is `uploaded` from creation until the upload is confirmed. While a retry is pending it is back in `queued`, with the last error in `error`. Every change is logged as one JSON line, `{"event":"match_status",...}`.

## Setup for M2

`wrangler.jsonc` has placeholders. The top level is dev; `--env prod` is prod.

1. `wrangler d1 create demoview-dev` (and `demoview-prod`). Paste each `database_id` into `wrangler.jsonc`.
2. Create R2 buckets `demoview-dev` and `demoview-prod`. Allow `PUT` from the viewer origin in each bucket's CORS policy, because browsers upload straight to R2.
3. `wrangler queues create` for `demoview-dev-parse`, `demoview-dev-parse-dlq`, `demoview-prod-parse` and `demoview-prod-parse-dlq`.
4. Set `R2_ACCOUNT_ID` and `PARSER_URL` in `wrangler.jsonc`. `R2_BUCKET` already names each environment's bucket; the parser service reads the same variable.
5. `wrangler secret put` for `UPLOAD_TOKEN`, `PARSE_SECRET`, `R2_ACCESS_KEY_ID` and `R2_SECRET_ACCESS_KEY`. Repeat with `--env prod` for prod. The R2 keys come from an R2 API token with Object Read & Write on that environment's bucket.
6. `wrangler d1 migrations apply DB --remote` (and `--env prod`), then `wrangler deploy` (and `--env prod`).

## Local development

`npm run dev` runs the API and both queue consumers with D1, R2 and Queues emulated locally. Run `npm run migrate:local` once first. To parse locally, set `PARSER_URL=http://localhost:8080` in `.dev.vars` and run the parser container. Presigned URLs always point at real R2, so for a local end-to-end run, put the demo with `wrangler r2 object put demoview-dev/demos/<id>.dem --local --file <demo>` instead.
