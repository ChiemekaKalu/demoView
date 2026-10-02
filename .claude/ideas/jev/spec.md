# Jev: Match Pulse

Status: **exploration, not approved.** Written 2026-10-02 by the `aiguy` session under `cloud`. The measurements behind every number here are in [experiments.md](experiments.md). Nothing in `parser/`, `worker/`, `web/` or `schema/` has changed.

## What Jev is

Jev is TypeSafe AI's "System One" model, on Workers AI as `typesafe/jev`.
- **Input:** a `state` (text or JSON, up to 32k tokens) and named typed questions: `noul` (calibrated yes/no), `choice` (one of up to 255 options) or `score` (ordered levels).
- **Output:** typed answers with probabilities and confidence, in about 0.2 s. It writes no text.
- **Price:** $0.042 per 1M input tokens; output and cached input are free.

It reads a situation and makes a judgment, which is exactly what a parsed round is.

## The feature in three sentences

Match Pulse draws a calibrated win-probability line under every round's scrubber, with each kill sized by how far it moved the odds. The match page gets a "rounds worth watching" shelf, picked by Jev's highlight score, where each pick links straight to that round's biggest swing. Everything is precomputed after the parse and stored as one small file per match, so the viewer only fetches and draws.

## Verdict: iterate once, then ship as a v1.2 add-on

1. **The core signal is real and cheap.**
   - Zero-shot win probability scores Brier 0.146 and AUC 0.88, and it's calibrated (ECE 0.07).
   - Tension tracks closeness (r 0.82).
   - A match costs about $0.01 and under a minute of background work.
2. **The evidence is thin.**
   - It's 28 rounds from two demos.
   - On those, a logistic model on alive counts matched Jev's win probability.
   - The highlight eval used this session's rubric, not human ratings.
   - Jev's edge, zero setup and rich context with no feature engineering, needs a bigger, human-rated eval to prove.
3. **Facts belong in code.** Jev got "how it was won" and "clutch" 100% right, but code computes them exactly. It fails at counting and comparing ("comeback" 29%, turning point near random). Jev should answer only judgment questions.
4. **It needs the narrative log first.** The bomb timeline takes "how it was won" from 64% to 100% and helps after the plant. Economy helps at round start. Those facts come from the narrative log (v1.2) and entity decoding (M4), so Pulse fits after them.
5. **Operations are manageable, and the failure modes are known.**
   - The good: deterministic, p50 185 ms, small injection effect when names stay out.
   - Prepaid credits can run dry (code 2021).
   - The gateway rate-limits at about 9 requests/s.
   - The model version can change under us.
   - The design below makes Pulse optional and observable, so none of these can hold a match back from `ready`.

**Best single idea:** Match Pulse with a self-grading loop. Every match grades Jev, because the round outcomes are known once the parse ends. So the feature monitors its own quality in prod for free.

## What Jev is good for here

Ideas, in order of promise. ✅ marks the ones tested.

| Idea | What it does | Status |
| --- | --- | --- |
| **Match Pulse** ✅ | Win-probability line and swing-sized kill marks per round | Brier 0.146, calibrated. The recommendation. |
| **Rounds worth watching** ✅ | Highlight and share scores rank the match's rounds | ρ 0.74 vs rubric; needs human ratings |
| **Headline picker** ✅ | Jev picks one of ~11 headline templates, and code fills in names and numbers, so no text generation | 80% precise with the bomb timeline. Pairs with the narrative log's `narrate(log)`. |
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
| Kills, alive counts, HP | `match.json`, round files (today) | Win probability already at Brier 0.150 | Have it |
| Bomb plant (site, player, tick), defuse start (kit), defuse, explosion | game events: narrative log v0 facts | "How it was won" 64% → 100%; Brier after plant 0.090 → 0.084 | **Yes, minimum** |
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

**Recalibration:** Jev is slightly underconfident at the extremes. Code applies a one-parameter temperature, refitted from the self-grading data once at least 500 graded moments exist. Until then it's identity.

## Output: `pulse.json`

A new file, so it needs a new `schema/pulse.schema.json` with its own `pulse_version`. That's the user's call, since the schema is the contract. `match.json` and round files don't change.

```json
{
  "pulse_version": 1, "model": "jev-1.13.0", "state_version": 1, "parse_id": "…",
  "rounds": [
    { "n": 9,
      "moments": [{"tick": 72446, "p_ct": 0.55, "tension": 1.5}, {"tick": 73977, "p_ct": 0.40, "tension": 1.3},
                  "…", {"tick": 77084, "p_ct": 0.22, "tension": 1.7}, {"tick": 77115, "p_ct": 0.48, "tension": 2.9},
                  {"tick": 77396, "p_ct": 0.79, "tension": 0.7}],
      "swing_tick": 77396, "highlight": 3.63, "share": 0.82, "headline": "clutch_big",
      "facts": {"clutch": "1v2", "multikill": 4, "how_won": "ct_retake"} }
  ],
  "worth_watching": [9, 18, 12]
}
```

These are Jev's real answers for Nuke r9, the 1v2 clutch: the CTs fall to 22%, the clutch kill makes it a knife-edge 1v1 (tension 2.9 of 3), and the last kill lifts them to 79% with the defuse still to do. `worth_watching` is Jev's real top three for that match; r18, a one-sided hold, is the pick a human rating would likely drop. The file is about 15 KB per match before compression.

## Viewer

- **Replay:** a thin line under the scrubber shows P(CT wins) over the round. Each kill marker is sized by |Δp|. Hovering shows "CT 41% → 12%". The biggest swing gets a ⚡ that seeks to that moment.
- **Match page:** a "rounds worth watching" shelf with the top three by `highlight`, each with its headline. A click opens `/m/:id/r/:n?t=<swing seconds>`.
- **No Pulse yet, or Pulse failed:** the strip and shelf don't render, and nothing else changes.

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
| Credits exhausted (`2021 Insufficient balance`) | Yes, first call | Pulse `failed:credits`, no retry, the match stays `ready`, the counter shows up in observability |
| Gateway rate limit (`429 code 2018`) | Yes, ~9 req/s | Pace at ≤ 5 req/s, back off and retry up to 3 times, then let the queue retry the whole match |
| Model version change | Not yet | `model` stored per file and per quality row; a new version re-runs Pulse, and calibration is compared before and after |
| Prompt injection via names | Tested: +0.025 mean, 1/40 flips | Names never enter a state |
| Future leaking into a moment | Tested: the shortcut does worse, not better | One call per moment, built from events up to the cut |
| Confident wrong counts | Tested: comeback 29% | Counting questions are never asked |
| Cost runaway | — | At most 400 calls per match, a monthly token cap in the Worker, a gateway spend limit |
| Jev down | — | Pulse is optional: its own queue and status, and the viewer hides it |

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
- **CI eval:** a job using `CLOUDFLARE_JEV` re-runs E1 on the two public fixtures when the state builder or questions change. It fails if Brier rises above 0.16 or ECE above 0.10. That's about 200k tokens per run, under 1 cent.

## Eval plan

| What | Ground truth | Metric | Ship bar |
| --- | --- | --- | --- |
| Win probability | Round winner | Brier, log loss, AUC, ECE, reliability bins; bootstrap by round | Brier ≤ 0.16 and ECE ≤ 0.08 after recalibration, on ≥ 150 rounds |
| `how_won`, clutch, comeback, multi-kill | Computed from facts | Accuracy (these stay in code; the eval only checks the states read right) | `how_won` ≥ 95% as a cross-check |
| Highlight, share | Human ratings: the user scores ≥ 40 rounds 0–4, blind to Jev | Spearman; top-3 overlap per match | ρ ≥ 0.6, and the shelf's picks judged good in ≥ 2 of 3 matches |
| Headline | Deterministic tests where they exist, human judgment where not | Precision | ≥ 85% after code filtering |
| Calibration in prod | Self-grading rows | Weekly Brier and ECE by model | Within 0.02 of the eval |

The public fixtures give only 28 rounds. The user's 8 local matches would give about 200 and pass the bar's sample size. Running Jev on them locally is the user's call (decision 3 below).

## Decisions for the user

1. **Scope:** Match Pulse as a v1.2 candidate. Win probability is out of scope through v1.1, so this is a scope change.
2. **Schema:** a new `pulse.json` output with its own `schema/pulse.schema.json` and `pulse_version`. `match.json` and round files are untouched.
3. **Eval data:** may the eval run locally on your own 8 matches? States carry no names, and Jev has zero data retention. Results stay local, and only aggregate numbers go in the notes.
4. **Budget:** about $0.01 per match from AI Gateway credits, with a gateway spend limit (suggested: $5 a month).
5. **CI:** use `CLOUDFLARE_JEV` for a CI eval job (owned by pipeline).

## Open questions

- Does `env.AI.run` from a Worker hit the same wholesale gateway limit as the REST API? Measure it on dev.
- Should Pulse wait for M4 entity data, or ship on `events`-tier states first? The measured gain from entity data is small, so `events` is probably enough.
- Can the Workers AI model id be pinned to a version, or does `typesafe/jev` always mean the latest? Every response reports `jev-1.13.0` today.
