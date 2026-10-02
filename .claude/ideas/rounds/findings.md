# Round dynamics in pro CS2

What decides a round, how much it depends on the map, and which situations are balanced. Written 2026-10-02 by the `aiguy` session under `cloud`. The numbers come from the full-dataset run 37054356072 on branch `research/bulk-demos`. A first pass on 130 demos (runs 37003041437 and 37051653572) pointed the same way. One map moved: Dust2 read 53% CT on the small sample and is 48% here.

## Data and caveats

- **Sample:** every usable demo in the public Hugging Face dataset `skkwowee/chimera-cs2`, at revision `69b9cbaafa`. That's 541 demos, 11,423 rounds (10,862 of them regulation) and 86,474 decision moments (round start plus every kill), across nine maps.
- **These are pro GOTV demos.** Tournament play has coordinated utility, stacked economies and fewer throws. The numbers may not transfer to matchmaking or Premier.
- **Facts come from demoinfocs,** the same library as the oracle, through a name-free extractor. Players appear only as a per-demo index. The run happened entirely in GitHub Actions: each demo was streamed, read and deleted. Results live in `demoview-dev` under `research/bulk/<run>/`.
- **Win rates come with Wilson 95% intervals.** Train (11 demos) and Vertigo (9) are the whole dataset's supply for those maps, so their intervals stay wide (±7 points).
- **Events like "threw more smokes" are correlations, not causes.** Players who stay alive longer throw more utility, so these partly measure who was already winning.

## Headlines

1. **The man advantage at the plant decides post-plants.**

   | T alive − CT alive at the plant | −3 | −2 | −1 | 0 | +1 | +2 | +3 |
   | --- | --- | --- | --- | --- | --- | --- | --- |
   | T win | 2% | 14% | 34% | 66% | 91% | 98% | 100% |

   Across all plants with a T advantage of any size, the Ts win 96%.
2. **The opening kill is worth about 22 points.** The side that gets it wins 72% (CT 71%, T 73%). Trading it back within 5 seconds lifts the side that lost the opener from about 28% to 41%.
3. **Economy dominates before the first kill.**
   - An eco beats a full buy 12% of the time; a force buy, 34%.
   - A full-buy CT side beats a T eco 97% of the time.
   - The pistol-round winner takes the next round 80% of the time.
4. **Map balance spans 18 points.** Nuke is the most CT-sided map (57%), Mirage and Inferno follow, and Vertigo (39%), Anubis (44%) and Dust2 (48%) lean T.
5. **Pistol rounds are exact coin flips** (CT 50%), and so is the T side's flash advantage (53%).
6. **Clutches are rare.** A lone T wins a 1v1 71% of the time and a lone CT 58%; 1v2s are won 15–24% of the time; a 1v3 or worse, 1–2%.
7. **Knowing the map barely improves predictions.** Once alive counts, the bomb and time are known, adding the map moves Brier from 0.1465 to 0.1460. Adding each side's economy moves it to 0.1377, and it helps on every map.

## Map balance (regulation rounds)

| Map | Demos | Rounds | CT win | 95% CI | Pistol CT win | Plant rate |
| --- | --- | --- | --- | --- | --- | --- |
| Nuke | 82 | 1,707 | 57% | 55–60% | 55% | 45% |
| Mirage | 126 | 2,608 | 56% | 54–58% | 52% | 47% |
| Inferno | 83 | 1,594 | 55% | 52–57% | 46% | 56% |
| Train | 11 | 210 | 54% | 48–61% | 62% | 50% |
| Overpass | 29 | 623 | 53% | 49–57% | 57% | 42% |
| Ancient | 60 | 1,152 | 53% | 50–56% | 45% | 54% |
| Dust2 | 104 | 2,065 | 48% | 46–50% | 49% | 52% |
| Anubis | 37 | 719 | 44% | 40–47% | 41% | 61% |
| Vertigo | 9 | 184 | 39% | 32–46% | 39% | 55% |
| **All** | 541 | 10,862 | 53% | 52–54% | 50% | 50% |

## Events, from least to most balanced

The win rate for the side the event favours, all maps. "Spread" is the gap between the highest and lowest map (maps with n ≥ 40).

| Event | n | Win rate | 95% CI | Spread across maps |
| --- | --- | --- | --- | --- |
| T plants with a man advantage | 3,214 | 96% | 95–96% | 3 pts |
| Eco vs full buy (eco side) | 2,145 | 12% | 11–14% | 8 pts |
| Plant at B (T) | 2,584 | 77% | 76–79% | **30 pts** |
| Bomb planted (T) | 5,484 | 77% | 76–78% | 14 pts |
| Plant at A (T) | 2,900 | 76% | 75–78% | 19 pts |
| Opening kill (T) | 4,438 | 73% | 72–74% | 18 pts |
| Opening kill with an AWP | 1,459 | 73% | 70–75% | 10 pts |
| T plants while down players | 967 | 28% | 25–31% | 8 pts |
| Flash-assisted opening kill | 1,098 | 72% | 69–74% | 16 pts |
| Opening kill (CT) | 6,399 | 71% | 70–72% | 12 pts |
| Won the previous round (CT) | 5,165 | 67% | 65–68% | 8 pts |
| T has more AWPs | 1,032 | 64% | 62–67% | 18 pts |
| CT has more AWPs | 3,426 | 62% | 61–64% | 18 pts |
| Won the previous round (T) | 4,650 | 62% | 61–63% | 17 pts |
| Opening kill traded within 5 s (traded side) | 2,785 | 41% | 39–43% | 18 pts |
| More enemy blind time (T) | 3,300 | 53% | 51–54% | 20 pts |
| Pistol round (CT) | 1,047 | 50% | 47–53% | 16 pts |

The utility counts ("more smokes thrown", 63–74%) are left out because survivors throw more, which confounds them.

## Man advantage and clutches

The CTs' chance to win after a kill, no bomb planted:

| State (CT v T) | All | Lowest map | Highest map |
| --- | --- | --- | --- |
| 5v4 | 71% (n=6,675) | Anubis 62% | Mirage 74% |
| 4v5 | 28% (n=4,584) | Vertigo 18% | Train 34% |
| 4v4 | 47% (n=5,404) | Vertigo 38% | Train 53% |
| 3v3 | 47% (n=3,336) | Anubis 39% | Train 55% |
| 2v2 | 54% (n=1,689) | Anubis 45% | Vertigo 64% |
| 1v1 | 65% (n=553) | Inferno 57% | Overpass 76% |
| 2v1 / 1v2 | 90% / 19% | | |

After a plant, the odds tilt to the T side:
- 2v2 is 28% for the CTs, 1v1 33% and 3v3 21%.
- A 1v2 retake succeeds 7% of the time.
- Holding the plant 2v1 down a player, Ts still win 33%.

Each side's first 1vN of a round, counted once:

| Situation | Lone CT wins | Lone T wins |
| --- | --- | --- |
| 1v1 | 58% (n=815) | 71% (n=677) |
| 1v2 | 15% (n=1,217) | 24% (n=1,273) |
| 1v3 or worse | 1% (n=2,880) | 2% (n=4,737) |

The lone T clutches better. The likely reason, not split out here, is that many T clutches come after a plant, where the clock works for them.

## After the plant

| Map | Plants | T win | Site A | Site B | Mean plant time | CT defuses with a kit |
| --- | --- | --- | --- | --- | --- | --- |
| Overpass | 278 | 86% | 81% | **89%** | 1:24 | 45% |
| Vertigo | 108 | 83% | 84% | 83% | 1:13 | 56% |
| Dust2 | 1,118 | 81% | 80% | 83% | 1:18 | 56% |
| Anubis | 455 | 80% | 78% | 81% | 1:18 | 50% |
| Mirage | 1,301 | 75% | 73% | 79% | 1:12 | 61% |
| Inferno | 930 | 74% | 74% | 74% | 1:20 | 63% |
| Nuke | 823 | 74% | 79% | 68% | 1:15 | 59% |
| Ancient | 668 | 71% | 67% | 75% | 1:08 | 64% |
| Train | 113 | 71% | **86%** | **59%** | 1:14 | 42% |

- **Site matters most on Train** (A holds 86%, B 59%; small sample) **and on Nuke** (A 79%, B 68%).
- **Overpass B is the strongest plant in the dataset:** 89%.
- **Plant timing:** T win rate after the plant is 78% before 0:45, 79% between 0:45 and 1:15, and 74% after 1:15. The likely reasons, not yet tested: the CTs have had time to rotate, and a late plant often comes from a T side that's already lost players.

## Where the first duel happens

The victim's callout for each map's most common opening kill, and how often the side that won that duel took the round. All maps together average 72%.

| Map | Most common opening | Share of openings | Round won | Also notable |
| --- | --- | --- | --- | --- |
| Nuke | Outside, won by CT | 21% | 77% | T openers Outside are worth less (66%) |
| Inferno | Banana, won by CT | 21% | 67% | Below average: winning the Banana fight is worth less than an average opener |
| Ancient | Middle, won by CT | 13% | 74% | Winning Middle as T is worth 64% |
| Anubis | Canal, won by CT | 12% | 67% | A T opener on B is worth 78% |
| Overpass | Lower Park, won by CT | 11% | 68% | CT picks at Fountain (82%) and Canal (79%) |
| Mirage | Top of Mid, won by CT | 10% | 76% | A T opener on A site is worth 73% |
| Dust2 | Long Doors, won by CT | 9% | 65% | T openers in Long A are worth 78% |

On Dust2, Anubis and Vertigo, the T side's opener is worth far more than the CT's (79% / 78% / 82% against 67% / 62% / 65%). Those are the same maps that lean T.

## Economy

| Map | Eco beats full | Force beats full | Pistol winner wins the next round |
| --- | --- | --- | --- |
| Train | 11% | **46%** | 62% (n=21) |
| Vertigo | 7% | 38% | 78% (n=18) |
| Dust2 | 16% | 36% | 79% |
| Inferno | 13% | 35% | 73% |
| Overpass | 13% | 34% | 88% |
| Ancient | 8% | 33% | 83% |
| Anubis | 10% | 32% | 80% |
| Nuke | 13% | 32% | 82% |
| Mirage | 11% | 30% | 84% |
| **All** | 12% | 34% | 80% (n=1,044) |

The CTs' win rate by matchup, all maps:

| Matchup | CT win |
| --- | --- |
| CT full v T full | 52% (n=5,133) |
| CT full v T force | 67% |
| CT full v T semi | 79% |
| CT full v T eco | 97% |
| CT force v T force | 30% |
| CT force v T full | 34% |
| CT semi v T full | 19% |
| CT eco v T full | 6% |

## Predictability

A logistic model trained on four demo groups and tested on the fifth. Brier score, lower is better.

| Map | Moments | Base (alive, bomb, time) | + map | + economy | Round start only (+ economy) |
| --- | --- | --- | --- | --- | --- |
| Vertigo | 1,502 | 0.138 | 0.138 | **0.128** | 0.203 |
| Nuke | 13,818 | 0.140 | 0.139 | 0.132 | 0.221 |
| Mirage | 21,029 | 0.141 | 0.141 | 0.131 | 0.213 |
| Train | 1,665 | 0.142 | 0.142 | 0.137 | 0.225 |
| Dust2 | 16,034 | 0.147 | 0.146 | 0.140 | 0.219 |
| Overpass | 4,933 | 0.151 | 0.151 | 0.144 | 0.218 |
| Ancient | 9,431 | 0.152 | 0.152 | 0.140 | 0.214 |
| Anubis | 5,608 | 0.153 | 0.150 | 0.143 | 0.216 |
| Inferno | 12,454 | **0.155** | 0.156 | 0.148 | 0.223 |
| **All** | 86,474 | 0.147 | 0.146 | 0.138 | |

- **Most predictable:** Vertigo, Nuke and Mirage.
- **Least predictable:** Inferno, then Anubis and Ancient.
- **Economy helps on every map.** The map term pays off noticeably only on Anubis, the strongest T lean.
- **For Match Pulse:** a map-agnostic model plus economy is the right shape. Jev on pro play, in [../jev/experiments.md](../jev/experiments.md) E5, is beaten by exactly this kind of model.

## Rounds like this one

Every regulation round of the first 130-demo pass (2,575 rounds) was embedded with `@cf/baai/bge-m3`, as a short name-free story: buys, every kill, the bomb and the result. For each round, the 5 nearest rounds from other matches were compared with it (run 37057102040):

| Shared with the round | Story with map and callouts | Story without them | Two random rounds |
| --- | --- | --- | --- |
| Winner | 92% | 99% | 50% |
| Way of winning | 87% | 98% | 30% |
| Buy matchup | 58% | 78% | 26% |
| Order of the first three deaths | 24% | 29% | 13% |
| Clutch | 81% | 85% | 77% |
| Map | 100% | 12% | 12% |

- **Map names and callouts dominate the text.** With them in, every neighbour is on the same map.
- **With them out, neighbours come from any map but tell the same story:** same winner, same way of winning, mostly the same buys.
- **The stories state the result and the buys,** so agreement on those is partly by construction. The first-death order (29% against 13%) is the less trivial signal.
- A "rounds like this one" panel is cheap (52 embedding calls for 2,575 rounds) and works without any training.

## Our parser on pro demos

The sweeps run our `demo parse` and `demo diff` against the oracle, demo by demo, in the cloud ("parser mode" on the research branch):

| Build | Sample | Rounds collapsed | match.json clean | Entity failures | Notes |
| --- | --- | --- | --- | --- | --- |
| main at 22b3884 (baseline) | full run, 545 | 474 | 17 diff OK | — | Events-only rules tuned on matchmaking break tournament GOTV |
| main at 22b3884 | 132 | 113 | 5 | — | Run 37003041437 |
| `parser/pro-rounds` 68a02cf | 132 | 46 | 14 | 21 | Run 37054367353; round files now on |
| `parser/pro-rounds` 1dd244c | 132 | 46 | 35 | 13 | Run 37056136267 |
| `parser/pro-rounds` ca49811 | 11 hard cases | 5 of 5 still | halftime and CWorld fixed | 0 | Run 37057084416; the full-132 run with event timelines follows |

- **Speed and memory stay far inside budget:** at most 7.4 s and 219 MB with round files on.
- **The remaining collapse family ends round 1 near tick 8,635 as "time expired"** and finds no later round. Parser is working on it from the event timelines.

## Next

- **The ca49811 full-132 parser sweep,** with event timelines on every demo, then whatever parser sends next.
- **Model comparison on pro rounds:** done, in [../jev/experiments.md](../jev/experiments.md) E6. Claude is the best-calibrated zero-shot predictor, Jev ranks best and costs about 1/95 as much, and the trained model has the lowest raw Brier.
- **Interactive page:** a private claude.ai page, Round Odds, shows these results.
