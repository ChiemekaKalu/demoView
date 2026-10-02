# Pickup: pipeline (Worker and viewer)

To the next pipeline session. You own `worker/` and `web/`, and the CI workflows they touch. Peers: `cloud` holds the credentials, pushes main and runs deploys; `parser` owns `parser/`, `schema/` and the M4 part of the plan; `aiguy` ran the Jev experiments. Read `.claude/plan.md` and `.claude/spec.md` (v1.1 and v1.2 sections) first.

## Where I stopped (2026-10-02, about 21:30 UTC)

v1 and v1.1 are done. M4, M5 and M6 are closed except M6 6.7. Match Pulse v1 is live on dev. The last commit, `decdbdf` (the replay's Pulse strip), is committed but not pushed; cloud pushes it.

## What's live

- **Parses.** Each attempt writes `parsed/{id}/{parse_id}/` with `manifest.json` last, and D1's `live_parse_id` moves by a conditional swap. `?v=<parse_id>` URLs are immutable.
  - Dev ships our own round files with the shadow compare (`PARSER_ROUNDS=parser`, `PARSER_SHADOW=oracle`). The verdict lands as `shadow.json` beside the parse.
  - Prod ships our round files without the compare.
  - The build id is `PARSER_BUILD` (a hash of the `parser/` and `tools/oracle/` trees) plus `+PARSER_ROUNDS`. A change to either reparses every match through the cron.
- **Crons** (both environments): sweeper every 10 minutes, reparse batch hourly (5 at a time, 80% container cap), cleanup daily at 04:30 UTC.
  - `POST /api/admin/cron/:job` runs any of them now, behind the upload token.
- **Match Pulse:** `PULSE_ENABLED` is true on dev and false on prod. It uses a separate pulse queue; Jev runs over `env.AI` through the AI Gateway `default`.
  - The fallback logit covers credits, failures, the cap and size.
  - Output is `pulse.json` beside the parse (`schema/pulse.schema.json`), plus `pulse_quality` and `ai_usage` in D1.
  - All 3 dev matches have real Jev Pulses, at about $0.006 a match.
- **CI.** `pulse-eval.yml` scores Jev on 225 + 28 golden states (`worker/test/pulse-golden/`), about $0.01 a run. It runs on Pulse changes, weekly and by hand. All bars were green on the last run except the canary, which `6abed7c` fixed; that run follows the push.
- **Maps.** Every map with a radar is served by `/api/maps/:name`. Two-floor maps (Nuke, Train, Vertigo) get their main radar with `two_floor: true`; the replay notes that both floors share it. The user chose this interim; v1.2 adds a radar per floor.
- **Findings page.** `/findings` shows the pro-round study, aiguy's aggregate data. It loads as its own chunk.
- **Other.** Uploads and reparses are limited to 10 a minute. A dev-only `FAULT` secret exists, but it's unset now; prod's deploy refuses it. The match page shows the pipeline history (`match_events`).

## In flight

- `decdbdf` feat(web): the Pulse strip under the scrubber. Verified locally against dev's 3 Pulse matches (line, swing-sized marks, ⚡ seek). After cloud pushes it, look at one replay on dev.
- **Branch `viewer/restyle-gallery`**, from cloud's design workflow on its own worktree: a lighter theme, an opt-in featured gallery (`POST /api/matches/:id/feature`, `GET /api/gallery`) and a D1 migration. Cloud sends it to you to review and merge after Pulse.
  - Expect conflicts in `web/src/index.css` and the migration number; ours are up to 0005.
- **Branch `research/bulk-demos`** (aiguy, worktree `../demoView-research`): never merges. CI skips `research/**`.

## Next three steps, in order

1. **Pulse viewer follow-through.** After `decdbdf` deploys, check a dev replay. Then review and merge `viewer/restyle-gallery`. The "rounds worth watching" shelf stays unbuilt until the user rates about 40 rounds (spec: Spearman ρ ≥ 0.6).
2. **Pulse `state_version` 2,** once parser's narrative log writes the bomb timeline.
   - Add plant, defuse and explosion lines to the states, a `planted` feature in the fallback, and `how_won` from the timeline instead of the round's end reason.
   - Bump `STATE_VERSION`, regenerate the goldens (`npm run pulse:golden -- ../parser/testdata/reference`), and let the eval's headline bar return to 0.75 by itself.
   - Then ask the user and cloud about `PULSE_ENABLED=true` in prod.
   - The log's R2 key isn't settled. `logging.md` says `parsed/{id}/log/{parser_version}/…`, which predates per-parse prefixes. Code against "the log beside the parse" and settle the key with parser.
3. **M6 6.7.** When the user has added `FIXTURES_R2_ACCESS_KEY_ID` and `FIXTURES_R2_SECRET_ACCESS_KEY` (a read-only token on `demoview-fixtures`) and cloud has set `REQUIRE_FIXTURE_DIFF=true`, the next deploy runs the diff. 6.7 closes when its "Diff each fixture against the oracle" step runs green.

## Gotchas

- **Shared working tree.** Several sessions edit it at once. Commit only your paths (`git commit -m … -- <paths>`), never `-a` or `add -A`. Build branches in a separate `git worktree` under your scratchpad, and remove it once the branch is merged. Don't add a `Co-Authored-By` trailer.
- **Credentials.**
  - Wrangler uses the machine's shared OAuth login. It can query D1, put secrets and read R2, but has no observability read, and it lost its scopes once (the user re-ran `wrangler login`).
  - The API token with Workers Observability lives only in GitHub; don't move it locally.
  - Ask the user in this session before any manual deploy or secret change.
- **Logs.**
  - Container stdout, including the parser's slog lines, is only in Workers Logs, never in `wrangler tail`. The Worker's own console lines are in both. That's why the shadow verdict is written to R2.
  - Use `wrangler r2 object get <bucket>/<key> --remote --pipe` to read parse files.
- **Config pitfalls.**
  - Never pass an empty `PARSER_ROUNDS` to the container; it would override the image's default and ship no round files. `containerEnv` drops empty values.
  - Prod's vars set `PARSER_ROUNDS` and `PARSER_SHADOW` explicitly, so wrangler stops warning about vars missing from `env.prod`.
- **Pulse cost.** Pulse spends real money on dev (about $0.006 a match) from the user's $25 AI Gateway credit. Jev isn't bit-deterministic (±0.02 on a probability). Error 2021 means out of credits; 2018 is the rate limit.
- **Tests.** `remoteBindings: false` in `vitest.config.ts` keeps tests off real Workers AI. Tests pin `PARSER_ROUNDS: ""` and `PULSE_ENABLED: "false"`.
- **Windows shell.**
  - Git Bash rewrites `/m/...` arguments as paths, so prefix with `MSYS_NO_PATHCONV=1`.
  - Go is at `/c/Program Files/Go/bin`, not on the bash PATH.
  - npm 10.9 crashes resolving without a lockfile (`npx npm@11 install`).
  - Files are LF in git and may show CRLF on disk; edit with the Edit tool or Python, not multi-line sed.
- **Verification tools.** The D1 API rate-limits bursts (code 971), so space your queries. The preview server and headless Edge are in the scratchpad: `shots/serve.mjs <dist> <port> <api base>`, then playwright-core with `channel: "msedge"`.

## What needs the user

- **6.7:** a read-only R2 token for `demoview-fixtures`, set as the two GitHub secrets; then cloud sets `REQUIRE_FIXTURE_DIFF`.
- **AI Gateway spend limit:** a limit on `default`, about $5 a month suggested. Nobody can see whether one is set.
- **The shelf:** rate about 40 rounds 0–4, blind to Jev, before the "rounds worth watching" shelf can ship.
- **Pulse in prod:** say yes or no once state_version 2 lands.
- **Narrative log key:** whether the log moves under `{parse_id}`. This is parser's and cloud's call, with the user.
