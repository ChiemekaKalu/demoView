# Narrative log: proposal

Status: **design approved by the user on 2026-10-02** (all six decisions below). The `parser/eventlog` groundwork lands in M4. The log output, its schema and the story pane are v1.2. It merges three design reports from 2026-10-02: event taxonomy, storage and transport, and parser integration.

## Goal

The narrative log is a structured, tick-stamped record of everything in a match and everything the parser decided about it. It is a product output of the parse job, stored in R2 next to `match.json`, and could one day become a ledger or dataset. Platform logs (Workers Logs) are for operations only and never carry the narrative.

## Shape

Three layers, kept separable so derived events can be recomputed without the demo:

| Layer | Contents | Depends on |
| --- | --- | --- |
| `fact` | What the demo said, with identity resolved and nothing else. Immutable. | the demo |
| `derived` | Interpretations: round boundaries, winners, trades, clutches, multi-kills. Each cites the facts it came from. | facts, `match.json`, round files |
| `note` | Deterministic parser decisions and anomalies: inferences, demo quirks, stage counts. | the demo and the parser build |

R2 layout, versioned so a reparse never destroys an older log:

```text
parsed/{id}/log/manifest.json             {log_version, latest, versions: [{parser_version, complete, counts, sha256s, rules}]}
parsed/{id}/log/{parser_version}/facts.ndjson.gz
parsed/{id}/log/{parser_version}/derived.ndjson.gz
parsed/{id}/log/{parser_version}/notes.ndjson.gz
```

- The manifest is written last, with `complete: true`. That is the commit marker.
- Versioned files are cached `immutable`. The manifest gets `max-age=60`.

Line envelope:

```json
{"seq":412,"tick":18230,"round":7,"type":"combat.death","v":1,"src":"ge:player_death","d":{"attacker":3,"victim":5,"weapon":"ak47","headshot":true}}
{"seq":88,"tick":18230,"round":7,"type":"kill.opening","v":1,"rule":"opening@1","basis":[412],"d":{"attacker":3,"victim":5}}
```

- `seq` is per file, 0-based and in output order. Derived records cite facts by `seq` in `basis`.
- Order is `(tick, source rank, emit order)`, never map iteration.
- Players are `idx` as in `match.json`. A `player.registered` fact maps `idx` to steam id and name, so stripping steam ids later touches one record type.
- `round` is 0 outside live rounds. The log records the whole demo, including warmup and knife rounds, and a `match.live_window` derived record says what counted.
- There are no floats, no wall-clock values and no rendered sentences in the files. A pure `narrate(log)` function renders text, so wording can change without a reparse.

## Determinism

- The same demo and parser version give byte-identical decompressed files. gzip is pinned with mtime 0 and no file name.
- Tests:
  - parse twice in separate processes and compare
  - golden files on synthetic demos
  - per-type count summaries on the real fixtures
  - invariant: derived kills equal `match.json` kills, and every `basis` seq exists

## Event coverage

- **v0, game events only:**
  - facts: match settings, players, team changes, spawns, freeze end, hurt, death, bomb plant/defuse/explode/drop, grenade detonations and effects, blinds, purchases, surrender votes
  - derived: round begin and end (with `method`: game event, bomb, elimination, timer, surrender), score, halves, overtime, effective damage, opening kills, trades, multi-kills, alive counts, clutches, bomb timeline
- **M4, entity decoding:**
  - facts: loadouts at freeze end, exact game-rules round state, grenade paths, flash state
  - derived: buy types, saves, utility summaries, zones, site takes, rotations
  - Exact `round.end` records (`method: rules`) supersede inferred ones.
- `item_purchase`, surrender votes and some `player_death` fields must be checked with `demo inspect` against the pinned build before field lists are locked.

## Parser integration

- **`parser/eventlog`:** envelope, `Emitter` interface (`Enabled`, `Emit`), `Nop`, and `Spool`. It imports nothing from the parser.
- **Buffered, then written once.** `Spool` holds typed events until the walk ends, because round numbers and final `idx` only exist after `matchlogic.Build`. The demo still streams, and only events are held, with a hard cap and a deterministic truncation marker.
- **Hooks:**
  - facts through a `Recorder.Sink` and a field accessor on `events.Event`
  - derived and notes from `matchlogic.Build`, whose existing `notes` become `note` records
  - entity-stage emits later
- **Measured on the two fixtures** (all game events, as JSONL):
  - 60 MB Mirage: 6.8k events, 66 KB gzipped
  - 299 MB Nuke: 16k events, 143 KB gzipped
  - about 0.7 µs per event, under 3 MB of memory
- **CLI:** `demo parse x.dem --out dir [--log off|full]`, default `full`, writes `dir/log/`. A `standard` level arrives with M4, when entity events make size matter.
- **Failure:** a failed parse writes no log, matching `match.json`.

## Watching it

- **Pipeline, live:** the Worker logs one JSON line per status change. The container prints stage markers and the spec's one-line parse summary to stdout, which Workers Logs stores and the dashboard can tail live. Retention is 7 days on paid.
- **Story, after the parse:**
  - The match page shows round-by-round lines rendered from the log.
  - The replay page shows lines up to the current frame, with click-to-seek, fetched once per match (well under 200 KB).
- A parse takes about 30 s for a 45-minute match, so a real-time narrative tail isn't worth the cost. A paced playback of the finished log gives the same effect.

## Cost

- Storage, R2 writes, Workers Logs and Analytics Engine are all inside free allowances at hobby scale.
- **Containers: default `sleepAfter` is 10 minutes, and idle time is billed.** At 4 GiB that is about 2,400 GiB-s per parse, so the 25 GiB-h monthly allowance would last about 37 parses. Set `sleepAfter` to about 30 s, or stop the container when the parse returns. That one setting matters more than all logging costs combined.

## Build order

1. `parser/eventlog` with unit and golden tests. It touches no other code.
2. Derived and note records from `matchlogic.Build`, plus the invariant test.
3. `--log` flag, output files, run-twice determinism test.
4. Fact hooks in `events` and `matchlogic`.
5. Pipeline (M2): R2 upload, manifest commit, `GET /api/matches/:id/log/...`, observability, one Analytics Engine point per parse.
6. Viewer (M3): story and replay panes.

## Decisions (all approved 2026-10-02)

1. Approve the log as a new output, with the R2 layout above. This is a spec addition.
2. Approve a new `schema/log.schema.json` with its own `log_version`, independent of `schema_version`.
3. Approve no rendered `msg` strings in the files, with a narrator function instead.
4. Approve default `--log full`.
5. Optional: a D1 `match_events` table (about 10 rows per match) for a durable pipeline timeline, since Workers Logs expire after 7 days.
6. Allow the two small hooks in `events/` and `matchlogic/`, which other sessions are editing.
