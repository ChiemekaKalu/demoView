# Jev: Match Pulse

Status: **approved for v1.2; the iterate pass is done.** Written 2026-10-02 by the `aiguy` session under `cloud`. The measurements behind every number here are in [experiments.md](experiments.md): 192 rounds from ten matches, of which 28 are public and 164 are the user's own, run locally. Nothing in `parser/`, `worker/`, `web/` or `schema/` has changed. v1.1 comes first for shared code.

## What Jev is

Jev is TypeSafe AI's "System One" model, on Workers AI as `typesafe/jev`.
- **Input:** a `state` (text or JSON, up to 32k tokens) and named typed questions: `noul` (calibrated yes/no), `choice` (one of up to 255 options) or `score` (ordered levels).
- **Output:** typed answers with probabilities and confidence, in about 0.2 s. It writes no text.
- **Price:** $0.042 per 1M input tokens; output and cached input are free.

It reads a situation and makes a judgment, which is exactly what a parsed round is.

## The feature in three sentences

Match Pulse draws a calibrated win-probability line under every round's scrubber, with each kill sized by how far it moved the odds. A "rounds worth watching" shelf on the match page, picked by Jev's highlight score and linking to each round's biggest swing, follows once human ratings back it. Everything is precomputed after the parse and stored as one small file per match, so the viewer only fetches and draws.

## Verdict: ship the line, hold the shelf

The iterate pass grew the eval from 28 rounds to 192.

1. **The win-probability line clears the ship bar.**
   - Recalibrated, Jev scores Brier 0.134, AUC 0.90 and ECE 0.052 over 1,593 moments. The bar was Brier ≤ 0.16 and ECE ≤ 0.08 on 150+ rounds.
   - It holds on every map: from Overpass at 0.130 to Nuke at 0.159.
   - Tension tracks closeness (r 0.82).
   - It costs about $0.01 and under a minute of background work per match.
2. **Jev's probabilities need recalibration.**
   - Raw, they're underconfident: at 65% the CTs won 85% of the time.
   - One temperature (0.54, stable in every fold) cuts Brier by 0.009 (95% CI −0.014 to −0.004).
   - That constant ships in `pulse.json` and is refitted from the self-grading rows.
3. **A small fitted model ties Jev, so it becomes the fallback.**
   - A 7-feature logistic regression on alive counts, plant and time, fitted on other matches, scores Brier 0.138. Jev's edge over it is within noise.
   - Jev is better where it counts for the viewer. It ranks moments better (AUC +0.026, CI +0.008 to +0.044), which is what picks the biggest swing. It also reads economy at round start, where the small model is blind (AUC 0.71 vs 0.33).
   - So Jev draws the line, and the logistic model draws it whenever Jev is out of credits, rate-limited or down. A Pulse is then never missing, only marked `fallback`.
4. **The shelf isn't proven yet.**
   - Highlight scores track this session's rubric at ρ 0.60, and the top round matched in 3 of 10 matches.
   - Headlines are 68% right unfiltered.
   - The shelf ships only after the user rates about 40 rounds and Jev reaches ρ ≥ 0.6 against those ratings. Headlines always go through the code filter.
5. **Facts stay in code, and the bomb timeline is still needed.**
   - "How it was won" is 99% with the bomb timeline and 66% without. Counting questions fail: comeback 31%.
   - Code computes every fact.
   - The bomb timeline (narrative log v0) feeds the facts and headlines. It doesn't feed the odds: alive counts and time already carry them.
   - Economy (M4) is the one entity input worth having, because it's what makes round start readable.

**Best single idea:** Match Pulse with a self-grading loop. Every parse grades Jev against round outcomes known the moment it ends, so the feature monitors its own calibration and drift in prod for free. The fitted model doubles as its fallback and as the CI baseline.

## What Jev is good for here

Ideas, in order of promise. ✅ marks the ones tested.

| Idea | What it does | Status |
| --- | --- | --- |
| **Match Pulse** ✅ | Win-probability line and swing-sized kill marks per round | Brier 0.134 recalibrated, on 192 rounds. Ships first. |
| **Rounds worth watching** ✅ | Highlight and share scores rank the match's rounds | ρ 0.60 vs rubric on 192 rounds; waits for human ratings |
| **Headline picker** ✅ | Jev picks one of ~11 headline templates, and code fills in names and numbers, so no text generation | 68% precise unfiltered; ships only behind the code filter. Pairs with the narrative log's `narrate(log)`. |
| **Beat the bot** | The replay pauses at a moment, the viewer guesses the winner, and the guess is scored against Jev's probability and the real result | No new calls; reuses Pulse data |
| **Round search** | "Rounds where we lost a 4v2": one Choice over round ids, the line-search pattern from TypeSafe's cookbook | Untested; search is out of scope through v1.1 |
| **Shadow-compare triage** (odd) | During M6's shadow compare, Jev labels each round-file mismatch with a likely cause (yaw wrap, late spawn, grenade id) for a human to confirm | Untested |

Not worth Jev: clutch, comeback, multi-kills, the turning point, alive counts and buy type. Code is exact and free there.

## Where it runs

```text
parse consumer ── match ready ──► pulse queue ──► Worker (pulse consumer)
                                                   1. read match.json + narrative log facts from R2 (live parse_id)
                                                   2. build states (TypeScript port of the scratch builder)
                                                   3. env.AI.run('typesafe/jev'), ≤ 4 in flight, paced ≤ 5 req/s
                                                   4. compute code-side facts (swings, turning point, clutch)
                                                   5. write parsed/{id}/{parse_id}/pulse.json
                                                   6. D1: pulse_status, pulse_model, quality row
viewer ── GET /api/matches/:id/pulse.json?v=<parse_id> ──► R2
```

- **It follows spec principle 2:** precompute at parse time, and the viewer only fetches.
- **Its own queue,** so a Jev outage never delays `ready` or retries a parse.
- **In prod it uses the Worker's `env.AI` binding,** so the API token isn't needed there. `CLOUDFLARE_JEV` becomes the token for a CI eval job and for local experiments.
- **The parser container never calls Jev.** It stays deterministic. It only has to write the facts the states need.
- **A reparse makes a new `parse_id`, which re-runs Pulse.** A new Jev model version can re-run Pulse alone, since it costs cents.

## Data we'd have to collect

| Data | Source | Measured effect | Needed? |
| --- | --- | --- | --- |
| Kills, alive counts, HP | `match.json`, round files (today) | Win probability already at Brier 0.147 raw (192 rounds) | Have it |
| Bomb plant (site, player, tick), defuse start (kit), defuse, explosion | game events: narrative log v0 facts | "How it was won" 66% → 99%; no gain in win probability after the plant (0.097 vs 0.100) | **Yes, minimum** |
| Kill modifiers: wallbang, no-scope, through smoke, attacker blind, flash assist | `player_death` fields: narrative log v0 facts | Feeds highlight and headline | Yes, cheap |
| Team buy, rifles, AWPs, armor and kits at freeze end | entity decoding (M4) | Round-start Brier 0.241 → 0.225, and sharper predictions | Nice to have |
| Callout names (`m_szLastPlaceName`) | entity decoding (M4) | Readable states; no measured accuracy gain | Nice to have |
| Player names, Steam ids | — | Prompt-injection surface | **Never in state** |

## State format

One JSON object per call, under 1.1k input tokens with questions included. The rules come from the experiments and TypeSafe's jaggedness notes:
- Players are `p<idx>` only. Names and Steam ids never enter a state.
- **Code does all counting.** Every timeline line carries the alive count after it, and `now.alive` repeats it.
- Words, not numbers, where Jev reads numbers poorly: HP buckets ("full HP", "hurt", "low HP", "one shot from death"), buy buckets, time left as a phrase.
- A moment state holds only events up to that moment. No future lines, no result.
- No irrelevant detail: no per-tick positions, no grenade paths.

Examples:
- [state/valve_mm_mirage_2024_r06_moment.json](state/valve_mm_mirage_2024_r06_moment.json): a moment, Mirage r6 after the 4th kill, 2 CT v 4 T, bomb down.
- [state/faceit_nuke_2026_r09_round.json](state/faceit_nuke_2026_r09_round.json): a full round with line ids and the result, Nuke r9, the 1v2 clutch.

```json
{
  "map": "de_mirage", "round": "6", "score_before_round": "CT 2 - T 3",
  "players": {"CT": ["p0", "p1", "p2", "p3", "p4"], "T": ["p5", "p6", "p7", "p8", "p9"]},
  "economy": {"CT": {"buy": "full buy", "rifles": 5, "awps": 1, "armor": 5, "kits": 2}, "T": {"buy": "full buy", "rifles": 4, "awps": 1, "armor": 4}},
  "timeline": ["0:51 p7 (T) plants the bomb at bombsite A. The CTs have 40 seconds to defuse.",
               "0:56 p2 (CT) kills p6 (T) at BombsiteA (M4A1). Alive: 2 CT v 4 T."],
  "now": {"time": "bomb planted, about 35 seconds until it explodes", "alive": "2 CT v 4 T",
          "alive_players": {"CT": ["p0 hurt at PalaceAlley", "p2 full HP at Catwalk"], "T": ["p5 full HP at BombsiteA", "..."]}}
}
```

## Question set

**Moment call:** at round start (freeze end) and after every in-round kill, about 8 per round.

| Name | Type | Instructions | Criteria |
| --- | --- | --- | --- |
| `ct_wins` | noul | Will the CT side win this round from this moment? | true: "The CTs win: they eliminate every T, defuse the bomb, or run out the clock with no plant". false: "The Ts win: they eliminate every CT or the bomb explodes" |
| `tension` | score | How tense is this moment for a spectator? | "Decided: one side clearly has it" · "Leaning one way" · "Open: either side could win" · "Knife-edge: one duel decides it" |

**Round call:** one per round, on the full round with line ids and the result.

| Name | Type | Instructions | Criteria |
| --- | --- | --- | --- |
| `highlight` | score | How worth rewatching is this round for someone who plays CS2? | "Routine: one side rolled over the other with nothing notable" · "Some good plays, but a normal round" · "A notable play: a multi-kill, a clean retake, or a tense finish" · "A standout: a clutch, a comeback, or a player taking over the round" · "Instant highlight: an ace, a 1v3 or bigger clutch, or a wild comeback" |
| `share` | noul | Would a player in this round want to send a clip of it to friends? | — |
| `headline` | choice | Which headline fits this round best? | 11 templates, e.g. `clutch_big`: "The last player alive on the winning side beat two or more opponents"; `retake`: "The CTs retook the site after the plant". Code drops a pick whose deterministic test fails. |
| `how_won` | choice | How did the winning side win this round? Use the result and the timeline. | 5 options (`t_post_plant`, `ct_retake`, `t_before_plant`, `ct_hold`, `ct_time`). Kept only as a cross-check against code; a disagreement is logged. |

**Code computes, and never asks:** clutch and its size, comeback, multi-kills, buy type, the turning point (the biggest |Δp| between moments), and per-player win probability added (the decisive player).

**Recalibration:** raw Jev is underconfident, so code applies `p_ct = sigmoid(logit(p_raw) / T)`.
- It starts at **T = 0.54**, fitted on the 1,593 eval moments; every leave-one-match-out fold gave 0.52–0.54.
- It's refitted from the self-grading rows when the model id changes, or every 1,000 new graded moments.
- A refit is a config change, not a reparse. `pulse.json` records the T it used.

**Fallback model:** a 7-feature logistic regression on alive CT, alive T, their difference, planted, planted × alive CT, and time left. Seven weights live in code, fitted on the eval set and refitted with T.
- It draws the line whenever Jev fails for a match: credits, rate limit after retries, or an outage.
- That file gets `"model": "fallback-logit-v1"`, no highlight or headline, and the shelf skips the match.
- It scored Brier 0.138 on held-out matches, against 0.134 for recalibrated Jev, so the viewer stays useful.

## Output: `pulse.json`

A new file with its own `pulse_version`; the full proposal is [pulse.schema.json](pulse.schema.json). It keeps the raw Jev answer beside the calibrated one, for self-grading. `match.json` and round files don't change.

```json
{
  "pulse_version": 1, "model": "jev-1.13.0", "state_version": 1, "parse_id": "…",
  "calibration": {"temperature": 0.54},
  "rounds": [
    { "n": 9,
      "moments": [{"tick": 72446, "p_ct": 0.59, "p_raw": 0.55, "tension": 1.5},
                  {"tick": 73977, "p_ct": 0.32, "p_raw": 0.40, "tension": 1.3}, "…",
                  {"tick": 77084, "p_ct": 0.09, "p_raw": 0.22, "tension": 1.7},
                  {"tick": 77115, "p_ct": 0.46, "p_raw": 0.48, "tension": 2.9},
                  {"tick": 77396, "p_ct": 0.92, "p_raw": 0.79, "tension": 0.7}],
      "swing_tick": 77396, "highlight": 3.63, "share": 0.82, "headline": "clutch_big",
      "facts": {"clutch": "1v2", "multikill": 4, "comeback": true, "how_won": "ct_retake"} }
  ],
  "worth_watching": [9, 18, 12]
}
```

These are Jev's real answers for Nuke r9, the 1v2 clutch. After recalibration, the CTs fall to 9%, the clutch kill makes it a knife-edge 1v1 (tension 2.9 of 3), and the last kill lifts them to 92% with the defuse still to do. `worth_watching` is Jev's real top three for that match; r18, a one-sided hold, is the pick a human rating would likely drop. The file is about 15 KB per match before compression.

## Viewer

- **Replay (ships first):** a thin line under the scrubber shows P(CT wins) over the round. Each kill marker is sized by |Δp|. Hovering shows "CT 41% → 12%". The biggest swing gets a ⚡ that seeks to that moment, and a tension strip runs beneath.
- **Match page (after the human-rating gate):** a "rounds worth watching" shelf with the top three by `highlight`, each with a headline that passed the code filter. A click opens `/m/:id/r/:n?t=<swing seconds>`.
- **No Pulse yet:** the strip and shelf don't render, and nothing else changes.
- **Pulse from the fallback model:** the line renders with a quiet "estimated" note, and the shelf skips the match.
- **Mock:** a private claude.ai page, Match Pulse, renders this from the public fixtures' real answers.

## Cost and latency

| | Per round | Per 24-round match |
| --- | --- | --- |
| Moment calls | ~8 × ~870 tokens | ~195 calls, ~170k tokens |
| Round call | 1 × ~1.7k tokens | 24 calls, ~41k tokens |
| **Total** | | **~220 calls, ~210k tokens, ≈ $0.009** |

- **Latency:** p50 185 ms and p95 300–530 ms per call. Paced at 5 requests/s under the gateway limit, a match takes about 45 s, after `ready`.
- **Scale:** 100 matches a month is about $0.90 of AI Gateway credit. Set a gateway spend limit.

## Token budget

- **Fixed overhead:** about 270 input tokens per call.
- **Moment calls:** 740–870 on average, at most 1,063.
- **Round calls with four questions:** about 1.2k. The nine-question experiment set averaged 1.67k, at most 1.86k.
- **Headroom:** a whole match as one state is about 4.5k tokens (Nuke, 19 rounds). Everything fits far under the 32k limit, so match-level questions such as an MVP or a match story are possible later.

## Failure modes

| Failure | Seen? | Handling |
| --- | --- | --- |
| Credits exhausted (`2021 Insufficient balance`) | Yes, first call | The fallback model draws the line, the result is logged as `fallback:credits`, no retry, the match stays `ready` |
| Gateway rate limit (`429 code 2018`) | Yes: ~10% of calls at 4 in flight | Pace at ≤ 5 req/s, back off and retry up to 4 times; if a match still fails, the fallback draws it, and the queue retries Jev later |
| Model version change | Not yet | `model` stored per file and per quality row; a new version re-runs Pulse, and calibration is compared before and after |
| Prompt injection via names | Tested: +0.025 mean, 1/40 flips | Names never enter a state |
| Future leaking into a moment | Tested: the shortcut does worse, not better | One call per moment, built from events up to the cut |
| Confident wrong counts | Tested: comeback 31% on 192 rounds | Counting questions are never asked |
| Underconfident odds | Tested: at 65% the CTs won 85% | Temperature 0.54, refitted from self-grading |
| Cost runaway | — | At most 400 calls per match, a monthly token cap in the Worker, a gateway spend limit |
| Jev down | — | The fallback model draws the line; Pulse has its own queue and status, so `ready` never waits |

## Observability

The user asked for this explicitly, and the experiment ledger proved its worth: it caught the billing failure and the rate limit before any answer looked wrong.

- **Per match:** one Workers Logs line:
  ```json
  {"event":"pulse","match_id":"…","parse_id":"…","model":"jev-1.13.0","calls":219,"errors":0,"retries":2,"http_429":2,"in_tokens":208113,"usd":0.0087,"p50_ms":184,"p95_ms":410,"wall_ms":46210,"result":"ok"}
  ```
- **Per match:** one Analytics Engine point (calls, tokens, cost, p95, errors, model) for a cost-and-latency view over time.
- **AI Gateway:** its own analytics and logs (requests, tokens, cost, errors) come with the Unified Billing path at no cost. Turn on its logging and spend limit for both environments.
- **Self-grading quality:** after each Pulse, the Worker scores the match's moments against the known round winners. It writes one D1 row, `pulse_quality(match_id, parse_id, model, moments, brier, logloss, ece_bins)`.
  - A rollup query gives Brier and calibration by week and by model version. That's drift detection for free whenever TypeSafe ships a new Jev.
  - The cross-checks (`how_won` vs code, a `headline` that fails its test) are counted in the same row.
- **Debug, dev only:** a flag writes every request and response for a match to `debug/pulse/{id}/{parse_id}.ndjson` in R2. That's safe because states carry no names. Never in prod.
- **CI eval:** a job using `CLOUDFLARE_JEV` scores golden states from the two public fixtures whenever the state builder or questions change, and weekly to catch a model change. About $0.01 per run. The full spec is [ci-eval.md](ci-eval.md).

## Eval plan

| What | Ground truth | Ship bar | Result (192 rounds) |
| --- | --- | --- | --- |
| Win probability | Round winner | Brier ≤ 0.16 and ECE ≤ 0.08 after recalibration, on ≥ 150 rounds | **Met:** Brier 0.134, ECE 0.052, AUC 0.90 |
| `how_won` cross-check | Computed from facts | ≥ 95% | **Met:** 99% |
| Highlight, share | Human ratings: the user scores ≥ 40 rounds 0–4, blind to Jev | ρ ≥ 0.6, and the shelf's picks judged good in ≥ 2 of 3 matches | **Open:** ρ 0.60 vs this session's rubric; no human ratings yet |
| Headline | Deterministic tests where they exist, human judgment where not | ≥ 85% after code filtering | **Open:** 68% unfiltered; the filter removes the checkable misses |
| Calibration in prod | Self-grading rows | Weekly Brier and ECE within 0.02 of the eval | After launch |

**Next step for the shelf:** a rating pass. The user scores about 40 rounds 0–4 from the replay, blind to Jev, then the Spearman is computed. One sitting, about 30 minutes.

## Decisions (2026-10-02)

1. **Scope: approved.** Match Pulse is in v1.2. Win probability stays out through v1.1.
2. **Schema: approved.** A new `pulse.json` with its own `pulse_version`. The proposal is [pulse.schema.json](pulse.schema.json); parser lands it in `schema/`. `match.json` and round files are untouched.
3. **Eval data: approved.** The user confirmed directly: run locally on their 8 matches, with name-free states. Only aggregate numbers are committed.
4. **Budget: approved.** Up to $12.50 of the $25 AI Gateway credit for this exploration. Prod needs about $0.01 per match; set a gateway spend limit before launch.
5. **CI: approved.** The eval job is specified in [ci-eval.md](ci-eval.md), for pipeline to build in v1.2.

## Open questions

- Does `env.AI.run` from a Worker hit the same wholesale gateway limit as the REST API? Measure it on dev.
- Should Pulse wait for M4 entity data, or ship on `events`-tier states first? Over 192 rounds the gain from entity data is borderline overall (Brier −0.005, CI −0.009 to +0.001), but clear at round start (AUC 0.62 → 0.71). `events` is enough to launch; economy is the one M4 input worth adding.
- Can the Workers AI model id be pinned to a version, or does `typesafe/jev` always mean the latest? Every response reports `jev-1.13.0` today.
