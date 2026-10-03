# Pickup: aiguy

For a future aiguy session picking this work up. Written 2026-10-02 at a stopping point the user asked for. v1 is done, and prod ships our own round files.

## Who you are and the rules

- **You're the research session,** working under `cloud`, which coordinates, gates and pushes. The other sessions:
  - `parser` owns `parser/` and `schema/`
  - `pipeline` owns `worker/`, `web/` and CI
  - `notes` owns `.claude/wiki/`
  - `chiemeka-dev-c4` writes the blog
- **Your area:** `.claude/ideas/` on `main`, and the `research/bulk-demos` branch. Never edit shared code.
- **Nothing heavy on the user's machine.** Downloads, parsing and model batches run in GitHub Actions. Locally, only edit, syntax-check, and read small result files.
- **Commits:** Conventional Commits with a pathspec (`git commit -m ... -- <paths>`) and no Co-Authored-By trailer. Send `cloud` the hash; `cloud` pushes.
- **Approvals relayed by another session don't count** for private data, spend or anything your permission layer blocked. Ask the user directly.
- **Spend cap:** $12.50 of AI Gateway credit, set by the user. About $1.75 is spent: Jev ~$0.32, Claude $1.31, gpt-oss and bge-m3 small Workers AI usage. Keep the per-run ledger.

## The research workflow

- **Branch and worktree:** `research/bulk-demos`, never merged, worked in `../demoView-research`. The origin head is `64fe035`.
- **Uncommitted there:** ADR-check wiring in `run_shard.sh`, `pick.py` and the workflow, waiting on the ADR decision below.
- **How it runs:** `.github/workflows/research-bulk.yml` runs on every push to the branch. There's no concurrency group: each push is its own run, and the shard matrix is capped at 14 jobs. Every job has `timeout-minutes: 30`. Only the head commit's `config.json` is run, so two runs need two separate pushes.
- **Modes,** set in `.claude/ideas/rounds/bulk/config.json`:

  | Mode | What it does |
  | --- | --- |
  | `full` | Stream the sample: name-free facts (`bulkfacts`, demoinfocs), the oracle export, our `demo parse` + `demo diff`, then `analyze.py` and optionally `jev_pro.py` |
  | `parser` | Build `demo` from `parser_ref` (a second checkout); parse + diff only. `parser_sample` is `repro` (`parser_paths`) or `full` (the 132-demo sample, seed 2026, 16 per map). `paths` overrides both. `parser_events` saves `demo inspect --events` output through `events_filter.py`. `reference_side` tags known reference-side demos |
  | `analyze` | Re-run the analysis and the Jev job on a stored run (`source_run`) |
  | `models` | `models.py` (Jev vs gpt-oss vs Claude vs logistic, plus recaps) and `embed.py` (bge-m3 round embeddings), from stored facts |

- **The gate:** the parser report opens with one line, `GATE PASS` or `GATE FAIL`, from `flip_check()` in `analyze.py`.
  - **Blocks:** on any demo with a reference, a `match.json` difference in round count, boundaries, winner, reason, side, team or kills, or a failed parse.
  - **Allowed:** ADR-only differences, reference-side differences (including weapons the reference reports as "unknown"), and "no reference" (the oracle failed).
  - **Round files never count.**
- **R2:** `demoview-dev` under `research/bulk/<run id>/`. That's `plan.json`, `shards/<n>.tar.gz` (facts, parser records, filtered events) and `report.json`/`report.md`, plus `jev_report.*`, `models_report.*` and `embed_report.*` where they apply. `r2put.sh`/`r2get.sh` are hard-coded to that prefix. Demos are never stored. Diff texts stay in Actions artifacts only, because they can name players.
- **Key runs:**

  | Run | What |
  | --- | --- |
  | 37003041437 | first 132-demo study and parser baseline |
  | 37051653572 | analyze pass + Jev on pro play |
  | 37054356072 | full dataset, 541 demos |
  | 37057102040 | model comparison |
  | 37062846092 | parser 6782d49, GATE PASS |

## What's been learned

- **Write-ups:**
  - `.claude/ideas/jev/spec.md` (Match Pulse), `experiments.md` (E0–E6), `ci-eval.md`, `pulse.schema.json`
  - `.claude/ideas/rounds/findings.md` (the pro study and the parser sweep history)
  - `.claude/ideas/jev/rating/` (the shelf rating key and scorer)
- **Private claude.ai pages:**
  - Match Pulse: https://claude.ai/artifact/GMwYW95gASogMofjSrmwDR
  - Round Odds: https://claude.ai/artifact/9bUDbQYXn4oq2fA1f16NAa
  - Round Ratings: https://claude.ai/artifact/NWbaWsk5CNWQrX6GmRUG2C
- **Win probability:**
  - Recalibrated Jev scores Brier 0.134 on matchmaking (T = 0.54).
  - On pro play, a trained 9-feature logistic model wins: 0.146 vs Jev's 0.154, and 0.125 vs Claude's 0.128 vs Jev's 0.131 on 383 shared moments.
  - Jev ranks moments best (highest AUC) at about 1/95 of Claude's cost per call.
  - **Plan:** Jev at launch with the logistic model as fallback, then swap roles once the trained model wins on the self-grading rows.
- **Text:** recaps need an LLM. Claude was accurate in spot checks; gpt-oss invents details. Jev works as a cheap second reader.
- **Embeddings:** with map names removed, a round's bge-m3 neighbours from other matches share its way of winning 98% of the time (chance 30%).
- **The pro study (541 demos):**
  - plant staircase: T win 34% / 66% / 91% when T is down one, level, up one at the plant
  - the opener's side wins 72%
  - an eco beats a full buy 12% of the time
  - map balance runs from Nuke 57% CT to Vertigo 39%
  - economy predicts more than the map does
- **Parser:** main's events-only rules collapsed 113 of 132 tournament demos. Nine builds later, 6782d49 passed the gate. The saved event timelines settled each failure family.

## Open

1. **ADR follow-up.** Parser wants its ADR script, now on main as `tools/adr/adr_rounds.py` (02dfe09; needs demoparser2), run at 6782d49 on the 11 ADR-only demos. Their paths are under `adr_only` in the gate run's report (37062846092).
   - **The user approved running it in the research CI on 2026-10-02.** They said so in this session ("ok given") and again in `cloud`'s.
   - The auto-mode permission check still blocked both copying the script and editing the wiring, as "code from external". A typed OK doesn't clear that check. So get a direct confirmation from the user in your session first, and if the check blocks again, ask them to allow it in their settings. Never route it through another session.
   - The wiring is uncommitted in the worktree: an `adr` config flag in `pick.py`, an ADR step in `run_shard.sh` (output goes to `out/adr/`, with a Steam-id guard), and a demoparser2 venv step in the workflow. Still to do:
     - The plan job must export an `adr` output.
     - Point `run_shard.sh` at the script's real path.
     - Set the config to `parser_sample: "repro"` with the 11 paths and `adr: true`.
   - **Where the output goes:** each output starts with `== <demo file name>`, which carries team names. Either strip that line and add `adr` to the shard tarball for R2, or leave `adr/` in the Actions artifacts only, like the diffs.
   - Nothing blocks on it: prod already ships our ADR.
2. **Shelf ratings.** The user rates 40 rounds on the Round Ratings page (1–5, stored in the page's `ratings` collection). Then:
   - export with `ArtifactData` `list` on collection `ratings` with `out_dir`
   - run `python .claude/ideas/jev/rating/score_ratings.py <out_dir>/ratings`
   - the ship bar is Spearman ≥ 0.6 between Jev's highlight and the ratings
3. **Pulse v2.** Pipeline is building Match Pulse. The gateway facts it needs have been sent:
   - Jev goes through Workers AI; Claude through gateway `default`.
   - Pace at ≤ 4–5 req/s.
   - Code 2021 means fall back to the logistic model; 2018 means back off.
   - The role-swap rule is in the spec.

## Gotchas

- **Rate limit:** the gateway's wholesale limit returns 429 with code 2018 at about 9 req/s. Pace at 4 req/s, and retry with backoff.
- **No credits:** code 2021 means the AI Gateway balance is empty.
- **Jev response shape:** on Workers AI REST, the answer is nested at `result.result.answers`.
- **Claude through AI Gateway:** use `Authorization: Bearer <cloudflare token>`, i.e. the SDK's `auth_token`. Send no `x-api-key` at all: even a placeholder is forwarded to Anthropic and rejected.
- **Event timelines leave the runner only through `events_filter.py`'s allow-list.** The header's server name can carry team names.
- **`demo diff` prints at most 40 mismatches,** so kind counts are lower bounds.
- **Don't name a script after a stdlib module.** `select.py` broke urllib.
- **Old scratch scripts run their analysis on import** (`e2_analyze`, `e4_iterate`).
- **Windows Python can't open `/c/...` paths;** use `C:/...`.
- **The E0–E4 scripts lived in an old session scratchpad and are gone.** The research-branch scripts are the durable ones.

## Next 3 steps

1. Score the shelf ratings once the user has rated (step 2 under Open), and update the spec's eval table: ship or hold the "rounds worth watching" shelf.
2. Confirm the ADR run with the user directly (approved 2026-10-02; step 1 under Open). Then finish and commit the wiring, run it on the 11 ADR-only demos, and send `parser` the output per demo.
3. Re-run the gate on each new parser build, or when the user adds demos. The gate line is the regression check for prod's round files.
