# Pulse eval job (CI)

Spec for pipeline to build in v1.2. Approved by the user on 2026-10-02 (decision 5 in [spec.md](spec.md)). It scores Jev's answers on the two public fixtures and gates the pieces that can change them: the state builder, the question set, and the model behind `typesafe/jev`.

## When it runs

| Trigger | Why |
| --- | --- |
| A PR touching the Pulse state builder, the question set or `pulse.schema.json` | A wording change can move every answer |
| Weekly on `main` (cron) | `typesafe/jev` can change model version with no change on our side |
| `workflow_dispatch` | Manual re-baseline after a model change |

It never runs on PRs that touch neither, so the usual CI stays as it is.

## Inputs

- **Golden states:** under the Pulse test fixtures, generated from the two public fixtures by the real state builder and committed.
  - 228 moment states and 28 round states, about 300 KB.
  - Name-free by construction: players are `p<idx>`.
  - Regenerated, and the diff reviewed in the PR, whenever the builder changes.
  - This lets the eval run without the demo bucket.
- **Ground truth beside them:** each round's winner, and the facts code computes (how it was won, clutch, multi-kill), taken from the fixture's `match.json` and narrative log.
- **End to end, later:** once the `FIXTURES_R2_*` secrets exist, a second mode can rebuild the states from the demos with the parser image first. That checks the builder on fresh parser output as well.
- **Secrets:**
  - `CLOUDFLARE_JEV`: a user API token with Workers AI access, already set in the repo.
  - `CLOUDFLARE_ACCOUNT_ID`, already set.
  - Nothing else.

## What it scores

| Check | Computed over | Fails when |
| --- | --- | --- |
| Win-probability Brier | 228 moments, after the deployed temperature | > 0.16 |
| Win-probability AUC | 228 moments | < 0.85 |
| Calibration error (ECE, 5 bins) | 228 moments | > 0.10 |
| `how_won` cross-check | 28 rounds | < 95% agreement with code |
| Headline precision | rounds whose template has a deterministic test | < 75%; **provisionally < 55% while `state_version` is 1** (kills-only states measured 60% in the experiments, against 80% with economy and the bomb timeline). The script raises it back to 75% when `state_version` reaches 2. |
| Fallback model Brier | the same 228 moments, scored by the 7-weight logistic fallback in code (no calls) | > 0.17 |
| Schema | the `pulse.json` assembled from the answers | fails `pulse.schema.json` |
| Canary determinism | 10 fixed states, asked twice | any answer differs between the two asks: a noul by more than 0.01, a score by more than 0.05, or a different choice. Probability vectors aren't compared. |
| Model id | every response | differs from the id recorded in the fixtures: **warn**, never fail |

- **The thresholds come from the measured public-fixture numbers,** with room for noise: Brier 0.146, AUC 0.88, ECE 0.073 (5 bins), `how_won` 100% and headline 80%. See [experiments.md](experiments.md).
- **On a new model id, the job warns and posts the old and new metrics.** A human re-baselines the fixtures' recorded id with `workflow_dispatch` once the numbers are acceptable.

## Output

- **Job summary:** one table of each metric, its threshold and its value on the last green `main` run, so a PR shows its delta.
- **Artifact `pulse-eval.json`:** the metrics, the model id, the ledger rollup (calls, retries, 429s, p50 and p95, input tokens, cost), and every answer keyed by state sha. That's enough to diff two runs answer by answer.
- **Ledger lines:** one per call, with no state bodies, so a failed run is debuggable from the artifact alone.

## Cost and time

| | Calls | Input tokens | Cost |
| --- | --- | --- | --- |
| Moments (2 questions each) | 228 | ~200k | $0.008 |
| Rounds (4 questions each) | 28 | ~34k | $0.001 |
| Canary, asked twice | 20 | ~16k | < $0.001 |
| **Per run** | **276** | **~250k** | **≈ $0.01** |

- **Time:** about 90 s at 4 calls in flight, paced at ≤ 5 requests/s.
- **Monthly:** weekly runs plus about 10 PR runs come to about $0.15.

## Failure handling

| Failure | Behaviour |
| --- | --- |
| `429 code 2018` (gateway rate limit) | Back off and retry up to 4 times; the run fails only if more than 2% of calls still fail |
| `2021 Insufficient balance` | Fail at once with "AI Gateway credits are empty; top up in the dashboard", and make no further calls |
| Token invalid or missing | Skip with a notice, as the `diff` job does without its fixture secrets, so forks and secretless runs stay green |
| Threshold breach on a PR | The check fails, and merging needs a deliberate re-baseline commit |
| Threshold breach on the weekly run | The run goes red. There's no alerting in v1; the wiki's runbook says what to check |

## Not in scope

- Running on the user's own matches. They stay local and never enter CI, the bucket or the fixtures.
- Scoring highlight quality. That needs human ratings and is a one-off eval, not a gate.
