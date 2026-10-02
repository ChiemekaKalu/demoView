# Round dynamics in pro CS2

What decides a round, how much it depends on the map, and which situations are balanced. Written 2026-10-02 by the `aiguy` session under `cloud`, from GitHub Actions run 37003041437 on branch `research/bulk-demos`.

## Data and caveats

- **Sample:** 132 public pro demos from the Hugging Face dataset `skkwowee/chimera-cs2`, at revision `69b9cbaafa`.
  - Map-balanced: 16 each of Mirage, Dust2, Inferno, Nuke, Overpass and Anubis, 14 usable on Ancient, and all available on Train (11) and Vertigo (9).
  - 130 demos had rounds: 2,700 rounds, 2,575 of them regulation, and 20,491 decision moments (round start plus every kill).
- **These are pro GOTV demos.** Tournament play has coordinated utility, stacked economies and fewer throws. The numbers may not transfer to matchmaking or Premier.
- **Facts come from demoinfocs,** the same library as the oracle, through a name-free extractor. Players appear only as a per-demo index. The run happened entirely in the cloud: each demo was streamed, read and deleted. The results live in `demoview-dev` under `research/bulk/37003041437/`.
- **Win rates come with Wilson 95% intervals.** Train and Vertigo have about 200 rounds each, so their intervals are wide (±7 points).
- **Events like "threw more smokes" are correlations, not causes.** Players who stay alive longer throw more utility, so these partly measure who was already winning.

## Headlines

1. **The man advantage at the plant decides post-plants.** A T side that plants up a player wins 91%, up two 98%. Planting while down one wins 34%, down two 9%.
2. **The opening kill is worth about 21 points.** The side that gets it wins 71% (CT 70%, T 72%). Trading it back within 5 seconds lifts the side that lost the opener from about 29% to 43%.
3. **Economy dominates before the first kill.** An eco beats a full buy 12% of the time; a force buy, 34%. The pistol-round winner wins the next round 80% of the time.
4. **Map balance spans 20 points.** Nuke is the most CT-sided map (59% CT), and Vertigo (39%) and Anubis (42%) are T-sided.
5. **Pistol rounds are coin flips:** CT 52% overall. So is the T side's flash advantage (52%).
6. **Knowing the map barely improves predictions.** Once alive counts, the bomb and time are known, adding the map changes Brier from 0.1466 to 0.1464. Adding economy improves it to 0.139. Anubis is the exception, because of its T-side lean.

## Map balance (regulation rounds)

| Map | Demos | Rounds | CT win | 95% CI | Pistol CT win | Plant rate |
| --- | --- | --- | --- | --- | --- | --- |
| Nuke | 16 | 309 | 59% | 53–64% | 61% | 42% |
| Mirage | 16 | 340 | 57% | 52–62% | 50% | 51% |
| Overpass | 16 | 338 | 55% | 50–60% | 64% | 40% |
| Train | 11 | 210 | 54% | 48–61% | 62% | 50% |
| Ancient | 14 | 260 | 54% | 48–60% | 44% | 57% |
| Dust2 | 16 | 307 | 53% | 47–58% | 55% | 50% |
| Inferno | 16 | 281 | 52% | 46–57% | 57% | 59% |
| Anubis | 16 | 346 | 42% | 37–47% | 34% | 57% |
| Vertigo | 9 | 184 | 39% | 32–46% | 39% | 55% |
| **All** | 130 | 2,575 | 52% | 50–54% | 52% | 51% |

Pistol-round samples are about 30 per map, so those cells are ±17 points.

## Events, from least to most balanced

The win rate for the side the event favours, all maps. "Spread" is the gap between the highest and lowest map (maps with n ≥ 40).

| Event | n | Win rate | 95% CI | Spread across maps |
| --- | --- | --- | --- | --- |
| T plants with a man advantage | 788 | 96% | 94–97% | 5 pts |
| Eco vs full buy (eco side) | 503 | 12% | 10–16% | 12 pts |
| Bomb planted (T) | 1,310 | 78% | 75–80% | 15 pts |
| Plant at B (T) | 645 | 77% | 73–80% | **31 pts** |
| Flash-assisted opening kill | 269 | 74% | 68–79% | — |
| Opening kill (T) | 1,088 | 73% | 70–75% | 19 pts |
| T plants while down players | 226 | 27% | 22–34% | — |
| Opening kill with an AWP | 318 | 72% | 67–77% | 6 pts |
| Opening kill (CT) | 1,483 | 70% | 68–72% | 18 pts |
| T has more AWPs | 250 | 67% | 61–73% | 5 pts |
| Won the previous round (CT) | 1,207 | 67% | 64–70% | 11 pts |
| Won the previous round (T) | 1,119 | 64% | 61–67% | 17 pts |
| CT has more AWPs | 806 | 61% | 57–64% | 24 pts |
| Opening kill traded within 5 s (traded side) | 669 | 43% | 40–47% | 23 pts |
| Pistol round (CT) | 249 | 52% | 46–58% | — |

Utility counts ("more smokes thrown", 64–74%) are left out of the headlines because survivors throw more, which confounds them.

## Man advantage and clutches

The CTs' chance to win after a kill, no bomb planted:

| State (CT v T) | All | Lowest map | Highest map |
| --- | --- | --- | --- |
| 5v4 | 71% | Anubis 58% | Nuke 77% |
| 4v5 | 28% | Vertigo 18% | Ancient 35% |
| 4v4 | 48% | Anubis and Vertigo 38% | Mirage 56% |
| 3v3 | 48% | Anubis 35% | Mirage 58% |
| 2v2 | 54% | Anubis 35% | Ancient 66% |
| 1v1 | 69% | | |
| 2v1 / 1v2 | 91% / 22% | | |

- **The man advantage converts differently by map.** A CT 5v4 converts 77% of the time on Nuke but 58% on Anubis. On Anubis, even trades favour the Ts at every level.
- **After a plant, the odds flip toward the T side:**
  - 2v2 is 30% for the CTs and 1v1 is 33%.
  - A 1v2 retake succeeds 5% of the time.
  - Holding the plant 2v1 down a player, Ts still win 30%.

## After the plant

| Map | Plants | T win | Site A | Site B | Mean plant time | Defuses with a kit |
| --- | --- | --- | --- | --- | --- | --- |
| Anubis | 208 | 85% | 83% | 86% | 1:19 | 48% |
| Overpass | 138 | 83% | 76% | **90%** | 1:22 | 39% |
| Vertigo | 108 | 83% | 84% | 83% | 1:13 | 56% |
| Dust2 | 155 | 78% | 81% | 74% | 1:21 | 56% |
| Inferno | 169 | 77% | 76% | 78% | 1:22 | 51% |
| Nuke | 141 | 76% | 80% | 72% | 1:13 | 74% |
| Train | 113 | 71% | **86%** | **59%** | 1:14 | 42% |
| Ancient | 148 | 70% | 71% | 70% | 1:10 | 77% |
| Mirage | 196 | 70% | 68% | 73% | 1:15 | 75% |

**T win rate by (T alive − CT alive) at the plant:**

| T alive − CT alive | −3 | −2 | −1 | 0 | +1 | +2 | +3 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| T win | 0% | 9% | 34% | 67% | 91% | 98% | 100% |

- **Site matters most on Train:** an A plant holds 86% of the time, a B plant 59%.
- **Overpass B is the strongest plant in the sample:** 90%.

## Economy

| Map | Eco beats full | Force beats full | Pistol winner wins the next round |
| --- | --- | --- | --- |
| Train | 11% | **46%** | 62% |
| Inferno | 18% | 39% | 63% |
| Vertigo | 7% | 38% | 78% |
| Anubis | 10% | 36% | 88% |
| Nuke | 11% | 36% | 81% |
| Mirage | 9% | 30% | 81% |
| Overpass | 14% | 30% | 87% |
| Dust2 | 19% | 29% | 77% |
| Ancient | 12% | 21% | 100% (n=25) |
| **All** | 12% | 34% | 80% |

The CTs' win rate by matchup, all maps:

| Matchup | CT win |
| --- | --- |
| CT full v T full | 53% |
| CT full v T force | 62% |
| CT full v T semi | 78% |
| CT full v T eco | 94% |
| CT force v T full | 29% |
| CT semi v T full | 20% |
| CT eco v T full | 3% |

## Predictability

A logistic model trained on four demo groups and tested on the fifth. Brier score, lower is better.

| Map | Moments | Base (alive, bomb, time) | + map | + economy | Round start only (+ economy) |
| --- | --- | --- | --- | --- | --- |
| Vertigo | 1,502 | 0.137 | 0.139 | **0.128** | 0.205 |
| Nuke | 2,463 | 0.139 | 0.139 | 0.134 | 0.226 |
| Train | 1,665 | 0.142 | 0.144 | 0.138 | 0.227 |
| Mirage | 3,050 | 0.147 | 0.147 | 0.138 | 0.217 |
| Overpass | 2,626 | 0.148 | 0.146 | 0.140 | 0.221 |
| Dust2 | 2,365 | 0.148 | 0.149 | 0.139 | 0.220 |
| Inferno | 2,113 | 0.148 | 0.148 | 0.148 | 0.232 |
| Anubis | 2,707 | 0.152 | 0.146 | 0.141 | 0.216 |
| Ancient | 2,000 | **0.155** | 0.159 | 0.143 | 0.212 |
| **All** | 20,491 | 0.147 | 0.146 | 0.139 | |

- **Most predictable:** Vertigo and Nuke, where the advantage converts.
- **Least predictable:** Ancient, where rounds stay open longest.
- **Economy is worth more than the map** everywhere except Inferno, where it adds nothing.
- **For Match Pulse:** a map-agnostic model plus economy is the right shape. A per-map term only pays off on T-sided maps.

## Our parser on pro demos

- The same run put every demo through our `demo parse` and `demo diff` against the oracle.
- **Speed and memory are no concern:** at most 2.6 s and 61 MB.
- **Round logic collapsed on 113 of 132 demos,** across game builds 9961 to 10847. Ours finds exactly 1 round where the reference finds 18 or more. The cause is match rules tuned on matchmaking, Premier and FACEIT servers, which tournament GOTV breaks.
- **Also:**
  - 2 demos failed with "no live rounds"
  - 9 swapped team assignments, mostly on build 9842
  - only 5 diffed clean
- **Status:** sent to the parser session with dataset paths. It's now the parser's top priority and gates the M4 round-file swap. A parser-only re-run on the same sample is the regression check.

## Next

- A parser-only workflow mode, so parser fixes are checked in the cloud on all 132 demos.
- **More analysis, re-run from the stored facts with no re-download:**
  - where opening duels happen on each map, and how much each spot decides
  - plant timing
  - clutch odds by map
- Optionally, Jev on a pro sample, to see whether its calibration (T = 0.54, fitted on matchmaking) holds for pro play.
